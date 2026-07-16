package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/httpapi"
)

func runServe(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Cortex data directory")
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
		fmt.Fprintf(stderr, "serve Cortex: %v\n", err)
		return 1
	}
	hub, err := cortex.Open(cortex.Config{DatabasePath: config.DatabasePath(*dataDir), AdminAgents: file.AdminAgents})
	if err != nil {
		fmt.Fprintf(stderr, "open Cortex: %v\n", err)
		return 1
	}
	defer hub.Close()
	authenticator, err := config.NewReloadingAuthenticator(*dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "initialize authenticator: %v\n", err)
		return 1
	}
	server := &http.Server{
		Addr: address, Handler: httpapi.New(hub, authenticator),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(stdout, "Cortex listening on http://%s\n", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "serve Cortex: %v\n", err)
		return 1
	}
	return 0
}
