package handler

import (
	"context"
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
		return RefreshResult{}, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	hash := hashOpaqueToken(presented)
	rotation, err := h.lockRotation(ctx, tx, hash)
	if err != nil {
		return RefreshResult{}, err
	}
	if rotation.reusedAfterGrace(h.auth.RefreshGrace) {
		return RefreshResult{}, h.revokeReusedRotation(ctx, tx, rotation)
	}
	queries := store.Queries(tx)
	if !rotation.token.UsedAt.Valid {
		err = queries.MarkRefreshTokenUsed(ctx, sqlcgen.MarkRefreshTokenUsedParams{
			TokenHash: hash,
			SessionID: rotation.session.SessionID,
			UsedAt:    pgtype.Timestamptz{Time: rotation.now, Valid: true},
		})
		if err != nil {
			return RefreshResult{}, fmt.Errorf("mark the refresh token used: %w", err)
		}
	}
	if h.observer != nil {
		h.observer.beforeRefreshTokenInsert(ctx, rotation.session.SessionID)
	}
	refreshExpiresAt := rotation.now.Add(h.auth.RefreshTTL)
	issued, err := issueRefreshToken(ctx, tx, rotation.session.SessionID, refreshExpiresAt)
	if err != nil {
		return RefreshResult{}, err
	}
	err = queries.ExtendAuthSession(ctx, sqlcgen.ExtendAuthSessionParams{
		SessionID: rotation.session.SessionID,
		ExpiresAt: refreshExpiresAt,
	})
	if err != nil {
		return RefreshResult{}, fmt.Errorf("extend the session family: %w", err)
	}
	accessToken, accessExpiresAt, err := h.signer.Mint(
		rotation.tutor.TutorID, rotation.tutor.Timezone, rotation.tutor.Language,
	)
	if err != nil {
		return RefreshResult{}, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("commit the rotated session: %w", err)
	}
	return RefreshResult{
		AccessToken:     accessToken,
		AccessExpiresAt: accessExpiresAt,
		Refresh:         issued,
	}, nil
}

type lockedRotation struct {
	session sqlcgen.AuthSession
	tutor   sqlcgen.GetTutorRow
	token   sqlcgen.RefreshToken
	now     time.Time
}

func (h *Handler) lockRotation(
	ctx context.Context, tx pgx.Tx, hash []byte,
) (lockedRotation, error) {
	queries := store.Queries(tx)
	sessionID, err := queries.GetRefreshTokenSessionID(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedRotation{}, ErrSessionRefused
		}
		return lockedRotation{}, fmt.Errorf("find the refresh token family: %w", err)
	}
	session, err := queries.LockAuthSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedRotation{}, ErrSessionRefused
		}
		return lockedRotation{}, fmt.Errorf("lock the session family: %w", err)
	}
	if h.observer != nil {
		h.observer.afterSessionLock(ctx, sessionID)
	}
	now := time.Now().UTC()
	if session.RevokedAt.Valid || !session.ExpiresAt.After(now) {
		return lockedRotation{}, ErrSessionRefused
	}
	tutor, err := queries.GetTutor(ctx, session.TutorID)
	if err != nil {
		return lockedRotation{}, fmt.Errorf("read the session tutor: %w", err)
	}
	token, err := queries.LockRefreshToken(ctx, sqlcgen.LockRefreshTokenParams{
		TokenHash: hash,
		SessionID: sessionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedRotation{}, ErrSessionRefused
		}
		return lockedRotation{}, fmt.Errorf("lock the refresh token: %w", err)
	}
	if !token.ExpiresAt.After(now) {
		return lockedRotation{}, ErrSessionRefused
	}
	return lockedRotation{session: session, tutor: tutor, token: token, now: now}, nil
}

func (r lockedRotation) reusedAfterGrace(grace time.Duration) bool {
	return r.token.UsedAt.Valid && !withinGrace(r.token.UsedAt.Time, r.now, grace)
}

