//nolint:testpackage // These white box tests exercise transport errors without database work.
package http

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/handler"
)

var errSensitiveRouteFailure = errors.New("database failed with phone 0901234567")

// covers: spec 0013 AC-1, AC-6
func TestMux_RequiresAnExplicitRateAmount(t *testing.T) {
	t.Parallel()
	verifier, token := teachingRouteToken(t)
	server := Mux(Deps{
		Verifier: verifier, Logger: slog.New(slog.DiscardHandler),
		Health: vermouth.Health{Service: "teaching", Logger: slog.New(slog.DiscardHandler)},
	})
	for _, test := range []struct {
		name    string
		body    string
		message string
	}{
		{"empty object", `{}`, "rate_amount must be an integer"},
		{"null body", `null`, "rate_amount must be an integer"},
		{"null amount", `{"rate_amount":null}`, "rate_amount must be an integer"},
		// An explicit zero passes transport validation and reaches the domain key check.
		{"explicit zero", `{"rate_amount":0}`, "Idempotency-Key"},
	} {
		for _, endpoint := range []struct{ name, method, path string }{
			{"rate", stdhttp.MethodPut, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates/2026-08-01"},
			{"class", stdhttp.MethodPost, "/classes"},
		} {
			t.Run(endpoint.name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				request := httptest.NewRequestWithContext(t.Context(), endpoint.method, endpoint.path, strings.NewReader(test.body))
				request.Header.Set("Authorization", "Bearer "+token)
				recorder := httptest.NewRecorder()
				server.ServeHTTP(recorder, request)
				require.Equal(t, stdhttp.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), test.message)
			})
		}
	}
}

// covers: AC-6, AC-12
// covers: spec 0013 AC-1, AC-21, AC-23
func TestMux_RequiresAValidTokenOnEveryTeachingOperation(t *testing.T) {
	t.Parallel()

	verifier, _ := teachingRouteToken(t)
	server := Mux(Deps{
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Health:   vermouth.Health{Service: "teaching", Logger: slog.New(slog.DiscardHandler)},
	})
	tests := []struct {
		method string
		path   string
	}{
		{stdhttp.MethodPost, "/classes"},
		{stdhttp.MethodGet, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates"},
		{stdhttp.MethodPut, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates/2026-08-01"},
		{stdhttp.MethodPut, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/schedule"},
		{stdhttp.MethodGet, "/students"},
		{stdhttp.MethodPost, "/students"},
		{stdhttp.MethodGet, "/students/018f8f7e-91b0-7cc4-bd8c-f4d9030ca422"},
		{stdhttp.MethodPatch, "/students/018f8f7e-91b0-7cc4-bd8c-f4d9030ca422"},
		{stdhttp.MethodDelete, "/students/018f8f7e-91b0-7cc4-bd8c-f4d9030ca422"},
		{stdhttp.MethodGet, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/roster"},
		{stdhttp.MethodPut, "/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/roster"},
		{stdhttp.MethodGet, "/sessions/018f8f7e-91b0-7cc4-bd8c-f4d9030ca423/attendance"},
		{stdhttp.MethodPut, "/sessions/018f8f7e-91b0-7cc4-bd8c-f4d9030ca423/attendance"},
		{stdhttp.MethodGet, "/home"},
		{stdhttp.MethodGet, "/schedule?from=2026-08-30&through=2026-09-05"},
	}

	for _, test := range tests {
		request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, stdhttp.NoBody)
		request.Header.Set(vermouth.RequestIDHeader, "request-teaching")
		recorder := httptest.NewRecorder()

		server.ServeHTTP(recorder, request)

		require.Equal(t, stdhttp.StatusUnauthorized, recorder.Code)
		require.JSONEq(t,
			`{"error":{"code":"unauthenticated","message":"a valid bearer token is required","request_id":"request-teaching"}}`,
			recorder.Body.String(),
		)
	}
}

// covers: AC-2, AC-4, AC-5, AC-11, AC-12
// covers: spec 0013 AC-1, AC-21
func TestMux_RejectsInvalidTransportInputBeforeBusinessWork(t *testing.T) {
	t.Parallel()

	verifier, token := teachingRouteToken(t)
	server := Mux(Deps{
		Verifier: verifier,
		Logger:   slog.New(slog.DiscardHandler),
		Health:   vermouth.Health{Service: "teaching", Logger: slog.New(slog.DiscardHandler)},
	})
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"unknown class field", stdhttp.MethodPost, "/classes", `{"unknown":true}`},
		{"two student values", stdhttp.MethodPost, "/students", `{"name":"Mai"}{"name":"Lan"}`},
		{"invalid class rate id", stdhttp.MethodGet, "/classes/not-a-uuid/rates", `{}`},
		{
			"invalid class rate write id",
			stdhttp.MethodPut,
			"/classes/not-a-uuid/rates/2026-08-01",
			`{"rate_amount":250000}`,
		},
		{
			"unknown class rate field",
			stdhttp.MethodPut,
			"/classes/018f8f7e-91b0-7cc4-bd8c-f4d9030ca421/rates/2026-08-01",
			`{"rate_amount":250000,"unknown":true}`,
		},
		{"invalid class id", stdhttp.MethodPut, "/classes/not-a-uuid/roster", `{}`},
		{"invalid schedule class id", stdhttp.MethodPut, "/classes/not-a-uuid/schedule", `{}`},
		{"invalid attendance id", stdhttp.MethodGet, "/sessions/not-a-uuid/attendance", `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				t.Context(), test.method, test.path, strings.NewReader(test.body),
			)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set(vermouth.RequestIDHeader, "request-invalid")
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, request)

			require.Equal(t, stdhttp.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"code":"invalid_input"`)
			require.NotContains(t, recorder.Body.String(), token)
		})
	}
}

// covers: AC-6, AC-10, AC-12
func TestWriteHandlerError_MapsDomainOutcomesWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation", &handler.ValidationError{Field: "name", Message: "is required"}, stdhttp.StatusBadRequest, "invalid_input"},
		{"missing owned resource", handler.ErrNotFound, stdhttp.StatusNotFound, "not_found"},
		{"state conflict", handler.ErrConflict, stdhttp.StatusConflict, "conflict"},
		{"idempotency conflict", handler.ErrIdempotencyConflict, stdhttp.StatusConflict, "idempotency_conflict"},
		{"internal failure", errSensitiveRouteFailure, stdhttp.StatusInternalServerError, "internal"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(t.Context(), stdhttp.MethodPost, "/classes", stdhttp.NoBody)
			request = request.WithContext(vermouth.WithRequestID(request.Context(), "request-error"))
			recorder := httptest.NewRecorder()
			deps := Deps{Logger: slog.New(slog.DiscardHandler)}

			writeHandlerError(deps, recorder, request, "Test operation", test.err)

			require.Equal(t, test.wantStatus, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"code":"`+test.wantCode+`"`)
			require.Contains(t, recorder.Body.String(), `"request_id":"request-error"`)
			require.NotContains(t, recorder.Body.String(), errSensitiveRouteFailure.Error())
		})
	}
}

func teachingRouteToken(t *testing.T) (*vermouth.Verifier, string) {
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
