package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store/sqlcgen"
)

// revokedRetention is how long a revoked row is kept before the sweep takes it.
// Presenting a revoked token again is how a stolen copy shows up, and a deleted
// row is indistinguishable from one that never existed, so the evidence outlives
// the session by a week.
const revokedRetention = 7 * 24 * time.Hour

// ErrSessionRefused is the one answer to every refresh or sign out that cannot be
// honoured: unknown, expired, revoked, or a reuse past the grace window. The
// caller answers 401 without saying which, because the difference is only useful
// to whoever is holding the wrong token.
var ErrSessionRefused = errors.New("the refresh token is not a live session")

// IssuedRefreshToken is the value the cookie carries and when it stops working.
// The value exists in this struct and in the browser, never in the database: only
// its sha256 is stored.
type IssuedRefreshToken struct {
	Value     string
	ExpiresAt time.Time
}

// RefreshResult is what a refresh answers with: a token for the next fifteen
// minutes of requests, and the rotated cookie beside it.
type RefreshResult struct {
	AccessToken     string
	AccessExpiresAt time.Time
	Refresh         IssuedRefreshToken
}

// Refresh rotates the presented token and mints an access token for its tutor.
// It is the only path to an access token, so a reload and the moment after a
// callback are the same call (AC-3).
func (h *Handler) Refresh(ctx context.Context, presented string) (RefreshResult, error) {
	if strings.TrimSpace(presented) == "" {
		return RefreshResult{}, ErrSessionRefused
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return RefreshResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	claimed, err := h.claimRotation(ctx, tx, hashRefreshToken(presented))
	if err != nil {
		return RefreshResult{}, err
	}
	if claimed.familyRevoked {
		// The family was just ended by a reuse. That write has to land even
		// though this call is about to refuse, or the theft goes unrecorded.
		commitErr := tx.Commit(ctx)
		if commitErr != nil {
			return RefreshResult{}, fmt.Errorf("commit the revoked family: %w", commitErr)
		}
		return RefreshResult{}, ErrSessionRefused
	}

	tutor, err := store.Queries(tx).GetTutor(ctx, claimed.tutorID)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("read the tutor a session belongs to: %w", err)
	}
	issued, err := h.issueRefreshToken(ctx, tx, claimed.tutorID, claimed.sessionID)
	if err != nil {
		return RefreshResult{}, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("commit the rotated session: %w", err)
	}

	accessToken, expiresAt, err := h.signer.Mint(tutor.TutorID, tutor.Timezone, tutor.Language)
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{
		AccessToken:     accessToken,
		AccessExpiresAt: expiresAt,
		Refresh:         issued,
	}, nil
}

// rotation is what a claim decided: which family to continue, or that the family
// has just been ended.
type rotation struct {
	tutorID       uuid.UUID
	sessionID     uuid.UUID
	familyRevoked bool
}

// claimRotation decides whether the presented token may rotate. The first use is
// a single statement, so two tabs booting together cannot both win it: the loser
// reads the row back and lands in the grace window branch, which is the whole
// reason that window exists (AC-7).
func (h *Handler) claimRotation(
	ctx context.Context, tx pgx.Tx, hash []byte,
) (rotation, error) {
	queries := store.Queries(tx)
	claimed, err := queries.MarkRefreshTokenUsed(ctx, hash)
	if err == nil {
		return rotation{tutorID: claimed.TutorID, sessionID: claimed.SessionID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return rotation{}, fmt.Errorf("rotate the refresh token: %w", err)
	}

	// Nothing was claimed, so the row is missing, finished, or already rotated.
	row, err := queries.GetRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rotation{}, ErrSessionRefused
		}
		return rotation{}, fmt.Errorf("read the refresh token: %w", err)
	}
	switch {
	case row.RevokedAt.Valid, !row.ExpiresAt.After(time.Now()):
		return rotation{}, ErrSessionRefused
	case row.UsedAt.Valid && withinGrace(row.UsedAt.Time, h.auth.RefreshGrace):
		return rotation{tutorID: row.TutorID, sessionID: row.SessionID}, nil
	default:
		return h.revokeFamily(ctx, tx, row)
	}
}

