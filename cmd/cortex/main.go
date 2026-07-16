package main

import (
	"fmt"
	"io"
	"os"

	"cortex.local/cortex/internal/autostart"
)

const version = "0.3.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "agent":
		return runAgent(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "open":
		return runOpen(args[1:], stdout, stderr)
	case "service":
		return runService(args[1:], stdout, stderr, autostart.New())
	case "connector":
		return runConnector(args[1:], stdout, stderr)
	case "import":
		return runImport(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `Cortex - standalone local memory hub

Usage:
  cortex init [--data-dir DIR] [--admin AGENT] [--listen ADDRESS]
  cortex agent add --id AGENT [--admin] [--data-dir DIR]
  cortex agent token --id AGENT [--data-dir DIR]
  cortex connector sync hermes --home HERMES_HOME [--data-dir DIR]
  cortex import holographic --database MEMORY_STORE_DB --agent AGENT [--project PROJECT]
  cortex serve [--data-dir DIR] [--listen ADDRESS]
  cortex open [--data-dir DIR]
  cortex service install [--data-dir DIR]
  cortex service start [--data-dir DIR]
  cortex service status|uninstall
  cortex version`)
}
