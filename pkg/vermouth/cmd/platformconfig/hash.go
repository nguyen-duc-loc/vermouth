//nolint:err113,funlen,gocognit,gocritic,gosec,noinlineerr // Repository paths come from a strict committed inventory and hash writes cannot fail.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type imagePlan struct {
	Workload       string   `json:"workload"`
	RepositoryPath string   `json:"repository_path"`
	Dockerfile     string   `json:"dockerfile"`
	Target         string   `json:"target"`
	Platforms      []string `json:"platforms"`
	Inputs         []string `json:"inputs"`
	BaseProvenance string   `json:"base_provenance"`
	InputHash      string   `json:"input_hash"`
	Tag            string   `json:"tag"`
}

type inputFile struct {
	path string
	mode fs.FileMode
	data []byte
}

func writeImagePlan(root, workload, mode string, output io.Writer) error {
	lock, err := strictYAML[imageLock](filepath.Join(root, "deploy/images.lock.yaml"))
	if err != nil {
		return err
	}
	image, ok := lock.Built[workload]
	if !ok {
		return fmt.Errorf("unknown workload %q", workload)
	}

	platforms := image.NativePlatforms
	template := image.NativeTagTemplate
	if mode == "multi" {
		platforms = image.MultiPlatforms
		template = image.MultiTagTemplate
	}
	if mode != "native" && mode != "multi" {
		return fmt.Errorf("unknown image plan mode %q", mode)
	}
	if len(platforms) == 0 {
		return fmt.Errorf("workload %q has no %s output", workload, mode)
	}

	hash, err := canonicalImageHash(root, image, lock.External, platforms)
	if err != nil {
		return fmt.Errorf("hash %s: %w", workload, err)
	}
	plan := imagePlan{
		Workload:       image.Workload,
		RepositoryPath: image.RepositoryPath,
		Dockerfile:     image.Dockerfile,
		Target:         image.Target,
		Platforms:      slices.Clone(platforms),
		Inputs:         slices.Clone(image.Inputs),
		BaseProvenance: baseProvenance(image, lock.External),
		InputHash:      hash,
		Tag:            strings.ReplaceAll(template, "{hash12}", hash[:12]),
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(plan); err != nil {
		return fmt.Errorf("encode image plan: %w", err)
	}
	return nil
}

func baseProvenance(image builtImage, external map[string]externalImage) string {
	baseLocks := slices.Clone(image.BaseLocks)
	slices.Sort(baseLocks)
	parts := make([]string, 0, len(baseLocks))
	for _, key := range baseLocks {
		parts = append(parts, key+"="+external[key].Digest)
	}
	return strings.Join(parts, ",")
}

func canonicalImageHash(
	root string,
	image builtImage,
	external map[string]externalImage,
	platforms []string,
) (string, error) {
	files, err := collectInputFiles(root, image.Inputs)
	if err != nil {
		return "", err
	}
	dockerfilePath := filepath.Join(root, filepath.FromSlash(image.Dockerfile))
	dockerfile, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return "", fmt.Errorf("read Dockerfile: %w", err)
	}

	hash := sha256.New()
	writeScalar(hash, []byte(image.HashSchema))
	writeScalar(hash, []byte(image.Dockerfile))
	writeScalar(hash, dockerfile)

	dockerignorePath := filepath.Join(root, ".dockerignore")
	dockerignore, err := os.ReadFile(dockerignorePath)
	switch {
	case err == nil:
		hash.Write([]byte{0x01})
		writeScalar(hash, dockerignore)
	case errors.Is(err, os.ErrNotExist):
		hash.Write([]byte{0x00})
	default:
		return "", fmt.Errorf("read .dockerignore: %w", err)
	}

	writeScalar(hash, []byte(image.Target))
	writeCollectionCount(hash, len(platforms))
	for _, platform := range platforms {
		writeScalar(hash, []byte(platform))
	}

	buildArgKeys := sortedMapKeys(image.BuildArgs)
	writeCollectionCount(hash, len(buildArgKeys))
	for _, key := range buildArgKeys {
		writeScalar(hash, []byte(key))
		writeScalar(hash, []byte(image.BuildArgs[key]))
	}

	baseLocks := slices.Clone(image.BaseLocks)
	slices.Sort(baseLocks)
	writeCollectionCount(hash, len(baseLocks))
	for _, key := range baseLocks {
		base, ok := external[key]
		if !ok {
			return "", fmt.Errorf("unknown base lock %q", key)
		}
		writeScalar(hash, []byte(key))
		writeScalar(hash, []byte(base.Digest))
	}

	writeCollectionCount(hash, len(files))
	for _, file := range files {
		writeScalar(hash, []byte(file.path))
		var mode [4]byte
		binary.BigEndian.PutUint32(mode[:], uint32(file.mode.Perm()))
		hash.Write(mode[:])
		writeScalar(hash, file.data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func collectInputFiles(root string, declared []string) ([]inputFile, error) {
	if err := validateDeclaredInputs(root, declared); err != nil {
		return nil, err
	}
	files := []inputFile{}
	seen := make(map[string]struct{})
	for _, input := range declared {
		full := filepath.Join(root, filepath.FromSlash(input))
		info, err := os.Lstat(full)
		if err != nil {
			return nil, fmt.Errorf("inspect %q: %w", input, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("input %q is a symlink", input)
		}
		if info.Mode().IsRegular() {
			file, err := readInputFile(root, full, info)
			if err != nil {
				return nil, err
			}
			files = append(files, file)
			continue
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("input %q is not a regular file or directory", input)
		}
		err = filepath.WalkDir(full, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != full && entry.IsDir() && ignoredInputDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				return nil
			}
			fileInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if fileInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("input %q is a symlink", path)
			}
			if !fileInfo.Mode().IsRegular() {
				return fmt.Errorf("input %q is not a regular file", path)
			}
			file, err := readInputFile(root, path, fileInfo)
			if err != nil {
				return err
			}
			if _, exists := seen[file.path]; exists {
				return fmt.Errorf("duplicate input path %q", file.path)
			}
			seen[file.path] = struct{}{}
			files = append(files, file)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk input %q: %w", input, err)
		}
	}
	slices.SortFunc(files, func(left, right inputFile) int {
		return strings.Compare(left.path, right.path)
	})
	return files, nil
}

func ignoredInputDirectory(name string) bool {
	switch name {
	case ".git", ".tmp", ".vite", "dist", "node_modules":
		return true
	default:
		return false
	}
}

func readInputFile(root, path string, info fs.FileInfo) (inputFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return inputFile{}, fmt.Errorf("read input %q: %w", path, err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return inputFile{}, fmt.Errorf("relativize input %q: %w", path, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return inputFile{}, fmt.Errorf("input %q is outside the repository", path)
	}
	return inputFile{
		path: filepath.ToSlash(relative),
		mode: info.Mode(),
		data: data,
	}, nil
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func writeScalar(output io.Writer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = output.Write(length[:])
	_, _ = output.Write(value)
}

func writeCollectionCount(output io.Writer, count int) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(count))
	_, _ = output.Write(length[:])
}

func writeSecretHash(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(bufio.NewReader(input))
	values := make(map[string]string)
	if err := decoder.Decode(&values); err != nil {
		return fmt.Errorf("decode secret values: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("secret values must contain one JSON object")
	}
	if len(values) == 0 {
		return errors.New("secret values must not be empty")
	}

	hash := sha256.New()
	_, _ = hash.Write([]byte("vermouth-secret-v1"))
	for _, key := range sortedMapKeys(values) {
		writeUint32Bytes(hash, []byte(key))
		writeUint32Bytes(hash, []byte(values[key]))
	}
	if _, err := fmt.Fprintln(output, hex.EncodeToString(hash.Sum(nil))); err != nil {
		return fmt.Errorf("write secret hash: %w", err)
	}
	return nil
}

func writeUint32Bytes(output io.Writer, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = output.Write(length[:])
	_, _ = output.Write(value)
}