// revokeFamily ends a session because one of its already rotated tokens came
// back. Neither the holder of the stolen copy nor the tutor keeps the session:
// the tutor signs in with Google again, which is cheap, while a silent theft is
// not.
func (h *Handler) revokeFamily(
	ctx context.Context, tx pgx.Tx, row sqlcgen.RefreshToken,
) (rotation, error) {
	_, err := store.Queries(tx).RevokeSessionFamily(ctx, sqlcgen.RevokeSessionFamilyParams{
		TutorID:   row.TutorID,
		SessionID: row.SessionID,
	})
	if err != nil {
		return rotation{}, fmt.Errorf("revoke the session family: %w", err)
	}
	h.logger.WarnContext(ctx, "Refresh token reused after rotation",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", row.TutorID.String()),
		slog.String("session_id", row.SessionID.String()),
	)
	return rotation{familyRevoked: true}, nil
}

// SignOut revokes every refresh token in the presented token's family and reports
// nothing about what it found. Signing out twice, or with a cookie from a session
// that already ended, is not a failure (AC-6).
func (h *Handler) SignOut(ctx context.Context, presented string) error {
	if strings.TrimSpace(presented) == "" {
		return nil
	}
	queries := store.Queries(h.pool)
	row, err := queries.GetRefreshToken(ctx, hashRefreshToken(presented))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("read the refresh token: %w", err)
	}
	revoked, err := queries.RevokeSessionFamily(ctx, sqlcgen.RevokeSessionFamilyParams{
		TutorID:   row.TutorID,
		SessionID: row.SessionID,
	})
	if err != nil {
		return fmt.Errorf("revoke the session family: %w", err)
	}
	h.logger.InfoContext(ctx, "Signed out",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", row.TutorID.String()),
		slog.String("session_id", row.SessionID.String()),
		slog.Int64("revoked", revoked),
	)
	return nil
}

// issueRefreshToken writes one row and hands its value back. The value is
// generated here and stored only as a hash, so a database copy hands nobody a
// session and no comparison has to be timing safe.
func (h *Handler) issueRefreshToken(
	ctx context.Context, tx pgx.Tx, tutorID, sessionID uuid.UUID,
) (IssuedRefreshToken, error) {
	value, err := randomToken()
	if err != nil {
		return IssuedRefreshToken{}, err
	}
	expiresAt := time.Now().UTC().Add(h.auth.RefreshTTL)
	_, err = store.Queries(tx).InsertRefreshToken(ctx, sqlcgen.InsertRefreshTokenParams{
		TokenHash: hashRefreshToken(value),
		SessionID: sessionID,
		TutorID:   tutorID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return IssuedRefreshToken{}, fmt.Errorf("issue a refresh token: %w", err)
	}
	return IssuedRefreshToken{Value: value, ExpiresAt: expiresAt}, nil
}

// hashRefreshToken is what the database holds instead of the token.
func hashRefreshToken(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// withinGrace says whether a rotation is recent enough to be two tabs rather than
// a stolen copy. A zero grace turns the window off entirely.
func withinGrace(usedAt time.Time, grace time.Duration) bool {
	return grace > 0 && time.Since(usedAt) <= grace
}

// RunSweep deletes what has expired, on its own ticker rather than the relay's:
// these rows live ten minutes and thirty days, so waking every 750ms would be
// four thousand pointless statements an hour. It blocks until the context is
// cancelled.
func (h *Handler) RunSweep(ctx context.Context) error {
	ticker := time.NewTicker(h.auth.SweepInterval)
	defer ticker.Stop()
	h.logger.InfoContext(ctx, "Sweep started", slog.Duration("interval", h.auth.SweepInterval))
	for {
		select {
		case <-ctx.Done():
			h.logger.InfoContext(ctx, "Sweep stopped")
			return nil
		case <-ticker.C:
			err := h.sweepOnce(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				h.logger.ErrorContext(ctx, "Sweep failed", slog.String("error", err.Error()))
			}
		}
	}
}

// sweepOnce takes the expired attempts and the finished tokens in two statements,
// and says how many so a growing table is visible in the log rather than only in
// the database.
func (h *Handler) sweepOnce(ctx context.Context) error {
	queries := store.Queries(h.pool)
	attempts, err := queries.DeleteExpiredLoginAttempts(ctx)
	if err != nil {
		return fmt.Errorf("sweep expired sign in attempts: %w", err)
	}
	tokens, err := queries.DeleteFinishedRefreshTokens(ctx, pgtype.Timestamptz{
		Time:  time.Now().UTC().Add(-revokedRetention),
		Valid: true,
	})
	if err != nil {
		return fmt.Errorf("sweep finished refresh tokens: %w", err)
	}
	if attempts+tokens > 0 {
		h.logger.InfoContext(ctx, "Swept",
			slog.Int64("login_attempts", attempts),
			slog.Int64("refresh_tokens", tokens),
		)
	}
	return nil
}
