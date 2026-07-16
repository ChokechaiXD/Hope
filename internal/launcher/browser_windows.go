//go:build windows

package launcher

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

func Open(_ context.Context, rawURL string) error {
	if err := validateDashboardURL(rawURL); err != nil {
		return err
	}
	command := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x08000000}
	if err := command.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	return command.Process.Release()
}
