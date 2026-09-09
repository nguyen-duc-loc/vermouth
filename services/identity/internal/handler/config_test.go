//nolint:testpackage // These white box tests exercise strict unexported configuration validation.
package handler

import (
	urlpkg "net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// covers: AC-15
func TestValidateAppOrigin_AcceptsOnlySecureOriginShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "production HTTPS", raw: "https://app.example", want: "https://app.example"},
		{name: "localhost HTTP", raw: "http://localhost:5173", want: "http://localhost:5173"},
		{name: "local Kubernetes HTTP", raw: "http://vermouth.localhost:8080", want: "http://vermouth.localhost:8080"},
		{name: "external HTTP", raw: "http://app.example", wantErr: errSecureOrigin},
		{name: "localhost lookalike", raw: "http://localhost.example", wantErr: errSecureOrigin},
		{name: "path", raw: "https://app.example/path", wantErr: errAppOriginShape},
		{name: "query", raw: "https://app.example?next=/", wantErr: errAppOriginShape},
		{name: "fragment", raw: "https://app.example#next", wantErr: errAppOriginShape},
		{name: "user info", raw: "https://user@app.example", wantErr: errAppOriginShape},
		{name: "missing scheme", raw: "app.example", wantErr: errAppOriginShape},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateAppOrigin(test.raw)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got.String())
		})
	}
}

// covers: AC-15
func TestValidateGoogleRedirect_RequiresTheConfiguredAppOriginAndExactPath(t *testing.T) {
	t.Parallel()

	production, err := urlpkg.Parse("https://app.example:8443")
	require.NoError(t, err)
	local, err := urlpkg.Parse("http://localhost:5173")
	require.NoError(t, err)
	localKubernetes, err := urlpkg.Parse("http://vermouth.localhost:5173")
	require.NoError(t, err)

	tests := []struct {
		name    string
		raw     string
		app     *urlpkg.URL
		wantErr error
	}{
		{name: "production match", raw: "https://app.example:8443/api/auth/google/callback", app: production},
		{name: "localhost ports may differ", raw: "http://localhost:8081/api/auth/google/callback", app: local},
		{name: "local Kubernetes ports may differ", raw: "http://vermouth.localhost:8080/api/auth/google/callback", app: localKubernetes},
		{name: "local Kubernetes hostname must match", raw: "http://other.localhost:8080/api/auth/google/callback", app: localKubernetes, wantErr: errCallbackOrigin},
		{name: "wrong path", raw: "https://app.example:8443/callback", app: production, wantErr: errCallbackShape},
		{name: "query", raw: "https://app.example:8443/api/auth/google/callback?x=1", app: production, wantErr: errCallbackShape},
		{name: "fragment", raw: "https://app.example:8443/api/auth/google/callback#x", app: production, wantErr: errCallbackShape},
		{name: "user info", raw: "https://user@app.example:8443/api/auth/google/callback", app: production, wantErr: errCallbackShape},
		{name: "external HTTP", raw: "http://app.example:8443/api/auth/google/callback", app: production, wantErr: errSecureOrigin},
		{name: "wrong hostname", raw: "https://other.example:8443/api/auth/google/callback", app: production, wantErr: errCallbackOrigin},
		{name: "wrong production port", raw: "https://app.example:9443/api/auth/google/callback", app: production, wantErr: errCallbackExternalPort},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateGoogleRedirect(test.raw, test.app)
			if test.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

// covers: AC-15
func TestIsLocalhostNameAcceptsOnlyTheLoopbackNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "localhost", raw: "localhost", want: true},
		{name: "mixed case localhost", raw: "LocalHost", want: true},
		{name: "local Kubernetes host", raw: "vermouth.localhost", want: true},
		{name: "nested local host", raw: "app.dev.localhost", want: true},
		{name: "empty label", raw: ".localhost", want: false},
		{name: "lookalike suffix", raw: "evillocalhost", want: false},
		{name: "external suffix", raw: "localhost.example", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, isLocalhostName(test.raw))
		})
	}
}

