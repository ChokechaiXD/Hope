package hermes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProfileSettingsUpdateIsAtomicAndPreservesCredentials(t *testing.T) {
	t.Parallel()

	hermesHome := filepath.Join(t.TempDir(), "hermes")
	profileHome := filepath.Join(hermesHome, "profiles", "sora")
	for _, home := range []string{hermesHome, profileHome} {
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatalf("create profile: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(profileHome, "cortex.json"), []byte(`{
  "url": "http://127.0.0.1:7777",
  "token": "keep-secret",
  "agent_id": "sora",
  "future_option": "keep-me"
}`), 0o600); err != nil {
		t.Fatalf("write connector config: %v", err)
	}

	result, err := UpdateProfileSettings(UpdateProfileSettingsOptions{
		HermesHome: hermesHome,
		DataDir:    filepath.Join(t.TempDir(), "cortex"),
		RootAgent:  "mika",
		Settings: ProfileSettings{
			AgentID: "sora", DefaultProject: "cortex", DefaultDomain: "coding",
			AutoCaptureEnabled: true, AutoCaptureEveryTurns: 10, AutoCaptureMaxChars: 1000,
			PrefetchTokenBudget: 500, RecallTokenBudget: 1200,
		},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if result.BackupFile == "" {
		t.Fatal("settings update did not create a backup")
	}
	if _, err := os.Stat(result.BackupFile); err != nil {
		t.Fatalf("settings backup missing: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(profileHome, "cortex.json"))
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	if document["token"] != "keep-secret" || document["future_option"] != "keep-me" {
		t.Fatalf("settings update discarded protected or future fields: %#v", document)
	}
	if document["default_project"] != "cortex" || document["default_domain"] != "coding" ||
		document["auto_capture_every_turns"] != float64(10) || document["prefetch_token_budget"] != float64(500) {
		t.Fatalf("settings were not written: %#v", document)
	}
}

func TestListProfileSettingsAppliesProviderDefaultsAndValidatesKeys(t *testing.T) {
	t.Parallel()

	hermesHome := filepath.Join(t.TempDir(), "hermes")
	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		t.Fatalf("create Hermes home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hermesHome, "cortex.json"), []byte(`{
  "url": "http://127.0.0.1:7777", "token": "secret", "agent_id": "mika"
}`), 0o600); err != nil {
		t.Fatalf("write connector config: %v", err)
	}

	settings, err := ListProfileSettings(hermesHome, "mika")
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	if len(settings) != 1 || !settings[0].AutoCaptureEnabled ||
		settings[0].AutoCaptureEveryTurns != 5 || settings[0].AutoCaptureMaxChars != 1000 ||
		settings[0].PrefetchTokenBudget != 700 || settings[0].RecallTokenBudget != 1200 {
		t.Fatalf("provider defaults = %#v", settings)
	}

	_, err = UpdateProfileSettings(UpdateProfileSettingsOptions{
		HermesHome: hermesHome, DataDir: t.TempDir(), RootAgent: "mika",
		Settings: ProfileSettings{
			AgentID: "mika", DefaultProject: "../../escape", AutoCaptureEnabled: true,
			AutoCaptureEveryTurns: 5, AutoCaptureMaxChars: 1000,
			PrefetchTokenBudget: 700, RecallTokenBudget: 1200,
		},
	})
	if err == nil {
		t.Fatal("invalid project key was accepted")
	}
}
