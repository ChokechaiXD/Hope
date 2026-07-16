package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeAndAddAgent(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	created, mikaToken, err := Initialize(dataDir, "mika", "127.0.0.1:7777")
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	if mikaToken == "" || !created.IsAdmin("mika") {
		t.Fatalf("initial config = %#v, token empty=%v", created, mikaToken == "")
	}
	if agentID, ok := created.Authenticate(mikaToken); !ok || agentID != "mika" {
		t.Fatalf("authenticate initial token = %q, %v", agentID, ok)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), mikaToken) {
		t.Fatal("config persisted the raw bearer token")
	}

	solaToken, err := AddAgent(dataDir, "sola", false)
	if err != nil {
		t.Fatalf("add agent: %v", err)
	}
	loaded, err := Load(dataDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if agentID, ok := loaded.Authenticate(solaToken); !ok || agentID != "sola" {
		t.Fatalf("authenticate added token = %q, %v", agentID, ok)
	}
	if _, err := AddAgent(dataDir, "sola", false); err == nil {
		t.Fatal("adding duplicate agent succeeded")
	}
	secondMikaToken, err := IssueToken(dataDir, "mika")
	if err != nil {
		t.Fatalf("issue additional token: %v", err)
	}
	loaded, err = Load(dataDir)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if agentID, ok := loaded.Authenticate(secondMikaToken); !ok || agentID != "mika" {
		t.Fatalf("additional token authenticated as %q, %v", agentID, ok)
	}
}

func TestInitializeRejectsNonLoopbackListenAddress(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"0.0.0.0:7777", "192.168.1.20:7777", ":7777", "localhost"} {
		address := address
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			if _, _, err := Initialize(t.TempDir(), "mika", address); err == nil {
				t.Fatalf("Initialize accepted non-local or malformed listen address %q", address)
			}
		})
	}
}

func TestLoadRejectsNonLoopbackListenAddress(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	created, _, err := Initialize(dataDir, "mika", "127.0.0.1:7777")
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	created.Listen = "0.0.0.0:7777"
	raw, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("encode invalid config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, FileName), raw, 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	if _, err := Load(dataDir); err == nil {
		t.Fatal("Load accepted a public listen address")
	}
}

func TestValidateListenAcceptsLoopbackIPv4IPv6AndLocalhost(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"127.0.0.1:7777", "[::1]:7777", "localhost:7777"} {
		if err := ValidateListen(address); err != nil {
			t.Errorf("ValidateListen(%q): %v", address, err)
		}
	}
}

func TestInitializeRefusesExistingConfig(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("first initialize: %v", err)
	}
	if _, _, err := Initialize(dataDir, "mika", "127.0.0.1:7777"); err == nil {
		t.Fatal("second initialize overwrote existing config")
	}
}

func TestReloadingAuthenticatorAcceptsNewAgentWithoutRestart(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	_, initialToken, err := Initialize(dataDir, "mika", "127.0.0.1:7777")
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	auth, err := NewReloadingAuthenticator(dataDir)
	if err != nil {
		t.Fatalf("create reloading authenticator: %v", err)
	}
	if agentID, ok := auth.Authenticate(initialToken); !ok || agentID != "mika" {
		t.Fatalf("authenticate initial agent = %q, %v", agentID, ok)
	}
	solaToken, err := AddAgent(dataDir, "sola", false)
	if err != nil {
		t.Fatalf("add agent: %v", err)
	}
	if agentID, ok := auth.Authenticate(solaToken); !ok || agentID != "sola" {
		t.Fatalf("authenticate newly added agent = %q, %v", agentID, ok)
	}
}

func TestDashboardPINIsHashedAndCannotAuthenticateAgentAPI(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	_, _, err := Initialize(dataDir, "mika", "127.0.0.1:7777")
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	if err := SetDashboardPIN(dataDir, "4826"); err != nil {
		t.Fatalf("set dashboard PIN: %v", err)
	}
	loaded, err := Load(dataDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if agentID, ok := loaded.AuthenticateDashboard("4826"); !ok || agentID != "mika" {
		t.Fatalf("dashboard PIN authenticated as %q, %v", agentID, ok)
	}
	if _, ok := loaded.Authenticate("4826"); ok {
		t.Fatal("dashboard PIN authenticated as an agent bearer token")
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "4826") {
		t.Fatal("config persisted the raw dashboard PIN")
	}
}

func TestDashboardPINValidation(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	for _, pin := range []string{"", "123", "123456789", "abcd", "12 34"} {
		if err := SetDashboardPIN(dataDir, pin); err == nil {
			t.Errorf("SetDashboardPIN accepted %q", pin)
		}
	}
}
