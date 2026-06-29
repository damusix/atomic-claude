package selfupdate_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// basic ordering
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		// minor ordering: 1.2.0 vs 1.10.0
		{"1.2.0", "1.10.0", -1},
		{"1.10.0", "1.2.0", 1},
		// patch ordering
		{"1.0.1", "1.0.2", -1},
		{"1.0.2", "1.0.1", 1},
		// prerelease < release (semver 2.0)
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		// build metadata stripped
		{"1.2.3+build.1", "1.2.3", 0},
		// leading v is normalised
		{"v1.0.0", "1.0.0", 0},
		{"v1.0.0", "v2.0.0", -1},
		// malformed versions treated as floor (0.0.0)
		{"bad", "1.0.0", -1},
		{"1.0.0", "bad", 1},
		{"bad", "bad", 0},
		{"bad", "0.0.0", 0},
		{"0.0.0", "bad", 0},
	}

	for _, tc := range cases {
		got := selfupdate.CompareSemver(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
