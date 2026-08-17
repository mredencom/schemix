// Package comparison holds cross-library validation benchmarks for schemix.
//
// It lives in its own module so that competitor libraries never leak into the
// dependency tree of github.com/mredencom/schemix. Run it with:
//
//	cd benchmarks/comparison && go test -bench=. -benchmem
//
// All fixtures, equivalence tests and benchmarks live in comparison_test.go,
// organised into four sections: fixtures, the equivalence gate, cross-library
// benchmarks, and the schemix breakdown.
package comparison
