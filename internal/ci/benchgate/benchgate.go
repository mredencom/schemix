// Package benchgate provides a fail-closed benchmark regression gate.
package benchgate

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

const significanceLevel = 0.05

// Regression describes a statistically significant benchmark regression.
type Regression struct {
	Name          string
	Metric        string
	ChangePercent float64
	PValue        float64
}

// Result is the outcome of one gate run over a benchstat comparison.
type Result struct {
	// Regressions are the metrics that both exceeded the threshold and reached
	// statistical significance.
	Regressions []Regression

	// Incomparable names benchmarks benchstat found in only one of its two
	// inputs, one entry per metric section, formatted as "Name metric".
	//
	// A benchmark added by the change under review lands here, and so does one
	// that was removed. Neither has a comparison to judge, but both are worth
	// surfacing: a newly added benchmark is not yet guarded by this gate, and one
	// that disappeared may not have been meant to.
	Incomparable []string
}

// ParseAndCheck reads benchstat -format=csv output and reports regressions
// whose positive change is both statistically significant and above threshold.
func ParseAndCheck(path string, threshold float64) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, fmt.Errorf("open benchstat CSV: %w", err)
	}
	defer func() { _ = f.Close() }()

	return parseAndCheck(f, threshold)
}

func parseAndCheck(input io.Reader, threshold float64) (Result, error) {
	var result Result
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 {
		return Result{}, fmt.Errorf("invalid threshold %v", threshold)
	}

	reader := csv.NewReader(input)
	reader.FieldsPerRecord = -1 // benchstat emits metadata and intentionally short first header rows.
	reader.TrimLeadingSpace = true

	changeIdx, pIdx := -1, -1
	metric := ""
	sawHeader := false
	comparableRows := 0

	for recordNumber := 1; ; recordNumber++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, fmt.Errorf("parse CSV record %d: %w", recordNumber, err)
		}
		if isBlankRecord(record) {
			continue
		}

		if idx := findColumn(record, "vs base"); idx >= 0 {
			pColumn := findColumn(record, "p")
			if pColumn < 0 {
				return Result{}, fmt.Errorf("record %d: comparison header has no P column: %v", recordNumber, record)
			}
			changeIdx, pIdx = idx, pColumn
			metric = metricName(record, idx)
			if metric == "" {
				return Result{}, fmt.Errorf("record %d: comparison header has no metric: %v", recordNumber, record)
			}
			sawHeader = true
			continue
		}

		first := strings.TrimSpace(record[0])
		if len(record) == 1 && strings.Contains(first, ":") {
			changeIdx, pIdx, metric = -1, -1, ""
			continue // goos:, goarch:, pkg:, cpu:, and other benchstat metadata.
		}
		if first == "" {
			continue // The intentionally short file-label header.
		}
		if changeIdx < 0 {
			return Result{}, fmt.Errorf("record %d: data row before comparison header: %v", recordNumber, record)
		}
		if strings.EqualFold(first, "geomean") {
			continue
		}
		if changeIdx >= len(record) || pIdx >= len(record) {
			// For a benchmark present in only one input, benchstat omits the
			// comparison columns rather than leaving them empty: three fields
			// when only base has it, five when only head does. Any change that
			// adds or removes a benchmark produces these, and there is no
			// comparison to judge, so record and move on rather than failing.
			result.Incomparable = append(result.Incomparable, first+" "+metric)
			continue
		}

		changeRaw := strings.TrimSpace(record[changeIdx])
		pRaw := strings.TrimSpace(record[pIdx])
		if changeRaw == "" && pRaw == "" {
			// Same situation, padded out to full width.
			result.Incomparable = append(result.Incomparable, first+" "+metric)
			continue
		}
		if changeRaw == "" || pRaw == "" {
			return Result{}, fmt.Errorf("record %d: incomplete comparison for %q", recordNumber, first)
		}

		pValue, err := parsePValue(pRaw)
		if err != nil {
			return Result{}, fmt.Errorf("record %d benchmark %q: %w", recordNumber, first, err)
		}
		comparableRows++
		if changeRaw == "~" {
			continue
		}

		change, err := parsePercent(changeRaw)
		if err != nil {
			return Result{}, fmt.Errorf("record %d benchmark %q: %w", recordNumber, first, err)
		}
		if change > threshold && pValue < significanceLevel {
			result.Regressions = append(result.Regressions, Regression{
				Name:          first,
				Metric:        metric,
				ChangePercent: change,
				PValue:        pValue,
			})
		}
	}

	if !sawHeader {
		return Result{}, fmt.Errorf("benchstat CSV has no comparison header")
	}
	if comparableRows == 0 {
		// Every row was one-sided, so the two runs share no benchmark and the
		// gate has judged nothing. That is a broken comparison, not a pass.
		return Result{}, fmt.Errorf("benchstat CSV has no comparable benchmark rows")
	}
	return result, nil
}

func isBlankRecord(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func findColumn(record []string, name string) int {
	for i, field := range record {
		if strings.EqualFold(strings.TrimSpace(field), name) {
			return i
		}
	}
	return -1
}

func metricName(header []string, changeIdx int) string {
	for i := 1; i < changeIdx && i < len(header); i++ {
		field := strings.TrimSpace(header[i])
		if field != "" && !strings.EqualFold(field, "CI") {
			return field
		}
	}
	return ""
}

func parsePercent(raw string) (float64, error) {
	if !strings.HasSuffix(raw, "%") {
		return 0, fmt.Errorf("invalid change %q: missing %% suffix", raw)
	}
	value := strings.TrimSuffix(strings.TrimPrefix(raw, "+"), "%")
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid change %q", raw)
	}
	return parsed, nil
}

func parsePValue(raw string) (float64, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "p=") {
		return 0, fmt.Errorf("invalid P value %q", raw)
	}
	parsed, err := strconv.ParseFloat(strings.TrimPrefix(fields[0], "p="), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 1 {
		return 0, fmt.Errorf("invalid P value %q", raw)
	}
	return parsed, nil
}
