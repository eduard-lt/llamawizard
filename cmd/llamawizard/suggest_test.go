package main

import "testing"

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"model", "models", 1},
		{"ad", "add", 1},
		{"kitten", "sitting", 3},
		{"x", "x", 0},
		{"Model", "model", 0}, // case-insensitive
		{"", "abc", 3},
		{"abc", "", 3},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDidYouMean(t *testing.T) {
	candidates := []string{"list", "add", "show", "remove", "delete"}

	if got := didYouMean("ad", candidates, 2); got != "add" {
		t.Errorf("didYouMean(ad) = %q, want %q", got, "add")
	}
	if got := didYouMean("lst", candidates, 2); got != "list" {
		t.Errorf("didYouMean(lst) = %q, want %q", got, "list")
	}

	// Nothing within the threshold.
	if got := didYouMean("xyzzy", candidates, 2); got != "" {
		t.Errorf("didYouMean(xyzzy) = %q, want empty", got)
	}

	// Tie: "x" is 1 edit from both "xa" and "xb"; the first candidate in
	// slice order wins.
	if got := didYouMean("x", []string{"xa", "xb"}, 1); got != "xa" {
		t.Errorf("tie: didYouMean(x, [xa xb]) = %q, want first candidate %q", got, "xa")
	}

	// maxDist is respected: "ad" is 1 from "add", but with maxDist 0 there
	// is no exact match.
	if got := didYouMean("ad", candidates, 0); got != "" {
		t.Errorf("didYouMean(ad, maxDist 0) = %q, want empty", got)
	}
	if got := didYouMean("add", candidates, 0); got != "add" {
		t.Errorf("didYouMean(add, maxDist 0) = %q, want %q", got, "add")
	}
}
