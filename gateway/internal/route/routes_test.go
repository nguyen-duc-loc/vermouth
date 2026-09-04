package route_test

import (
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/ratelimit"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/route"
)

// covers: AC-14
func TestMuxPassesThroughDeclaredAuthUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		upstreamStatus int
		upstreamBody   string
		wantStatus     int
		wantBody       string
	}{
		{
			name:           "declared service unavailable passes through",
			upstreamStatus: http.StatusServiceUnavailable,
			upstreamBody:   `{"error":{"code":"auth_unavailable","message":"Google sign in is not configured for this local environment","request_id":"req-1"}}`,
			wantStatus:     http.StatusServiceUnavailable,
			wantBody:       `{"error":{"code":"auth_unavailable","message":"Google sign in is not configured for this local environment","request_id":"req-1"}}`,
		},
		{
			name:           "unexpected internal failure is masked",
			upstreamStatus: http.StatusInternalServerError,
			upstreamBody:   `{"error":"database details"}`,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"error":{"code":"upstream_error","message":"a service behind the gateway failed","request_id":"req-1"}}`,
		},
		{
			name:           "undeclared service unavailable failure is masked",
			upstreamStatus: http.StatusServiceUnavailable,
			upstreamBody:   `{"error":"identity internals"}`,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"error":{"code":"upstream_error","message":"a service behind the gateway failed","request_id":"req-1"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			must := require.New(t)
			writeErrors := make(chan error, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.upstreamStatus)
				_, err := w.Write([]byte(test.upstreamBody))
				writeErrors <- err
			}))
			t.Cleanup(upstream.Close)

			client := aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL})
			handler := route.Mux(route.Deps{
				Client:        client,
				Logger:        slog.New(slog.DiscardHandler),
				Service:       "gateway",
				AuthRateGuard: unrestrictedAuthGuard(),
			})
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				"/api/auth/google/start?redirect_to=%2F",
				http.NoBody,
			)
			request.Header.Set(vermouth.RequestIDHeader, "req-1")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			must.NoError(<-writeErrors)
			must.Equal(test.wantStatus, recorder.Code)
			must.Equal("application/json", recorder.Header().Get("Content-Type"))
			must.Equal("req-1", recorder.Header().Get(vermouth.RequestIDHeader))
			must.JSONEq(test.wantBody, recorder.Body.String())
		})
	}
}

// covers: AC-14
func TestMuxPassesThroughAuthSessionHeaders(t *testing.T) {
	t.Parallel()

	must := require.New(t)
	writeErrors := make(chan error, 1)
	forwardedRequests := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		forwardedRequests <- request.URL.RequestURI()
		w.Header().Add("Set-Cookie", "refresh=first; HttpOnly; SameSite=Lax")
		w.Header().Add("Set-Cookie", "state=second; HttpOnly; SameSite=Lax")
		w.Header().Set("Location", "/auth/continue")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusFound)
		_, err := w.Write([]byte(`{"status":"continue"}`))
		writeErrors <- err
	}))
	t.Cleanup(upstream.Close)

	client := aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL})
	handler := route.Mux(route.Deps{
		Client:        client,
		Logger:        slog.New(slog.DiscardHandler),
		Service:       "gateway",
		AuthRateGuard: unrestrictedAuthGuard(),
	})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/auth/google/start?redirect_to=%2Fthread",
		http.NoBody,
	)
	request.Header.Set(vermouth.RequestIDHeader, "req-session")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	must.NoError(<-writeErrors)
	must.Equal("/auth/google/start?redirect_to=%2Fthread", <-forwardedRequests)
	must.Equal(http.StatusFound, recorder.Code)
	must.Equal([]string{
		"refresh=first; HttpOnly; SameSite=Lax",
		"state=second; HttpOnly; SameSite=Lax",
	}, recorder.Header().Values("Set-Cookie"))
	must.Equal("/auth/continue", recorder.Header().Get("Location"))
	must.Equal("application/json", recorder.Header().Get("Content-Type"))
	must.JSONEq(`{"status":"continue"}`, recorder.Body.String())
}

// covers: AC-14
func TestMuxForwardsEveryPublicAuthRouteToItsFixedIdentityPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		inboundURI string
		wantURI    string
	}{
		{
			name:       "Google start",
			method:     http.MethodGet,
			inboundURI: "/api/auth/google/start?redirect_to=%2Fthread",
			wantURI:    "/auth/google/start?redirect_to=%2Fthread",
		},
		{
			name:       "Google callback",
			method:     http.MethodGet,
			inboundURI: "/api/auth/google/callback?code=a%2Bb&state=state-1",
			wantURI:    "/auth/google/callback?code=a%2Bb&state=state-1",
		},
		{
			name:       "refresh",
			method:     http.MethodPost,
			inboundURI: "/api/auth/refresh",
			wantURI:    "/auth/refresh",
		},
		{
			name:       "sign out",
			method:     http.MethodPost,
			inboundURI: "/api/auth/signout",
			wantURI:    "/auth/signout",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			type forwardedRequest struct {
				method    string
				uri       string
				cookie    string
				origin    string
				requestID string
			}
			forwarded := make(chan forwardedRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				forwarded <- forwardedRequest{
					method:    request.Method,
					uri:       request.URL.RequestURI(),
					cookie:    request.Header.Get("Cookie"),
					origin:    request.Header.Get("Origin"),
					requestID: request.Header.Get(vermouth.RequestIDHeader),
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(upstream.Close)

			client := aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL})
			handler := route.Mux(route.Deps{
				Client:        client,
				Logger:        slog.New(slog.DiscardHandler),
				Service:       "gateway",
				AuthRateGuard: unrestrictedAuthGuard(),
			})
			request := httptest.NewRequestWithContext(t.Context(), test.method, test.inboundURI, http.NoBody)
			request.Header.Set("Cookie", "refresh=session-token")
			request.Header.Set("Origin", "https://app.example")
			request.Header.Set(vermouth.RequestIDHeader, "req-public-auth")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			received := <-forwarded
			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.Equal(t, test.method, received.method)
			require.Equal(t, test.wantURI, received.uri)
			require.Equal(t, "refresh=session-token", received.cookie)
			require.Equal(t, "https://app.example", received.origin)
			require.Equal(t, "req-public-auth", received.requestID)
		})
	}
}

// covers: spec 0007 AC-1, AC-4, AC-5
func TestMuxLimitsGoogleStartBeforeForwarding(t *testing.T) {
	t.Parallel()

	forwarded := make(chan struct{}, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded <- struct{}{}
		w.Header().Set("Location", "https://accounts.google.com/authorize")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	caller := ratelimit.BucketPairConfig{Minute: 1, Hour: 20}
	global := ratelimit.BucketPairConfig{Minute: 100, Hour: 500}
	guard := ratelimit.NewGuard(ratelimit.Config{
		Start: ratelimit.EndpointPolicy{IP: caller, Global: global},
		Callback: ratelimit.EndpointPolicy{
			IP: caller, Global: global,
		},
		Refresh: ratelimit.EndpointPolicy{
			IP: caller, RefreshToken: caller, Global: global,
		},
	}, slog.New(slog.DiscardHandler), func() time.Time {
		return time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	})
	handler := route.Mux(route.Deps{
		Client:        aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
		Logger:        slog.New(slog.DiscardHandler),
		Service:       "gateway",
		AuthRateGuard: guard,
	})

	allowed := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/api/auth/google/start", http.NoBody,
	)
	allowed.RemoteAddr = "198.51.100.8:42000"
	allowedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allowedRecorder, allowed)

	limited := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/api/auth/google/start", http.NoBody,
	)
	limited.RemoteAddr = "198.51.100.8:42001"
	limited.Header.Set(vermouth.RequestIDHeader, "request-limited")
	limitedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(limitedRecorder, limited)

	require.Equal(t, http.StatusFound, allowedRecorder.Code)
	require.Equal(t, "https://accounts.google.com/authorize", allowedRecorder.Header().Get("Location"))
	require.Equal(t, http.StatusFound, limitedRecorder.Code)
	require.Equal(t, "/signin?error=rate_limited", limitedRecorder.Header().Get("Location"))
	require.Equal(t, "60", limitedRecorder.Header().Get("Retry-After"))
	require.Equal(t, "no-store", limitedRecorder.Header().Get("Cache-Control"))
	require.Len(t, forwarded, 1)
}

// covers: spec 0007 AC-1, AC-4, AC-5
func TestMuxLimitsGoogleCallbackBeforeForwarding(t *testing.T) {
	t.Parallel()

	forwarded := make(chan struct{}, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded <- struct{}{}
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	caller := ratelimit.BucketPairConfig{Minute: 1, Hour: 60}
	global := ratelimit.BucketPairConfig{Minute: 200, Hour: 1_000}
	handler := route.Mux(route.Deps{
		Client:  aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
		Logger:  slog.New(slog.DiscardHandler),
		Service: "gateway",
		AuthRateGuard: routeTestGuard(
			ratelimit.EndpointPolicy{IP: caller, Global: global},
			ratelimit.EndpointPolicy{IP: caller, Global: global},
			ratelimit.EndpointPolicy{IP: caller, RefreshToken: caller, Global: global},
		),
	})

	for index := range 2 {
		request := httptest.NewRequestWithContext(
			t.Context(), http.MethodGet, "/api/auth/google/callback", http.NoBody,
		)
		request.RemoteAddr = fmt.Sprintf("198.51.100.8:%d", 42_000+index)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if index == 0 {
			require.Equal(t, http.StatusFound, recorder.Code)
			continue
		}
		require.Equal(t, "/signin?error=rate_limited", recorder.Header().Get("Location"))
		require.Equal(t, "60", recorder.Header().Get("Retry-After"))
	}
	require.Len(t, forwarded, 1)
}

// covers: spec 0007 AC-1, AC-4, AC-5, AC-7
func TestMuxLimitsRefreshByTokenWithoutChangingTheCookie(t *testing.T) {
	t.Parallel()

	forwarded := make(chan struct{}, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	large := ratelimit.BucketPairConfig{Minute: 100, Hour: 1_000}
	token := ratelimit.BucketPairConfig{Minute: 1, Hour: 120}
	handler := route.Mux(route.Deps{
		Client:  aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
		Logger:  slog.New(slog.DiscardHandler),
		Service: "gateway",
		AuthRateGuard: routeTestGuard(
			ratelimit.EndpointPolicy{IP: large, Global: large},
			ratelimit.EndpointPolicy{IP: large, Global: large},
			ratelimit.EndpointPolicy{IP: large, RefreshToken: token, Global: large},
		),
	})

	for index := range 2 {
		request := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, "/api/auth/refresh", http.NoBody,
		)
		request.RemoteAddr = fmt.Sprintf("198.51.100.%d:42000", 8+index)
		request.Header.Add("Cookie", "vermouth_refresh=refresh-secret")
		request.Header.Set(vermouth.RequestIDHeader, "request-refresh")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if index == 0 {
			require.Equal(t, http.StatusNoContent, recorder.Code)
			continue
		}
		require.Equal(t, http.StatusTooManyRequests, recorder.Code)
		require.Equal(t, "60", recorder.Header().Get("Retry-After"))
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		require.Empty(t, recorder.Header().Values("Set-Cookie"))
		require.JSONEq(t,
			`{"error":{"code":"rate_limited","message":"too many authentication requests; try again later","request_id":"request-refresh"}}`,
			recorder.Body.String(),
		)
	}
	require.Len(t, forwarded, 1)
}

// covers: spec 0007 AC-1, AC-4, AC-5, AC-7
func TestMuxLimitsRefreshWithoutACookieByIP(t *testing.T) {
	t.Parallel()

	forwarded := make(chan struct{}, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"this session is over, sign in again","request_id":"request-refresh"}}`))
	}))
	t.Cleanup(upstream.Close)
	caller := ratelimit.BucketPairConfig{Minute: 1, Hour: 120}
	large := ratelimit.BucketPairConfig{Minute: 100, Hour: 1_000}
	handler := route.Mux(route.Deps{
		Client:  aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
		Logger:  slog.New(slog.DiscardHandler),
		Service: "gateway",
		AuthRateGuard: routeTestGuard(
			ratelimit.EndpointPolicy{IP: large, Global: large},
			ratelimit.EndpointPolicy{IP: large, Global: large},
			ratelimit.EndpointPolicy{IP: caller, RefreshToken: large, Global: large},
		),
	})

	for index := range 2 {
		request := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, "/api/auth/refresh", http.NoBody,
		)
		request.RemoteAddr = fmt.Sprintf("198.51.100.8:%d", 42_000+index)
		request.Header.Set(vermouth.RequestIDHeader, "request-refresh")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if index == 0 {
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			continue
		}
		require.Equal(t, http.StatusTooManyRequests, recorder.Code)
		require.Equal(t, "60", recorder.Header().Get("Retry-After"))
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		require.Empty(t, recorder.Header().Values("Set-Cookie"))
		require.JSONEq(t,
			`{"error":{"code":"rate_limited","message":"too many authentication requests; try again later","request_id":"request-refresh"}}`,
			recorder.Body.String(),
		)
	}
	require.Len(t, forwarded, 1)
}

