//nolint:err113,noinlineerr // Archive validation reports the exact rejected field or path.
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	archivepath "path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	productionManifestMaximum = 8 * 1024 * 1024
	productionArchiveMaximum  = int64(128 * 1024 * 1024 * 1024)
	productionManifestName    = "manifest.json"
)

type productionGooseVersions struct {
	Identity      int64 `json:"identity"`
	Teaching      int64 `json:"teaching"`
	Billing       int64 `json:"billing"`
	Notifications int64 `json:"notifications"`
}

type productionStorageIdentity struct {
	VMName      string  `json:"vm_name"`
	NodeName    string  `json:"node_name"`
	StorageMode string  `json:"storage_mode"`
	StorageRoot string  `json:"storage_root"`
	StorageUUID *string `json:"storage_uuid"`
	SelectedAt  string  `json:"selected_at"`
	MarkerSHA   string  `json:"marker_sha256"`
}

type productionExportRelease struct {
	Identity                      string            `json:"identity"`
	HelmRevision                  int64             `json:"helm_revision"`
	ImagesJSONSHA256              string            `json:"images_json_sha256"`
	DeploymentBaseSHA256          string            `json:"deployment_base_sha256"`
	MigrationCompatEvidenceSHA256 string            `json:"migration_compat_evidence_sha256"`
	RateLimitEvidenceSHA256       string            `json:"rate_limit_evidence_sha256"`
	BundleManifestSHA256          string            `json:"bundle_manifest_sha256"`
	ImageDigests                  map[string]string `json:"image_digests"`
}

type productionExportFile struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type productionExportMetadata struct {
	SchemaVersion   int                       `json:"schema_version"`
	CreatedAt       string                    `json:"created_at"`
	Hostname        string                    `json:"hostname"`
	StorageIdentity productionStorageIdentity `json:"storage_identity"`
	CurrentRelease  productionExportRelease   `json:"current_release"`
	PreviousRelease *productionExportRelease  `json:"previous_release"`
	GooseVersions   productionGooseVersions   `json:"goose_versions"`
	Roots           []string                  `json:"roots"`
}

type productionExportManifest struct {
	productionExportMetadata

	RegularFileBytes int64                           `json:"regular_file_bytes"`
	Files            map[string]productionExportFile `json:"files"`
}

//nolint:funlen,gocognit // One walk enforces every path, type, root, size, and checksum invariant together.
func writeProductionExportManifest(metadataPath, treeRoot string, output io.Writer) error {
	metadata, err := strictJSONFile[productionExportMetadata](metadataPath)
	if err != nil {
		return fmt.Errorf("read production export metadata: %w", err)
	}
	if err := validateProductionExportMetadata(metadata); err != nil {
		return err
	}

	absRoot, err := filepath.Abs(treeRoot)
	if err != nil {
		return fmt.Errorf("resolve export tree %q: %w", treeRoot, err)
	}
	info, err := os.Lstat(absRoot)
	if err != nil {
		return fmt.Errorf("inspect export tree %q: %w", treeRoot, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("export tree %q must be a directory without a symbolic link", treeRoot)
	}

	files := make(map[string]productionExportFile)
	var regularFileBytes int64
	seenRoots := make(map[string]bool)
	err = filepath.WalkDir(absRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == absRoot {
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("export entry %q is a symbolic link", filePath)
		}
		if !entry.IsDir() && !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("export entry %q is not a directory or regular file", filePath)
		}
		relative, relativeErr := filepath.Rel(absRoot, filePath)
		if relativeErr != nil {
			return fmt.Errorf("relativize export entry %q: %w", filePath, relativeErr)
		}
		relative = filepath.ToSlash(relative)
		if err := validateProductionArchivePath(relative); err != nil {
			return err
		}
		markProductionExportRoots(metadata.Roots, relative, seenRoots)
		if entry.IsDir() {
			return nil
		}
		if relative == productionManifestName {
			return errors.New("export tree already contains manifest.json")
		}
		//nolint:gosec // WalkDir confined the regular file to the verified export tree.
		file, openErr := os.Open(filePath)
		if openErr != nil {
			return fmt.Errorf("open export entry %q: %w", relative, openErr)
		}
		digest := sha256.New()
		size, copyErr := io.Copy(digest, file)
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return fmt.Errorf("hash export entry %q: %w", relative, err)
		}
		if size != entryInfo.Size() {
			return fmt.Errorf("export entry %q changed while it was hashed", relative)
		}
		files[relative] = productionExportFile{
			Size:   size,
			SHA256: hex.EncodeToString(digest.Sum(nil)),
		}
		regularFileBytes += size
		if regularFileBytes > productionArchiveMaximum {
			return errors.New("production export exceeds the archive size limit")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk production export tree: %w", err)
	}
	for _, root := range metadata.Roots {
		if !seenRoots[root] {
			return fmt.Errorf("export root %q is missing", root)
		}
	}

	manifest := productionExportManifest{
		productionExportMetadata: metadata,
		RegularFileBytes:         regularFileBytes,
		Files:                    files,
	}
	return encodeCanonicalJSON(output, manifest)
}