// covers: AC-3, AC-15
func TestAuthConfig_AllowsOnlyTheExactBrowserOrigin(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{AppURL: "https://App.Example:8443"}
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "exact origin", raw: "https://app.example:8443", want: true},
		{name: "case insensitive host", raw: "HTTPS://APP.EXAMPLE:8443", want: true},
		{name: "missing", raw: "", want: false},
		{name: "null", raw: "null", want: false},
		{name: "foreign host", raw: "https://evil.example:8443", want: false},
		{name: "foreign port", raw: "https://app.example:9443", want: false},
		{name: "path", raw: "https://app.example:8443/path", want: false},
		{name: "query", raw: "https://app.example:8443?x=1", want: false},
		{name: "user info", raw: "https://user@app.example:8443", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, cfg.AllowsOrigin(test.raw))
		})
	}
}

// covers: AC-15
func TestAuthConfig_ResolveRedirectNeverLeavesTheAppOrigin(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{AppURL: "https://app.example"}
	require.Equal(t, "https://app.example/thread?day=1", cfg.ResolveRedirect("/thread?day=1"))
	require.Equal(t, "https://app.example/", cfg.ResolveRedirect("//evil.example"))
	require.Equal(t, "https://app.example/", cfg.ResolveRedirect("https://evil.example"))
}

// covers: AC-10, AC-15
func TestAuthConfigFromEnv_ParsesTheCompleteGoogleConfiguration(t *testing.T) {
	t.Setenv(envGoogleEnabled, "true")
	t.Setenv(envGoogleClientID, "client")
	t.Setenv(envGoogleSecret, "secret")
	t.Setenv(envGoogleRedirect, "http://localhost:8081/api/auth/google/callback")
	t.Setenv(envAppURL, "http://localhost:5173")
	t.Setenv(envSignupAllowlist, " Tutor@Example.com,second@example.com ")
	t.Setenv(envRefreshTTL, "24h")
	t.Setenv(envRefreshGrace, "3s")
	t.Setenv(envSweepInterval, "1m")
	t.Setenv(envCookieSecure, "false")

	cfg, err := AuthConfigFromEnv()

	require.NoError(t, err)
	require.True(t, cfg.GoogleEnabled)
	require.Equal(t, map[string]bool{
		"tutor@example.com":  true,
		"second@example.com": true,
	}, cfg.SignupAllowlist)
	require.Equal(t, 24*time.Hour, cfg.RefreshTTL)
	require.Equal(t, 3*time.Second, cfg.RefreshGrace)
	require.Equal(t, time.Minute, cfg.SweepInterval)
	require.False(t, cfg.CookieSecure)
}

// covers: AC-13
func TestAuthConfigFromEnv_DisabledGoogleNeedsNoProviderSecret(t *testing.T) {
	t.Setenv(envGoogleEnabled, "false")
	t.Setenv(envGoogleClientID, "")
	t.Setenv(envGoogleSecret, "")
	t.Setenv(envGoogleRedirect, "")
	t.Setenv(envAppURL, "http://localhost:5173")
	t.Setenv(envSignupAllowlist, "")
	t.Setenv(envRefreshTTL, "")
	t.Setenv(envRefreshGrace, "")
	t.Setenv(envSweepInterval, "")
	t.Setenv(envCookieSecure, "")

	cfg, err := AuthConfigFromEnv()

	require.NoError(t, err)
	require.False(t, cfg.GoogleEnabled)
}

// covers: AC-5
func TestParseAllowlistRejectsAnEntryThatIsNotAnEmailAddress(t *testing.T) {
	t.Parallel()

	allowlist, err := parseAllowlist("tutor@example.com,not-an-address")

	require.Error(t, err)
	require.Nil(t, allowlist)
}

// covers: AC-3, AC-7
func TestOptionalAuthValuesRejectMalformedConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		read  func(string) error
	}{
		{
			name:  "boolean",
			value: "sometimes",
			read: func(name string) error {
				_, err := envBool(name, true)
				return err
			},
		},
		{
			name:  "duration",
			value: "tomorrow",
			read: func(name string) error {
				_, err := envDuration(name, time.Minute)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const name = "VERMOUTH_AUTH_TEST_VALUE"
			t.Setenv(name, test.value)

			require.Error(t, test.read(name))
		})
	}
}
