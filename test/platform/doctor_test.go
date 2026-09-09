package platform_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-17
func TestDoctorChecksEveryRequiredToolBeforePlatformState(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	callLog := filepath.Join(directory, "calls")
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "doctor.sh"),
		},
		`need() {
  printf '%s\n' "$1" >>"$CALL_LOG"
  [ "$1" != rg ] || fail "rg is required"
}
validate_platform_inputs() { printf 'validate\n' >>"$CALL_LOG"; }
doctor
`,
		"",
		map[string]string{"CALL_LOG": callLog},
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "rg is required")
	calls, err := os.ReadFile(callLog)
	require.NoError(t, err)
	require.Equal(t,
		"colima\ndocker\nk3d\nkubectl\nhelm\ntask\ngo\nnode\ncorepack\njq\nopenssl\nshasum\ncurl\nlsof\nrg\n",
		string(calls),
	)
	require.NotContains(t, string(calls), "validate")
}

// covers: AC-1
func TestDoctorRejectsUnsupportedHostsBeforeVersionChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		system    string
		machine   string
		wantError string
	}{
		{name: "non macOS host", system: "Linux", machine: "arm64", wantError: "supports macOS"},
		{name: "non Apple silicon host", system: "Darwin", machine: "x86_64", wantError: "supports Apple silicon"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := runSourcedShell(
				t,
				[]string{
					repoFile(t, "deploy", "platform", "common.sh"),
					repoFile(t, "deploy", "platform", "doctor.sh"),
				},
				`need() { :; }
validate_platform_inputs() { :; }
uname() {
  case "$1" in
    -s) printf '%s\n' "$TEST_SYSTEM" ;;
    -m) printf '%s\n' "$TEST_MACHINE" ;;
  esac
}
doctor
`,
				"",
				map[string]string{
					"TEST_SYSTEM":  test.system,
					"TEST_MACHINE": test.machine,
				},
			)

			require.Error(t, result.err)
			require.Contains(t, result.stderr, test.wantError)
		})
	}
}
