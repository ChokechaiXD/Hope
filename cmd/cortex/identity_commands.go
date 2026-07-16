package main

import (
	"flag"
	"fmt"
	"io"

	"cortex.local/cortex/internal/config"
)

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Cortex data directory")
	admin := flags.String("admin", "mika", "initial administrator agent id")
	listen := flags.String("listen", "127.0.0.1:7777", "HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: cortex init [--data-dir DIR] [--admin AGENT] [--listen ADDRESS]")
		return 2
	}
	_, token, err := config.Initialize(*dataDir, *admin, *listen)
	if err != nil {
		fmt.Fprintf(stderr, "initialize Cortex: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "data_dir=%s\nagent=%s\ntoken=%s\n", *dataDir, *admin, token)
	return 0
}

func runAgent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: cortex agent add|token --id AGENT")
		return 2
	}
	flags := flag.NewFlagSet("agent "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Cortex data directory")
	agentID := flags.String("id", "", "agent id")
	admin := flags.Bool("admin", false, "grant review and governance permission")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: cortex agent add|token --id AGENT")
		return 2
	}
	var token string
	var err error
	switch args[0] {
	case "add":
		token, err = config.AddAgent(*dataDir, *agentID, *admin)
	case "token":
		if *admin {
			fmt.Fprintln(stderr, "--admin is valid only with agent add")
			return 2
		}
		token, err = config.IssueToken(*dataDir, *agentID)
	default:
		fmt.Fprintf(stderr, "unknown agent command %q\n", args[0])
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "agent %s: %v\n", args[0], err)
		return 1
	}
	fmt.Fprintf(stdout, "agent=%s\ntoken=%s\n", *agentID, token)
	return 0
}
