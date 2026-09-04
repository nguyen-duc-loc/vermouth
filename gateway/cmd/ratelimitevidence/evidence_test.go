//nolint:paralleltest // These tests intentionally replace process environment values.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/ratelimit"
)

// covers: AC-14
func TestWriteAndVerifyEvidence_UsesCanonicalCurrentInputs(t *testing.T) {
	root := newEvidenceRepository(t)
	setEvidenceEnvironment(t)
	path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
	now := time.Date(2026, time.September, 4, 8, 9, 10, 999, time.FixedZone("test", 7*60*60))

	require.NoError(t, writeEvidence(root, evidenceRelativePath, func() time.Time { return now }))
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	document, err := decodeEvidence(payload)
	require.NoError(t, err)
	require.Equal(t, 1, document.SchemaVersion)
	require.Equal(t, evidenceTestTarget, document.TestTarget)
	require.True(t, document.Pass)
	require.Equal(t, "2026-09-04T01:09:10Z", document.Timestamp)
	require.Equal(t, expectedThresholdHash(), document.ThresholdConfigurationSHA256)
	require.Regexp(t, gitSHAExpression, document.GitSHA)
	require.Equal(t, byte('\n'), payload[len(payload)-1])
	require.NoError(t, verifyEvidence(root, evidenceRelativePath))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

// covers: AC-14
func TestVerifyEvidence_TreatsTimestampAsMetadata(t *testing.T) {
	root := newEvidenceRepository(t)
	setEvidenceEnvironment(t)
	path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
	require.NoError(t, writeEvidence(root, evidenceRelativePath, time.Now))
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	document, err := decodeEvidence(payload)
	require.NoError(t, err)
	document.Timestamp = "2025-01-02T03:04:05Z"
	payload, err = canonicalEvidence(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, payload, 0o644))

	require.NoError(t, verifyEvidence(root, evidenceRelativePath))
}

// covers: AC-14
func TestWriteEvidence_RejectsDirtyNamedInputsAndUnsafeDestinations(t *testing.T) {
	setEvidenceEnvironment(t)

	t.Run("dirty input", func(t *testing.T) {
		root := newEvidenceRepository(t)
		openAPI := filepath.Join(root, "api", "openapi.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(openAPI), 0o755))
		require.NoError(t, os.WriteFile(openAPI, []byte("openapi: 3.1.0\n"), 0o644))
		runGit(t, root, "add", "api/openapi.yaml")
		runGit(t, root, "commit", "-m", "track api")
		require.NoError(t, os.WriteFile(openAPI, []byte("openapi: 3.1.1\n"), 0o644))

		err := writeEvidence(root, evidenceRelativePath, time.Now)

		require.ErrorIs(t, err, errDirtyInputs)
		_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(evidenceRelativePath)))
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("symlink destination", func(t *testing.T) {
		root := newEvidenceRepository(t)
		path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		target := filepath.Join(t.TempDir(), "outside.json")
		require.NoError(t, os.WriteFile(target, []byte("keep"), 0o644))
		require.NoError(t, os.Symlink(target, path))

		err := writeEvidence(root, evidenceRelativePath, time.Now)

		require.ErrorIs(t, err, errUnsafeDestination)
		content, readErr := os.ReadFile(target)
		require.NoError(t, readErr)
		require.Equal(t, "keep", string(content))
	})

	t.Run("nonregular destination", func(t *testing.T) {
		root := newEvidenceRepository(t)
		path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
		require.NoError(t, os.MkdirAll(path, 0o755))

		err := writeEvidence(root, evidenceRelativePath, time.Now)

		require.ErrorIs(t, err, errUnsafeDestination)
	})

	t.Run("wrong path", func(t *testing.T) {
		root := newEvidenceRepository(t)
		err := writeEvidence(root, "evidence.json", time.Now)
		require.ErrorIs(t, err, errWrongDestination)
	})
}

// covers: AC-14
func TestVerifyEvidence_RejectsChangedThresholdAndNoncanonicalBytes(t *testing.T) {
	root := newEvidenceRepository(t)
	setEvidenceEnvironment(t)
	path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
	require.NoError(t, writeEvidence(root, evidenceRelativePath, time.Now))

	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append([]byte(" "), payload...), 0o644))
	require.ErrorIs(t, verifyEvidence(root, evidenceRelativePath), errInvalidEvidence)

	require.NoError(t, os.WriteFile(path, payload, 0o644))
	t.Setenv(ratelimit.EnvStartIP, "6/1m,20/1h")
	require.ErrorIs(t, verifyEvidence(root, evidenceRelativePath), errInvalidEvidence)
}

