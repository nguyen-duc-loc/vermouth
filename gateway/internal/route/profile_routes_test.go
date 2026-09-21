package route_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/route"
)

// covers: AC-1, AC-2, AC-3, AC-9, AC-14
func TestMux_ForwardsProfileResourcesOnlyToBilling(t *testing.T) {
	t.Parallel()

	verifier, token := gatewayTestToken(t)
	tests := []struct {
		name     string
		method   string
		path     string
		wantPath string
		body     string
		cache    string
	}{
		{name: "read profile", method: http.MethodGet, path: "/api/invoice-profile", wantPath: "/invoice-profile", cache: "no-store"},
		{
			name: "save profile", method: http.MethodPut, path: "/api/invoice-profile", wantPath: "/invoice-profile",
			body:  `{"expected_revision":0,"legal_name":"Nguyễn An","contact_line":null,"bank_code":null,"bank_account_number":null,"bank_account_holder":null}`,
			cache: "no-store",
		},
		{name: "read banks", method: http.MethodGet, path: "/api/banks", wantPath: "/banks", cache: "private, max-age=86400"},
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
					method: r.Method, path: r.URL.Path,
					authorization: r.Header.Get("Authorization"), body: string(body),
				}
				w.Header().Set("Cache-Control", test.cache)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"forwarded":true}`))
			}))
			t.Cleanup(upstream.Close)
			handler := route.Mux(route.Deps{
				Client:   aggregate.NewClient(aggregate.Upstreams{Billing: upstream.URL}),
				Verifier: verifier, Logger: slog.New(slog.DiscardHandler), Service: "gateway",
			})
			request := httptest.NewRequestWithContext(
				t.Context(), test.method, test.path, strings.NewReader(test.body),
			)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			received := <-forwarded
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, test.method, received.method)
			require.Equal(t, test.wantPath, received.path)
			require.Equal(t, "Bearer "+token, received.authorization)
			if test.body == "" {
				require.Empty(t, received.body)
			} else {
				require.JSONEq(t, test.body, received.body)
			}
			require.Equal(t, test.cache, recorder.Header().Get("Cache-Control"))
		})
	}
}

// covers: AC-6, AC-9
func TestMux_RejectsIncompleteProfileRepresentationsBeforeForwarding(t *testing.T) {
	t.Parallel()

	verifier, token := gatewayTestToken(t)
	handler := route.Mux(route.Deps{
		Client: aggregate.NewClient(aggregate.Upstreams{}), Verifier: verifier,
		Logger: slog.New(slog.DiscardHandler), Service: "gateway",
	})
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPut, "/api/invoice-profile",
		strings.NewReader(`{"expected_revision":0,"legal_name":null}`),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"invalid_input"`)
}
