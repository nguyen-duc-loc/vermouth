//nolint:err113,noinlineerr // The CLI validates exact release inputs and reports the rejected value.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	productionTreeSchema           = "vermouth-tree-v1"
	productionMigrationsSchema     = "vermouth-migrations-v1"
	productionServiceBilling       = "billing"
	productionServiceIdentity      = "identity"
	productionServiceTeaching      = "teaching"
	productionServiceNotifications = "notifications"
	productionScannerInitial       = 64 * 1024
	productionScannerMaximum       = 1024 * 1024
	productionGitCommandLimit      = 30 * time.Second
	gitStatusFixedArguments        = 4
)

var (
	lowerHex40Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	lowerHex64Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	namespacePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	runIDPattern      = regexp.MustCompile(`^[1-9]\d*$`)
	timestampPattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
)

func productionImageWorkloads() []string {
	return []string{
		productionServiceBilling,
		"billing-migration",
		"garage-init",
		"gateway",
		productionServiceIdentity,
		"identity-migration",
		productionServiceNotifications,
		"notifications-migration",
		productionServiceTeaching,
		"teaching-migration",
		"web",
	}
}

func productionMigrationServices() []string {
	return []string{
		productionServiceIdentity,
		productionServiceTeaching,
		productionServiceBilling,
		productionServiceNotifications,
	}
}

type productionImageMetadata struct {
	GitSHA           string `json:"git_sha"`
	GitHubRunID      string `json:"github_run_id"`
	GitHubRunAttempt string `json:"github_run_attempt"`
	GeneratedAt      string `json:"generated_at"`
	ChartSHA256      string `json:"chart_sha256"`
}

type verifiedProductionImage struct {
	Workload         string   `json:"workload"`
	Repository       string   `json:"repository"`
	Tag              string   `json:"tag"`
	Digest           string   `json:"digest"`
	Platforms        []string `json:"platforms"`
	BuildInputSHA256 string   `json:"build_input_sha256"`
	SourceRevision   string   `json:"source_revision"`
	CreatedAt        string   `json:"created_at"`
}

func writeProductionImagePlan(
	root string,
	workload string,
	sourceRevision string,
	dockerHubNamespace string,
	output io.Writer,
) error {
	if !lowerHex40Pattern.MatchString(sourceRevision) {
		return fmt.Errorf("source revision %q must be 40 lowercase hexadecimal characters", sourceRevision)
	}
	if !namespacePattern.MatchString(dockerHubNamespace) {
		return fmt.Errorf("Docker Hub namespace %q is invalid", dockerHubNamespace)
	}
	if !slices.Contains(productionImageWorkloads(), workload) {
		return fmt.Errorf("unknown production workload %q", workload)
	}

	revision, err := repositoryRevision(root)
	if err != nil {
		return err
	}
	if sourceRevision != revision {
		return fmt.Errorf("source revision %q does not match repository HEAD %q", sourceRevision, revision)
	}

	lock, err := strictYAML[imageLock](filepath.Join(root, "deploy", "images.lock.yaml"))
	if err != nil {
		return err
	}
	image, ok := lock.Built[workload]
	if !ok {
		return fmt.Errorf("production workload %q is missing from the image lock", workload)
	}
	if !slices.Equal(image.MultiPlatforms, []string{platformARM64, platformAMD64}) {
		return fmt.Errorf("production workload %q must hash linux/arm64 then linux/amd64", workload)
	}

	trackedInputs := append([]string{".dockerignore", "deploy/images.lock.yaml", image.Dockerfile}, image.Inputs...)
	if err := ensureGitPathsClean(root, trackedInputs); err != nil {
		return fmt.Errorf("production image inputs: %w", err)
	}
	inputHash, err := canonicalImageHash(root, image, lock.External, image.MultiPlatforms)
	if err != nil {
		return fmt.Errorf("hash production workload %q: %w", workload, err)
	}

	baseLocks := slices.Clone(image.BaseLocks)
	slices.Sort(baseLocks)
	plan := map[string]any{
		"base_locks":         baseLocks,
		"build_args":         image.BuildArgs,
		"build_input_sha256": inputHash,
		"dockerfile":         image.Dockerfile,
		"platforms":          []string{platformAMD64, platformARM64},
		"repository":         "docker.io/" + dockerHubNamespace + "/vermouth-" + workload,
		"source_revision":    sourceRevision,
		"tag":                "git-" + sourceRevision[:12] + "-" + inputHash,
		"target":             image.Target,
		"workload":           workload,
	}
	return encodeCanonicalJSON(output, plan)
}

