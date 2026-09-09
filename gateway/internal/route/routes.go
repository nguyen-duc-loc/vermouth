package route

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/auth"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/ratelimit"
)

const maxRequestBody = 1 << 20

const refreshCookieName = "vermouth_refresh"

// Deps is what the routes need. There is no database in here on purpose.
type Deps struct {
	Client        *aggregate.Client
	Verifier      *vermouth.Verifier
	Logger        *slog.Logger
	Service       string
	AuthRateGuard *ratelimit.Guard
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
	mux.HandleFunc(
		"GET /api/auth/google/start",
		limitedBrowserAuth(deps, ratelimit.EndpointStart, "/auth/google/start"),
	)
	mux.HandleFunc(
		"GET /api/auth/google/callback",
		limitedBrowserAuth(deps, ratelimit.EndpointCallback, "/auth/google/callback"),
	)
	mux.HandleFunc("POST /api/auth/refresh", limitedRefresh(deps))
	mux.HandleFunc("POST /api/auth/signout", forwardAuth(deps, "/auth/signout"))
	for _, path := range []string{
		"/api/auth/google/start",
		"/api/auth/google/callback",
		"/api/auth/refresh",
	} {
		mux.HandleFunc("HEAD "+path, rejectHead)
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

	authenticated.HandleFunc("POST /api/classes", proxyCreateClass(deps))
	authenticated.HandleFunc("POST /api/students", proxyCreateStudent(deps))
	authenticated.HandleFunc("POST /api/classes/{class_id}/roster", proxyJoinRoster(deps))
	authenticated.HandleFunc(
		"PUT /api/sessions/{session_id}/attendance/{student_id}",
		proxyMarkAttendance(deps),
	)
	authenticated.HandleFunc("GET /api/home", readHome(deps))
	authenticated.HandleFunc("GET /api/home/billing-projection", readHomeBillingProjection(deps))

	mux.Handle("/api/", auth.Middleware(deps.Verifier, deps.Logger, authenticated))

	return vermouth.RequestIDMiddleware(deps.Logger, mux)
}

func proxyCreateClass(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input apitypes.CreateClassRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.CallWithHeaders(
			r.Context(),
			http.MethodPost,
			deps.Client.Upstreams().Teaching,
			"/classes",
			vermouth.BearerToken(r),
			http.Header{"Idempotency-Key": []string{r.Header.Get("Idempotency-Key")}},
			input,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func proxyCreateStudent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input apitypes.CreateStudentRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.CallWithHeaders(
			r.Context(),
			http.MethodPost,
			deps.Client.Upstreams().Teaching,
			"/students",
			vermouth.BearerToken(r),
			http.Header{"Idempotency-Key": []string{r.Header.Get("Idempotency-Key")}},
			input,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func proxyJoinRoster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		classID, err := uuid.Parse(r.PathValue("class_id"))
		if err != nil {
			vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "class_id must be a UUID")
			return
		}
		var input apitypes.JoinRosterRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.Call(
			r.Context(),
			http.MethodPost,
			deps.Client.Upstreams().Teaching,
			"/classes/"+classID.String()+"/roster",
			vermouth.BearerToken(r),
			input,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func proxyMarkAttendance(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, sessionErr := uuid.Parse(r.PathValue("session_id"))
		studentID, studentErr := uuid.Parse(r.PathValue("student_id"))
		if sessionErr != nil || studentErr != nil {
			vermouth.WriteError(
				r.Context(),
				w,
				http.StatusBadRequest,
				"invalid_input",
				"session_id and student_id must be UUID values",
			)
			return
		}
		var input apitypes.MarkAttendanceRequest
		if !decodeJSONBody(w, r, &input) {
			return
		}
		response, err := deps.Client.Call(
			r.Context(),
			http.MethodPut,
			deps.Client.Upstreams().Teaching,
			"/sessions/"+sessionID.String()+"/attendance/"+studentID.String(),
			vermouth.BearerToken(r),
			input,
		)
		if err != nil {
			upstreamFailed(r.Context(), deps.Logger, w, "teaching", err)
			return
		}
		passThrough(r.Context(), deps.Logger, w, response)
	}
}

func readHome(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := deps.Client.Home(
			r.Context(),
			vermouth.BearerToken(r),
			r.URL.Query().Get("cursor"),
		)
		if err != nil {
			if responseError, ok := errors.AsType[*aggregate.RequiredResponseError](err); ok {
				passThrough(r.Context(), deps.Logger, w, responseError.Response)
				return
			}
			upstreamFailed(r.Context(), deps.Logger, w, "a required home service", err)
			return
		}
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, status)
	}
}

func readHomeBillingProjection(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := deps.Client.HomeBillingProjection(r.Context(), vermouth.BearerToken(r))
		vermouth.WriteJSON(r.Context(), deps.Logger, w, http.StatusOK, status)
	}
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if err != nil {
		vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "the JSON body is invalid")
		return false
	}
	err = decoder.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		vermouth.WriteError(r.Context(), w, http.StatusBadRequest, "invalid_input", "the JSON body must contain one value")
		return false
	}
	return true
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

