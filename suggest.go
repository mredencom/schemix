package schemix

import (
	"unicode"
	"unicode/utf8"
)

// suggestion is dropped. Guessing past this point produces noise rather than
// help, so no suggestion is preferable.
const maxSuggestionDistance = 2

// that "usd" suggests "USD".
func suggestClosest(value string, candidates []string) string {
	if value == "" || len(candidates) == 0 {
		return ""
	}

	best, bestDist := "", maxSuggestionDistance+1
	for _, c := range candidates {
		d := levenshteinFold(value, c)
		if d < bestDist {
			best, bestDist = c, d
		}
	}
	if bestDist > maxSuggestionDistance {
		return ""
	}
	// A short value must not be "corrected" to something entirely different:
	// require the distance to stay below the value's own length.
	if bestDist >= utf8.RuneCountInString(value) {
		return ""
	}
	return best
}

// levenshteinBufSize bounds the stack buffer used for edit distance. Enum values
// longer than this fall back to a heap allocation, which is acceptable because
// the path only runs when validation has already failed.
const levenshteinBufSize = 64

// levenshteinFold computes the case-insensitive edit distance between two
// strings using a single rolling row. Case folding happens per rune during
// comparison rather than by lowercasing both inputs up front, which would
// allocate two strings per candidate. The row lives on the stack for the short
// strings that enum values almost always are, so a failed validation allocates
// nothing here. Only invoked on the error path, never during successful
// validation.
func levenshteinFold(a, b string) int {
	la, lb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	var buf [levenshteinBufSize + 1]int
	var prev []int
	if lb < levenshteinBufSize {
		prev = buf[:lb+1]
	} else {
		prev = make([]int, lb+1)
	}
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	i := 0
	for _, ca := range a {
		i++
		diag := prev[0] // prev[j-1] before it is overwritten
		prev[0] = i
		j := 0
		for _, cb := range b {
			j++
			cost := 1
			if ca == cb || unicode.ToLower(ca) == unicode.ToLower(cb) {
				cost = 0
			}
			cur := min(prev[j]+1, min(prev[j-1]+1, diag+cost))
			diag, prev[j] = prev[j], cur
		}
	}
	return prev[lb]
}
