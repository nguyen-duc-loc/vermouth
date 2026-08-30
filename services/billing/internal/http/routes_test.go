package http_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
