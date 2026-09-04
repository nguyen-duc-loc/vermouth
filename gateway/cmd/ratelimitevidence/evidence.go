package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/ratelimit"
)

const (
	evidenceSchemaVersion  = 1
	evidenceTestTarget     = "task test:auth-rate-limit"
	evidenceRelativePath   = ".tmp/production/rate-limit-evidence.json"
	evidenceFileMode       = 0o644
	evidenceDirectoryMode  = 0o750
	gitCommandTimeout      = 10 * time.Second
	gitStatusArgumentCount = 6
)

var (
	errDirtyInputs       = errors.New("rate limit evidence inputs are not clean")
	errUnsafeDestination = errors.New("evidence destination must be a regular file or absent")
	errWrongDestination  = errors.New("evidence path must be .tmp/production/rate-limit-evidence.json")
	errInvalidEvidence   = errors.New("rate limit evidence is invalid")
	gitSHAExpression     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type evidence struct {
	SchemaVersion                int    `json:"schema_version"`
	GitSHA                       string `json:"git_sha"`
	TestTarget                   string `json:"test_target"`
	ThresholdConfigurationSHA256 string `json:"threshold_configuration_sha256"`
	Pass                         bool   `json:"pass"`
	Timestamp                    string `json:"timestamp"`
}

func writeEvidence(root, rawPath string, now func() time.Time) error {
	root, path, err := resolvePaths(root, rawPath)
	if err != nil {
		return err
	}
	err = requireCleanInputs(root)
	if err != nil {
		return err
	}
	err = requireWritableDestination(path)
	if err != nil {
		return err
	}
	document, err := currentEvidence(root, now)
	if err != nil {
		return err
	}
	canonical, err := canonicalEvidence(document)
	if err != nil {
		return err
	}
	return writeAtomic(path, canonical)
}

func verifyEvidence(root, rawPath string) error {
	root, path, err := resolvePaths(root, rawPath)
	if err != nil {
		return err
	}
	err = requireCleanInputs(root)
	if err != nil {
		return err
	}
	err = requireReadableDestination(path)
	if err != nil {
		return err
	}
	//nolint:gosec // resolvePaths restricts this to the one spec owned evidence path.
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read evidence: %w", err)
	}
	document, err := decodeEvidence(payload)
	if err != nil {
		return err
	}
	current, err := currentEvidence(root, func() time.Time {
		parsed, _ := time.Parse(time.RFC3339, document.Timestamp)
		return parsed
	})
	if err != nil {
		return err
	}
	if document != current {
		return errInvalidEvidence
	}
	canonical, err := canonicalEvidence(document)
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, canonical) {
		return fmt.Errorf("%w: bytes are not canonical", errInvalidEvidence)
	}
	return nil
}

func currentEvidence(root string, now func() time.Time) (evidence, error) {
	config, err := ratelimit.ConfigFromEnv()
	if err != nil {
		return evidence{}, err
	}
	gitSHA, err := currentGitSHA(root)
	if err != nil {
		return evidence{}, err
	}
	if now == nil {
		now = time.Now
	}
	timestamp := now().UTC().Truncate(time.Second).Format(time.RFC3339)
	return evidence{
		SchemaVersion:                evidenceSchemaVersion,
		GitSHA:                       gitSHA,
		TestTarget:                   evidenceTestTarget,
		ThresholdConfigurationSHA256: thresholdHash(config.NormalizedThresholds()),
		Pass:                         true,
		Timestamp:                    timestamp,
	}, nil
}

func thresholdHash(values map[string]string) string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	var canonical strings.Builder
	for _, name := range names {
		canonical.WriteString(name)
		canonical.WriteByte('=')
		canonical.WriteString(values[name])
		canonical.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(digest[:])
}

func canonicalEvidence(document evidence) ([]byte, error) {
	values := map[string]any{
		"git_sha":                        document.GitSHA,
		"pass":                           document.Pass,
		"schema_version":                 document.SchemaVersion,
		"test_target":                    document.TestTarget,
		"threshold_configuration_sha256": document.ThresholdConfigurationSHA256,
		"timestamp":                      document.Timestamp,
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode evidence: %w", err)
	}
	return append(payload, '\n'), nil
}

func decodeEvidence(payload []byte) (evidence, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var document evidence
	err := decoder.Decode(&document)
	if err != nil {
		return evidence{}, fmt.Errorf("%w: %w", errInvalidEvidence, err)
	}
	err = decoder.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		return evidence{}, fmt.Errorf("%w: document must contain one value", errInvalidEvidence)
	}
	parsed, err := time.Parse(time.RFC3339, document.Timestamp)
	if err != nil || parsed.UTC().Truncate(time.Second).Format(time.RFC3339) != document.Timestamp {
		return evidence{}, fmt.Errorf("%w: timestamp must be UTC whole seconds", errInvalidEvidence)
	}
	return document, nil
}

