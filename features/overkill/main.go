package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/axsh/arctic-tern/features/overkill/analyzer"
)

func main() {
	path := flag.String("path", ".", "Root directory to scan")
	exclude := flag.String("exclude", "reference_repo,vendor,tmp,.git", "Comma-separated exclude patterns")
	execute := flag.Bool("execute", false, "Actually remove dead code (default: report only)")
	jsonOut := flag.Bool("json", false, "Output in JSON format")
	verbose := flag.Bool("verbose", false, "Show reference details")
	flag.Parse()

	excludePatterns := strings.Split(*exclude, ",")

	result, err := analyzer.Analyze(*path, excludePatterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		analyzer.ReportJSON(os.Stdout, result)
	} else {
		analyzer.ReportText(os.Stdout, result, *verbose)
	}

	if *execute {
		removed, err := analyzer.Execute(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error during execution: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nRemoved %d dead symbols.\n", removed)
	}
}
