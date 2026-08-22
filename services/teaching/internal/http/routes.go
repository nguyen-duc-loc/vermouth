// Package http is how teaching is reached. Only the gateway calls it (INV-1).
package http

import (
	"log/slog"
	"net/http"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

// Deps is what the routes need: the logger every answer is traced through, and
// the health checks this service owes.
type Deps struct {
	Logger *slog.Logger
	Health vermouth.Health
}

// Mux carries the two endpoints every service owes today: liveness, and a
// readiness check that also reports its database and broker connection
// (spec 0001, the contract every service obeys). Business routes arrive with
// the feature that needs them.
func Mux(deps Deps) http.Handler {
	mux := http.NewServeMux()
	deps.Health.Mount(mux)
	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}
