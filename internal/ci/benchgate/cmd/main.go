// Command benchgate fails CI when benchstat reports significant regressions.
//
// Usage:
//
//	go run ./internal/ci/benchgate/cmd -csv benchstat.csv -threshold 5 -raw benchstat_human.txt
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mredencom/schemix/internal/ci/benchgate"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("benchgate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	csvPath := flags.String("csv", "", "benchstat CSV output path")
	threshold := flags.Float64("threshold", 5, "regression threshold percentage")
	rawPath := flags.String("raw", "", "human-readable benchstat output path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *csvPath == "" {
		_, _ = fmt.Fprintln(stderr, "ERROR: -csv flag is required")
		return 2
	}

	regressions, err := benchgate.ParseAndCheck(*csvPath, *threshold)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: cannot parse benchstat CSV: %v\n", err)
		_, _ = fmt.Fprintln(stderr, "Failing closed. Raw benchstat output:")
		if *rawPath != "" {
			raw, readErr := os.ReadFile(*rawPath)
			if readErr != nil {
				_, _ = fmt.Fprintf(stderr, "ERROR: cannot read raw benchstat output: %v\n", readErr)
			} else {
				_, _ = stderr.Write(raw)
			}
		}
		return 1
	}
	if len(regressions) != 0 {
		_, _ = fmt.Fprintf(stderr, "FAIL: %d benchmark metric(s) regressed >%.2f%%:\n", len(regressions), *threshold)
		for _, regression := range regressions {
			_, _ = fmt.Fprintf(stderr, "  %s %s: %+.2f%% (p=%.3f)\n",
				regression.Name,
				regression.Metric,
				regression.ChangePercent,
				regression.PValue,
			)
		}
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "PASS: no statistically significant regressions above threshold")
	return 0
}
