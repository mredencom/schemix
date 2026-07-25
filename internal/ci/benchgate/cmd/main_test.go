package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		csv        string
		raw        string
		args       func(csvPath, rawPath string) []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name: "pass",
			csv:  cliCSV("+4.00%", "p=0.001 n=6"),
			args: func(csvPath, rawPath string) []string {
				return []string{"-csv", csvPath, "-raw", rawPath}
			},
			wantCode:   0,
			wantStdout: "PASS:",
		},
		{
			name: "regression",
			csv:  cliCSV("+6.00%", "p=0.001 n=6"),
			args: func(csvPath, rawPath string) []string {
				return []string{"-csv", csvPath, "-raw", rawPath}
			},
			wantCode:   1,
			wantStderr: "Bench-8 sec/op: +6.00% (p=0.001)",
		},
		{
			name: "parse error includes raw report",
			csv:  "invalid\n",
			raw:  "human benchmark report\n",
			args: func(csvPath, rawPath string) []string {
				return []string{"-csv", csvPath, "-raw", rawPath}
			},
			wantCode:   1,
			wantStderr: "human benchmark report",
		},
		{
			name: "missing csv flag",
			args: func(csvPath, rawPath string) []string {
				return nil
			},
			wantCode:   2,
			wantStderr: "-csv flag is required",
		},
		{
			name: "missing raw file remains failure",
			csv:  "invalid\n",
			args: func(csvPath, rawPath string) []string {
				return []string{"-csv", csvPath, "-raw", rawPath + ".missing"}
			},
			wantCode:   1,
			wantStderr: "cannot read raw benchstat output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			csvPath := filepath.Join(dir, "benchstat.csv")
			rawPath := filepath.Join(dir, "benchstat.txt")
			if err := os.WriteFile(csvPath, []byte(tt.csv), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(rawPath, []byte(tt.raw), 0o600); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			code := run(tt.args(csvPath, rawPath), &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("code=%d, want %d; stdout=%q stderr=%q", code, tt.wantCode, stdout.String(), stderr.String())
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout=%q, want substring %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr=%q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func cliCSV(change, pValue string) string {
	return `,base,,head
,sec/op,CI,sec/op,CI,vs base,P
Bench-8,1,0%,1,0%,` + change + `,` + pValue + "\n"
}
