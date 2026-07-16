//go:build windows

package autostart

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestInstallCopiesBinaryAndRegistersUserLogonEntry(t *testing.T) {
	t.Parallel()

	source := filepath.Join(t.TempDir(), "cortex-source.exe")
	if err := os.WriteFile(source, []byte("cortex-binary"), 0o700); err != nil {
		t.Fatalf("write source binary: %v", err)
	}
	dataDir := t.TempDir()
	var commands []recordedCommand
	controller := newController(
		func() (string, error) { return source, nil },
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, recordedCommand{name: name, args: slices.Clone(args)})
			return []byte("SUCCESS"), nil
		},
		func(string, ...string) error { return nil },
	)

	result, err := controller.Install(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("install autostart: %v", err)
	}
	wantExecutable := filepath.Join(dataDir, "bin", "cortex.exe")
	if result.Executable != wantExecutable || result.EntryName != EntryName {
		t.Fatalf("install result = %#v", result)
	}
	installed, err := os.ReadFile(wantExecutable)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(installed) != "cortex-binary" {
		t.Fatalf("installed binary = %q", installed)
	}
	if len(commands) != 1 || commands[0].name != "reg.exe" {
		t.Fatalf("commands = %#v", commands)
	}
	wantArgs := []string{
		"ADD", RunKey, "/V", EntryName, "/T", "REG_SZ", "/D", startupCommand(wantExecutable, dataDir), "/F",
	}
	if !slices.Equal(commands[0].args, wantArgs) {
		t.Fatalf("registry args = %#v, want %#v", commands[0].args, wantArgs)
	}
}

type recordedCommand struct {
	name string
	args []string
}

func TestStartRunsInstalledCortexProcess(t *testing.T) {
	t.Parallel()

	var command recordedCommand
	dataDir := t.TempDir()
	controller := newController(nil, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("start should not invoke the registry command runner")
		return nil, nil
	}, func(name string, args ...string) error {
		command = recordedCommand{name: name, args: slices.Clone(args)}
		return nil
	})
	output, err := controller.Start(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("start autostart task: %v", err)
	}
	wantExecutable := filepath.Join(dataDir, "bin", "cortex.exe")
	if output == "" || command.name != wantExecutable ||
		!slices.Equal(command.args, []string{"serve", "--data-dir", dataDir}) {
		t.Fatalf("output=%q command=%#v", output, command)
	}
}

func TestStatusQueriesRegisteredCortexEntry(t *testing.T) {
	t.Parallel()

	var command recordedCommand
	controller := newController(nil, func(_ context.Context, name string, args ...string) ([]byte, error) {
		command = recordedCommand{name: name, args: slices.Clone(args)}
		return []byte("Status: Running"), nil
	}, nil)
	output, err := controller.Status(context.Background())
	if err != nil {
		t.Fatalf("query autostart task: %v", err)
	}
	if output != "Status: Running" || command.name != "reg.exe" ||
		!slices.Equal(command.args, []string{"QUERY", RunKey, "/V", EntryName}) {
		t.Fatalf("output=%q command=%#v", output, command)
	}
}

func TestUninstallDeletesOnlyRegisteredCortexEntry(t *testing.T) {
	t.Parallel()

	var command recordedCommand
	controller := newController(nil, func(_ context.Context, name string, args ...string) ([]byte, error) {
		command = recordedCommand{name: name, args: slices.Clone(args)}
		return []byte("SUCCESS"), nil
	}, nil)
	if err := controller.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall autostart task: %v", err)
	}
	if command.name != "reg.exe" ||
		!slices.Equal(command.args, []string{"DELETE", RunKey, "/V", EntryName, "/F"}) {
		t.Fatalf("command=%#v", command)
	}
}
