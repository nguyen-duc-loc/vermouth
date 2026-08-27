package platform_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-12
func TestCheckPortFreeRejectsAnExistingListener(t *testing.T) {
	t.Parallel()

	sources := []string{
		repoFile(t, "deploy", "platform", "common.sh"),
		repoFile(t, "deploy", "platform", "lifecycle.sh"),
	}
	result := runSourcedShell(
		t,
		sources,
		"lsof() { printf 'listener\\n'; }\ncheck_port_free 8080\n",
		"",
		nil,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "local TCP port 8080 is already in use")
}

// covers: AC-1, AC-17
func TestRenderTraefikConfigCombinesCommittedValues(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	commonPath := filepath.Join(directory, "common.yaml")
	localPath := filepath.Join(directory, "local.yaml")
	outputPath := filepath.Join(directory, "rendered.yaml")
	require.NoError(t, os.WriteFile(commonPath, []byte("deployment:\n  replicas: 1\n"), 0o600))
	require.NoError(t, os.WriteFile(localPath, []byte("ports:\n  web:\n    port: 80\n"), 0o600))

	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "lifecycle.sh"),
		},
		"TRAEFIK_COMMON=$COMMON_FILE\nTRAEFIK_LOCAL=$LOCAL_FILE\nrender_traefik_config \"$OUTPUT_FILE\"\n",
		"",
		map[string]string{
			"COMMON_FILE": commonPath,
			"LOCAL_FILE":  localPath,
			"OUTPUT_FILE": outputPath,
		},
	)
	require.NoError(t, result.err, result.stderr)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "kind: HelmChartConfig")
	require.Contains(t, string(content), "    deployment:\n      replicas: 1\n")
	require.Contains(t, string(content), "    ports:\n      web:\n        port: 80\n")
}

// covers: AC-7, AC-11
func TestRedeployRejectsAMissingTargetBeforeMutation(t *testing.T) {
	t.Parallel()

	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "lifecycle.sh"),
		},
		"redeploy\n",
		"",
		nil,
	)

	require.Error(t, result.err)
	require.Contains(t, result.stderr, "name gateway, identity, teaching, billing, notifications, or web")
}

// covers: AC-11, AC-12
func TestCleanJobDeletesOnlyAFailedInactiveJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		active     string
		failed     string
		wantError  string
		wantDelete bool
	}{
		{name: "running Job", active: "1", failed: "1", wantError: "is still running"},
		{name: "successful Job", active: "0", wantError: "did not fail"},
		{name: "failed inactive Job", active: "0", failed: "1", wantDelete: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			callLog := filepath.Join(directory, "calls")
			result := runSourcedShell(
				t,
				[]string{
					repoFile(t, "deploy", "platform", "common.sh"),
					repoFile(t, "deploy", "platform", "lifecycle.sh"),
				},
				`require_context() { :; }
kube() {
  printf '%s\n' "$*" >>"$CALL_LOG"
  case "$*" in
    "get job job-1") return 0 ;;
    "get job job-1 -o jsonpath={.status.active}") printf '%s' "${ACTIVE:-}" ;;
    "get job job-1 -o jsonpath={.status.failed}") printf '%s' "${FAILED:-}" ;;
    "delete job job-1") return 0 ;;
  esac
}
clean_job job-1
`,
				"",
				map[string]string{
					"CALL_LOG": callLog,
					"ACTIVE":   test.active,
					"FAILED":   test.failed,
				},
			)
			if test.wantError == "" {
				require.NoError(t, result.err, result.stderr)
			} else {
				require.Error(t, result.err)
				require.Contains(t, result.stderr, test.wantError)
			}
			calls, err := os.ReadFile(callLog)
			require.NoError(t, err)
			if test.wantDelete {
				require.Contains(t, string(calls), "delete job job-1")
			} else {
				require.NotContains(t, string(calls), "delete job job-1")
			}
		})
	}
}

// covers: AC-11, AC-12
func TestRecoverPendingApplicationReleaseUsesTheLastGoodRevision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		history    string
		wantAction string
	}{
		{
			name:       "prior deployed revision",
			history:    `[{"revision":3,"status":"deployed"},{"revision":4,"status":"pending-upgrade"}]`,
			wantAction: "rollback vermouth 3",
		},
		{
			name:       "failed first install",
			history:    `[]`,
			wantAction: "uninstall vermouth",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			callLog := filepath.Join(directory, "calls")
			result := runSourcedShell(
				t,
				[]string{
					repoFile(t, "deploy", "platform", "common.sh"),
					repoFile(t, "deploy", "platform", "lifecycle.sh"),
				},
				`helm() {
  printf '%s\n' "$*" >>"$CALL_LOG"
  case "$*" in
    *"status vermouth -o json"*) printf '%s\n' '{"info":{"status":"pending-upgrade"}}' ;;
    *"history vermouth -o json"*) printf '%s\n' "$HISTORY" ;;
  esac
}
recover_pending_application_release
`,
				"",
				map[string]string{
					"CALL_LOG": callLog,
					"HISTORY":  test.history,
				},
			)
			require.NoError(t, result.err, result.stderr)
			calls, err := os.ReadFile(callLog)
			require.NoError(t, err)
			require.Contains(t, string(calls), test.wantAction)
		})
	}
}
