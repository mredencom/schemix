package benchgate

import (
	"strings"
	"testing"
)

const realBenchstatCSV = `goos: linux
goarch: amd64
pkg: github.com/mredencom/schemix
cpu: Example CPU
,base,,head
,sec/op,CI,sec/op,CI,vs base,P
ProcessValid-8,4.90e-06,1%,5.20e-06,1%,+6.12%,p=0.010 n=6
ProcessStable-8,5.00e-06,2%,5.01e-06,2%,~,p=0.900 n=6
geomean,4.95e-06,,5.10e-06,,+3.03%,

,base,,head
,B/op,CI,B/op,CI,vs base,P
ProcessValid-8,4096,0%,4506,0%,+10.01%,p=0.001 n=6
geomean,4096,,4506,,+10.01%,

,base,,head
,allocs/op,CI,allocs/op,CI,vs base,P
ProcessValid-8,61,0%,61,0%,~,p=1.000 n=6
`

func TestParseAndCheckRealBenchstatCSV(t *testing.T) {
	regressions, err := parseAndCheck(strings.NewReader(realBenchstatCSV), 5)
	if err != nil {
		t.Fatalf("parseAndCheck: %v", err)
	}
	if len(regressions) != 2 {
		t.Fatalf("regressions = %d, want 2: %+v", len(regressions), regressions)
	}
	if regressions[0].Name != "ProcessValid-8" || regressions[0].Metric != "sec/op" {
		t.Errorf("first regression = %+v, want ProcessValid-8 sec/op", regressions[0])
	}
	if regressions[1].Metric != "B/op" {
		t.Errorf("second metric = %q, want B/op", regressions[1].Metric)
	}
	if regressions[0].ChangePercent != 6.12 || regressions[0].PValue != 0.01 {
		t.Errorf("first regression values = %+v", regressions[0])
	}
}

func TestParseAndCheckThresholdAndSignificance(t *testing.T) {
	tests := []struct {
		name       string
		change     string
		pValue     string
		regression bool
	}{
		{name: "above threshold significant", change: "+5.01%", pValue: "p=0.049 n=6", regression: true},
		{name: "at threshold", change: "+5.00%", pValue: "p=0.001 n=6"},
		{name: "below threshold", change: "+4.99%", pValue: "p=0.001 n=6"},
		{name: "not significant", change: "+20.00%", pValue: "p=0.050 n=6"},
		{name: "improvement", change: "-20.00%", pValue: "p=0.001 n=6"},
		{name: "benchstat tilde", change: "~", pValue: "p=0.900 n=6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := comparisonCSV(tt.change, tt.pValue)
			regressions, err := parseAndCheck(strings.NewReader(input), 5)
			if err != nil {
				t.Fatalf("parseAndCheck: %v", err)
			}
			if got := len(regressions) == 1; got != tt.regression {
				t.Fatalf("regression=%v, want %v; rows=%+v", got, tt.regression, regressions)
			}
		})
	}
}

func TestParseAndCheckSkipsUnmatchedBenchmark(t *testing.T) {
	input := `,base,,head
,sec/op,CI,sec/op,CI,vs base,P
OnlyInHead-8,,,,,,
Comparable-8,1,0%,1,0%,~,p=1.000 n=6
`
	regressions, err := parseAndCheck(strings.NewReader(input), 5)
	if err != nil {
		t.Fatalf("parseAndCheck: %v", err)
	}
	if len(regressions) != 0 {
		t.Fatalf("regressions = %+v, want none", regressions)
	}
}

func TestParseAndCheckRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "metadata only", input: "goos: linux\ngoarch: amd64\n"},
		{name: "malformed CSV", input: `,"unterminated`},
		{name: "missing P header", input: `,base,,head
,sec/op,CI,sec/op,CI,vs base
Bench-8,1,0%,2,0%,+100.00%
`},
		{name: "malformed change", input: comparisonCSV("faster", "p=0.001 n=6")},
		{name: "change without percent", input: comparisonCSV("+6.00", "p=0.001 n=6")},
		{name: "malformed P", input: comparisonCSV("+6.00%", "0.001")},
		{name: "P out of range", input: comparisonCSV("+6.00%", "p=1.5 n=6")},
		{name: "short row", input: `,base,,head
,sec/op,CI,sec/op,CI,vs base,P
Bench-8,1,0%
`},
		{name: "no comparable rows", input: `,base,,head
,sec/op,CI,sec/op,CI,vs base,P
OnlyInHead-8,,,,,,
`},
		{name: "data before header", input: "Bench-8,1,2,3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseAndCheck(strings.NewReader(tt.input), 5); err == nil {
				t.Fatal("parseAndCheck returned nil error")
			}
		})
	}
}

func TestParseAndCheckRejectsInvalidThreshold(t *testing.T) {
	for _, threshold := range []float64{-1} {
		if _, err := parseAndCheck(strings.NewReader(realBenchstatCSV), threshold); err == nil {
			t.Fatalf("threshold %v returned nil error", threshold)
		}
	}
}

func TestParseAndCheckMissingFile(t *testing.T) {
	if _, err := ParseAndCheck(t.TempDir()+"/missing.csv", 5); err == nil {
		t.Fatal("ParseAndCheck returned nil error")
	}
}

func TestParsePValue(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    float64
		wantErr bool
	}{
		{name: "benchstat", raw: "p=0.002 n=6", want: 0.002},
		{name: "zero", raw: "p=0.000 n=6", want: 0},
		{name: "missing prefix", raw: "0.002", wantErr: true},
		{name: "not number", raw: "p=nope n=6", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePValue(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("value = %v, want %v", got, tt.want)
			}
		})
	}
}

func comparisonCSV(change, pValue string) string {
	return `goos: linux
,base,,head
,sec/op,CI,sec/op,CI,vs base,P
Bench-8,1.00e-06,1%,1.10e-06,1%,` + change + `,` + pValue + "\n"
}
