package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// covers: AC-17, AC-18
func TestProductionExportManifestAndArchiveValidation(t *testing.T) {
	t.Parallel()

	_, lookupErr := exec.LookPath("zstd")
	if lookupErr != nil {
		t.Skip("zstd is required for the production archive test")
	}
	tree := t.TempDir()
	for _, root := range productionExportRoots() {
		if filepath.Ext(root) == "" {
			root += "/marker"
		}
		contents := "archive fixture\n"
		if root == "storage/postgres-identity/marker" {
			contents = "database marker\n"
		}
		writeArchiveFixture(t, tree, root, contents)
	}
	metadataPath := filepath.Join(t.TempDir(), "metadata.json")
	metadata := productionExportMetadata{
		SchemaVersion: schemaVersion,
		CreatedAt:     "2026-09-04T01:02:03Z",
		Hostname:      "vermouth-test.southeastasia.cloudapp.azure.com",
		StorageIdentity: productionStorageIdentity{
			VMName:      "nguyenducloc-vm2",
			NodeName:    "nguyenducloc-vm2",
			StorageMode: "dedicated",
			StorageRoot: "/var/lib/vermouth",
			StorageUUID: new("37f07a74-8426-45fd-8e55-79515376a136"),
			SelectedAt:  "2026-09-04T00:00:00Z",
			MarkerSHA:   strings.Repeat("a", 64),
		},
		CurrentRelease: productionArchiveReleaseFixture("a", 3),
		GooseVersions:  productionGooseVersions{Identity: 4, Teaching: 4, Billing: 3, Notifications: 3},
		Roots:          productionExportRoots(),
	}
	metadataBytes, err := json.Marshal(metadata) //nolint:errchkjson // This fixed struct contains only supported JSON field types.
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(metadataPath, append(metadataBytes, '\n'), 0o600))

	var manifest bytes.Buffer
	require.NoError(t, writeProductionExportManifest(metadataPath, tree, &manifest))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "manifest.json"), manifest.Bytes(), 0o600))
	archive := buildProductionArchive(t, tree)
	archiveFile, err := os.Open(archive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, archiveFile.Close()) })

	var output bytes.Buffer
	require.NoError(t, validateProductionArchive(archiveFile, &output))
	archiveBytes, err := os.ReadFile(archive)
	require.NoError(t, err)
	digest := sha256.Sum256(archiveBytes)
	require.Equal(t, hex.EncodeToString(digest[:])+"\n", output.String())

	require.NoError(t, os.WriteFile(
		filepath.Join(tree, "storage", "postgres-identity", "marker"),
		[]byte("changed marker!\n"),
		0o600,
	))
	tampered := buildProductionArchive(t, tree)
	tamperedFile, err := os.Open(tampered)
	require.NoError(t, err)
	err = validateProductionArchive(tamperedFile, io.Discard)
	require.ErrorContains(t, err, `production archive file "storage/postgres-identity/marker" does not match its manifest`)
	require.NoError(t, tamperedFile.Close())
}

// covers: AC-17, AC-18
func TestProductionArchiveValidationRejectsTraversal(t *testing.T) {
	t.Parallel()

	_, lookupErr := exec.LookPath("zstd")
	if lookupErr != nil {
		t.Skip("zstd is required for the production archive test")
	}
	raw := filepath.Join(t.TempDir(), "unsafe.tar")
	file, err := os.Create(raw)
	require.NoError(t, err)
	writer := tar.NewWriter(file)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name:     "../escape",
		Mode:     0o600,
		Size:     1,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatUSTAR,
	}))
	_, err = writer.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
	archive := compressProductionArchive(t, raw)
	archiveFile, err := os.Open(archive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, archiveFile.Close()) })

	err = validateProductionArchive(archiveFile, io.Discard)
	require.ErrorContains(t, err, "is unsafe")
}

func productionArchiveReleaseFixture(revisionCharacter string, helmRevision int64) productionExportRelease {
	digests := make(map[string]string, len(productionImageWorkloads()))
	for _, workload := range productionImageWorkloads() {
		digests[workload] = "sha256:" + strings.Repeat("b", 64)
	}
	hash := strings.Repeat("c", 64)
	return productionExportRelease{
		Identity:                      strings.Repeat(revisionCharacter, 40) + "/123-1",
		HelmRevision:                  helmRevision,
		ImagesJSONSHA256:              hash,
		DeploymentBaseSHA256:          hash,
		MigrationCompatEvidenceSHA256: hash,
		RateLimitEvidenceSHA256:       hash,
		BundleManifestSHA256:          hash,
		ImageDigests:                  digests,
	}
}

func writeArchiveFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func buildProductionArchive(t *testing.T, root string) string {
	t.Helper()
	raw := filepath.Join(t.TempDir(), "export.tar")
	file, err := os.Create(raw)
	require.NoError(t, err)
	writer := tar.NewWriter(file)
	entries := []string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		entries = append(entries, filepath.ToSlash(relative))
		return nil
	}))
	slices.Sort(entries)
	for _, name := range entries {
		path := filepath.Join(root, filepath.FromSlash(name))
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		header, headerErr := tar.FileInfoHeader(info, "")
		require.NoError(t, headerErr)
		header.Name = name
		header.Format = tar.FormatUSTAR
		header.ModTime = header.ModTime.Truncate(time.Second)
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		require.NoError(t, writer.WriteHeader(header))
		if info.IsDir() {
			continue
		}
		entryFile, openErr := os.Open(path)
		require.NoError(t, openErr)
		_, copyErr := io.Copy(writer, entryFile)
		require.NoError(t, copyErr)
		require.NoError(t, entryFile.Close())
	}
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
	return compressProductionArchive(t, raw)
}

func compressProductionArchive(t *testing.T, raw string) string {
	t.Helper()
	archive := raw + ".zst"
	output, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "zstd", "-q", "-c", raw)
	command.Stdout = output
	var stderr bytes.Buffer
	command.Stderr = &stderr
	require.NoError(t, command.Run(), stderr.String())
	require.NoError(t, output.Close())
	return archive
}