//nolint:gocognit // The manifest contract is clearer when its related identity checks stay together.
func validateProductionExportMetadata(metadata productionExportMetadata) error {
	if metadata.SchemaVersion != schemaVersion {
		return errors.New("production export metadata schema_version must be 1")
	}
	if err := validateWholeSecondTimestamp(metadata.CreatedAt); err != nil {
		return fmt.Errorf("production export created_at: %w", err)
	}
	if metadata.Hostname == "" || metadata.StorageIdentity.VMName != "nguyenducloc-vm2" ||
		metadata.StorageIdentity.NodeName != "nguyenducloc-vm2" {
		return errors.New("production export target identity is incomplete")
	}
	if err := validateWholeSecondTimestamp(metadata.StorageIdentity.SelectedAt); err != nil {
		return fmt.Errorf("production export storage selected_at: %w", err)
	}
	validDedicated := metadata.StorageIdentity.StorageMode == "dedicated" &&
		metadata.StorageIdentity.StorageRoot == "/var/lib/vermouth" && metadata.StorageIdentity.StorageUUID != nil
	validFallback := metadata.StorageIdentity.StorageMode == "os-fallback" &&
		metadata.StorageIdentity.StorageRoot == "/var/lib/rancher/k3s/storage/vermouth" &&
		metadata.StorageIdentity.StorageUUID == nil
	if !validDedicated && !validFallback {
		return errors.New("production export storage identity is inconsistent")
	}
	if !lowerHex64Pattern.MatchString(metadata.StorageIdentity.MarkerSHA) {
		return errors.New("production export storage marker SHA256 is invalid")
	}
	if err := validateProductionExportRelease(metadata.CurrentRelease); err != nil {
		return fmt.Errorf("production export current release: %w", err)
	}
	if metadata.PreviousRelease != nil {
		if err := validateProductionExportRelease(*metadata.PreviousRelease); err != nil {
			return fmt.Errorf("production export previous release: %w", err)
		}
	}
	expectedRoots := productionExportRoots()
	if metadata.PreviousRelease != nil {
		expectedRoots = productionExportRootsWithPrevious()
	}
	if !slices.Equal(metadata.Roots, expectedRoots) {
		return errors.New("production export roots do not match the fixed archive contract")
	}
	if err := validateProductionGooseVersions(metadata.GooseVersions); err != nil {
		return err
	}
	if len(metadata.Roots) == 0 || !slices.IsSorted(metadata.Roots) {
		return errors.New("production export roots must be a nonempty sorted list")
	}
	for index, root := range metadata.Roots {
		if err := validateProductionArchivePath(root); err != nil {
			return fmt.Errorf("production export root: %w", err)
		}
		if index > 0 && metadata.Roots[index-1] == root {
			return fmt.Errorf("production export root %q appears more than once", root)
		}
	}
	return nil
}