func (h *Handler) revokeReusedRotation(
	ctx context.Context, tx pgx.Tx, rotation lockedRotation,
) error {
	_, err := store.Queries(tx).RevokeAuthSession(ctx, sqlcgen.RevokeAuthSessionParams{
		SessionID: rotation.session.SessionID,
		RevokedAt: pgtype.Timestamptz{Time: rotation.now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("revoke the session family: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit the revoked family: %w", err)
	}
	h.logger.WarnContext(ctx, "Refresh token reused after rotation",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", rotation.session.TutorID.String()),
		slog.String("session_id", rotation.session.SessionID.String()),
	)
	return ErrSessionRefused
}

// SignOut revokes the presented token's locked family and reports nothing about
// what it found. Signing out twice, or with a cookie from a session that already
// ended, is not a failure (AC-6).
func (h *Handler) SignOut(ctx context.Context, presented string) error {
	if strings.TrimSpace(presented) == "" {
		return nil
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sign out: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := store.Queries(tx)
	hash := hashOpaqueToken(presented)
	sessionID, err := queries.GetRefreshTokenSessionID(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("find the sign out family: %w", err)
	}
	session, err := queries.LockAuthSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock the sign out family: %w", err)
	}
	_, err = queries.LockRefreshToken(ctx, sqlcgen.LockRefreshTokenParams{
		TokenHash: hash,
		SessionID: sessionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock the sign out token: %w", err)
	}
	now := time.Now().UTC()
	revoked, err := queries.RevokeAuthSession(ctx, sqlcgen.RevokeAuthSessionParams{
		SessionID: sessionID,
		RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("revoke the session family: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit sign out: %w", err)
	}
	h.logger.InfoContext(ctx, "Signed out",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", session.TutorID.String()),
		slog.String("session_id", session.SessionID.String()),
		slog.Int64("revoked", revoked),
	)
	return nil
}

// issueRefreshToken writes one row and hands its value back. The value is
// generated here and stored only as a hash, so a database copy hands nobody a
// session and no comparison has to be timing safe.
func issueRefreshToken(
	ctx context.Context, tx pgx.Tx, sessionID uuid.UUID, expiresAt time.Time,
) (IssuedRefreshToken, error) {
	value, err := randomToken()
	if err != nil {
		return IssuedRefreshToken{}, err
	}
	_, err = store.Queries(tx).InsertRefreshToken(ctx, sqlcgen.InsertRefreshTokenParams{
		TokenHash: hashOpaqueToken(value),
		SessionID: sessionID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return IssuedRefreshToken{}, fmt.Errorf("issue a refresh token: %w", err)
	}
	return IssuedRefreshToken{Value: value, ExpiresAt: expiresAt}, nil
}

// withinGrace says whether a rotation is recent enough to be two tabs rather than
// a stolen copy. A zero grace turns the window off entirely.
func withinGrace(usedAt, now time.Time, grace time.Duration) bool {
	elapsed := now.Sub(usedAt)
	return grace > 0 && elapsed >= 0 && elapsed <= grace
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

// sweepOnce takes the expired attempts and finished families in two statements,
// and says how many so a growing table is visible in the log rather than only in
// the database.
func (h *Handler) sweepOnce(ctx context.Context) error {
	queries := store.Queries(h.pool)
	attempts, err := queries.DeleteExpiredLoginAttempts(ctx)
	if err != nil {
		return fmt.Errorf("sweep expired sign in attempts: %w", err)
	}
	sessions, err := queries.DeleteFinishedAuthSessions(ctx, pgtype.Timestamptz{
		Time:  time.Now().UTC().Add(-revokedRetention),
		Valid: true,
	})
	if err != nil {
		return fmt.Errorf("sweep finished auth sessions: %w", err)
	}
	if attempts+sessions > 0 {
		h.logger.InfoContext(ctx, "Swept",
			slog.Int64("login_attempts", attempts),
			slog.Int64("auth_sessions", sessions),
		)
	}
	return nil
}
