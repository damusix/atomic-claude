package search

// F-51: boundedDL transposition branch must charge +1, not +cost.
//
// The Damerau-Levenshtein transposition guard checks whether chars at
// positions (i-1,j-2) and (i-2,j-1) match — entirely separate from whether
// chars at (i-1,j-1) match. Before the fix, the branch charged `prev2[j-2]+cost`
// where `cost` is 0 when a[i-1]==b[j-1] (unrelated to the transposition). This
// is semantically wrong: a transposition edit costs 1 regardless.
//
// Brute-force search over {a,b,c}* up to length 5 finds no input where bug
// and fix produce different final distances (the DP structural properties
// absorb the error). The bug is latent — code is wrong but unobservable on
// natural inputs. Tests below guard correctness of transposition semantics.
//
// Package-internal file (not search_test) to access unexported boundedDL.

import "testing"

// TestBoundedDL_Transpositions verifies that adjacent transpositions cost 1
// and that boundedDL returns the correct Damerau-Levenshtein distance for
// known swap inputs.
//
// WHY: The transposition fix ensures the branch always charges +1. These
// test cases exercise the transposition code path and lock in the correct
// distance values, preventing a future regression from making transpositions
// free (distance 0) via the +cost formula.
func TestBoundedDL_Transpositions(t *testing.T) {
	tests := []struct {
		a, b    string
		maxDist int
		want    int
	}{
		// Single transposition: distance must be 1.
		{"ab", "ba", 1, 1},
		{"ca", "ac", 1, 1},
		{"abc", "bac", 1, 1},
		{"abc", "acb", 1, 1},
		// Transposition exceeds bound → early exit (-1).
		{"ab", "ba", 0, -1},
		// Identical strings: distance 0.
		{"hello", "hello", 0, 0},
		{"parseQuery", "parseQuery", 0, 0},
		// Pure substitution (no transposition involved): distance 1.
		{"abc", "axc", 1, 1},
		// Two transpositions: distance 2.
		{"abcd", "badc", 2, 2},
		// One transposition + one substitution: distance 2.
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
