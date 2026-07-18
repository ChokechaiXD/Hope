package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/controlcenter"
	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/hope"
	"cortex.local/cortex/internal/httpapi"
	"cortex.local/cortex/internal/intelligence"
	"cortex.local/cortex/internal/localauth"
)

func runServe(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Hope HUB data directory")
	listen := flags.String("listen", "", "override configured HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: cortex serve [--data-dir DIR] [--listen ADDRESS]")
		return 2
	}
	file, err := config.Load(*dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	address := file.Listen
	if *listen != "" {
		address = *listen
	}
	if err := config.ValidateListen(address); err != nil {
		fmt.Fprintf(stderr, "serve Hope HUB: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime := controlcenter.NewRuntime(version, address, *dataDir)
	if err := runServeControlLoop(func() (controlcenter.Action, error) {
		return serveCortexCycle(ctx, address, *dataDir, runtime, stdout)
	}); err != nil {
		fmt.Fprintf(stderr, "serve Hope HUB: %v\n", err)
		return 1
	}
	return 0
}

func runServeControlLoop(cycle func() (controlcenter.Action, error)) error {
	for {
		action, err := cycle()
		if err != nil {
			return err
		}
		if action != controlcenter.ActionRestart {
			return nil
		}
	}
}

func serveCortexCycle(
	ctx context.Context,
	address, dataDir string,
	runtime *controlcenter.Runtime,
	stdout io.Writer,
) (controlcenter.Action, error) {
	file, err := config.Load(dataDir)
	if err != nil {
		return controlcenter.ActionStop, fmt.Errorf("reload config: %w", err)
	}
	hub, err := cortex.Open(cortex.Config{DatabasePath: config.DatabasePath(dataDir), AdminAgents: file.AdminAgents})
	if err != nil {
		return controlcenter.ActionStop, fmt.Errorf("open Hope HUB: %w", err)
	}
	skillMem, err := hope.Open(config.HopeDatabasePath(dataDir), "")
	if err != nil {
		_ = hub.Close()
		return controlcenter.ActionStop, fmt.Errorf("open Hope HUB skill store: %w", err)
	}
	authenticator, err := config.NewReloadingAuthenticator(dataDir)
	if err != nil {
		_ = skillMem.Close()
		_ = hub.Close()
		return controlcenter.ActionStop, fmt.Errorf("initialize authenticator: %w", err)
	}
	if len(file.AdminAgents) == 0 {
		_ = skillMem.Close()
		_ = hub.Close()
		return controlcenter.ActionStop, fmt.Errorf("initialize dashboard launcher: no administrator is configured")
	}
	launcherKey, err := localauth.Ensure(dataDir)
	if err != nil {
		_ = skillMem.Close()
		_ = hub.Close()
		return controlcenter.ActionStop, fmt.Errorf("initialize dashboard launcher: %w", err)
	}
	dashboardLauncher, err := localauth.NewBroker(launcherKey, file.AdminAgents[0])
	if err != nil {
		_ = skillMem.Close()
		_ = hub.Close()
		return controlcenter.ActionStop, fmt.Errorf("initialize dashboard launcher: %w", err)
	}
	advisor := intelligence.NewClient()
	server := &http.Server{
		Addr: address, Handler: httpapi.NewWithSkillMem(
			hub, authenticator, runtime, dashboardLauncher, advisor, skillMem,
		),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		_ = skillMem.Close()
		_ = hub.Close()
		return controlcenter.ActionStop, fmt.Errorf("listen on %s: %w", address, err)
	}
	runtime.MarkReady()
	cycleCtx, cancelCycle := context.WithCancel(ctx)
	defer cancelCycle()
	actions := make(chan controlcenter.Action, 1)
	go func() {
		action, actionErr := runtime.Next(cycleCtx)
		if actionErr != nil {
			if ctx.Err() == nil {
				return
			}
			action = controlcenter.ActionStop
		}
		actions <- action
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(stdout, "Hope HUB listening on http://%s (memory kernel ready)\n", address)
	serveErr := server.Serve(listener)
	cancelCycle()
	hopeCloseErr := skillMem.Close()
	closeErr := hub.Close()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return controlcenter.ActionStop, serveErr
	}
	if closeErr != nil {
		return controlcenter.ActionStop, fmt.Errorf("close Hope HUB: %w", closeErr)
	}
	if hopeCloseErr != nil {
		return controlcenter.ActionStop, fmt.Errorf("close HOPE: %w", hopeCloseErr)
	}
	select {
	case action := <-actions:
		return action, nil
	default:
		return controlcenter.ActionStop, nil
	}
}
