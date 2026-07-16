package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cortex.local/cortex/internal/autostart"
	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/controlcenter"
	_ "modernc.org/sqlite"
)

func TestInitAndAgentAddCommands(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--data-dir", dataDir, "--admin", "mika"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init exit = %d, stderr=%s", code, stderr.String())
	}
	mikaToken := outputValue(stdout.String(), "token")
	if mikaToken == "" {
		t.Fatalf("init output omitted token: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"agent", "add", "--data-dir", dataDir, "--id", "sola"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent add exit = %d, stderr=%s", code, stderr.String())
	}
	solaToken := outputValue(stdout.String(), "token")
	loaded, err := config.Load(dataDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if agentID, ok := loaded.Authenticate(solaToken); !ok || agentID != "sola" {
		t.Fatalf("new token authenticated as %q, %v", agentID, ok)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"agent", "token", "--data-dir", dataDir, "--id", "mika"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent token exit = %d, stderr=%s", code, stderr.String())
	}
	issuedToken := outputValue(stdout.String(), "token")
	loaded, err = config.Load(dataDir)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if agentID, ok := loaded.Authenticate(issuedToken); !ok || agentID != "mika" {
		t.Fatalf("issued token authenticated as %q, %v", agentID, ok)
	}
}

func TestImportHolographicCommand(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "cortex")
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex: %v", err)
	}
	legacyPath := filepath.Join(t.TempDir(), "memory_store.db")
	db, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE facts (
  fact_id INTEGER PRIMARY KEY, content TEXT, category TEXT, tags TEXT,
  trust_score REAL, retrieval_count INTEGER, helpful_count INTEGER,
  created_at TIMESTAMP, updated_at TIMESTAMP
);
INSERT INTO facts VALUES (1, 'Use backups', 'project', 'backup', 0.8, 2, 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatalf("create legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"import", "holographic",
		"--database", legacyPath,
		"--agent", "sola",
		"--project", "novelclaw",
		"--data-dir", dataDir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("import exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "imported=1") || !strings.Contains(stdout.String(), "replayed=0") {
		t.Fatalf("import output = %s", stdout.String())
	}
}

func TestConnectorSyncHermesCommand(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "cortex")
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex: %v", err)
	}
	hermesHome := filepath.Join(t.TempDir(), "hermes")
	if err := os.MkdirAll(filepath.Join(hermesHome, "profiles", "sola"), 0o700); err != nil {
		t.Fatalf("create Hermes profile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"connector", "sync", "hermes",
		"--home", hermesHome,
		"--data-dir", dataDir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("connector sync exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "profile=sola") {
		t.Fatalf("connector sync output = %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(hermesHome, "profiles", "sola", "plugins", "cortex", "__init__.py")); err != nil {
		t.Fatalf("connector not installed: %v", err)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code == 0 {
		t.Fatalf("unknown command succeeded: stdout=%s", stdout.String())
	}
}

func TestServeControlLoopReopensOnlyForRestart(t *testing.T) {
	t.Parallel()

	cycles := 0
	err := runServeControlLoop(func() (controlcenter.Action, error) {
		cycles++
		if cycles == 1 {
			return controlcenter.ActionRestart, nil
		}
		return controlcenter.ActionStop, nil
	})
	if err != nil || cycles != 2 {
		t.Fatalf("control loop cycles=%d err=%v", cycles, err)
	}
}

func TestServiceInstallCommandUsesConfiguredDataDirectory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	var stdout, stderr bytes.Buffer
	code := runService([]string{"install", "--data-dir", dataDir}, &stdout, &stderr, controller)
	if code != 0 {
		t.Fatalf("service install exit=%d stderr=%s", code, stderr.String())
	}
	if controller.installedDataDir != dataDir || !strings.Contains(stdout.String(), "entry=Cortex Memory Hub") {
		t.Fatalf("data_dir=%q stdout=%s", controller.installedDataDir, stdout.String())
	}
}

func TestServiceStartWaitsForHealthAndRejectsTrailingArguments(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	checker := readinessChecker{
		probe:    func(context.Context, string) error { return errors.New("not ready") },
		timeout:  1,
		interval: 0,
	}
	var stdout, stderr bytes.Buffer
	code := runServiceWithReadiness([]string{"start", "--data-dir", dataDir}, &stdout, &stderr, controller, checker)
	if code != 1 || controller.startedDataDir != dataDir || !strings.Contains(stderr.String(), "health check failed") {
		t.Fatalf("service start exit=%d started=%q stderr=%s", code, controller.startedDataDir, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	controller.startedDataDir = ""
	code = runServiceWithReadiness([]string{"start", "--data-dir", dataDir, "ignored"}, &stdout, &stderr, controller, checker)
	if code != 2 || controller.startedDataDir != "" {
		t.Fatalf("trailing argument exit=%d started=%q", code, controller.startedDataDir)
	}
}

func TestServiceStartReportsReadyOnlyAfterHealthSucceeds(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	probes := 0
	checker := readinessChecker{
		probe: func(context.Context, string) error {
			probes++
			if probes < 3 {
				return errors.New("not ready")
			}
			return nil
		},
		timeout:  100 * time.Millisecond,
		interval: time.Millisecond,
	}
	var stdout, stderr bytes.Buffer
	code := runServiceWithReadiness([]string{"start", "--data-dir", dataDir}, &stdout, &stderr, controller, checker)
	if code != 0 || controller.startedDataDir != dataDir || !strings.Contains(stdout.String(), "started") {
		t.Fatalf("service start exit=%d started=%q stdout=%s stderr=%s", code, controller.startedDataDir, stdout.String(), stderr.String())
	}
}

func TestServiceStartDoesNotSpawnDuplicateWhenAlreadyHealthy(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	checker := readinessChecker{probe: func(context.Context, string) error { return nil }}
	var stdout, stderr bytes.Buffer
	code := runServiceWithReadiness([]string{"start", "--data-dir", dataDir}, &stdout, &stderr, controller, checker)
	if code != 0 || controller.startedDataDir != "" || !strings.Contains(stdout.String(), "already healthy") {
		t.Fatalf("service start exit=%d started=%q stdout=%s stderr=%s", code, controller.startedDataDir, stdout.String(), stderr.String())
	}
}

func TestOpenDashboardStartsOnlyWhenNeededAndOpensConfiguredLoopbackURL(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	probes := 0
	checker := readinessChecker{
		probe: func(context.Context, string) error {
			probes++
			if probes == 1 {
				return errors.New("not running")
			}
			return nil
		},
		timeout: 100 * time.Millisecond, interval: time.Millisecond,
	}
	var opened string
	openURL := func(_ context.Context, rawURL string) error {
		opened = rawURL
		return nil
	}
	issueSession := func(context.Context, string, string) (string, error) {
		return "http://127.0.0.1:7777/ui/session?code=once", nil
	}
	var stdout, stderr bytes.Buffer
	code := runOpenWithDependencies(
		[]string{"--data-dir", dataDir}, &stdout, &stderr, controller, checker, issueSession, openURL,
	)
	if code != 0 || controller.startedDataDir != dataDir || opened != "http://127.0.0.1:7777/ui/session?code=once" {
		t.Fatalf("open exit=%d started=%q opened=%q stdout=%s stderr=%s", code, controller.startedDataDir, opened, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "code=once") {
		t.Fatalf("open output leaked one-time dashboard code: %s", stdout.String())
	}
}

func TestOpenDashboardDoesNotSpawnDuplicateOrAcceptTrailingArguments(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	checker := readinessChecker{probe: func(context.Context, string) error { return nil }}
	opened := 0
	openURL := func(context.Context, string) error { opened++; return nil }
	issueSession := func(context.Context, string, string) (string, error) {
		return "http://127.0.0.1:7777/ui/session?code=once", nil
	}
	var stdout, stderr bytes.Buffer
	code := runOpenWithDependencies(
		[]string{"--data-dir", dataDir}, &stdout, &stderr, controller, checker, issueSession, openURL,
	)
	if code != 0 || controller.startedDataDir != "" || opened != 1 {
		t.Fatalf("healthy open exit=%d started=%q opened=%d stderr=%s", code, controller.startedDataDir, opened, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	opened = 0
	code = runOpenWithDependencies(
		[]string{"--data-dir", dataDir, "ignored"}, &stdout, &stderr, controller, checker, issueSession, openURL,
	)
	if code != 2 || opened != 0 {
		t.Fatalf("trailing argument exit=%d opened=%d stderr=%s", code, opened, stderr.String())
	}
}

func TestServiceInstallRejectsTrailingArguments(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	controller := &fakeServiceController{}
	var stdout, stderr bytes.Buffer
	code := runService([]string{"install", "--data-dir", dataDir, "ignored"}, &stdout, &stderr, controller)
	if code != 2 || controller.installedDataDir != "" {
		t.Fatalf("service install trailing argument exit=%d installed=%q", code, controller.installedDataDir)
	}
}

func TestAgentCommandRejectsTrailingArgumentsBeforeIssuingToken(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if _, _, err := config.Initialize(dataDir, "mika", "127.0.0.1:7777"); err != nil {
		t.Fatalf("initialize Cortex config: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dataDir, config.FileName))
	if err != nil {
		t.Fatalf("read initial config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runAgent([]string{"token", "--data-dir", dataDir, "--id", "mika", "ignored"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("agent token trailing argument exit=%d stderr=%s", code, stderr.String())
	}
	after, err := os.ReadFile(filepath.Join(dataDir, config.FileName))
	if err != nil {
		t.Fatalf("read final config: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected agent command still issued a credential")
	}
}

type fakeServiceController struct {
	installedDataDir string
	startedDataDir   string
}

func (controller *fakeServiceController) Install(_ context.Context, dataDir string) (autostart.InstallResult, error) {
	controller.installedDataDir = dataDir
	return autostart.InstallResult{
		EntryName: autostart.EntryName, Executable: filepath.Join(dataDir, "bin", "cortex.exe"),
		Shortcut: autostart.ShortcutName,
	}, nil
}

func (controller *fakeServiceController) Start(_ context.Context, dataDir string) (string, error) {
	controller.startedDataDir = dataDir
	return "started", nil
}

func (controller *fakeServiceController) Status(context.Context) (string, error) {
	return "status", nil
}

func (controller *fakeServiceController) Uninstall(context.Context) error {
	return nil
}

func outputValue(output, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