func limitedBrowserAuth(deps Deps, endpoint ratelimit.Endpoint, upstreamPath string) http.HandlerFunc {
	forward := forwardAuth(deps, upstreamPath)
	return func(w http.ResponseWriter, r *http.Request) {
		decision := deps.AuthRateGuard.Check(r, endpoint)
		if decision.Allowed {
			forward(w, r)
			return
		}
		writeRetryHeaders(w, decision.RetryAfter)
		http.Redirect(w, r, "/signin?error=rate_limited", http.StatusFound)
	}
}

func limitedRefresh(deps Deps) http.HandlerFunc {
	forward := forwardAuth(deps, "/auth/refresh")
	return func(w http.ResponseWriter, r *http.Request) {
		cookies := r.CookiesNamed(refreshCookieName)
		additional := make([]ratelimit.Key, 0, 1)
		malformed := len(cookies) > 1 || len(cookies) == 1 && cookies[0].Value == ""
		if len(cookies) == 1 && cookies[0].Value != "" {
			additional = append(additional, ratelimit.RefreshTokenKey(cookies[0].Value))
		}

		decision := deps.AuthRateGuard.Check(r, ratelimit.EndpointRefresh, additional...)
		if !decision.Allowed {
			writeRetryHeaders(w, decision.RetryAfter)
			vermouth.WriteError(
				r.Context(),
				w,
				http.StatusTooManyRequests,
				"rate_limited",
				"too many authentication requests; try again later",
			)
			return
		}
		if malformed {
			vermouth.WriteError(
				r.Context(),
				w,
				http.StatusUnauthorized,
				"unauthenticated",
				"this session is over, sign in again",
			)
			return
		}
		forward(w, r)
	}
}

func rejectHead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", allowedMethodForPath(r.URL.Path))
	vermouth.WriteError(r.Context(), w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func allowedMethodForPath(path string) string {
	if path == "/api/auth/refresh" {
		return http.MethodPost
	}
	return http.MethodGet
}

func writeRetryHeaders(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := max(int64(retryAfter/time.Second), 1)
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	w.Header().Set("Cache-Control", "no-store")
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
// Set-Cookie, because the refresh cookie is identity's to write. The declared
// auth_unavailable response is decoded before it crosses the boundary, while
// every other 5xx is still masked.
func passThroughAuth(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, response aggregate.Response) {
	if unavailable, ok := declaredAuthUnavailable(response); ok {
		vermouth.WriteJSON(ctx, logger, w, http.StatusServiceUnavailable, unavailable)
		return
	}
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

func declaredAuthUnavailable(response aggregate.Response) (vermouth.APIError, bool) {
	var answer vermouth.APIError
	if response.Status != http.StatusServiceUnavailable {
		return answer, false
	}
	err := json.Unmarshal(response.Body, &answer)
	if err != nil {
		return answer, false
	}
	valid := answer.Error.Code == "auth_unavailable" &&
		answer.Error.Message != "" &&
		answer.Error.RequestID != ""
	return answer, valid
}

// upstreamFailed answers when a service could not be reached at all.
func upstreamFailed(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, service string, err error) {
	logger.ErrorContext(ctx, "Service unreachable",
		slog.String("upstream", service),
		slog.String("error", err.Error()),
	)
	vermouth.WriteError(ctx, w, http.StatusBadGateway, "upstream_unreachable", "could not reach "+service)
}
