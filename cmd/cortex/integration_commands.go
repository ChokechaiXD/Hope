package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/hermes"
	holographicimport "cortex.local/cortex/internal/importer/holographic"
)

func runImport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "holographic" {
		fmt.Fprintln(stderr, "usage: cortex import holographic --database MEMORY_STORE_DB --agent AGENT")
		return 2
	}
	flags := flag.NewFlagSet("import holographic", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Hope HUB data directory")
	databasePath := flags.String("database", "", "Holographic memory_store.db path")
	agentID := flags.String("agent", "", "agent that owned the legacy database")
	project := flags.String("project", "", "project scope for legacy project facts")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: cortex import holographic --database MEMORY_STORE_DB --agent AGENT")
		return 2
	}
	file, err := config.Load(*dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	hub, err := cortex.Open(cortex.Config{DatabasePath: config.DatabasePath(*dataDir), AdminAgents: file.AdminAgents})
	if err != nil {
		fmt.Fprintf(stderr, "open Hope HUB: %v\n", err)
		return 1
	}
	defer hub.Close()
	result, err := holographicimport.Import(context.Background(), hub, holographicimport.Options{
		DatabasePath: *databasePath, AgentID: *agentID, Project: *project,
	})
	if err != nil {
		fmt.Fprintf(stderr, "import Holographic memory: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "imported=%d\nreplayed=%d\n", result.Imported, result.Replayed)
	return 0
}

func runConnector(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "sync" || args[1] != "hermes" {
		fmt.Fprintln(stderr, "usage: cortex connector sync hermes --home HERMES_HOME")
		return 2
	}
	flags := flag.NewFlagSet("connector sync hermes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	hermesHome := flags.String("home", "", "root Hermes home")
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Hope HUB data directory")
	serverURL := flags.String("url", "http://127.0.0.1:7777", "Hope HUB server URL")
	rootAgent := flags.String("root-agent", "mika", "agent id for the root Hermes profile")
	activate := flags.Bool("activate", true, "set memory.provider to cortex in every profile")
	if err := flags.Parse(args[2:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: cortex connector sync hermes --home HERMES_HOME")
		return 2
	}
	result, err := hermes.Sync(hermes.SyncOptions{
		HermesHome: *hermesHome, DataDir: *dataDir, ServerURL: *serverURL,
		RootAgent: *rootAgent, Activate: *activate,
	})
	if err != nil {
		fmt.Fprintf(stderr, "sync Hermes connector: %v\n", err)
		if result.BackupDir != "" {
			fmt.Fprintf(stderr, "backup=%s\n", result.BackupDir)
		}
		return 1
	}
	fmt.Fprintf(stdout, "backup=%s\n", result.BackupDir)
	for _, profile := range result.Profiles {
		fmt.Fprintf(stdout, "profile=%s\nhome=%s\n", profile.AgentID, profile.Home)
	}
	return 0
}
