package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/dashboard-advisor/pkg/analyzer"
	"github.com/dashboard-advisor/pkg/fixer"
	"github.com/dashboard-advisor/pkg/output"
	"github.com/dashboard-advisor/pkg/server"
)

func main() {
	format := flag.String("format", "text", "Output format: text, json")
	failOn := flag.String("fail-on", "", "Exit code 1 if findings at this severity or above: low, medium, high, critical")
	fix := flag.Bool("fix", false, "Apply auto-fixes and write patched dashboard JSON to stdout")
	fixOutput := flag.String("output", "", "Write patched JSON to this file instead of stdout (requires --fix)")
	serve := flag.Bool("serve", false, "Start web UI server")
	addr := flag.String("addr", ":8080", "Server listen address (with --serve)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: dashboard-advisor [flags] <dashboard.json>\n\n")
		fmt.Fprintf(os.Stderr, "Analyze a Grafana dashboard JSON file for performance anti-patterns.\n\n")
		fmt.Fprintf(os.Stderr, "Modes:\n")
		fmt.Fprintf(os.Stderr, "  lint (default)  Analyze and report findings\n")
		fmt.Fprintf(os.Stderr, "  --fix           Apply auto-fixes and output patched JSON\n")
		fmt.Fprintf(os.Stderr, "  --serve         Start web UI server\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *serve {
		runServe(*addr)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}

	path := flag.Arg(0)

	if *fix {
		runFix(path, *fixOutput)
	} else {
		runLint(path, *format, *failOn)
	}
}

func runServe(addr string) {
	handler := server.Handler()
	log.Printf("Dashboard Advisor web UI: http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(2)
	}
}

func runLint(path, format, failOn string) {
	engine := analyzer.DefaultEngine()
	report, err := engine.AnalyzeFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	var formatter output.Formatter
	switch format {
	case "json":
		formatter = &output.JSONFormatter{Indent: true}
	case "text":
		formatter = &output.TextFormatter{}
	default:
		fmt.Fprintf(os.Stderr, "Unknown format: %s\n", format)
		os.Exit(2)
	}

	if err := formatter.Format(os.Stdout, report); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(2)
	}

	if failOn != "" {
		threshold := parseSeverity(failOn)
		if threshold < 0 {
			fmt.Fprintf(os.Stderr, "Unknown severity: %s\n", failOn)
			os.Exit(2)
		}
		for _, f := range report.Findings {
			if int(f.Severity) >= threshold {
				os.Exit(1)
			}
		}
	}
}

func runFix(path, outputPath string) {
	rawJSON, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(2)
	}

	// Analyze to get findings
	engine := analyzer.DefaultEngine()
	report, err := engine.AnalyzeFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing: %v\n", err)
		os.Exit(2)
	}

	// Apply fixes
	patched, fixCount, err := fixer.ApplyFixes(rawJSON, report.Findings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying fixes: %v\n", err)
		os.Exit(2)
	}

	if fixCount == 0 {
		fmt.Fprintf(os.Stderr, "No auto-fixable issues found.\n")
		os.Exit(0)
	}

	// Write output
	if outputPath != "" {
		if err := os.WriteFile(outputPath, patched, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "Applied %d fixes, wrote patched dashboard to %s\n", fixCount, outputPath)
	} else {
		os.Stdout.Write(patched)
	}
}

func parseSeverity(s string) int {
	switch s {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	default:
		return -1
	}
}
