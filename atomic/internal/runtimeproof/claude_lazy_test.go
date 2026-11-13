package runtimeproof

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// Claude Code reads an @-mentioned file before any model request, so a home
// with no credential still observes lazy delivery of nested loaders and rules.
func TestClaudeFileReadLoadsNestedWikiLoaderAndScopedRule(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	binary, err := ClaudeCLI()
	if err != nil {
		t.Skipf("claude not available: %v", err)
	}
	home, configDir, repo := claudeHome(t, false)
	if _, err := wiki.InitRepoScope(repo); err != nil {
		t.Fatalf("wiki init --scope repo: %v", err)
	}
	writeTree(t, repo, map[string]string{"docs/spec/probe.md": "# probe spec\n"})
	rule := filepath.Join(configDir, "rules", "specs", "spec-currency.md")
	if _, err := os.Stat(rule); err != nil {
		t.Fatalf("adapter did not install the scoped rule: %v", err)
	}
	wikiLoader := filepath.Join(repo, "docs", "wiki", "CLAUDE.md")
	wikiShared := filepath.Join(repo, "docs", "wiki", "AGENTS.md")

	run := func(name, prompt string) Observation {
		s := claudeLaunch(t, binary, home, configDir, repo, name)
		s.Prompt = prompt
		obs, err := Run(context.Background(), s)
		if errors.Is(err, ErrTimeout) {
			t.Fatalf("claude exceeded its bound: %+v", obs)
		}
		if err != nil {
			t.Fatalf("%s Run: %v", name, err)
		}
		return obs
	}
	expectLoads := func(obs Observation, want map[string]string) {
		t.Helper()
		counts := claudeGuidanceCounts(obs)
		for path, reason := range want {
			if got := counts[path]; got != 1 {
				t.Errorf("%s loaded %s %d times, want 1; all loads: %v", obs.Scenario, path, got, sortedCounts(counts))
			}
			if got := claudeLoadReason(obs, path); got != reason {
				t.Errorf("%s loaded %s for %q, want %q", obs.Scenario, path, got, reason)
			}
		}
	}

	startup := claudeGuidanceCounts(run("claude-startup", DefaultPrompt))
	for _, path := range []string{rule, wikiLoader, wikiShared} {
		if got := startup[path]; got != 0 {
			t.Errorf("startup without a matching read loaded %s %d times, want 0", path, got)
		}
	}

	wikiRead := run("claude-wiki-read", "Summarize @docs/wiki/index.md")
	expectLoads(wikiRead, map[string]string{wikiLoader: "nested_traversal", wikiShared: "include"})
	if parent := claudeParentOf(wikiRead, wikiShared); parent != wikiLoader {
		t.Errorf("nested guidance loaded with parent %q, want the adjacent loader %q", parent, wikiLoader)
	}

	expectLoads(run("claude-spec-read", "Summarize @docs/spec/probe.md"), map[string]string{rule: "path_glob_match"})

	versionCmd := exec.Command(binary, "--version")
	versionCmd.Env = launchEnv(claudeLaunch(t, binary, home, configDir, repo, "claude-version"))
	version, err := versionCmd.Output()
	if err != nil {
		t.Fatalf("claude --version: %v", err)
	}
	t.Logf("claude --version: %s", strings.TrimSpace(string(version)))
}

// claudeLoadReason returns the load_reason the observation reported for one
// loaded file, or "" when the file never loaded.
func claudeLoadReason(obs Observation, path string) string {
	for _, e := range obs.Events {
		if e.Name == "InstructionsLoaded" && filepath.Clean(e.FilePath) == path {
			return e.LoadReason
		}
	}
	return ""
}
