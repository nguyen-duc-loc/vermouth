// Package handler holds identity's work: what it does, separate from how it is
// reached.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/token"
)

// Handler is identity's work, holding only what it needs to do it.
type Handler struct {
	pool         *pgxpool.Pool
	logger       *slog.Logger
	signer       accessTokenSigner
	google       *googleProvider
	auth         AuthConfig
	publishTopic string
	observer     sessionTransitionObserver
}

type accessTokenSigner interface {
	Mint(tutorID uuid.UUID, timezone, language string) (string, time.Time, error)
}

// sessionTransitionObserver exposes the two locked transition points whose
// ordering spec 0004 requires concurrency tests to prove. Production leaves it
// nil, while tests use it to hold a real Postgres transaction at that point.
type sessionTransitionObserver interface {
	afterSessionLock(ctx context.Context, sessionID uuid.UUID)
	beforeRefreshTokenInsert(ctx context.Context, sessionID uuid.UUID)
}

// New wires the handler to this service's own pool, to the topic it publishes to,
// and to the Google client whose secret only this service holds. The topic
// travels as an argument because it is a property of the service's code rather
// than of the deployment (STK-11).
func New(
	pool *pgxpool.Pool, logger *slog.Logger, signer *token.Signer,
	publishTopic string, auth AuthConfig,
) *Handler {
	return &Handler{
		pool:         pool,
		logger:       logger,
		signer:       signer,
		google:       newGoogleProvider(auth),
		auth:         auth,
		publishTopic: publishTopic,
	}
}

// Tutor is what identity answers with. There is no password field, and no column
// behind one: Google proves who a tutor is (spec 0004).
type Tutor struct {
	TutorID     uuid.UUID `json:"tutor_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Timezone    string    `json:"timezone"`
	Language    string    `json:"language"`
	CreatedAt   time.Time `json:"created_at"`
}

// ErrNotFound is an unknown tutor.
var ErrNotFound = errors.New("tutor not found")

// tutorFields is the field list spec 0001's catalogue names for both of
// identity's events, which carry the same five. The publisher carries all of
// them: consumers then declare only the ones they need (INV-12).
//
// It is a separate shape from the stored row on purpose, so a column identity
// keeps to itself can never reach an event by being added to a table.
type tutorFields struct {
	TutorID     uuid.UUID `json:"tutor_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Timezone    string    `json:"timezone"`
	Language    string    `json:"language"`
}

// publishTutorEvent writes one of identity's two events into the outbox inside
// the caller's transaction, beside the write it describes (INV-3, STK-4).
// Publishing itself happens later, in the relay, never from here.
func (h *Handler) publishTutorEvent(
	ctx context.Context, tx pgx.Tx, name string, fields tutorFields,
) error {
	env, err := vermouth.NewEnvelope(ctx, name, 1, fields.TutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: fields.TutorID}, fields)
	if err != nil {
		return fmt.Errorf("build %s: %w", name, err)
	}
	err = vermouth.WriteOutbox(ctx, tx, h.publishTopic, env)
	if err != nil {
		return err
	}
	h.logger.InfoContext(ctx, "Event written to the outbox",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("event_name", name),
		slog.String("event_id", env.EventID.String()),
		slog.String("tutor_id", fields.TutorID.String()),
	)
	return nil
}

// Get answers about one tutor. The caller has already proved which tutor it is
// (INV-8), so there is no tutor_id input on the request path.
func (h *Handler) Get(ctx context.Context, tutorID uuid.UUID) (Tutor, error) {
	row, err := store.Queries(h.pool).GetTutor(ctx, tutorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tutor{}, ErrNotFound
		}
		return Tutor{}, fmt.Errorf("read tutor: %w", err)
	}
	return toTutor(row.TutorID, row.Email, row.DisplayName, row.Timezone, row.Language, row.CreatedAt), nil
}
