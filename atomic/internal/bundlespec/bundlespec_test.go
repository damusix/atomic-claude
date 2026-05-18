package bundlespec_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

func TestMatchesAgent(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"atomic-builder.md", true},
		{"atomic-reviewer.md", true},
		{"atomic-signals-inferrer.md", true},
		// no atomic- prefix
		{"builder.md", false},
		{"README.md", false},
		// wrong extension
		{"atomic-builder.txt", false},
		{"atomic-builder", false},
		// directory name (no extension)
		{"atomic-builder/", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesAgent(tc.name)
		if got != tc.want {
			t.Errorf("MatchesAgent(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesSkillDir(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"atomic-commit", true},
		{"atomic-tdd", true},
		{"atomic-prose", true},
		// no prefix
		{"commit", false},
		{"_templates", false},
		// partial prefix
		{"atomiccommit", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesSkillDir(tc.name)
		if got != tc.want {
			t.Errorf("MatchesSkillDir(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesOutputStyle(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"atomic.md", true},
		{"atomic-verbose.md", true},
		// no prefix
		{"verbose.md", false},
		{"README.md", false},
		// wrong extension
		{"atomic.txt", false},
		// atomic prefix present, no .md suffix
		{"atomic", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesOutputStyle(tc.name)
		if got != tc.want {
			t.Errorf("MatchesOutputStyle(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesCommand(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"commit-only.md", true},
		{"atomic-plan.md", true},
		{"README.md", true}, // no allowlist — any .md file matches
		// wrong extension
		{"commit-only.txt", false},
		{"commit-only", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesCommand(tc.name)
		if got != tc.want {
			t.Errorf("MatchesCommand(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesRule(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"rules/python/style.md", true},
		{"rules/typescript/style.md", true},
		{"rules/go/naming.md", true},
		// wrong extension
		{"rules/python/style.txt", false},
		{"rules/python/style", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesRule(tc.path)
		if got != tc.want {
			t.Errorf("MatchesRule(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsClaudeMd(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"CLAUDE.md", true},
		// wrong case
		{"claude.md", false},
		{"Claude.md", false},
		{"CLAUDE.MD", false},
		// with path components (predicate accepts bare name)
		{"path/CLAUDE.md", false},
	}
	for _, tc := range cases {
		got := bundlespec.IsClaudeMd(tc.name)
		if got != tc.want {
			t.Errorf("IsClaudeMd(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
