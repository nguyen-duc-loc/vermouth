package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-4, AC-20
func TestWriteProductionImagePlanBuildsCanonicalIdentity(t *testing.T) {
	t.Parallel()

	root, revision := newProductionImageRepository(t)
	var output bytes.Buffer
	require.NoError(t, writeProductionImagePlan(root, "gateway", revision, "example", &output))

	var plan map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &plan))
	require.Equal(t, "docker.io/example/vermouth-gateway", plan["repository"])
	require.Equal(t, revision, plan["source_revision"])
	require.Equal(t, []any{"linux/amd64", "linux/arm64"}, plan["platforms"])
	hash, ok := plan["build_input_sha256"].(string)
	require.True(t, ok)
	require.Regexp(t, `^[0-9a-f]{64}$`, hash)
	require.Equal(t, "git-"+revision[:12]+"-"+hash, plan["tag"])
}

// covers: AC-4
func TestWriteProductionImagePlanRejectsDirtyInputs(t *testing.T) {
	t.Parallel()

	root, revision := newProductionImageRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "main.go"), []byte("changed\n"), 0o600))

	err := writeProductionImagePlan(root, "gateway", revision, "example", &bytes.Buffer{})
	require.ErrorContains(t, err, "Git inputs are dirty")
}

// covers: AC-4, AC-20
func TestWriteProductionImagesDocumentRequiresTheExactInventory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	metadataPath := filepath.Join(directory, "metadata.json")
	recordsPath := filepath.Join(directory, "records.jsonl")
	revision := strings.Repeat("a", 40)
	hash := strings.Repeat("b", 64)
	digest := "sha256:" + strings.Repeat("c", 64)
	require.NoError(t, os.WriteFile(metadataPath, []byte(`{
  "git_sha":"`+revision+`",
  "github_run_id":"123",
  "github_run_attempt":"2",
  "generated_at":"2026-09-03T02:03:04Z",
  "chart_sha256":"`+hash+`"
}`), 0o600))

	var records strings.Builder
	for _, workload := range productionImageWorkloads() {
		record := verifiedProductionImage{
			Workload:         workload,
			Repository:       "docker.io/example/vermouth-" + workload,
			Tag:              "git-" + revision[:12] + "-" + hash,
			Digest:           digest,
			Platforms:        []string{platformAMD64, platformARM64},
			BuildInputSHA256: hash,
			SourceRevision:   revision,
			CreatedAt:        "2026-09-03T01:02:03Z",
		}
		encoded, _ := json.Marshal(record)
		records.Write(encoded)
		records.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(recordsPath, []byte(records.String()), 0o600))

	var output bytes.Buffer
	require.NoError(t, writeProductionImagesDocument(metadataPath, recordsPath, &output))
	require.True(t, strings.HasSuffix(output.String(), "\n"))
	require.NotContains(t, output.String(), " ")
	require.Contains(t, output.String(), `"schema_version":1`)
	require.Contains(t, output.String(), `"github_run_attempt":"2"`)

	require.NoError(t, os.WriteFile(recordsPath, []byte(strings.SplitN(records.String(), "\n", 2)[1]), 0o600))
	err := writeProductionImagesDocument(metadataPath, recordsPath, &bytes.Buffer{})
	require.ErrorContains(t, err, `production image record "billing" is missing`)
}

// covers: AC-4, AC-14, AC-20
func TestWriteTreeSHA256RejectsDirtyTrees(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tree := filepath.Join(root, "chart")
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "templates"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "Chart.yaml"), []byte("name: test\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "templates", "app.yaml"), []byte("kind: Service\n"), 0o640))
	revision := initializeGitRepository(t, root)
	require.NotEmpty(t, revision)

	var first bytes.Buffer
	require.NoError(t, writeTreeSHA256(tree, &first))
	require.Regexp(t, `^[0-9a-f]{64}\n$`, first.String())

	require.NoError(t, os.WriteFile(filepath.Join(tree, "Chart.yaml"), []byte("name: changed\n"), 0o600))
	err := writeTreeSHA256(tree, &bytes.Buffer{})
	require.ErrorContains(t, err, "Git inputs are dirty")
}

// covers: AC-13
func TestWriteProductionMigrationSetSHA256IsStableAndRejectsDirtyInputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, service := range productionMigrationServices() {
		directory := filepath.Join(root, "services", service, "db", "migrations")
		require.NoError(t, os.MkdirAll(directory, 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(directory, "00001_base.sql"),
			[]byte("-- +goose Up\nSELECT 1;\n"),
			0o600,
		))
	}
	initializeGitRepository(t, root)

	var first bytes.Buffer
	var second bytes.Buffer
	require.NoError(t, writeProductionMigrationSetSHA256(root, &first))
	require.NoError(t, writeProductionMigrationSetSHA256(root, &second))
	require.Equal(t, first.String(), second.String())
	require.Regexp(t, `^[0-9a-f]{64}\n$`, first.String())

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "services", "identity", "db", "migrations", "00001_base.sql"),
		[]byte("-- +goose Up\nSELECT 2;\n"),
		0o600,
	))
	err := writeProductionMigrationSetSHA256(root, &bytes.Buffer{})
	require.ErrorContains(t, err, "Git inputs are dirty")
}

func newProductionImageRepository(t *testing.T) (root string, revision string) {
	t.Helper()

	root = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "deploy"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".dockerignore"), []byte(".git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "main.go"), []byte("package main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "deploy", "images.lock.yaml"), []byte(`
schemaVersion: 1
external: {}
built:
  gateway:
    workload: gateway
    repositoryPath: vermouth/gateway
    dockerfile: Dockerfile
    target: runtime
    nativePlatforms: [linux/arm64]
    multiPlatforms: [linux/arm64, linux/amd64]
    inputs: [app]
    buildArgs: {}
    baseLocks: []
    hashSchema: vermouth-image-v1
    nativeTagTemplate: dev-{hash12}
    multiTagTemplate: multi-{hash12}
    childTagTemplate: multi-{hash12}-{architecture}
`), 0o600))
	return root, initializeGitRepository(t, root)
}

func initializeGitRepository(t *testing.T, root string) string {
	t.Helper()

	runGit(t, root, "init", "--quiet")
	runGit(t, root, "add", ".")
	runGit(
		t,
		root,
		"-c",
		"user.name=Platform Test",
		"-c",
		"user.email=platform@example.invalid",
		"commit",
		"--quiet",
		"-m",
		"test fixture",
	)
	return strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
}

func runGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()

	commandArguments := append([]string{"-C", root}, arguments...)
	command := exec.CommandContext(t.Context(), "git", commandArguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}
