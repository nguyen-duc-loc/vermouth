package vermouth

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// readyCheckTimeout bounds the readiness probe: a dependency that cannot
// answer in three seconds counts as not ready, rather than hanging the probe.
const readyCheckTimeout = 3 * time.Second

// Check reports whether one dependency is reachable.
type Check func(ctx context.Context) error

// Health answers the two endpoints every service owes (spec 0001, the contract
// every service obeys): GET /health for liveness, and GET /ready which also
// reports its database and broker connection.
type Health struct {
	Service string
	Checks  map[string]Check
	Logger  *slog.Logger
}

type healthBody struct {
	Status  string            `json:"status"`
	Service string            `json:"service"`
	Checks  map[string]string `json:"checks,omitempty"`
}

// Mount registers /health and /ready on a mux.
func (h Health) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(r.Context(), h.Logger, w, http.StatusOK, healthBody{Status: "ok", Service: h.Service})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyCheckTimeout)
		defer cancel()
		results := make(map[string]string, len(h.Checks))
		status := http.StatusOK
		for name, check := range h.Checks {
			err := check(ctx)
			if err != nil {
				results[name] = "failing: " + err.Error()
				status = http.StatusServiceUnavailable
				continue
			}
			results[name] = "ok"
		}
		body := healthBody{Status: "ready", Service: h.Service, Checks: results}
		if status != http.StatusOK {
			body.Status = "not ready"
		}
		WriteJSON(ctx, h.Logger, w, status, body)
	})
}
