// cmd/bench/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "page-diff":
		runPageDiff(os.Args[2:])
	case "tasks":
		runTasks(os.Args[2:])
	case "all":
		runAll(os.Args[2:])
	case "sample-urls":
		runSampleURLs(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`bench — DocuMcp token-savings benchmark

Usage:
  bench page-diff   [--urls FILE]
  bench tasks       [--questions FILE] [--trials N]
  bench all         [--urls FILE] [--questions FILE] [--trials N]
  bench sample-urls --per-source N

Environment:
  ANTHROPIC_API_KEY   Anthropic API key (required for tasks/all)
  DOCUMCP_BENCH_URL   DocuMcp instance URL (default: http://127.0.0.1:8080)
  DOCUMCP_API_KEY     Bearer token if DocuMcp requires auth`)
}

func runTasks(_ []string) {
	fmt.Fprintln(os.Stderr, "tasks: not yet implemented")
	os.Exit(1)
}

func runAll(_ []string) {
	fmt.Fprintln(os.Stderr, "all: not yet implemented")
	os.Exit(1)
}

