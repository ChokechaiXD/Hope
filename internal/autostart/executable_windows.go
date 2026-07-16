//go:build windows

package autostart

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func installExecutable(source, destination string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve source executable: %w", err)
	}
	if strings.EqualFold(filepath.Clean(source), filepath.Clean(destination)) {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open Cortex executable: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("inspect Cortex executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Cortex executable is not a regular file")
	}
	if existing, readErr := os.ReadFile(destination); readErr == nil {
		sourceBytes, sourceErr := io.ReadAll(input)
		if sourceErr != nil {
			return fmt.Errorf("compare Cortex executable: %w", sourceErr)
		}
		if bytes.Equal(existing, sourceBytes) {
			return nil
		}
		if _, err := input.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("rewind Cortex executable: %w", err)
		}
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Cortex binary directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".cortex-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Cortex executable: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := io.Copy(temporary, input); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("copy Cortex executable: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync Cortex executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Cortex executable: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o700); err != nil {
		return fmt.Errorf("protect Cortex executable: %w", err)
	}

	backupFile, err := os.CreateTemp(directory, ".cortex-previous-*.exe")
	if err != nil {
		return fmt.Errorf("reserve Cortex executable backup: %w", err)
	}
	backupPath := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		return fmt.Errorf("close Cortex executable backup: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("prepare Cortex executable backup: %w", err)
	}
	defer func() { _ = os.Remove(backupPath) }()
	if err := replaceExecutable(temporaryPath, destination, backupPath, os.Rename, os.Remove); err != nil {
		return err
	}
	return nil
}

func replaceExecutable(
	temporary, destination, backup string,
	rename func(string, string) error,
	remove func(string) error,
) error {
	hadPrevious := false
	if _, err := os.Stat(destination); err == nil {
		if err := rename(destination, backup); err != nil {
			return fmt.Errorf("backup Cortex executable: %w", err)
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect installed Cortex executable: %w", err)
	}
	if err := rename(temporary, destination); err != nil {
		if hadPrevious {
			if restoreErr := rename(backup, destination); restoreErr != nil {
				return fmt.Errorf("install Cortex executable: %w; restore previous executable: %v", err, restoreErr)
			}
		}
		return fmt.Errorf("install Cortex executable: %w", err)
	}
	if hadPrevious {
		if err := remove(backup); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove previous Cortex executable: %w", err)
		}
	}
	return nil
}
