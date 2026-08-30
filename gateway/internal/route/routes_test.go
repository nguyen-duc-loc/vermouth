package route_test

import (
	"crypto/ed25519"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
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
				Client:  client,
				Logger:  slog.New(slog.DiscardHandler),
				Service: "gateway",
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
		Client:  client,
		Logger:  slog.New(slog.DiscardHandler),
		Service: "gateway",
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
				Client:  client,
				Logger:  slog.New(slog.DiscardHandler),
				Service: "gateway",
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
