package aggregate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
)

// covers: AC-1, AC-3, AC-6, AC-8, AC-15
func TestForwardPreservesTheBrowserAuthBoundaryAndReturnsRedirects(t *testing.T) {
	t.Parallel()

	type receivedRequest struct {
		method        string
		path          string
		query         string
		cookie        string
		origin        string
		requestID     string
		authorization string
	}
	received := make(chan receivedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- receivedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			query:         r.URL.RawQuery,
			cookie:        r.Header.Get("Cookie"),
			origin:        r.Header.Get("Origin"),
			requestID:     r.Header.Get(vermouth.RequestIDHeader),
			authorization: r.Header.Get("Authorization"),
		}
		w.Header().Add("Set-Cookie", "vermouth_refresh=next; Path=/api/auth; HttpOnly")
		w.Header().Set("Location", "https://accounts.google.com/authorize")
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("redirected by identity"))
	}))
	t.Cleanup(upstream.Close)

	client := aggregate.NewClient(aggregate.Upstreams{})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://gateway.example/a/caller/chosen/path?state=state-1&code=code-1",
		http.NoBody,
	)
	request.Header.Set("Cookie", "vermouth_login=binding-1")
	request.Header.Set("Origin", "https://app.example")
	request.Header.Set("Authorization", "Bearer must-not-cross-this-route")
	ctx := vermouth.WithRequestID(request.Context(), "request-1")

	response, err := client.Forward(
		ctx,
		request,
		upstream.URL,
		"/auth/google/callback",
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.Status)
	require.Equal(t, "https://accounts.google.com/authorize", response.Header.Get("Location"))
	require.Equal(t, "vermouth_refresh=next; Path=/api/auth; HttpOnly", response.Header.Get("Set-Cookie"))
	require.Equal(t, []byte("redirected by identity"), response.Body)
	require.Equal(t, receivedRequest{
		method:    http.MethodGet,
		path:      "/auth/google/callback",
		query:     "state=state-1&code=code-1",
		cookie:    "vermouth_login=binding-1",
		origin:    "https://app.example",
		requestID: "request-1",
	}, <-received)
}

// covers: AC-15
func TestForwardRejectsAnInvalidUpstreamAddress(t *testing.T) {
	t.Parallel()

	client := aggregate.NewClient(aggregate.Upstreams{})
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "https://gateway.example/api/auth/refresh", http.NoBody,
	)

	_, err := client.Forward(t.Context(), request, "://identity", "/auth/refresh")

	require.ErrorContains(t, err, "parse upstream address")
}
