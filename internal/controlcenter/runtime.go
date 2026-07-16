package controlcenter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cortex.local/cortex/internal/hermes"
)

type Action string

const (
	ActionRestart Action = "restart"
	ActionStop    Action = "stop"
)

var (
	ErrInvalidAction        = errors.New("invalid runtime action")
	ErrActionPending        = errors.New("runtime action already pending")
	ErrInvalidAgentSettings = hermes.ErrInvalidProfileSettings
)

type Status struct {
	Running bool
	Version string
	Listen  string
	Port    int
	PID     int
	DataDir string
	Uptime  time.Duration
	Pending Action
	Syncing bool
}

type SyncResult struct {
	Agents    []string
	BackupDir string
}

type AgentSettings = hermes.ProfileSettings
type AgentSettingsResult = hermes.UpdateProfileSettingsResult

type syncHermes func(context.Context) (SyncResult, error)

type Runtime struct {
	version   string
	listen    string
	port      int
	dataDir   string
	startedAt time.Time
	actions   chan Action
	mu        sync.Mutex
	pending   Action
	syncing   bool
	sync      syncHermes
}

func NewRuntime(version, listen, dataDir string) *Runtime {
	hermesHome := defaultHermesHome()
	return newRuntime(version, listen, dataDir, func(ctx context.Context) (SyncResult, error) {
		result, err := hermes.Sync(hermes.SyncOptions{
			HermesHome: hermesHome, DataDir: dataDir, ServerURL: "http://" + listen,
			RootAgent: "mika", Activate: true,
		})
		if err != nil {
			return SyncResult{}, err
		}
		agents := make([]string, 0, len(result.Profiles))
		for _, profile := range result.Profiles {
			agents = append(agents, profile.AgentID)
		}
		return SyncResult{Agents: agents, BackupDir: result.BackupDir}, nil
	})
}

func newRuntime(version, listen, dataDir string, syncer syncHermes) *Runtime {
	_, portText, _ := net.SplitHostPort(listen)
	port, _ := strconv.Atoi(portText)
	return &Runtime{
		version: version, listen: listen, port: port, dataDir: dataDir,
		startedAt: time.Now(), actions: make(chan Action, 1), sync: syncer,
	}
}

func (runtime *Runtime) Status(context.Context) (Status, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return Status{
		Running: true, Version: runtime.version, Listen: runtime.listen, Port: runtime.port,
		PID: os.Getpid(), DataDir: runtime.dataDir, Uptime: time.Since(runtime.startedAt),
		Pending: runtime.pending, Syncing: runtime.syncing,
	}, nil
}

func (runtime *Runtime) Request(action Action) error {
	if action != ActionRestart && action != ActionStop {
		return fmt.Errorf("%w: %q", ErrInvalidAction, action)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.pending != "" || runtime.syncing {
		return ErrActionPending
	}
	runtime.pending = action
	// ponytail: a short delay lets the browser receive the action page before
	// the listener closes; no client-side JavaScript or second control process.
	time.AfterFunc(150*time.Millisecond, func() { runtime.actions <- action })
	return nil
}

func (runtime *Runtime) SyncHermes(ctx context.Context) (SyncResult, error) {
	if err := runtime.beginHermesWrite(); err != nil {
		return SyncResult{}, ErrActionPending
	}
	defer runtime.endHermesWrite()
	if runtime.sync == nil {
		return SyncResult{}, fmt.Errorf("Hermes sync is unavailable")
	}
	return runtime.sync(ctx)
}

func (runtime *Runtime) AgentSettings(context.Context) ([]AgentSettings, error) {
	return hermes.ListProfileSettings(defaultHermesHome(), "mika")
}

func (runtime *Runtime) UpdateAgentSettings(
	_ context.Context,
	settings AgentSettings,
) (AgentSettingsResult, error) {
	if err := runtime.beginHermesWrite(); err != nil {
		return AgentSettingsResult{}, err
	}
	defer runtime.endHermesWrite()
	return hermes.UpdateProfileSettings(hermes.UpdateProfileSettingsOptions{
		HermesHome: defaultHermesHome(), DataDir: runtime.dataDir, RootAgent: "mika", Settings: settings,
	})
}

func (runtime *Runtime) beginHermesWrite() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.pending != "" || runtime.syncing {
		return ErrActionPending
	}
	runtime.syncing = true
	return nil
}

func (runtime *Runtime) endHermesWrite() {
	runtime.mu.Lock()
	runtime.syncing = false
	runtime.mu.Unlock()
}

func (runtime *Runtime) Next(ctx context.Context) (Action, error) {
	select {
	case action := <-runtime.actions:
		return action, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// MarkReady clears a restart only after the replacement listener owns the
// configured port. This keeps duplicate clicks from scheduling extra cycles.
func (runtime *Runtime) MarkReady() {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.pending == ActionRestart {
		runtime.pending = ""
	}
}

func defaultHermesHome() string {
	if configured := strings.TrimSpace(os.Getenv("HERMES_HOME")); configured != "" {
		return configured
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "hermes")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "hermes"
	}
	return filepath.Join(configDir, "hermes")
}
