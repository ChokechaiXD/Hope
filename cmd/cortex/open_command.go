package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"cortex.local/cortex/internal/autostart"
	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/launcher"
)

type serviceStarter interface {
	Start(context.Context, string) (string, error)
}

type dashboardOpener func(context.Context, string) error

func runOpen(args []string, stdout, stderr io.Writer) int {
	return runOpenWithDependencies(
		args, stdout, stderr, autostart.New(), defaultReadinessChecker(), launcher.Open,
	)
}

func runOpenWithDependencies(
	args []string,
	stdout, stderr io.Writer,
	starter serviceStarter,
	readiness readinessChecker,
	openURL dashboardOpener,
) int {
	flags := flag.NewFlagSet("open", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Cortex data directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: cortex open [--data-dir DIR]")
		return 2
	}
	file, err := config.Load(*dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "open Cortex dashboard: %v\n", err)
		return 1
	}
	ctx := context.Background()
	if readiness.probe(ctx, file.Listen) != nil {
		// ponytail: the loopback listener remains the single-instance lock;
		// a second launch can exit on bind without another resident process.
		if _, err := starter.Start(ctx, *dataDir); err != nil {
			fmt.Fprintf(stderr, "open Cortex dashboard: start service: %v\n", err)
			return 1
		}
		if err := readiness.wait(ctx, file.Listen); err != nil {
			fmt.Fprintf(stderr, "open Cortex dashboard: health check failed: %v\n", err)
			return 1
		}
	}
	dashboardURL := "http://" + file.Listen + "/"
	if err := openURL(ctx, dashboardURL); err != nil {
		fmt.Fprintf(stderr, "open Cortex dashboard: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "opened=%s\n", dashboardURL)
	return 0
}
