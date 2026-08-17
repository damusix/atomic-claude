package search

// Package-internal so boundedDL is reachable.

import "testing"

// Locks transpositions at cost 1. Charging +cost instead would make a swap free
// whenever the compared characters happen to match — a latent bug no natural
// input exposes, since the DP absorbs the error before the final distance.
func TestBoundedDL_Transpositions(t *testing.T) {
	tests := []struct {
		a, b    string
		maxDist int
		want    int
	}{
		{"ab", "ba", 1, 1},
		{"ca", "ac", 1, 1},
		{"abc", "bac", 1, 1},
		{"abc", "acb", 1, 1},
		{"ab", "ba", 0, -1},
		{"hello", "hello", 0, 0},
		{"parseQuery", "parseQuery", 0, 0},
		{"abc", "axc", 1, 1},
		{"abcd", "badc", 2, 2},
		{"abc", "bxc", 2, 2},
	}

	for _, tc := range tests {
		got := boundedDL(tc.a, tc.b, tc.maxDist)
		if got != tc.want {
			t.Errorf("boundedDL(%q, %q, %d) = %d, want %d",
				tc.a, tc.b, tc.maxDist, got, tc.want)
		}
	}
}
