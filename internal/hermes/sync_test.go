package hermes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cortex.local/cortex/internal/config"
	"gopkg.in/yaml.v3"
)

func TestSyncInstallsAndActivatesAllHermesProfiles(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "cortex-data")
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex: %v", err)
	}
	hermesHome := filepath.Join(t.TempDir(), "hermes")
	for _, profile := range []string{"sola", "nua"} {
		if err := os.MkdirAll(filepath.Join(hermesHome, "profiles", profile), 0o700); err != nil {
			t.Fatalf("create profile: %v", err)
		}
	}
	originalConfig := []byte("model:\n  provider: local\nmemory:\n  provider: holographic\nplugins:\n  hermes-memory-store:\n    db_path: legacy.db\n  keep-plugin:\n    enabled: true\ncustom: keep\n")
	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		t.Fatalf("create Hermes home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hermesHome, "config.yaml"), originalConfig, 0o600); err != nil {
		t.Fatalf("write Hermes config: %v", err)
	}
	legacyDB := filepath.Join(hermesHome, "memory_store.db")
	if err := os.WriteFile(legacyDB, []byte("legacy-data"), 0o600); err != nil {
		t.Fatalf("write legacy db marker: %v", err)
	}

	result, err := Sync(SyncOptions{
		HermesHome: hermesHome,
		DataDir:    dataDir,
		ServerURL:  "http://127.0.0.1:7777",
		RootAgent:  "mika",
		Activate:   true,
	})
	if err != nil {
		t.Fatalf("sync connector: %v", err)
	}
	if len(result.Profiles) != 3 {
		t.Fatalf("synced profiles = %#v, want root + 2 profiles", result.Profiles)
	}
	if result.BackupDir == "" {
		t.Fatal("sync did not report a rollback snapshot")
	}
	if _, err := os.Stat(filepath.Join(result.BackupDir, "manifest.json")); err != nil {
		t.Fatalf("snapshot manifest missing: %v", err)
	}

	loadedCortex, err := config.Load(dataDir)
	if err != nil {
		t.Fatalf("load Cortex config: %v", err)
	}
	profiles := map[string]string{
		"mika": hermesHome,
		"sola": filepath.Join(hermesHome, "profiles", "sola"),
		"nua":  filepath.Join(hermesHome, "profiles", "nua"),
	}
	for agentID, home := range profiles {
		if _, err := os.Stat(filepath.Join(home, "plugins", "cortex", "__init__.py")); err != nil {
			t.Errorf("connector missing for %s: %v", agentID, err)
		}
		raw, err := os.ReadFile(filepath.Join(home, "cortex.json"))
		if err != nil {
			t.Errorf("read %s connector config: %v", agentID, err)
			continue
		}
		var connectorConfig struct {
			URL     string `json:"url"`
			Token   string `json:"token"`
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal(raw, &connectorConfig); err != nil {
			t.Errorf("decode %s connector config: %v", agentID, err)
			continue
		}
		if connectorConfig.AgentID != agentID || connectorConfig.URL != "http://127.0.0.1:7777" {
			t.Errorf("%s connector config = %#v", agentID, connectorConfig)
		}
		if authenticated, ok := loadedCortex.Authenticate(connectorConfig.Token); !ok || authenticated != agentID {
			t.Errorf("%s token authenticated as %q, %v", agentID, authenticated, ok)
		}
		assertProvider(t, filepath.Join(home, "config.yaml"), "cortex")
	}
	assertLegacyProviderConfigRemoved(t, filepath.Join(hermesHome, "config.yaml"))

	legacy, err := os.ReadFile(legacyDB)
	if err != nil || string(legacy) != "legacy-data" {
		t.Fatalf("legacy database was modified: data=%q err=%v", legacy, err)
	}
	if _, err := os.Stat(filepath.Join(result.BackupDir, "hermes", "mika", "config.yaml")); err != nil {
		t.Fatalf("root config missing from snapshot: %v", err)
	}
	if archived, err := os.ReadFile(filepath.Join(result.BackupDir, "hermes", "mika", "memory_store.db")); err != nil || string(archived) != "legacy-data" {
		t.Fatalf("legacy database snapshot = %q, err=%v", archived, err)
	}

	if _, err := UpdateProfileSettings(UpdateProfileSettingsOptions{
		HermesHome: hermesHome, DataDir: dataDir, RootAgent: "mika",
		Settings: ProfileSettings{
			AgentID: "mika", DefaultProject: "cortex", DefaultDomain: "orchestration",
			AutoCaptureEnabled: false, AutoCaptureEveryTurns: 10, AutoCaptureMaxChars: 2000,
			PrefetchTokenBudget: 500, RecallTokenBudget: 900,
		},
	}); err != nil {
		t.Fatalf("configure connector before replay: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(hermesHome, "cortex.json"))
	if err != nil {
		t.Fatalf("read connector config before replay: %v", err)
	}
	if _, err := Sync(SyncOptions{
		HermesHome: hermesHome,
		DataDir:    dataDir,
		ServerURL:  "http://127.0.0.1:7777",
		RootAgent:  "mika",
		Activate:   true,
	}); err != nil {
		t.Fatalf("repeat sync: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(hermesHome, "cortex.json"))
	if err != nil {
		t.Fatalf("read connector config after replay: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("repeat sync rotated an already-valid connector token")
	}
}

func TestSyncRollsBackEveryProfileAndCortexConfigOnLateFailure(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "cortex-data")
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex: %v", err)
	}
	beforeCortex, err := os.ReadFile(filepath.Join(dataDir, config.FileName))
	if err != nil {
		t.Fatalf("read Cortex config: %v", err)
	}
	hermesHome := filepath.Join(t.TempDir(), "hermes")
	lateHome := filepath.Join(hermesHome, "profiles", "zeta")
	for _, home := range []string{hermesHome, lateHome} {
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatalf("create profile: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("memory:\n  provider: holographic\n"), 0o600); err != nil {
			t.Fatalf("write profile config: %v", err)
		}
	}
	// A directory at the connector config path fails only after the root profile was staged.
	if err := os.Mkdir(filepath.Join(lateHome, "cortex.json"), 0o700); err != nil {
		t.Fatalf("create late failure fixture: %v", err)
	}

	result, err := Sync(SyncOptions{
		HermesHome: hermesHome,
		DataDir:    dataDir,
		ServerURL:  "http://127.0.0.1:7777",
		RootAgent:  "mika",
		Activate:   true,
	})
	if err == nil {
		t.Fatal("sync succeeded despite an unwritable connector config target")
	}
	if result.BackupDir == "" {
		t.Fatal("failed sync did not preserve its rollback snapshot path")
	}
	afterCortex, readErr := os.ReadFile(filepath.Join(dataDir, config.FileName))
	if readErr != nil {
		t.Fatalf("read restored Cortex config: %v", readErr)
	}
	if string(afterCortex) != string(beforeCortex) {
		t.Fatal("failed sync did not restore Cortex credentials")
	}
	assertProvider(t, filepath.Join(hermesHome, "config.yaml"), "holographic")
	if _, statErr := os.Stat(filepath.Join(hermesHome, "cortex.json")); !os.IsNotExist(statErr) {
		t.Fatalf("root connector config survived rollback: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(hermesHome, "plugins", "cortex")); !os.IsNotExist(statErr) {
		t.Fatalf("root connector plugin survived rollback: %v", statErr)
	}
	info, statErr := os.Stat(filepath.Join(lateHome, "cortex.json"))
	if statErr != nil || !info.IsDir() {
		t.Fatalf("late profile fixture was not restored: info=%v err=%v", info, statErr)
	}
}

func assertProvider(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Hermes config %s: %v", path, err)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode Hermes config %s: %v", path, err)
	}
	memory, ok := decoded["memory"].(map[string]any)
	if !ok || memory["provider"] != want {
		t.Fatalf("memory provider in %s = %#v, want %q", path, decoded["memory"], want)
	}
}

func assertLegacyProviderConfigRemoved(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Hermes config %s: %v", path, err)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode Hermes config %s: %v", path, err)
	}
	plugins, ok := decoded["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("plugins in %s = %#v", path, decoded["plugins"])
	}
	if _, exists := plugins["hermes-memory-store"]; exists {
		t.Fatalf("legacy hermes-memory-store config remains in %s", path)
	}
	if _, exists := plugins["keep-plugin"]; !exists {
		t.Fatalf("unrelated plugin config was removed from %s", path)
	}
}
