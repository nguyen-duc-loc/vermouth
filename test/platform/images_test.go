package platform_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-7, AC-8
func TestBuildPlansExposeOnlyDeployableArtifacts(t *testing.T) {
	t.Parallel()

	sources := []string{
		repoFile(t, "deploy", "platform", "common.sh"),
		repoFile(t, "deploy", "platform", "images.sh"),
	}
	tests := []struct {
		name      string
		body      string
		overrides map[string]string
		want      string
		wantError string
	}{
		{
			name: "native plan contains every deployable artifact",
			body: "build_plan all\n",
			want: "gateway\nidentity\nteaching\nbilling\nnotifications\nweb\nidentity-migration\nteaching-migration\nbilling-migration\nnotifications-migration\ngarage-init\ndevtoken\n",
		},
		{
			name:      "database service includes its migration",
			body:      "build_plan \"$TARGET\"\n",
			overrides: map[string]string{"TARGET": "identity"},
			want:      "identity\nidentity-migration\n",
		},
		{
			name:      "gateway stays a single image",
			body:      "build_plan \"$TARGET\"\n",
			overrides: map[string]string{"TARGET": "gateway"},
			want:      "gateway\n",
		},
		{
			name: "multiple architecture plan excludes development token",
			body: "multi_build_plan\n",
			want: "gateway\nidentity\nteaching\nbilling\nnotifications\nweb\nidentity-migration\nteaching-migration\nbilling-migration\nnotifications-migration\ngarage-init\n",
		},
		{
			name:      "unknown redeploy target",
			body:      "build_plan \"$TARGET\"\n",
			overrides: map[string]string{"TARGET": "database"},
			wantError: "redeploy accepts",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := runSourcedShell(t, sources, test.body, "", test.overrides)
			if test.wantError != "" {
				require.Error(t, result.err)
				require.Contains(t, result.stderr, test.wantError)
				return
			}
			require.NoError(t, result.err, result.stderr)
			require.Equal(t, test.want, result.stdout)
		})
	}
}

// covers: AC-2, AC-7, AC-11
func TestAppendImageRecordWritesContentAddressedReferences(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	recordsPath := filepath.Join(directory, "images.json")
	require.NoError(t, os.WriteFile(
		recordsPath,
		[]byte(`{"schema_version":1,"source_revision":"","dirty":false,"built_at":"","images":{}}`),
		0o600,
	))
	digest := "sha256:" + strings.Repeat("a", 64)
	plan := `{"workload":"gateway","repository_path":"vermouth/gateway","platforms":["linux/arm64"],"input_hash":"input-hash","tag":"dev-inputhash"}`
	result := runSourcedShell(
		t,
		[]string{
			repoFile(t, "deploy", "platform", "common.sh"),
			repoFile(t, "deploy", "platform", "images.sh"),
		},
		"append_image_record \"$RECORDS\" \"$PLAN\" \"$DIGEST\" true revision-1 2026-08-27T00:00:00Z\n",
		"",
		map[string]string{
			"RECORDS": recordsPath,
			"PLAN":    plan,
			"DIGEST":  digest,
		},
	)
	require.NoError(t, result.err, result.stderr)

	content, err := os.ReadFile(recordsPath)
	require.NoError(t, err)
	var state struct {
		SourceRevision string `json:"source_revision"`
		BuiltAt        string `json:"built_at"`
		Images         map[string]struct {
			HostRef        string   `json:"host_ref"`
			ClusterRef     string   `json:"cluster_ref"`
			ManifestDigest string   `json:"manifest_digest"`
			Platforms      []string `json:"platforms"`
			Dirty          bool     `json:"dirty"`
		} `json:"images"`
	}
	require.NoError(t, json.Unmarshal(content, &state))
	require.Equal(t, "revision-1", state.SourceRevision)
	require.Equal(t, "2026-08-27T00:00:00Z", state.BuiltAt)
	image := state.Images["gateway"]
	require.Equal(t, "127.0.0.1:5111/vermouth/gateway:dev-inputhash", image.HostRef)
	require.Equal(t, "vermouth-registry:5000/vermouth/gateway@"+digest, image.ClusterRef)
	require.Equal(t, digest, image.ManifestDigest)
	require.Equal(t, []string{"linux/arm64"}, image.Platforms)
	require.True(t, image.Dirty)
}

// covers: AC-8, AC-11
func TestPushWithRetryUsesTheBoundedDelayPlan(t *testing.T) {
	t.Parallel()

	sources := []string{
		repoFile(t, "deploy", "platform", "common.sh"),
		repoFile(t, "deploy", "platform", "images.sh"),
	}
	tests := []struct {
		name      string
		succeedOn string
		wantError bool
	}{
		{name: "third push succeeds", succeedOn: "3"},
		{name: "three failed pushes stop", succeedOn: "4", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			attemptPath := filepath.Join(directory, "attempt")
			logPath := filepath.Join(directory, "calls")
			require.NoError(t, os.WriteFile(attemptPath, []byte("0\n"), 0o600))
			body := `
docker_platform() {
  attempt=$(cat "$ATTEMPT_FILE")
  attempt=$((attempt + 1))
  printf '%s\n' "$attempt" >"$ATTEMPT_FILE"
  printf 'push %s\n' "$*" >>"$CALL_LOG"
  [ "$attempt" -ge "$SUCCEED_ON" ]
}
sleep() {
  printf 'sleep %s\n' "$1" >>"$CALL_LOG"
}
push_with_retry 127.0.0.1:5111/vermouth/gateway:test
`
			result := runSourcedShell(t, sources, body, "", map[string]string{
				"ATTEMPT_FILE": attemptPath,
				"CALL_LOG":     logPath,
				"SUCCEED_ON":   test.succeedOn,
			})
			if test.wantError {
				require.Error(t, result.err)
			} else {
				require.NoError(t, result.err, result.stderr)
			}

			attempts, err := os.ReadFile(attemptPath)
			require.NoError(t, err)
			require.Equal(t, "3", strings.TrimSpace(string(attempts)))
			calls, err := os.ReadFile(logPath)
			require.NoError(t, err)
			require.Contains(t, string(calls), "sleep 2\n")
			require.Contains(t, string(calls), "sleep 5\n")
		})
	}
}