func writeProductionImagesDocument(metadataPath, recordsPath string, output io.Writer) error {
	metadata, err := strictJSONFile[productionImageMetadata](metadataPath)
	if err != nil {
		return fmt.Errorf("read production image metadata: %w", err)
	}
	if err := validateProductionImageMetadata(metadata); err != nil {
		return err
	}

	records, err := readVerifiedProductionImages(recordsPath)
	if err != nil {
		return err
	}
	images := make(map[string]any, len(records))
	var namespace string
	for index := range records {
		record := &records[index]
		resolvedNamespace, validateErr := validateVerifiedProductionImage(metadata, record)
		if validateErr != nil {
			return validateErr
		}
		if namespace == "" {
			namespace = resolvedNamespace
		}
		if namespace != resolvedNamespace {
			return errors.New("production image records use more than one Docker Hub namespace")
		}
		if _, exists := images[record.Workload]; exists {
			return fmt.Errorf("production image record %q appears more than once", record.Workload)
		}
		images[record.Workload] = map[string]any{
			"build_input_sha256": record.BuildInputSHA256,
			"created_at":         record.CreatedAt,
			"digest":             record.Digest,
			"platforms":          record.Platforms,
			"repository":         record.Repository,
			"source_revision":    record.SourceRevision,
			"tag":                record.Tag,
		}
	}
	workloads := productionImageWorkloads()
	for _, workload := range workloads {
		if _, ok := images[workload]; !ok {
			return fmt.Errorf("production image record %q is missing", workload)
		}
	}
	if len(images) != len(workloads) {
		return fmt.Errorf("production image records contain %d workloads, expected %d", len(images), len(workloads))
	}

	document := map[string]any{
		"chart_sha256":       metadata.ChartSHA256,
		"generated_at":       metadata.GeneratedAt,
		"git_sha":            metadata.GitSHA,
		"github_run_attempt": metadata.GitHubRunAttempt,
		"github_run_id":      metadata.GitHubRunID,
		"images":             images,
		"schema_version":     schemaVersion,
	}
	return encodeCanonicalJSON(output, document)
}

func writeTreeSHA256(directory string, output io.Writer) error {
	absDirectory, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("resolve tree %q: %w", directory, err)
	}
	absDirectory, err = filepath.EvalSymlinks(absDirectory)
	if err != nil {
		return fmt.Errorf("resolve tree links %q: %w", directory, err)
	}
	repositoryRoot, err := repositoryRoot(absDirectory)
	if err != nil {
		return err
	}
	relativeTree, err := filepath.Rel(repositoryRoot, absDirectory)
	if err != nil {
		return fmt.Errorf("relativize tree %q: %w", directory, err)
	}
	if relativeTree == ".." || strings.HasPrefix(relativeTree, ".."+string(filepath.Separator)) {
		return fmt.Errorf("tree %q is outside the repository", directory)
	}
	if err := ensureGitPathsClean(repositoryRoot, []string{filepath.ToSlash(relativeTree)}); err != nil {
		return fmt.Errorf("tree %q: %w", directory, err)
	}

	files, err := collectTreeFiles(absDirectory)
	if err != nil {
		return err
	}
	hash := sha256.New()
	writeScalar(hash, []byte(productionTreeSchema))
	writeCollectionCount(hash, len(files))
	for _, file := range files {
		writeScalar(hash, []byte(file.path))
		var mode [4]byte
		binary.BigEndian.PutUint32(mode[:], uint32(file.mode.Perm()))
		_, _ = hash.Write(mode[:])
		writeScalar(hash, file.data)
	}
	if _, err := fmt.Fprintln(output, hex.EncodeToString(hash.Sum(nil))); err != nil {
		return fmt.Errorf("write tree SHA256: %w", err)
	}
	return nil
}

