package route_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/route"
)

type forwardedTeachingRequest struct {
	method         string
	path           string
	authorization  string
	idempotencyKey string
	body           string
}

// covers: AC-1, AC-2, AC-4, AC-5, AC-10, AC-12
func TestMux_ForwardsTeachingCommandsThroughTheirDeclaredBoundary(t *testing.T) {
	t.Parallel()

	verifier, token := gatewayTestToken(t)
	tests := []struct {
		name               string
		method             string
		path               string
		body               string
		wantPath           string
		wantIdempotencyKey string
	}{
		{
			name:               "class and first session",
			method:             http.MethodPost,
			path:               "/api/classes",
			body:               `{"name":"Maths","rate_amount":250000,"first_session":{"local_date":"2026-08-30","start_time":"10:00","end_time":"11:00"}}`,
			wantPath:           "/classes",
			wantIdempotencyKey: "class-command",
		},
		{
			name:               "student",
			method:             http.MethodPost,
			path:               "/api/students",
			body:               `{"name":"Mai","phone":"0901234567"}`,
			wantPath:           "/students",
			wantIdempotencyKey: "student-command",
		},
		{
			name:     "roster",
			method:   http.MethodPost,
			path:     "/api/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/roster",
			body:     `{"student_id":"018f8f7e-91b0-7cc4-bd8c-f4d9030ca422","effective_from":"2026-08-30"}`,
			wantPath: "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/roster",
		},
		{
			name:     "attendance",
			method:   http.MethodPut,
			path:     "/api/sessions/018f8f7e-91b0-7cc4-bd8c-f4d9030ca423/attendance/018f8f7e-91b0-7cc4-bd8c-f4d9030ca422",
			body:     `{"state":"Present"}`,
			wantPath: "/sessions/018f8f7e-91b0-7cc4-bd8c-f4d9030ca423/attendance/018f8f7e-91b0-7cc4-bd8c-f4d9030ca422",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			forwarded := make(chan forwardedTeachingRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				forwarded <- forwardedTeachingRequest{
					method:         r.Method,
					path:           r.URL.Path,
					authorization:  r.Header.Get("Authorization"),
					idempotencyKey: r.Header.Get("Idempotency-Key"),
					body:           string(body),
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"saved":true}`))
			}))
			t.Cleanup(upstream.Close)
			handler := route.Mux(route.Deps{
				Client:   aggregate.NewClient(aggregate.Upstreams{Teaching: upstream.URL}),
				Verifier: verifier,
				Logger:   slog.New(slog.DiscardHandler),
				Service:  "gateway",
			})
			request := httptest.NewRequestWithContext(
				t.Context(), test.method, test.path, strings.NewReader(test.body),
			)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Idempotency-Key", test.wantIdempotencyKey)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			received := <-forwarded
			require.Equal(t, http.StatusCreated, recorder.Code)
			require.Equal(t, test.method, received.method)
			require.Equal(t, test.wantPath, received.path)
			require.Equal(t, "Bearer "+token, received.authorization)
			require.Equal(t, test.wantIdempotencyKey, received.idempotencyKey)
			require.JSONEq(t, test.body, received.body)
		})
	}
}

// covers: AC-6, AC-12
func TestMux_RejectsMissingBearerOnEveryTeachingRoute(t *testing.T) {
	t.Parallel()

	verifier, _ := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client:   aggregate.NewClient(aggregate.Upstreams{}),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Service:  "gateway",
	})
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/classes"},
		{http.MethodPost, "/api/students"},
		{http.MethodPost, "/api/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/roster"},
		{http.MethodPut, "/api/sessions/018f8f7e-91b0-7cc4-bd8c-f4d9030ca423/attendance/018f8f7e-91b0-7cc4-bd8c-f4d9030ca422"},
		{http.MethodGet, "/api/home"},
		{http.MethodGet, "/api/home/billing-projection"},
	}

	for _, test := range tests {
		request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, http.NoBody)
		request.Header.Set(vermouth.RequestIDHeader, "request-protected")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.JSONEq(t,
			`{"error":{"code":"unauthenticated","message":"a valid bearer token is required","request_id":"request-protected"}}`,
			recorder.Body.String(),
		)
	}
}

func gatewayTestToken(t *testing.T) (*vermouth.Verifier, string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	verifier, err := vermouth.NewVerifier(map[string]ed25519.PublicKey{"test": publicKey})
	require.NoError(t, err)
	tutorID, err := uuid.NewV7()
	require.NoError(t, err)
	claims := jwt.MapClaims{
		"sub":      tutorID.String(),
		"tz":       "Asia/Ho_Chi_Minh",
		"language": "en",
		"exp":      time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = "test"
	signed, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return verifier, signed
}
