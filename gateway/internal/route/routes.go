package route

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/auth"
)

// Deps is what the routes need. There is no database in here on purpose.
type Deps struct {
	Client   *aggregate.Client
	Verifier *vermouth.Verifier
	Logger   *slog.Logger
	Service  string
}

// Mux builds the gateway surface described by api/openapi.yaml (STK-10). The
// request id is created here and travels onward on every call, and from there
// onto every event published while handling it (INV-15).
func Mux(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK,
			apitypes.Health{Status: "ok", Service: deps.Service})
	})

	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) { ready(deps, w, r) })

	// The four sign in endpoints take no token, because between them they are
	// what produces one. They are registered here, on the outer mux, and Go's
	// ServeMux picks the more specific pattern, so /api/ below keeps going to the
	// authenticated mux with nothing rearranged.
	// The upstream path is a constant per route rather than the inbound one with
	// the prefix trimmed, so nothing a caller sends can steer where this calls.
	for pattern, upstream := range map[string]string{
		"GET /api/auth/google/start":    "/auth/google/start",
		"GET /api/auth/google/callback": "/auth/google/callback",
		"POST /api/auth/refresh":        "/auth/refresh",
		"POST /api/auth/signout":        "/auth/signout",
	} {
		mux.HandleFunc(pattern, forwardAuth(deps, upstream))
	}

	// Everything under /api/ below this line needs a verified token.
	authenticated := http.NewServeMux()

	authenticated.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		response, err := deps.Client.Call(ctx, http.MethodGet, deps.Client.Upstreams().Identity, "/me", vermouth.BearerToken(r), nil)
		if err != nil {
			upstreamFailed(ctx, deps.Logger, w, "identity", err)
			return
		}
		passThrough(ctx, deps.Logger, w, response)
	})

	authenticated.HandleFunc("GET /api/thread", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		claims, ok := auth.Claims(ctx)
		if !ok {
			vermouth.WriteError(ctx, w, http.StatusUnauthorized, "unauthenticated", "a valid bearer token is required")
			return
		}
		status, err := deps.Client.Thread(ctx, claims.TutorID, vermouth.BearerToken(r))
		if err != nil {
			upstreamFailed(ctx, deps.Logger, w, "identity", err)
			return
		}
		vermouth.WriteJSON(ctx, deps.Logger, w, http.StatusOK, status)
	})

	mux.Handle("/api/", auth.Middleware(deps.Verifier, deps.Logger, authenticated))

	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}

// forwardAuth hands one auth request to identity and copies its answer back. The
// gateway adds nothing here: no rule, no cookie of its own, no token.
func forwardAuth(deps Deps, upstreamPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		response, err := deps.Client.Forward(ctx, r, deps.Client.Upstreams().Identity, upstreamPath)
		if err != nil {
			upstreamFailed(ctx, deps.Logger, w, "identity", err)
			return
		}
		passThroughAuth(ctx, deps.Logger, w, response)
	}
}

// ready asks every service at once and reports each one, so a failing
// dependency is named rather than guessed at.
func ready(deps Deps, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	upstreams := deps.Client.Upstreams()
	targets := map[string]string{
		"identity":      upstreams.Identity,
		"teaching":      upstreams.Teaching,
		"billing":       upstreams.Billing,
		"notifications": upstreams.Notifications,
	}

	var (
		mu      sync.Mutex
		results = make(map[string]string, len(targets))
		group   errgroup.Group
	)
	for name, baseURL := range targets {
		group.Go(func() error {
			outcome := "ok"
			response, err := deps.Client.Call(ctx, http.MethodGet, baseURL, "/ready", "", nil)
			switch {
			case err != nil:
				outcome = "unreachable: " + err.Error()
			case response.Status != http.StatusOK:
				outcome = "not ready"
			}
			mu.Lock()
			results[name] = outcome
			mu.Unlock()
			return nil
		})
	}
	_ = group.Wait()

	status := http.StatusOK
	overall := "ready"
	for _, outcome := range results {
		if outcome != "ok" {
			status = http.StatusServiceUnavailable
			overall = "not ready"
		}
	}
	vermouth.WriteJSON(ctx, deps.Logger, w, status, apitypes.Readiness{
		Status:  overall,
		Service: deps.Service,
		Checks:  &results,
	})
}

// passThrough forwards a service's answer. A service error already arrives in
// the one error shape, so a 4xx is passed through unchanged; a 5xx is replaced,
// because an internal failure is never leaked raw (spec 0001).
func passThrough(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, response aggregate.Response) {
	if response.Status >= http.StatusInternalServerError {
		logger.ErrorContext(ctx, "Service failed",
			slog.Int("status", response.Status),
			slog.String("body", string(response.Body)),
		)
		vermouth.WriteError(ctx, w, http.StatusBadGateway, "upstream_error", "a service behind the gateway failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.Status)
	_, err := w.Write(response.Body)
	if err != nil {
		logger.ErrorContext(ctx, "Write answer", slog.String("error", err.Error()))
	}
}

// passThroughAuth forwards a sign in answer with the two headers a session needs:
// Location, because the browser has to follow the redirect itself, and every
// Set-Cookie, because the refresh cookie is identity's to write.
func passThroughAuth(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, response aggregate.Response) {
	if response.Status >= http.StatusInternalServerError {
		logger.ErrorContext(ctx, "Service failed",
			slog.Int("status", response.Status),
			slog.String("body", string(response.Body)),
		)
		vermouth.WriteError(ctx, w, http.StatusBadGateway, "upstream_error", "a service behind the gateway failed")
		return
	}
	for _, cookie := range response.Header.Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", cookie)
	}
	if location := response.Header.Get("Location"); location != "" {
		w.Header().Set("Location", location)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(response.Status)
	_, err := w.Write(response.Body)
	if err != nil {
		logger.ErrorContext(ctx, "Write answer", slog.String("error", err.Error()))
	}
}

// upstreamFailed answers when a service could not be reached at all.
func upstreamFailed(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, service string, err error) {
	logger.ErrorContext(ctx, "Service unreachable",
		slog.String("upstream", service),
		slog.String("error", err.Error()),
	)
	vermouth.WriteError(ctx, w, http.StatusBadGateway, "upstream_unreachable", "could not reach "+service)
}