func writeProductionMigrationSetSHA256(root string, output io.Writer) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root %q: %w", root, err)
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root links %q: %w", root, err)
	}
	discoveredRoot, err := repositoryRoot(absRoot)
	if err != nil {
		return err
	}
	if absRoot != discoveredRoot {
		return fmt.Errorf("migration root %q is not the repository root", root)
	}

	hash := sha256.New()
	writeScalar(hash, []byte(productionMigrationsSchema))
	services := productionMigrationServices()
	writeCollectionCount(hash, len(services))
	for _, service := range services {
		relativeDirectory := filepath.Join("services", service, "db", "migrations")
		if err := ensureGitPathsClean(absRoot, []string{filepath.ToSlash(relativeDirectory)}); err != nil {
			return fmt.Errorf("production migrations for %q: %w", service, err)
		}
		files, collectErr := collectTreeFiles(filepath.Join(absRoot, relativeDirectory))
		if collectErr != nil {
			return fmt.Errorf("collect production migrations for %q: %w", service, collectErr)
		}
		if len(files) == 0 {
			return fmt.Errorf("production migration set for %q is empty", service)
		}

		writeScalar(hash, []byte(service))
		writeCollectionCount(hash, len(files))
		for _, file := range files {
			writeScalar(hash, []byte(file.path))
			var mode [4]byte
			binary.BigEndian.PutUint32(mode[:], uint32(file.mode.Perm()))
			_, _ = hash.Write(mode[:])
			writeScalar(hash, file.data)
		}
	}
	if _, err := fmt.Fprintln(output, hex.EncodeToString(hash.Sum(nil))); err != nil {
		return fmt.Errorf("write production migration set SHA256: %w", err)
	}
	return nil
}

func repositoryRevision(root string) (string, error) {
	output, err := gitOutput(root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve repository HEAD: %w", err)
	}
	revision := strings.TrimSpace(output)
	if !lowerHex40Pattern.MatchString(revision) {
		return "", fmt.Errorf("repository HEAD %q is not a full Git SHA", revision)
	}
	return revision, nil
}

func repositoryRoot(path string) (string, error) {
	output, err := gitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(output)), nil
}

func ensureGitPathsClean(root string, paths []string) error {
	arguments := make([]string, 0, gitStatusFixedArguments+len(paths))
	arguments = append(arguments, "status", "--porcelain=v1", "--untracked-files=all", "--")
	arguments = append(arguments, paths...)
	output, err := gitOutput(root, arguments...)
	if err != nil {
		return fmt.Errorf("inspect Git state: %w", err)
	}
	if strings.TrimSpace(output) != "" {
		return fmt.Errorf("Git inputs are dirty:\n%s", strings.TrimSpace(output))
	}
	return nil
}

func gitOutput(root string, arguments ...string) (string, error) {
	commandArguments := append([]string{"-C", root}, arguments...)
	ctx, cancel := context.WithTimeout(context.Background(), productionGitCommandLimit)
	defer cancel()
	//nolint:gosec // The executable is fixed and Git receives each validated argument without a shell.
	command := exec.CommandContext(ctx, "git", commandArguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func collectTreeFiles(root string) ([]inputFile, error) {
	files := []inputFile{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("tree entry %q is not a regular file", path)
		}
		//nolint:gosec // WalkDir produced this path under the verified repository tree root.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read tree entry %q: %w", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize tree entry %q: %w", path, err)
		}
		files = append(files, inputFile{path: filepath.ToSlash(relative), mode: info.Mode(), data: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk tree %q: %w", root, err)
	}
	slices.SortFunc(files, func(left, right inputFile) int {
		return strings.Compare(left.path, right.path)
	})
	return files, nil
}

func strictJSONFile[T any](path string) (T, error) {
	var value T
	//nolint:gosec // The caller supplies a workflow owned metadata path and decoding rejects unknown fields.
	data, err := os.ReadFile(path)
	if err != nil {
		return value, fmt.Errorf("read %s: %w", path, err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, fmt.Errorf("decode %s: expected one JSON document", path)
	}
	return value, nil
}

func readVerifiedProductionImages(path string) ([]verifiedProductionImage, error) {
	//nolint:gosec // The caller supplies the workflow owned verified records path.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open production image records: %w", err)
	}

	records := make([]verifiedProductionImage, 0, len(productionImageWorkloads()))
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, productionScannerInitial), productionScannerMaximum)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			return nil, fmt.Errorf("production image record line %d is empty", line)
		}
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		var record verifiedProductionImage
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("decode production image record line %d: %w", line, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("production image record line %d must contain one JSON object", line)
		}
		records = append(records, record)
	}
	if err := errors.Join(scanner.Err(), file.Close()); err != nil {
		return nil, fmt.Errorf("read and close production image records: %w", err)
	}
	return records, nil
}

