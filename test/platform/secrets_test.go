package platform_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-11, AC-14
func TestLocalGoogleConfigurationFailsBeforeMutation(t *testing.T) {
	t.Parallel()

	sources := []string{
		repoFile(t, "deploy", "platform", "common.sh"),
		repoFile(t, "deploy", "platform", "secrets.sh"),
	}
	tests := []struct {
		name      string
		content   string
		wantError string
	}{
		{name: "Google disabled", content: "VERMOUTH_GOOGLE_AUTH_ENABLED=false\n"},
		{
			name: "exact local callback",
			content: "VERMOUTH_GOOGLE_AUTH_ENABLED=true\n" +
				"IDENTITY_GOOGLE_CLIENT_ID=client\n" +
				"IDENTITY_GOOGLE_CLIENT_SECRET=secret\n" +
				"IDENTITY_GOOGLE_REDIRECT_URL=http://vermouth.localhost:8080/api/auth/google/callback\n",
		},
		{
			name: "wrong callback",
			content: "VERMOUTH_GOOGLE_AUTH_ENABLED=true\n" +
				"IDENTITY_GOOGLE_CLIENT_ID=client\n" +
				"IDENTITY_GOOGLE_CLIENT_SECRET=secret\n" +
				"IDENTITY_GOOGLE_REDIRECT_URL=http://localhost/api/auth/google/callback\n",
			wantError: "must equal http://vermouth.localhost:8080/api/auth/google/callback",
		},
		{
			name: "missing Google client id",
			content: "VERMOUTH_GOOGLE_AUTH_ENABLED=true\n" +
				"IDENTITY_GOOGLE_CLIENT_SECRET=secret\n" +
				"IDENTITY_GOOGLE_REDIRECT_URL=http://vermouth.localhost:8080/api/auth/google/callback\n",
			wantError: "IDENTITY_GOOGLE_CLIENT_ID is required",
		},
		{
			name: "missing Google client secret",
			content: "VERMOUTH_GOOGLE_AUTH_ENABLED=true\n" +
				"IDENTITY_GOOGLE_CLIENT_ID=client\n" +
				"IDENTITY_GOOGLE_REDIRECT_URL=http://vermouth.localhost:8080/api/auth/google/callback\n",
			wantError: "IDENTITY_GOOGLE_CLIENT_SECRET is required",
		},
		{
			name: "missing Google redirect URL",
			content: "VERMOUTH_GOOGLE_AUTH_ENABLED=true\n" +
				"IDENTITY_GOOGLE_CLIENT_ID=client\n" +
				"IDENTITY_GOOGLE_CLIENT_SECRET=secret\n",
			wantError: "IDENTITY_GOOGLE_REDIRECT_URL is required",
		},
		{
			name:      "invalid enable value",
			content:   "VERMOUTH_GOOGLE_AUTH_ENABLED=yes\n",
			wantError: "must be true or false",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte(test.content), 0o600))
			result := runSourcedShell(
				t,
				sources,
				"ROOT=$TEST_ROOT\nvalidate_local_configuration\n",
				"",
				map[string]string{"TEST_ROOT": root},
			)
			if test.wantError == "" {
				require.NoError(t, result.err, result.stderr)
				return
			}
			require.Error(t, result.err)
			require.Contains(t, result.stderr, test.wantError)
		})
	}
}

// covers: AC-11, AC-16
func TestStableSecretRejectsChangedSourceWithoutMutation(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "secrets.sh"),
		},
		`secret_hash() { printf '%s\n' new-hash; }
kube() {
  printf '%s\n' "$*" >>"$CALL_LOG"
  case "$*" in
    "get secret garage-bootstrap") return 0 ;;
    "get secret garage-bootstrap -o jsonpath={.metadata.annotations.vermouth\\.dev/source-sha256}") printf '%s' old-hash ;;
  esac
}
apply_stable_secret garage-bootstrap "$SECRET_FILE"
`,
		"",
		map[string]string{
			"CALL_LOG":    callLog,
			"SECRET_FILE": filepath.Join(directory, "garage.env"),
		},
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "changed from its stored foundation values")
	calls, err := os.ReadFile(callLog)
	require.NoError(t, err)
	require.NotContains(t, string(calls), "create secret")
	require.NotContains(t, string(calls), "apply -f")
}

func TestEnsureEnvValuePreservesExistingValuesAndFillsBlankOnes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("KEEP=original\nEMPTY=   \n"), 0o644))
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "secrets.sh"),
		},
		"ROOT=$TEST_ROOT\nensure_env_value KEEP replacement\nensure_env_value EMPTY filled\n",
		"",
		map[string]string{"TEST_ROOT": root},
	)
	require.NoError(t, result.err, result.stderr)

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Equal(t, "KEEP=original\nEMPTY=filled\n", string(content))
	info, err := os.Stat(envPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
