// Package http is how billing is reached. Only the gateway calls it (INV-1).
package http

import (
	"log/slog"
	"net/http"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
)

// Deps is what the routes need: the logger every answer is traced through, and
// the health checks this service owes.
type Deps struct {
	Projection *handler.ProjectionReader
	Profile    *handler.ProfileService
	Billing    *handler.BillingService
	Verifier   *vermouth.Verifier
	Logger     *slog.Logger
	Health     vermouth.Health
}

// Mux carries health plus billing's narrow teaching projection status read.
func Mux(deps Deps) http.Handler {
	mux := http.NewServeMux()
	deps.Health.Mount(mux)
	mux.HandleFunc("GET /projections/teaching/status", teachingProjectionStatus(deps))
	mux.HandleFunc("GET /invoice-profile", getInvoiceProfile(deps))
	mux.HandleFunc("PUT /invoice-profile", putInvoiceProfile(deps))
	mux.HandleFunc("GET /banks", getBanks(deps))
	mux.HandleFunc("GET /classes/{class_id}/rates", getProjectedClassRates(deps))
	mux.HandleFunc("GET /billing-periods/default", getBillingPeriodDefault(deps))
	mux.HandleFunc("GET /billing-periods/{year}/{month}", getBillingPeriod(deps))
	mux.HandleFunc("POST /billing-periods/{year}/{month}/preview", previewBillingPeriod(deps))
	mux.HandleFunc("POST /billing-periods/{year}/{month}/issue", issueBillingPeriod(deps))
	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}

func teachingProjectionStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		claims, err := deps.Verifier.Verify(vermouth.BearerToken(r))
		if err != nil {
			vermouth.WriteError(
				ctx,
				w,
				http.StatusUnauthorized,
				"unauthenticated",
				"a valid bearer token is required",
			)
			return
		}
		status, err := deps.Projection.TeachingStatus(ctx, claims.TutorID)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "Read teaching projection status",
				slog.String("request_id", vermouth.RequestID(ctx)),
				slog.String("error", err.Error()),
			)
			vermouth.WriteError(ctx, w, http.StatusInternalServerError, "internal", "billing could not read projection progress")
			return
		}
		vermouth.WriteJSON(ctx, deps.Logger, w, http.StatusOK, status)
	}
}