func validateProductionImageMetadata(metadata productionImageMetadata) error {
	if !lowerHex40Pattern.MatchString(metadata.GitSHA) {
		return errors.New("production image metadata git_sha is invalid")
	}
	if !runIDPattern.MatchString(metadata.GitHubRunID) || !runIDPattern.MatchString(metadata.GitHubRunAttempt) {
		return errors.New("production image metadata run identity is invalid")
	}
	if !lowerHex64Pattern.MatchString(metadata.ChartSHA256) {
		return errors.New("production image metadata chart_sha256 is invalid")
	}
	if err := validateWholeSecondTimestamp(metadata.GeneratedAt); err != nil {
		return fmt.Errorf("production image metadata generated_at: %w", err)
	}
	return nil
}

func validateVerifiedProductionImage(
	metadata productionImageMetadata,
	record *verifiedProductionImage,
) (string, error) {
	if !slices.Contains(productionImageWorkloads(), record.Workload) {
		return "", fmt.Errorf("unknown production image record workload %q", record.Workload)
	}
	if record.SourceRevision != metadata.GitSHA || !lowerHex64Pattern.MatchString(record.BuildInputSHA256) {
		return "", fmt.Errorf("production image record %q has invalid source identity", record.Workload)
	}
	expectedTag := "git-" + record.SourceRevision[:12] + "-" + record.BuildInputSHA256
	if record.Tag != expectedTag {
		return "", fmt.Errorf("production image record %q has a noncanonical tag", record.Workload)
	}
	if !digestPattern.MatchString(record.Digest) {
		return "", fmt.Errorf("production image record %q has an invalid digest", record.Workload)
	}
	if !slices.Equal(record.Platforms, []string{platformAMD64, platformARM64}) {
		return "", fmt.Errorf("production image record %q has an invalid platform set", record.Workload)
	}
	if err := validateWholeSecondTimestamp(record.CreatedAt); err != nil {
		return "", fmt.Errorf("production image record %q created_at: %w", record.Workload, err)
	}
	prefix := "docker.io/"
	suffix := "/vermouth-" + record.Workload
	if !strings.HasPrefix(record.Repository, prefix) || !strings.HasSuffix(record.Repository, suffix) {
		return "", fmt.Errorf("production image record %q has an invalid repository", record.Workload)
	}
	namespace := strings.TrimSuffix(strings.TrimPrefix(record.Repository, prefix), suffix)
	if !namespacePattern.MatchString(namespace) {
		return "", fmt.Errorf("production image record %q has an invalid Docker Hub namespace", record.Workload)
	}
	return namespace, nil
}

func validateWholeSecondTimestamp(value string) error {
	if !timestampPattern.MatchString(value) {
		return fmt.Errorf("timestamp %q must use RFC 3339 whole seconds and Z", value)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.Location() != time.UTC {
		return fmt.Errorf("timestamp %q is invalid", value)
	}
	return nil
}

func encodeCanonicalJSON(output io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal canonical JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return fmt.Errorf("normalize canonical JSON: %w", err)
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return fmt.Errorf("encode canonical JSON: %w", err)
	}
	return nil
}