// covers: AC-14
func TestVerifyEvidence_RejectsExtraFieldsAndStaleGitIdentity(t *testing.T) {
	setEvidenceEnvironment(t)

	t.Run("extra field", func(t *testing.T) {
		root := newEvidenceRepository(t)
		path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
		require.NoError(t, writeEvidence(root, evidenceRelativePath, time.Now))
		payload, err := os.ReadFile(path)
		require.NoError(t, err)
		payload = bytes.Replace(payload, []byte("}\n"), []byte(",\"extra\":true}\n"), 1)
		require.NoError(t, os.WriteFile(path, payload, 0o644))

		require.ErrorIs(t, verifyEvidence(root, evidenceRelativePath), errInvalidEvidence)
	})

	t.Run("stale Git identity", func(t *testing.T) {
		root := newEvidenceRepository(t)
		path := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
		require.NoError(t, writeEvidence(root, evidenceRelativePath, time.Now))
		payload, err := os.ReadFile(path)
		require.NoError(t, err)
		document, err := decodeEvidence(payload)
		require.NoError(t, err)
		document.GitSHA = strings.Repeat("0", 40)
		payload, err = canonicalEvidence(document)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, payload, 0o644))

		require.ErrorIs(t, verifyEvidence(root, evidenceRelativePath), errInvalidEvidence)
	})
}

func newEvidenceRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	runGit(t, root, "config", "user.name", "Evidence Test")
	runGit(t, root, "config", "user.email", "evidence@example.invalid")
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644))
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "--quiet", "-m", "initial")
	return root
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	command := exec.CommandContext(t.Context(), "git", commandArgs...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

func setEvidenceEnvironment(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		ratelimit.EnvTrustedProxyCIDRs: "none",
		ratelimit.EnvStartIP:           "5/1m,20/1h",
		ratelimit.EnvStartGlobal:       "100/1m,500/1h",
		ratelimit.EnvCallbackIP:        "10/1m,60/1h",
		ratelimit.EnvCallbackGlobal:    "200/1m,1000/1h",
		ratelimit.EnvRefreshIP:         "30/1m,120/1h",
		ratelimit.EnvRefreshToken:      "10/1m,120/1h",
		ratelimit.EnvRefreshGlobal:     "300/1m,3000/1h",
	} {
		t.Setenv(name, value)
	}
}

func expectedThresholdHash() string {
	canonical := "" +
		"GATEWAY_AUTH_RATE_CALLBACK_GLOBAL=200/1m,1000/1h\n" +
		"GATEWAY_AUTH_RATE_CALLBACK_IP=10/1m,60/1h\n" +
		"GATEWAY_AUTH_RATE_REFRESH_GLOBAL=300/1m,3000/1h\n" +
		"GATEWAY_AUTH_RATE_REFRESH_IP=30/1m,120/1h\n" +
		"GATEWAY_AUTH_RATE_REFRESH_TOKEN=10/1m,120/1h\n" +
		"GATEWAY_AUTH_RATE_START_GLOBAL=100/1m,500/1h\n" +
		"GATEWAY_AUTH_RATE_START_IP=5/1m,20/1h\n"
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}

// covers: AC-14
func TestRun_RejectsUnknownArguments(t *testing.T) {
	require.ErrorIs(t, run(nil), errUsage)
	require.ErrorIs(t, run([]string{"unknown", ".", evidenceRelativePath}), errUsage)
}
