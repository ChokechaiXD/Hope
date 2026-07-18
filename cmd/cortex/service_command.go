package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"cortex.local/cortex/internal/autostart"
	"cortex.local/cortex/internal/config"
)

type serviceController interface {
	Install(context.Context, string) (autostart.InstallResult, error)
	Start(context.Context, string) (string, error)
	Status(context.Context) (string, error)
	Uninstall(context.Context) error
}

func runService(args []string, stdout, stderr io.Writer, controller serviceController) int {
	return runServiceWithReadiness(args, stdout, stderr, controller, defaultReadinessChecker())
}

func runServiceWithReadiness(
	args []string,
	stdout, stderr io.Writer,
	controller serviceController,
	readiness readinessChecker,
) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: cortex service install|start|status|uninstall")
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "install":
		flags := flag.NewFlagSet("service install", flag.ContinueOnError)
		flags.SetOutput(stderr)
		dataDir := flags.String("data-dir", config.DefaultDataDir(), "Hope HUB data directory")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: cortex service install [--data-dir DIR]")
			return 2
		}
		if _, err := config.Load(*dataDir); err != nil {
			fmt.Fprintf(stderr, "load Hope HUB config: %v\n", err)
			return 1
		}
		result, err := controller.Install(ctx, *dataDir)
		if err != nil {
			fmt.Fprintf(stderr, "install Hope HUB service: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "entry=%s\nexecutable=%s\nshortcut=%s\n", result.EntryName, result.Executable, result.Shortcut)
		return 0
	case "start":
		flags := flag.NewFlagSet("service start", flag.ContinueOnError)
		flags.SetOutput(stderr)
		dataDir := flags.String("data-dir", config.DefaultDataDir(), "Hope HUB data directory")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: cortex service start [--data-dir DIR]")
			return 2
		}
		file, err := config.Load(*dataDir)
		if err != nil {
			fmt.Fprintf(stderr, "load Hope HUB config: %v\n", err)
			return 1
		}
		if readiness.probe(ctx, file.Listen) == nil {
			fmt.Fprintf(stdout, "Hope HUB already healthy at http://%s\n", file.Listen)
			return 0
		}
		output, err := controller.Start(ctx, *dataDir)
		if err != nil {
			fmt.Fprintf(stderr, "start Hope HUB service: %v\n", err)
			return 1
		}
		if err := readiness.wait(ctx, file.Listen); err != nil {
			fmt.Fprintf(stderr, "start Hope HUB service: health check failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, output)
		return 0
	case "status":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: cortex service status")
			return 2
		}
		output, err := controller.Status(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "query Hope HUB service: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, output)
		return 0
	case "uninstall":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: cortex service uninstall")
			return 2
		}
		if err := controller.Uninstall(ctx); err != nil {
			fmt.Fprintf(stderr, "uninstall Hope HUB service: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "removed=%s\n", autostart.EntryName)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown service command %q\n", args[0])
		return 2
	}
}
