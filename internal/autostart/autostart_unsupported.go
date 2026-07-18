//go:build !windows

package autostart

import (
	"context"
	"fmt"
)

const (
	EntryName    = "Hope HUB"
	ShortcutName = "Hope HUB Dashboard.lnk"
)

type InstallResult struct {
	EntryName  string
	Executable string
	Shortcut   string
}

type Controller struct{}

func New() *Controller {
	return &Controller{}
}

func (*Controller) Install(context.Context, string) (InstallResult, error) {
	return InstallResult{}, fmt.Errorf("Hope HUB autostart is currently supported on Windows only")
}

func (*Controller) Start(context.Context, string) (string, error) {
	return "", fmt.Errorf("Hope HUB autostart is currently supported on Windows only")
}

func (*Controller) Status(context.Context) (string, error) {
	return "", fmt.Errorf("Hope HUB autostart is currently supported on Windows only")
}

func (*Controller) Uninstall(context.Context) error {
	return fmt.Errorf("Hope HUB autostart is currently supported on Windows only")
}
