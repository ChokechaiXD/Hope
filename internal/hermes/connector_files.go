package hermes

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	hermesconnector "cortex.local/cortex/connectors/hermes"
)

type connectorConfig struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	AgentID string `json:"agent_id"`
}

func installProvider(home string) error {
	destinationRoot := filepath.Join(home, "plugins", "cortex")
	return fs.WalkDir(hermesconnector.ProviderFiles, "provider", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel("provider", path)
		if err != nil || relative == "." {
			return err
		}
		destination := filepath.Join(destinationRoot, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		content, err := hermesconnector.ProviderFiles.ReadFile(path)
		if err != nil {
			return err
		}
		return writeAtomic(destination, content, 0o600)
	})
}

func writeConnectorConfig(home string, value connectorConfig) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(filepath.Join(home, "cortex.json"), raw, 0o600)
}

func writeAtomic(path string, content []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cortex-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
