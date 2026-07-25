package schemix

import (
	"math"
	"testing"
)

// FuzzProcess tests the full validation/transform pipeline with random inputs.
// NO recover() — panics propagate directly for the fuzzer to catch.
func FuzzProcess(f *testing.F) {
	v := MustNew(`{
		name:     string
		age:      int & >=0 & <=150
		currency: "156" | "840"
		amount:   int & >0
		pan:      =~"^[0-9]{16}$"
		rate:     float & >=0 & <=100
	}`)

	// Seed corpus
	f.Add("Alice", int64(30), "156", int64(100), "4111111111111111", 1.5)
	f.Add("", int64(0), "840", int64(1), "0000000000000000", 0.0)
	f.Add("Bob", int64(150), "999", int64(-1), "short", 100.0)
	f.Add("X", int64(-1), "", int64(0), "", -1.0)

	f.Fuzz(func(t *testing.T, name string, age int64, currency string, amount int64, pan string, rate float64) {
		data := map[string]any{
			"name":     name,
			"age":      age,
			"currency": currency,
			"amount":   amount,
			"pan":      pan,
			"rate":     rate,
		}

		// Process must not panic — no recover here
		r := v.Process(data)

		// Basic invariants
		if r.Valid != (len(r.Errors) == 0) {
			t.Errorf("Valid=%v with %d errors", r.Valid, len(r.Errors))
		}
		if !r.Valid && r.Output != nil {
			t.Errorf("Valid=false but Output is non-nil")
		}
	})
}

// FuzzProcessWithMode tests all FailModes with random inputs.
func FuzzProcessWithMode(f *testing.F) {
	v := MustNew(`{
		x: string
		y: int & >=0
		z: bool @blob(this.y > 10)
	}`)

	f.Add("hello", int64(20), 0)
	f.Add("", int64(-1), 1)
	f.Add("world", int64(0), 2)

	modes := []FailMode{FailAll, FailFast, FailPriority}

	f.Fuzz(func(t *testing.T, x string, y int64, modeIdx int) {
		data := map[string]any{"x": x, "y": y}
		mode := modes[normalizeIndex(modeIdx, len(modes))]

		// Must not panic
		r := v.ProcessWithMode(data, mode)

		if r.Valid != (len(r.Errors) == 0) {
			t.Errorf("Valid=%v with %d errors", r.Valid, len(r.Errors))
		}
		if !r.Valid && r.Output != nil {
			t.Errorf("Valid=false but Output non-nil")
		}
	})
}

func normalizeIndex(n, size int) int {
	idx := n % size
	if idx < 0 {
		idx += size
	}
	return idx
}

func TestNormalizeIndex(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want int
	}{
		{name: "zero", n: 0, want: 0},
		{name: "positive wraps", n: 4, want: 1},
		{name: "negative wraps", n: -1, want: 2},
		{name: "minimum int", n: math.MinInt, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeIndex(tt.n, 3); got != tt.want {
				t.Fatalf("normalizeIndex(%d, 3) = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}
