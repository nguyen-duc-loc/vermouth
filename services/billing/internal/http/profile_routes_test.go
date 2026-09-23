package http_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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

// covers: AC-1 to AC-9, AC-14
func TestInvoiceProfileRoutes_VerifyOwnerShapeCacheAndDomainOutcomes(t *testing.T) {
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
	tutorID := uuid.Must(uuid.NewV7())
	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(), //nolint:usetesting // Cleanup runs after the test context is canceled.
			`DELETE FROM invoice_profiles WHERE tutor_id = $1`, tutorID,
		)
		require.NoError(t, cleanupErr)
	})
	verifier, token := profileRouteToken(t, tutorID)
	server := billinghttp.Mux(billinghttp.Deps{
		Profile:  handler.NewProfileService(pool),
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Health: vermouth.Health{
			Service: "billing",
			Logger:  slog.New(slog.DiscardHandler),
		},
	})

	unauthorized := serveProfileRequest(t, server, http.MethodGet, "/invoice-profile", "", "")
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)
	require.Equal(t, "no-store", unauthorized.Header().Get("Cache-Control"))
	unauthorizedBanks := serveProfileRequest(t, server, http.MethodGet, "/banks", "", "")
	require.Equal(t, http.StatusUnauthorized, unauthorizedBanks.Code)
	require.Equal(t, "no-store", unauthorizedBanks.Header().Get("Cache-Control"))

	empty := serveProfileRequest(t, server, http.MethodGet, "/invoice-profile", "", token)
	require.Equal(t, http.StatusOK, empty.Code)
	require.Equal(t, "no-store", empty.Header().Get("Cache-Control"))
	require.JSONEq(t, `{
		"legal_name":null,"contact_line":null,"bank_code":null,"bank_name":null,
		"bank_account_number":null,"bank_account_holder":null,"revision":0,
		"is_complete":false,
		"missing_fields":["legal_name","contact_line","bank_code","bank_account_number","bank_account_holder"],
		"bank_status":"missing"
	}`, empty.Body.String())

	saved := serveProfileRequest(t, server, http.MethodPut, "/invoice-profile", `{
		"expected_revision":0,
		"legal_name":"  Nguyễn   An  ",
		"contact_line":null,
		"bank_code":null,
		"bank_account_number":null,
		"bank_account_holder":null
	}`, token)
	require.Equal(t, http.StatusOK, saved.Code)
	require.Contains(t, saved.Body.String(), `"legal_name":"Nguyễn An"`)
	require.Contains(t, saved.Body.String(), `"revision":1`)
	require.NotContains(t, saved.Body.String(), tutorID.String())

	conflict := serveProfileRequest(t, server, http.MethodPut, "/invoice-profile", `{
		"expected_revision":0,
		"legal_name":"Different",
		"contact_line":null,
		"bank_code":null,
		"bank_account_number":null,
		"bank_account_holder":null
	}`, token)
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.Contains(t, conflict.Body.String(), `"code":"profile_conflict"`)
	require.Contains(t, conflict.Body.String(), `"current_revision":1`)

	invalid := serveProfileRequest(t, server, http.MethodPut, "/invoice-profile", `{
		"expected_revision":1,
		"legal_name":"Nguyễn An",
		"contact_line":null,
		"bank_code":null,
		"bank_account_number":"12-34",
		"bank_account_holder":null
	}`, token)
	require.Equal(t, http.StatusUnprocessableEntity, invalid.Code)
	require.Contains(t, invalid.Body.String(), `"field":"bank_account_number"`)
	require.NotContains(t, invalid.Body.String(), "12-34")

	badShape := serveProfileRequest(t, server, http.MethodPut, "/invoice-profile", `{
		"expected_revision":1,"legal_name":null,"unexpected":true
	}`, token)
	require.Equal(t, http.StatusBadRequest, badShape.Code)

	banks := serveProfileRequest(t, server, http.MethodGet, "/banks", "", token)
	require.Equal(t, http.StatusOK, banks.Code)
	require.Equal(t, "private, max-age=86400", banks.Header().Get("Cache-Control"))
	var catalog struct {
		Banks []handler.Bank `json:"banks"`
	}
	require.NoError(t, json.Unmarshal(banks.Body.Bytes(), &catalog))
	require.NotEmpty(t, catalog.Banks)
}

func serveProfileRequest(
	t *testing.T,
	server http.Handler,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	request.Header.Set(vermouth.RequestIDHeader, "request-profile")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func profileRouteToken(t *testing.T, tutorID uuid.UUID) (*vermouth.Verifier, string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	verifier, err := vermouth.NewVerifier(map[string]ed25519.PublicKey{"test": publicKey})
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