func productionExportRoots() []string {
	return []string{
		"config/production.env",
		"helm/application.json",
		"helm/foundation.json",
		"postgres/billing.dump",
		"postgres/identity.dump",
		"postgres/notifications.dump",
		"postgres/teaching.dump",
		"releases/current",
		"storage/garage-data",
		"storage/garage-metadata",
		"storage/postgres-billing",
		"storage/postgres-identity",
		"storage/postgres-notifications",
		"storage/postgres-teaching",
		"storage/redpanda",
		"storage/traefik-acme",
	}
}

func productionExportRootsWithPrevious() []string {
	roots := append(productionExportRoots(), "releases/previous")
	slices.Sort(roots)
	return roots
}

func validateProductionExportRelease(release productionExportRelease) error {
	parts := strings.Split(release.Identity, "/")
	if len(parts) != 2 || !lowerHex40Pattern.MatchString(parts[0]) || !runIdentityPattern(parts[1]) {
		return errors.New("release identity is invalid")
	}
	if release.HelmRevision < 1 {
		return errors.New("Helm revision must be positive")
	}
	hashes := []string{
		release.ImagesJSONSHA256,
		release.DeploymentBaseSHA256,
		release.MigrationCompatEvidenceSHA256,
		release.RateLimitEvidenceSHA256,
		release.BundleManifestSHA256,
	}
	if !allProductionHashes(hashes) {
		return errors.New("release evidence checksum is invalid")
	}
	if len(release.ImageDigests) != len(productionImageWorkloads()) {
		return errors.New("release image digest inventory is incomplete")
	}
	for _, workload := range productionImageWorkloads() {
		if !digestPattern.MatchString(release.ImageDigests[workload]) {
			return fmt.Errorf("release image digest %q is invalid", workload)
		}
	}
	return nil
}

func validateProductionGooseVersions(versions productionGooseVersions) error {
	if versions.Identity < 0 || versions.Teaching < 0 || versions.Billing < 0 || versions.Notifications < 0 {
		return errors.New("production export Goose versions must be nonnegative")
	}
	return nil
}

func allProductionHashes(values []string) bool {
	for _, value := range values {
		if !lowerHex64Pattern.MatchString(value) {
			return false
		}
	}
	return true
}

func runIdentityPattern(value string) bool {
	parts := strings.Split(value, "-")
	return len(parts) == 2 && runIDPattern.MatchString(parts[0]) && runIDPattern.MatchString(parts[1])
}

func markProductionExportRoots(roots []string, entry string, seen map[string]bool) {
	for _, root := range roots {
		if entry == root || strings.HasPrefix(entry, root+"/") {
			seen[root] = true
		}
	}
}

func validateProductionArchive(input io.Reader, output io.Writer) error {
	hash := sha256.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, "zstd", "-q", "-dc")
	command.Stdin = io.TeeReader(input, hash)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open zstd output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start zstd validation: %w", err)
	}
	validateErr := validateProductionTar(stdout)
	if validateErr != nil {
		cancel()
		_ = command.Wait()
		return validateErr
	}
	_, drainErr := io.Copy(io.Discard, stdout)
	waitErr := command.Wait()
	if drainErr != nil {
		return fmt.Errorf("drain production archive: %w", drainErr)
	}
	if waitErr != nil {
		return fmt.Errorf("decompress production archive: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if _, err := fmt.Fprintln(output, hex.EncodeToString(hash.Sum(nil))); err != nil {
		return fmt.Errorf("write production archive SHA256: %w", err)
	}
	return nil
}

