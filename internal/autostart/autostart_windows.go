//go:build windows

package autostart

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
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
	return strings.TrimSpace(string(output)), nil
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
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create Cortex binary directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".cortex-*.tmp")
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
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace Cortex executable: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("install Cortex executable: %w", err)
	}
	return nil
}

func commandError(operation string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w: %s", operation, err, detail)
}