// covers: spec 0007 AC-4, AC-7
func TestMuxRejectsMalformedRefreshCookiesAfterRateChecks(t *testing.T) {
	t.Parallel()

	for _, cookies := range [][]string{
		{"vermouth_refresh="},
		{"vermouth_refresh=first", "vermouth_refresh=second"},
	} {
		t.Run(strings.Join(cookies, ";"), func(t *testing.T) {
			t.Parallel()

			called := make(chan struct{}, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called <- struct{}{}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(upstream.Close)
			handler := route.Mux(route.Deps{
				Client:        aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
				Logger:        slog.New(slog.DiscardHandler),
				Service:       "gateway",
				AuthRateGuard: unrestrictedAuthGuard(),
			})
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodPost, "/api/auth/refresh", http.NoBody,
			)
			request.Header.Set(vermouth.RequestIDHeader, "request-malformed")
			for _, cookie := range cookies {
				request.Header.Add("Cookie", cookie)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.JSONEq(t,
				`{"error":{"code":"unauthenticated","message":"this session is over, sign in again","request_id":"request-malformed"}}`,
				recorder.Body.String(),
			)
			require.Empty(t, called)
		})
	}
}

// covers: spec 0007 AC-1, AC-3
func TestMuxRejectsAuthHeadWithoutSpendingRateBudgets(t *testing.T) {
	t.Parallel()

	forwarded := make(chan string, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		forwarded <- request.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	capacity := ratelimit.BucketPairConfig{Minute: 1, Hour: 1}
	policy := ratelimit.EndpointPolicy{IP: capacity, RefreshToken: capacity, Global: capacity}
	handler := route.Mux(route.Deps{
		Client:        aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
		Logger:        slog.New(slog.DiscardHandler),
		Service:       "gateway",
		AuthRateGuard: routeTestGuard(policy, policy, policy),
	})

	for _, test := range []struct {
		path          string
		allowedMethod string
	}{
		{path: "/api/auth/google/start", allowedMethod: http.MethodGet},
		{path: "/api/auth/google/callback", allowedMethod: http.MethodGet},
		{path: "/api/auth/refresh", allowedMethod: http.MethodPost},
	} {
		head := httptest.NewRequestWithContext(t.Context(), http.MethodHead, test.path, http.NoBody)
		head.Header.Set(vermouth.RequestIDHeader, "request-head")
		headRecorder := httptest.NewRecorder()
		handler.ServeHTTP(headRecorder, head)
		require.Equal(t, http.StatusMethodNotAllowed, headRecorder.Code)
		require.Equal(t, test.allowedMethod, headRecorder.Header().Get("Allow"))
		require.JSONEq(t,
			`{"error":{"code":"method_not_allowed","message":"method not allowed","request_id":"request-head"}}`,
			headRecorder.Body.String(),
		)

		allowed := httptest.NewRequestWithContext(
			t.Context(), test.allowedMethod, test.path, http.NoBody,
		)
		allowedRecorder := httptest.NewRecorder()
		handler.ServeHTTP(allowedRecorder, allowed)
		require.Equal(t, http.StatusNoContent, allowedRecorder.Code)
	}
	require.Len(t, forwarded, 3)
}

// covers: spec 0007 AC-1
func TestMuxLeavesOtherRoutesOutsideAuthRateBudgets(t *testing.T) {
	t.Parallel()

	forwarded := make(chan struct{}, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	verifier, err := vermouth.NewVerifier(map[string]ed25519.PublicKey{
		"test": make(ed25519.PublicKey, ed25519.PublicKeySize),
	})
	require.NoError(t, err)
	handler := route.Mux(route.Deps{
		Client:   aggregate.NewClient(aggregate.Upstreams{Identity: upstream.URL}),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})

	for range 2 {
		signout := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, "/api/auth/signout", http.NoBody,
		)
		signout.RemoteAddr = "198.51.100.8:42000"
		signoutRecorder := httptest.NewRecorder()
		handler.ServeHTTP(signoutRecorder, signout)
		require.Equal(t, http.StatusNoContent, signoutRecorder.Code)

		health := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
		healthRecorder := httptest.NewRecorder()
		handler.ServeHTTP(healthRecorder, health)
		require.Equal(t, http.StatusOK, healthRecorder.Code)

		protected := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", http.NoBody)
		protectedRecorder := httptest.NewRecorder()
		handler.ServeHTTP(protectedRecorder, protected)
		require.Equal(t, http.StatusUnauthorized, protectedRecorder.Code)
	}
	require.Len(t, forwarded, 2)
}

func TestMuxRejectsMissingBearerTokenWithoutCallingAService(t *testing.T) {
	t.Parallel()

	verifier, err := vermouth.NewVerifier(map[string]ed25519.PublicKey{
		"test": make(ed25519.PublicKey, ed25519.PublicKeySize),
	})
	require.NoError(t, err)
	handler := route.Mux(route.Deps{
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", http.NoBody)
	request.Header.Set(vermouth.RequestIDHeader, "req-protected")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.JSONEq(t,
		`{"error":{"code":"unauthenticated","message":"a valid bearer token is required","request_id":"req-protected"}}`,
		recorder.Body.String(),
	)
}

func unrestrictedAuthGuard() *ratelimit.Guard {
	capacity := ratelimit.BucketPairConfig{Minute: 1_000_000, Hour: 1_000_000}
	policy := ratelimit.EndpointPolicy{
		IP: capacity, RefreshToken: capacity, Global: capacity,
	}
	return routeTestGuard(policy, policy, policy)
}

func routeTestGuard(start, callback, refresh ratelimit.EndpointPolicy) *ratelimit.Guard {
	return ratelimit.NewGuard(
		ratelimit.Config{Start: start, Callback: callback, Refresh: refresh},
		slog.New(slog.DiscardHandler),
		func() time.Time { return time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC) },
	)
}