//nolint:funlen,gocognit // Streaming validation keeps ordering, type, size, and checksum checks in one pass.
func validateProductionTar(input io.Reader) error {
	reader := tar.NewReader(input)
	observedFiles := make(map[string]productionExportFile)
	seenPaths := make(map[string]bool)
	var manifest *productionExportManifest
	var previousPath string
	var regularFileBytes int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read production tar header: %w", err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if err := validateProductionArchivePath(name); err != nil {
			return err
		}
		if previousPath != "" && strings.Compare(previousPath, name) >= 0 {
			return fmt.Errorf("production archive path %q is duplicated or out of order", name)
		}
		previousPath = name
		if seenPaths[name] {
			return fmt.Errorf("production archive path %q appears more than once", name)
		}
		seenPaths[name] = true
		if len(header.PAXRecords) != 0 {
			return fmt.Errorf("production archive path %q carries extended metadata", name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				return fmt.Errorf("production archive directory %q has data", name)
			}
		case tar.TypeReg, 0:
			if header.Size < 0 || header.Size > productionArchiveMaximum-regularFileBytes {
				return fmt.Errorf("production archive file %q exceeds the size limit", name)
			}
			digest := sha256.New()
			var manifestBuffer bytes.Buffer
			destination := io.Writer(digest)
			if name == productionManifestName {
				if header.Size > productionManifestMaximum {
					return errors.New("production export manifest exceeds its size limit")
				}
				destination = io.MultiWriter(digest, &manifestBuffer)
			}
			written, copyErr := io.Copy(destination, reader) //nolint:gosec // The checked tar size and global limit bound this copy.
			if copyErr != nil {
				return fmt.Errorf("hash production archive file %q: %w", name, copyErr)
			}
			if written != header.Size {
				return fmt.Errorf("production archive file %q size changed while reading", name)
			}
			file := productionExportFile{Size: written, SHA256: hex.EncodeToString(digest.Sum(nil))}
			if name == productionManifestName {
				parsed, parseErr := parseProductionExportManifest(manifestBuffer.Bytes())
				if parseErr != nil {
					return parseErr
				}
				manifest = &parsed
				continue
			}
			regularFileBytes += written
			observedFiles[name] = file
		default:
			return fmt.Errorf("production archive path %q is not a directory or regular file", name)
		}
	}
	if manifest == nil {
		return errors.New("production archive manifest.json is missing")
	}
	seenRoots := make(map[string]bool)
	for name := range seenPaths {
		markProductionExportRoots(manifest.Roots, name, seenRoots)
	}
	return validateObservedProductionArchive(*manifest, observedFiles, seenPaths, seenRoots, regularFileBytes)
}

func parseProductionExportManifest(data []byte) (productionExportManifest, error) {
	var manifest productionExportManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode production export manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return manifest, errors.New("production export manifest must contain one JSON object")
	}
	var canonical bytes.Buffer
	if err := encodeCanonicalJSON(&canonical, manifest); err != nil {
		return manifest, err
	}
	if !bytes.Equal(data, canonical.Bytes()) {
		return manifest, errors.New("production export manifest is not canonical JSON")
	}
	if manifest.Files == nil {
		return manifest, errors.New("production export manifest files map is missing")
	}
	if _, exists := manifest.Files[productionManifestName]; exists {
		return manifest, errors.New("production export manifest cannot checksum itself")
	}
	return manifest, nil
}

func validateObservedProductionArchive(
	manifest productionExportManifest,
	observedFiles map[string]productionExportFile,
	seenPaths map[string]bool,
	seenRoots map[string]bool,
	regularFileBytes int64,
) error {
	if err := validateProductionExportMetadata(manifest.productionExportMetadata); err != nil {
		return err
	}
	if manifest.RegularFileBytes != regularFileBytes {
		return errors.New("production archive regular file byte total does not match its manifest")
	}
	if len(manifest.Files) != len(observedFiles) {
		return errors.New("production archive file inventory does not match its manifest")
	}
	for name, expected := range manifest.Files {
		actual, ok := observedFiles[name]
		if !ok || actual != expected {
			return fmt.Errorf("production archive file %q does not match its manifest", name)
		}
	}
	for _, root := range manifest.Roots {
		if !seenRoots[root] && !seenPaths[root] {
			return fmt.Errorf("production archive root %q is missing", root)
		}
	}
	return nil
}

func validateProductionArchivePath(value string) error {
	isUnsafe := value == "" || value == "." || strings.HasPrefix(value, "/") ||
		strings.Contains(value, "\\") || strings.ContainsRune(value, 0) ||
		archivepath.Clean(value) != value || !fs.ValidPath(value)
	if isUnsafe {
		return fmt.Errorf("production archive path %q is unsafe", value)
	}
	return nil
}
