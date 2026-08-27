package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: AC-11, AC-12
func TestAtomicCommit(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	targetDirectory := filepath.Join(directory, "state")
	require.NoError(t, os.Mkdir(targetDirectory, 0o700))
	nextPath := filepath.Join(targetDirectory, "images.json.next")
	targetPath := filepath.Join(targetDirectory, "images.json")
	require.NoError(t, os.WriteFile(nextPath, []byte("new"), 0o644))
	require.NoError(t, os.WriteFile(targetPath, []byte("old"), 0o600))

	require.NoError(t, atomicCommit(nextPath, targetPath))
	content, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
	_, err = os.Stat(nextPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	info, err := os.Stat(targetPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// covers: AC-11, AC-12
func TestAtomicCommit_CreatesPrivateTargetDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	nextPath := filepath.Join(directory, "runtime-values.yaml.next")
	targetDirectory := filepath.Join(directory, "nested", "state")
	targetPath := filepath.Join(targetDirectory, "runtime-values.yaml")
	require.NoError(t, os.WriteFile(nextPath, []byte("complete"), 0o644))

	require.NoError(t, atomicCommit(nextPath, targetPath))

	content, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, "complete", string(content))
	targetInfo, err := os.Stat(targetPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), targetInfo.Mode().Perm())
	directoryInfo, err := os.Stat(targetDirectory)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), directoryInfo.Mode().Perm())
}

// covers: AC-11, AC-12
func TestAtomicCommit_LeavesExistingTargetWhenNextFileIsMissing(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	targetPath := filepath.Join(directory, "images.json")
	require.NoError(t, os.WriteFile(targetPath, []byte("previous"), 0o600))

	err := atomicCommit(filepath.Join(directory, "images.json.next"), targetPath)
	require.ErrorContains(t, err, "open next file")
	content, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	require.Equal(t, "previous", string(content))
}

// covers: AC-11, AC-12
func TestAtomicCommitLeavesTheNextFileWhenTheTargetDirectoryIsBlocked(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	nextPath := filepath.Join(directory, "images.json.next")
	blockedDirectory := filepath.Join(directory, "state")
	targetPath := filepath.Join(blockedDirectory, "images.json")
	require.NoError(t, os.WriteFile(nextPath, []byte("complete"), 0o644))
	require.NoError(t, os.WriteFile(blockedDirectory, []byte("not a directory"), 0o600))

	err := atomicCommit(nextPath, targetPath)
	require.ErrorContains(t, err, "create target directory")
	nextContent, readErr := os.ReadFile(nextPath)
	require.NoError(t, readErr)
	require.Equal(t, "complete", string(nextContent))
	blockedContent, readErr := os.ReadFile(blockedDirectory)
	require.NoError(t, readErr)
	require.Equal(t, "not a directory", string(blockedContent))
}
