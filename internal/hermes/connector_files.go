package hermes

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	hermesconnector "cortex.local/cortex/connectors/hermes"
)

type connectorConfig struct {
	URL                   string `json:"url"`
	Token                 string `json:"token"`
	AgentID               string `json:"agent_id"`
	DefaultProject        string `json:"default_project"`
	DefaultDomain         string `json:"default_domain"`
	AutoCaptureEnabled    bool   `json:"auto_capture_enabled"`
	AutoCaptureEveryTurns int    `json:"auto_capture_every_turns"`
	AutoCaptureMaxChars   int    `json:"auto_capture_max_chars"`
	PrefetchTokenBudget   int    `json:"prefetch_token_budget"`
	RecallTokenBudget     int    `json:"recall_token_budget"`
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
	document, err := readConnectorDocument(home)
	if err != nil {
		return err
	}
	known, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(known, &fields); err != nil {
		return err
	}
	for key, field := range fields {
		document[key] = field
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(filepath.Join(home, "cortex.json"), raw, 0o600)
}

func readConnectorConfig(home string) (connectorConfig, map[string]json.RawMessage, error) {
	document, err := readConnectorDocument(home)
	if err != nil {
		return connectorConfig{}, nil, err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return connectorConfig{}, nil, err
	}
	var value connectorConfig
	if err := json.Unmarshal(raw, &value); err != nil {
		return connectorConfig{}, nil, err
	}
	return value, document, nil
}

func readConnectorDocument(home string) (map[string]json.RawMessage, error) {
	path := filepath.Join(home, "cortex.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]json.RawMessage), nil
	}
	if err != nil {
		return nil, err
	}
	document := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return document, nil
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
