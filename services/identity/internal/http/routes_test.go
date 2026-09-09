//nolint:testpackage // Cookie helpers and route closures are deliberate white box boundary tests.
package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/handler"
)

// covers: AC-3, AC-6, AC-15
func TestSessionMutationRoutesRejectMissingOrForeignOriginsBeforeTheHandler(t *testing.T) {
	t.Parallel()

	deps := Deps{
		Logger: slog.New(slog.DiscardHandler),
		Auth:   handler.AuthConfig{AppURL: "https://app.example"},
	}
	tests := []struct {
		name   string
		origin string
		build  func(Deps) http.HandlerFunc
	}{
		{name: "refresh missing origin", build: refreshSession},
		{name: "refresh foreign origin", origin: "https://evil.example", build: refreshSession},
		{name: "sign out missing origin", build: signOut},
		{name: "sign out foreign origin", origin: "https://evil.example", build: signOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/session", http.NoBody)
			request.Header.Set("Origin", test.origin)
			recorder := httptest.NewRecorder()

			test.build(deps).ServeHTTP(recorder, request)

			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.JSONEq(t, `{
				"error": {
					"code": "forbidden",
					"message": "this origin may not change a session",
					"request_id": ""
				}
			}`, recorder.Body.String())
			require.Empty(t, recorder.Header().Values("Set-Cookie"))
		})
	}
}

// covers: AC-3, AC-6, AC-8, AC-16
func TestSessionCookiesUseMatchingSecurityAttributesWhenSetAndCleared(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(time.Hour).UTC()
	tests := []struct {
		name       string
		write      func(http.ResponseWriter)
		cookieName string
		path       string
		value      string
		cleared    bool
	}{
		{
			name: "refresh set",
			write: func(w http.ResponseWriter) {
				setRefreshCookie(w, handler.IssuedRefreshToken{Value: "refresh-value", ExpiresAt: expiresAt}, true)
			},
			cookieName: refreshCookieName,
			path:       refreshCookiePath,
			value:      "refresh-value",
		},
		{
			name:       "refresh clear",
			write:      func(w http.ResponseWriter) { clearRefreshCookie(w, true) },
			cookieName: refreshCookieName,
			path:       refreshCookiePath,
			cleared:    true,
		},
		{
			name:       "login set",
			write:      func(w http.ResponseWriter) { setLoginCookie(w, "binding-value", expiresAt, true) },
			cookieName: loginCookieName,
			path:       loginCookiePath,
			value:      "binding-value",
		},
		{
			name:       "login clear",
			write:      func(w http.ResponseWriter) { clearLoginCookie(w, true) },
			cookieName: loginCookieName,
			path:       loginCookiePath,
			cleared:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()

			test.write(recorder)

			cookies := recorder.Result().Cookies()
			require.Len(t, cookies, 1)
			cookie := cookies[0]
			require.Equal(t, test.cookieName, cookie.Name)
			require.Equal(t, test.value, cookie.Value)
			require.Equal(t, test.path, cookie.Path)
			if test.cleared {
				require.Equal(t, -1, cookie.MaxAge)
			} else {
				require.Positive(t, cookie.MaxAge)
			}
			require.Empty(t, cookie.Domain)
			require.True(t, cookie.HttpOnly)
			require.True(t, cookie.Secure)
			require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		})
	}
}

// covers: AC-8, AC-16
func TestPresentedSessionCookiesNeverSubstituteAnotherCookie(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "refresh-value"})
	request.AddCookie(&http.Cookie{Name: loginCookieName, Value: "binding-value"})

	require.Equal(t, "refresh-value", presentedRefreshToken(request))
	require.Equal(t, "binding-value", presentedLoginBinding(request))
	require.Empty(t, presentedRefreshToken(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)))
	require.Empty(t, presentedLoginBinding(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)))
}

// covers: AC-1, AC-8
func TestGoogleRoutesFailClosedWhenTheProviderIsDisabled(t *testing.T) {
	t.Parallel()

	deps := Deps{
		Logger: slog.New(slog.DiscardHandler),
		Auth: handler.AuthConfig{
			AppURL:        "https://app.example",
			GoogleEnabled: false,
		},
	}
	tests := []struct {
		name             string
		path             string
		build            func(Deps) http.HandlerFunc
		wantClearedLogin bool
	}{
		{name: "start", path: "/auth/google/start", build: startSignIn},
		{
			name:             "callback",
			path:             "/auth/google/callback",
			build:            completeSignIn,
			wantClearedLogin: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, http.NoBody)
			recorder := httptest.NewRecorder()

			test.build(deps).ServeHTTP(recorder, request)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.JSONEq(t, `{
				"error": {
					"code": "auth_unavailable",
					"message": "Google sign in is not configured for this local environment",
					"request_id": ""
				}
			}`, recorder.Body.String())
			cookies := recorder.Result().Cookies()
			if test.wantClearedLogin {
				require.Len(t, cookies, 1)
				require.Equal(t, loginCookieName, cookies[0].Name)
				require.Equal(t, -1, cookies[0].MaxAge)
				return
			}
			require.Empty(t, cookies)
		})
	}
}

// covers: AC-15, AC-16
func TestRedirectToSignInUsesOnlyTheConfiguredAppURLAndKnownCode(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "https://identity.example/auth/google/callback", http.NoBody,
	)
	recorder := httptest.NewRecorder()

	redirectToSignIn(recorder, request, "https://app.example", handler.SignInExpiredState)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, "https://app.example/signin?error=expired_state", recorder.Header().Get("Location"))
}
