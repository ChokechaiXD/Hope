package hermes

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"cortex.local/cortex/internal/config"
)

type snapshotEntry struct {
	Original  string `json:"original"`
	Backup    string `json:"backup"`
	Exists    bool   `json:"exists"`
	Directory bool   `json:"directory"`
	Restore   bool   `json:"restore"`
}

type snapshotManifest struct {
	CreatedAt  time.Time       `json:"created_at"`
	HermesHome string          `json:"hermes_home"`
	Entries    []snapshotEntry `json:"entries"`
}

type rollbackSnapshot struct {
	Dir     string
	entries []snapshotEntry
}

type snapshotTarget struct {
	original string
	relative string
	restore  bool
}

func createSnapshot(dataDir, hermesHome string, profiles []SyncedProfile) (*rollbackSnapshot, error) {
	backupRoot := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return nil, err
	}
	prefix := "hermes-pre-cortex-" + time.Now().UTC().Format("20060102-150405.000000000") + "-"
	directory, err := os.MkdirTemp(backupRoot, prefix)
	if err != nil {
		return nil, err
	}
	targets := []snapshotTarget{{
		original: filepath.Join(dataDir, config.FileName),
		relative: filepath.Join("cortex", config.FileName),
		restore:  true,
	}}
	for _, profile := range profiles {
		base := filepath.Join("hermes", profile.AgentID)
		for _, name := range []string{"config.yaml", "cortex.json", "memory_store.db", "memory_store.db-wal", "memory_store.db-shm"} {
			targets = append(targets, snapshotTarget{
				original: filepath.Join(profile.Home, name),
				relative: filepath.Join(base, name),
				restore:  name == "config.yaml" || name == "cortex.json",
			})
		}
		targets = append(targets, snapshotTarget{
			original: filepath.Join(profile.Home, "plugins", "cortex"),
			relative: filepath.Join(base, "plugins", "cortex"),
			restore:  true,
		})
	}
	manifest := snapshotManifest{CreatedAt: time.Now().UTC(), HermesHome: hermesHome}
	for _, target := range targets {
		entry, err := captureSnapshotTarget(directory, target)
		if err != nil {
			_ = os.RemoveAll(directory)
			return nil, err
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	manifestRaw = append(manifestRaw, '\n')
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), manifestRaw, 0o600); err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	return &rollbackSnapshot{Dir: directory, entries: manifest.Entries}, nil
}

func captureSnapshotTarget(snapshotDir string, target snapshotTarget) (snapshotEntry, error) {
	entry := snapshotEntry{Original: target.original, Backup: target.relative, Restore: target.restore}
	info, err := os.Lstat(target.original)
	if os.IsNotExist(err) {
		return entry, nil
	}
	if err != nil {
		return entry, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return entry, fmt.Errorf("refuse to snapshot symlink %s", target.original)
	}
	entry.Exists = true
	entry.Directory = info.IsDir()
	destination := filepath.Join(snapshotDir, target.relative)
	if entry.Directory {
		return entry, copyDirectory(target.original, destination)
	}
	return entry, copyFile(target.original, destination, info.Mode().Perm())
}

func (snapshot *rollbackSnapshot) Restore() error {
	for index := len(snapshot.entries) - 1; index >= 0; index-- {
		entry := snapshot.entries[index]
		if !entry.Restore {
			continue
		}
		if err := os.RemoveAll(entry.Original); err != nil {
			return fmt.Errorf("clear changed path %s: %w", entry.Original, err)
		}
		if !entry.Exists {
			continue
		}
		source := filepath.Join(snapshot.Dir, entry.Backup)
		if entry.Directory {
			if err := copyDirectory(source, entry.Original); err != nil {
				return fmt.Errorf("restore directory %s: %w", entry.Original, err)
			}
			continue
		}
		info, err := os.Stat(source)
		if err != nil {
			return fmt.Errorf("inspect snapshot file %s: %w", source, err)
		}
		if err := copyFile(source, entry.Original, info.Mode().Perm()); err != nil {
			return fmt.Errorf("restore file %s: %w", entry.Original, err)
		}
	}
	return nil
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to copy symlink %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
