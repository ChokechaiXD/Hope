package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"cortex.local/cortex/internal/autostart"
	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/launcher"
	"cortex.local/cortex/internal/localauth"
)

type serviceStarter interface {
	Start(context.Context, string) (string, error)
}

type dashboardOpener func(context.Context, string) error

type dashboardSessionIssuer func(context.Context, string, string) (string, error)

func runOpen(args []string, stdout, stderr io.Writer) int {
	return runOpenWithDependencies(
		args, stdout, stderr, autostart.New(), defaultReadinessChecker(),
		defaultDashboardSessionIssuer, launcher.Open,
	)
}

func runOpenWithDependencies(
	args []string,
	stdout, stderr io.Writer,
	starter serviceStarter,
	readiness readinessChecker,
	issueSession dashboardSessionIssuer,
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
	dashboardURL, err := issueSession(ctx, *dataDir, file.Listen)
	if err != nil {
		// ponytail: keep the manual token page as an emergency fallback for an older or damaged service.
		dashboardURL = "http://" + file.Listen + "/"
		fmt.Fprintf(stderr, "open Cortex dashboard: automatic sign-in unavailable: %v\n", err)
	}
	if err := openURL(ctx, dashboardURL); err != nil {
		fmt.Fprintf(stderr, "open Cortex dashboard: %v\n", err)
		return 1
	}
	// Do not persist the one-time dashboard code in console or launcher logs.
	fmt.Fprintf(stdout, "opened=http://%s/\n", file.Listen)
	return 0
}

func defaultDashboardSessionIssuer(ctx context.Context, dataDir, address string) (string, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	return localauth.RequestDashboardURL(ctx, client, dataDir, "http://"+address)
}
