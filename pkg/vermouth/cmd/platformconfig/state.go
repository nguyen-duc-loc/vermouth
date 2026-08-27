//nolint:gosec,noinlineerr // State paths are fixed under the validated platform directory.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	privateFileMode      = 0o600
	privateDirectoryMode = 0o700
)

func atomicCommit(nextPath, targetPath string) error {
	next, err := os.OpenFile(nextPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open next file %s: %w", nextPath, err)
	}
	syncErr := next.Sync()
	closeErr := next.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync and close next file %s: %w", nextPath, err)
	}
	if err := os.Chmod(nextPath, privateFileMode); err != nil {
		return fmt.Errorf("set mode on %s: %w", nextPath, err)
	}

	targetDirectory := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDirectory, privateDirectoryMode); err != nil {
		return fmt.Errorf("create target directory %s: %w", targetDirectory, err)
	}
	if err := os.Chmod(targetDirectory, privateDirectoryMode); err != nil {
		return fmt.Errorf("set mode on %s: %w", targetDirectory, err)
	}
	if err := os.Rename(nextPath, targetPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", nextPath, targetPath, err)
	}

	directory, err := os.Open(targetDirectory)
	if err != nil {
		return fmt.Errorf("open target directory %s: %w", targetDirectory, err)
	}
	syncErr = directory.Sync()
	closeErr = directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync and close target directory %s: %w", targetDirectory, err)
	}
	return nil
}
