package http_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/handler"
	billinghttp "github.com/nguyen-duc-loc/vermouth/services/billing/internal/http"
)

// covers: AC-8, AC-9, AC-12
func TestTeachingProjectionStatus_RequiresLocalVerificationAndReturnsTutorProgress(t *testing.T) {
	t.Parallel()

	databaseURL := os.Getenv("BILLING_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("BILLING_DATABASE_URL is unset, you may run task infra:up and task migrate:up")
	}
	pool, err := vermouth.OpenPool(t.Context(), databaseURL)
	if err != nil {
		t.Skipf("the billing database did not answer, you may run task infra:up: %v", err)
	}
	t.Cleanup(pool.Close)
	verifier, token := billingRouteToken(t)
	server := billinghttp.Mux(billinghttp.Deps{
		Projection: handler.NewProjectionReader(pool),
		Verifier:   verifier,
		Logger:     slog.New(slog.DiscardHandler),
		Health: vermouth.Health{
			Service: "billing",
			Logger:  slog.New(slog.DiscardHandler),
		},
	})

	unauthorized := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/projections/teaching/status", http.NoBody,
	)
	unauthorized.Header.Set(vermouth.RequestIDHeader, "request-billing")
	unauthorizedRecorder := httptest.NewRecorder()
	server.ServeHTTP(unauthorizedRecorder, unauthorized)
	require.Equal(t, http.StatusUnauthorized, unauthorizedRecorder.Code)
	require.NotContains(t, unauthorizedRecorder.Body.String(), token)

	authorized := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/projections/teaching/status", http.NoBody,
	)
	authorized.Header.Set("Authorization", "Bearer "+token)
	authorized.Header.Set(vermouth.RequestIDHeader, "request-billing")
	authorizedRecorder := httptest.NewRecorder()
	server.ServeHTTP(authorizedRecorder, authorized)

	require.Equal(t, http.StatusOK, authorizedRecorder.Code)
	require.JSONEq(t, `{
		"state":"waiting",
		"class_count":0,
		"session_count":0,
		"student_count":0,
		"open_roster_count":0,
		"attendance_count":0,
		"latest_updated_at":null
	}`, authorizedRecorder.Body.String())
}

// covers: spec 0013 AC-1, AC-21, AC-23
func TestBillingRoutesRequireAValidTokenAndDisableCaching(t *testing.T) {
	t.Parallel()

	verifier, _ := billingRouteToken(t)
	server := billinghttp.Mux(billinghttp.Deps{
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Health: vermouth.Health{
			Service: "billing",
			Logger:  slog.New(slog.DiscardHandler),
		},
	})
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates"},
		{http.MethodGet, "/billing-periods/default"},
		{http.MethodGet, "/billing-periods/2026/8"},
		{http.MethodPost, "/billing-periods/2026/8/preview"},
		{http.MethodPost, "/billing-periods/2026/8/issue"},
	}

	for _, test := range tests {
		request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, http.NoBody)
		request.Header.Set(vermouth.RequestIDHeader, "request-private-billing")
		recorder := httptest.NewRecorder()

		server.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		require.Contains(t, recorder.Body.String(), `"code":"invalid_token"`)
	}
}

// covers: spec 0013 AC-1, AC-4, AC-13, AC-21
func TestBillingRoutesRejectMalformedInputBeforeBusinessWork(t *testing.T) {
	t.Parallel()

	verifier, token := billingRouteToken(t)
	server := billinghttp.Mux(billinghttp.Deps{
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Health: vermouth.Health{
			Service: "billing",
			Logger:  slog.New(slog.DiscardHandler),
		},
	})
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		code   string
	}{
		{
			name:   "invalid class identifier",
			method: http.MethodGet,
			path:   "/classes/not-a-uuid/rates",
			code:   "invalid_input",
		},
		{
			name:   "invalid period path",
			method: http.MethodPost,
			path:   "/billing-periods/year/8/preview",
			code:   "invalid_period",
		},
		{
			name:   "unknown issue field",
			method: http.MethodPost,
			path:   "/billing-periods/2026/8/issue",
			body:   `{"preview_fingerprint":"digest","unknown":true}`,
			code:   "invalid_input",
		},
		{
			name:   "two issue values",
			method: http.MethodPost,
			path:   "/billing-periods/2026/8/issue",
			body:   `{"preview_fingerprint":"first"}{"preview_fingerprint":"second"}`,
			code:   "invalid_input",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				t.Context(), test.method, test.path, strings.NewReader(test.body),
			)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set(vermouth.RequestIDHeader, "request-invalid-billing")
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Contains(t, recorder.Body.String(), `"code":"`+test.code+`"`)
			require.NotContains(t, recorder.Body.String(), token)
		})
	}
}

func billingRouteToken(t *testing.T) (*vermouth.Verifier, string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	verifier, err := vermouth.NewVerifier(map[string]ed25519.PublicKey{"test": publicKey})
	require.NoError(t, err)
	tutorID, err := uuid.NewV7()
	require.NoError(t, err)
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub":      tutorID.String(),
		"tz":       "Asia/Ho_Chi_Minh",
		"language": "en",
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = "test"
	signed, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return verifier, signed
}
