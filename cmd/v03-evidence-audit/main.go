package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/good-fish-man/agent-runtime/internal/evidenceaudit"
)

func main() {
	format := flag.String("format", "text", "output format: text or json")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: v03-evidence-audit [--format text|json] PATH [PATH...]")
		os.Exit(2)
	}
	report := evidenceaudit.Scan(flag.Args())
	if *format == "json" {
		payload, err := report.JSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "encode audit report: %v\n", err)
			os.Exit(2)
		}
		fmt.Println(string(payload))
	} else {
		for _, finding := range report.Findings {
			fmt.Printf("credential finding path=%s line=%d rule=%s\n", finding.Path, finding.Line, finding.Rule)
		}
		for _, scanErr := range report.Errors {
			fmt.Fprintf(os.Stderr, "scan error: %s\n", scanErr)
		}
		fmt.Printf("scanned_files=%d findings=%d errors=%d\n", report.ScannedFiles, len(report.Findings), len(report.Errors))
	}
	if len(report.Errors) > 0 {
		os.Exit(2)
	}
	if len(report.Findings) > 0 {
		os.Exit(1)
	}
}
