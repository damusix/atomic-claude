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
		{"builder.md", false},
		{"README.md", false},
		{"atomic-builder.txt", false},
		{"atomic-builder", false},
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
		{"atomic-git-discipline", true},
		{"atomic-tdd", true},
		{"atomic-writing", true},
		{"commit", false},
		{"_templates", false},
		{"atomiccommit", false},
		// Name-only predicate: gating on IsDir() is the caller's job.
		{"atomic-foo.md", true},
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
		{"verbose.md", false},
		{"README.md", false},
		{"atomic.txt", false},
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

func TestIsGlobalSteeringSource(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"AGENTS.md", true},
		{"agents.md", false},
		{"CLAUDE.md", false},
		{"path/AGENTS.md", false},
	}
	for _, tc := range cases {
		got := bundlespec.IsGlobalSteeringSource(tc.name)
		if got != tc.want {
			t.Errorf("IsGlobalSteeringSource(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSteeringContract(t *testing.T) {
	if bundlespec.GlobalSteering.Loader {
		t.Error("global steering must render directly, not through a loader")
	}
	if !bundlespec.ScopeSteering.Loader {
		t.Error("repository and realm steering must use an adjacent loader")
	}
	if got := bundlespec.ScopeSteering.LoaderBody(); got != "@AGENTS.md\n" {
		t.Errorf("LoaderBody() = %q, want %q", got, "@AGENTS.md\n")
	}
}
