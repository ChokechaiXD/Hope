//go:build windows

package autostart

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

const (
	EntryName = "Cortex Memory Hub"
	RunKey    = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
)

type InstallResult struct {
	EntryName  string
	Executable string
}

type Controller struct {
	executable func() (string, error)
	run        func(context.Context, string, ...string) ([]byte, error)
	start      func(string, ...string) error
}

func New() *Controller {
	return newController(os.Executable, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}, startDetached)
}

func newController(
	executable func() (string, error),
	run func(context.Context, string, ...string) ([]byte, error),
	start func(string, ...string) error,
) *Controller {
	return &Controller{executable: executable, run: run, start: start}
}

func (controller *Controller) Install(ctx context.Context, dataDir string) (InstallResult, error) {
	dataDir, err := cleanDataDir(dataDir)
	if err != nil {
		return InstallResult{}, err
	}
	source, err := controller.executable()
	if err != nil {
		return InstallResult{}, fmt.Errorf("resolve Cortex executable: %w", err)
	}
	destination := filepath.Join(dataDir, "bin", "cortex.exe")
	if err := installExecutable(source, destination); err != nil {
		return InstallResult{}, err
	}
	action := startupCommand(destination, dataDir)
	output, err := controller.run(ctx, "reg.exe",
		"ADD", RunKey, "/V", EntryName, "/T", "REG_SZ", "/D", action, "/F",
	)
	if err != nil {
		return InstallResult{}, commandError("register Cortex autostart", output, err)
	}
	return InstallResult{EntryName: EntryName, Executable: destination}, nil
}

func (controller *Controller) Start(_ context.Context, dataDir string) (string, error) {
	dataDir, err := cleanDataDir(dataDir)
	if err != nil {
		return "", err
	}
	executable := filepath.Join(dataDir, "bin", "cortex.exe")
	if err := controller.start(executable, "serve", "--data-dir", dataDir); err != nil {
		return "", fmt.Errorf("start Cortex process: %w", err)
	}
	return "started Cortex from " + executable, nil
}

func (controller *Controller) Status(ctx context.Context) (string, error) {
	output, err := controller.run(ctx, "reg.exe", "QUERY", RunKey, "/V", EntryName)
	if err != nil {
		return "", commandError("query Cortex autostart", output, err)
	}
	return "autostart=registered\nregistry=" + strings.TrimSpace(string(output)), nil
}

func (controller *Controller) Uninstall(ctx context.Context) error {
	output, err := controller.run(ctx, "reg.exe", "DELETE", RunKey, "/V", EntryName, "/F")
	if err != nil {
		return commandError("remove Cortex autostart", output, err)
	}
	return nil
}

func startupCommand(executable, dataDir string) string {
	script := fmt.Sprintf(
		"Start-Process -WindowStyle Hidden -FilePath %s -ArgumentList @('serve','--data-dir',%s)",
		powerShellLiteral(executable), powerShellLiteral(dataDir),
	)
	encoded := utf16.Encode([]rune(script))
	raw := make([]byte, len(encoded)*2)
	for index, value := range encoded {
		binary.LittleEndian.PutUint16(raw[index*2:], value)
	}
	return "powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand " +
		base64.StdEncoding.EncodeToString(raw)
}

func powerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func startDetached(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x08000000}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func cleanDataDir(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("Cortex data directory is required")
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve Cortex data directory: %w", err)
	}
	if strings.ContainsRune(abs, '"') {
		return "", fmt.Errorf("Cortex data directory cannot contain a quote")
	}
	return filepath.Clean(abs), nil
}

func commandError(operation string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w: %s", operation, err, detail)
}
