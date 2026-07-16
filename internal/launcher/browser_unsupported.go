//go:build !windows

package launcher

import (
	"context"
	"fmt"
)

func Open(context.Context, string) error {
	return fmt.Errorf("opening the Cortex dashboard is currently supported on Windows only")
}
