package main

import "strings"

// levenshtein returns the edit distance between a and b (the minimum
// number of single-character insertions, deletions, or substitutions to
// turn one into the other). The comparison is case-insensitive.
func levenshtein(a, b string) int {
	a = strings.ToLower(a)
	b = strings.ToLower(b)

	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

// didYouMean returns the candidate closest to input within maxDist edits,
// or "" when nothing is close enough. Ties go to the first candidate in
// slice order.
func didYouMean(input string, candidates []string, maxDist int) string {
	best := ""
	bestDist := maxDist + 1
	for _, c := range candidates {
		d := levenshtein(input, c)
		if d <= maxDist && d < bestDist {
			best = c
			bestDist = d
		}
	}
	return best
}