func currentGitSHA(root string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	//nolint:gosec // root is used only as git's explicit working tree argument.
	command := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "HEAD^{commit}")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read Git revision: %w", err)
	}
	sha := strings.TrimSpace(string(output))
	if !gitSHAExpression.MatchString(sha) {
		return "", fmt.Errorf("read Git revision: %w", errInvalidEvidence)
	}
	return sha, nil
}

func requireCleanInputs(root string) error {
	paths := cleanInputPaths()
	args := make([]string, 0, gitStatusArgumentCount+len(paths))
	args = append(args, "-C", root, "status", "--porcelain=v1", "--untracked-files=all", "--")
	args = append(args, paths...)
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	//nolint:gosec // every argument is fixed except git's explicit repository root.
	command := exec.CommandContext(ctx, "git", args...)
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("inspect evidence inputs: %w", err)
	}
	if len(bytes.TrimSpace(output)) != 0 {
		return fmt.Errorf("%w: %s", errDirtyInputs, strings.TrimSpace(string(output)))
	}
	return nil
}

func resolvePaths(rawRoot, rawPath string) (root string, path string, returnErr error) {
	root, err := filepath.Abs(rawRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository root: %w", err)
	}
	root = filepath.Clean(root)
	path = rawPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	expected := filepath.Join(root, filepath.FromSlash(evidenceRelativePath))
	if path != expected {
		return "", "", errWrongDestination
	}
	return root, path, nil
}

func cleanInputPaths() []string {
	return []string{
		"api/openapi.yaml",
		"api/oapi-codegen.yaml",
		"gateway/",
		"web/src/api/",
		"web/src/pages/SignInPage.tsx",
		"web/src/pages/SignInPage.test.tsx",
		"web/src/routes.tsx",
		"web/src/routes.test.tsx",
		"web/package.json",
		"package.json",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
		"Taskfile.yml",
		".env.example",
		"deploy/helm/vermouth/",
		"deploy/production/rate-limit-evidence.schema.json",
		"test/authratelimit/",
	}
}

func requireWritableDestination(path string) error {
	//nolint:gosec // resolvePaths restricts this to the one spec owned evidence path.
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect evidence destination: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errUnsafeDestination
	}
	return nil
}

func requireReadableDestination(path string) error {
	//nolint:gosec // resolvePaths restricts this to the one spec owned evidence path.
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect evidence destination: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errUnsafeDestination
	}
	return nil
}

func writeAtomic(path string, payload []byte) (returnErr error) {
	directory := filepath.Dir(path)
	//nolint:gosec // directory is the parent of the one path resolvePaths permits.
	err := os.MkdirAll(directory, evidenceDirectoryMode)
	if err != nil {
		return fmt.Errorf("create evidence directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".rate-limit-evidence-*")
	if err != nil {
		return fmt.Errorf("create temporary evidence: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			closeErr := temporary.Close()
			if returnErr == nil && closeErr != nil {
				returnErr = fmt.Errorf("close temporary evidence: %w", closeErr)
			}
		}
		if temporaryPath != "" {
			//nolint:gosec // os.CreateTemp created this exact path in the fixed evidence directory.
			removeErr := os.Remove(temporaryPath)
			if returnErr == nil && removeErr != nil {
				returnErr = fmt.Errorf("remove temporary evidence: %w", removeErr)
			}
		}
	}()

	err = temporary.Chmod(evidenceFileMode)
	if err != nil {
		return fmt.Errorf("set evidence mode: %w", err)
	}
	_, err = temporary.Write(payload)
	if err != nil {
		return fmt.Errorf("write evidence: %w", err)
	}
	err = temporary.Sync()
	if err != nil {
		return fmt.Errorf("sync evidence: %w", err)
	}
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("close evidence: %w", err)
	}
	closed = true
	err = requireWritableDestination(path)
	if err != nil {
		return err
	}
	//nolint:gosec // both paths are fixed within the evidence directory after the safety recheck.
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("replace evidence: %w", err)
	}
	temporaryPath = ""
	return syncDirectory(directory)
}

func syncDirectory(path string) error {
	//nolint:gosec // path is the parent of the fixed evidence destination.
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open evidence directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	err = directory.Sync()
	if err != nil &&
		!errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
		return fmt.Errorf("sync evidence directory: %w", err)
	}
	return nil
}
