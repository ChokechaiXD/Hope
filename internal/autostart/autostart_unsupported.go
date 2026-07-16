//go:build !windows

package autostart

import (
	"context"
	"fmt"
)

const EntryName = "Cortex Memory Hub"

type InstallResult struct {
	EntryName  string
	Executable string
}

type Controller struct{}

func New() *Controller {
	return &Controller{}
}

func (*Controller) Install(context.Context, string) (InstallResult, error) {
	return InstallResult{}, fmt.Errorf("Cortex autostart is currently supported on Windows only")
}

func (*Controller) Start(context.Context, string) (string, error) {
	return "", fmt.Errorf("Cortex autostart is currently supported on Windows only")
}

func (*Controller) Status(context.Context) (string, error) {
	return "", fmt.Errorf("Cortex autostart is currently supported on Windows only")
}

func (*Controller) Uninstall(context.Context) error {
	return fmt.Errorf("Cortex autostart is currently supported on Windows only")
}
