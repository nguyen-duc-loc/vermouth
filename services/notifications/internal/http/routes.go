// Package http is how notifications is reached. Only the gateway calls it
// (INV-1).
package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store"
)

// Deps is what the routes need: this service's own pool, the logger every
// answer is traced through, and the health checks it owes.
type Deps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Health vermouth.Health
}

// projectionStatus is notifications' own fact: whether its recipient
// projection has caught up with a tutor yet, and when it did. It deliberately
// carries no identity fact such as the email, because a screen reads a fact
// from the service that owns it (INV-10).
type projectionStatus struct {
	TutorID    uuid.UUID  `json:"tutor_id"`
	Recorded   bool       `json:"recorded"`
	RecordedAt *time.Time `json:"recorded_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

// Mux builds notifications' routes: the two every service owes, plus the one
// question the gateway asks about this service's own projection.
func Mux(deps Deps) http.Handler {
	mux := http.NewServeMux()
	deps.Health.Mount(mux)

	// The gateway asks this while the browser waits for the relay to catch up.
	// The relay polls, so the answer is false for the first moment after a
	// registration and true shortly after (STK-18).
	mux.HandleFunc("GET /recipients/{tutor_id}/status", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tutorID, err := uuid.Parse(r.PathValue("tutor_id"))
		if err != nil {
			vermouth.WriteError(ctx, w, http.StatusBadRequest, "invalid_input", "tutor_id is not an identifier")
			return
		}
		row, err := store.Queries(deps.Pool).GetRecipient(ctx, tutorID)
		if errors.Is(err, pgx.ErrNoRows) {
			vermouth.WriteJSON(ctx, deps.Logger, w, http.StatusOK, projectionStatus{TutorID: tutorID, Recorded: false})
			return
		}
		if err != nil {
			deps.Logger.ErrorContext(ctx, "Read recipient", slog.String("error", err.Error()))
			vermouth.WriteError(ctx, w, http.StatusInternalServerError, "internal", "could not read the recipient projection")
			return
		}
		recordedAt := row.RecordedAt
		updatedAt := row.UpdatedAt
		vermouth.WriteJSON(ctx, deps.Logger, w, http.StatusOK, projectionStatus{
			TutorID:    row.TutorID,
			Recorded:   true,
			RecordedAt: &recordedAt,
			UpdatedAt:  &updatedAt,
		})
	})

	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}
