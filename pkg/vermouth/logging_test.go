package vermouth_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

// covers: AC-2, AC-4, AC-5, AC-12
func TestRequestIDMiddleware_PreservesTheCanonicalRequestHeader(t *testing.T) {
	t.Parallel()

	received := make(chan string, 1)
	handler := vermouth.RequestIDMiddleware(
		slog.New(slog.DiscardHandler),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- vermouth.RequestID(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	request.Header.Set("X-Request-ID", " request-teaching ")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, "X-Request-ID", vermouth.RequestIDHeader)
	require.Equal(t, "request-teaching", <-received)
	require.Equal(t, "request-teaching", recorder.Header().Get("X-Request-ID"))
}

// covers: AC-2, AC-4, AC-5, AC-12
func TestRequestIDMiddleware_CreatesAValidIdentifierWhenTheHeaderIsMissing(t *testing.T) {
	t.Parallel()

	received := make(chan string, 1)
	handler := vermouth.RequestIDMiddleware(
		slog.New(slog.DiscardHandler),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- vermouth.RequestID(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	requestID := <-received
	_, err := uuid.Parse(requestID)
	require.NoError(t, err)
	require.Equal(t, requestID, recorder.Header().Get(vermouth.RequestIDHeader))
}
