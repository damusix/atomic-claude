package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlemirror"
)

// buildMiniRepo creates a tiny fake repo structure for testing the mirror logic.
func buildMiniRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("agents/atomic-builder.md", "# atomic-builder\n")
	write("agents/atomic-reviewer.md", "# atomic-reviewer\n")
	write("agents/non-atomic-agent.md", "should be excluded\n")

	write("skills/atomic-tdd/SKILL.md", "# atomic-tdd\n")
	write("skills/atomic-verify/SKILL.md", "# atomic-verify\n")
	write("skills/non-atomic-skill/SKILL.md", "should be excluded\n")

	write("output-styles/atomic.md", "# atomic output style\n")
	write("output-styles/other.md", "should be excluded\n")

	write("commands/commit-only.md", "# commit-only\n")
	write("commands/atomic-plan.md", "# atomic-plan\n")
	write("commands/_templates/something.md", "should be excluded\n")

	write("rules/python/style.md", "# python style\n")
	write("rules/typescript/style.md", "# typescript style\n")

	write("claude.md", "# CLAUDE\n")

	return dir
}

func TestRunBasic(t *testing.T) {
	repoRoot := buildMiniRepo(t)
	outDir := t.TempDir()

	artifacts, err := bundlemirror.Run(repoRoot, outDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	counts := map[string]int{}
	for _, a := range artifacts {
		counts[a.Kind]++
	}

	if counts["agent"] != 2 {
		t.Errorf("agent count = %d, want 2", counts["agent"])
	}
	if counts["skill"] != 2 {
		t.Errorf("skill count = %d, want 2", counts["skill"])
	}
	if counts["output-style"] != 1 {
		t.Errorf("output-style count = %d, want 1", counts["output-style"])
	}
	if counts["command"] != 2 {
		t.Errorf("command count = %d, want 2", counts["command"])
	}
	if counts["rule"] != 2 {
		t.Errorf("rule count = %d, want 2", counts["rule"])
	}
	if counts["claude-md"] != 1 {
		t.Errorf("claude-md count = %d, want 1", counts["claude-md"])
	}
}

func TestRunExclusions(t *testing.T) {
	repoRoot := buildMiniRepo(t)
	outDir := t.TempDir()

	artifacts, err := bundlemirror.Run(repoRoot, outDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	excluded := []string{
		"agents/non-atomic-agent.md",
		"skills/non-atomic-skill/SKILL.md",
		"output-styles/other.md",
		"commands/_templates/something.md",
	}
	for _, a := range artifacts {
		for _, excl := range excluded {
			if a.Target == excl {
				t.Errorf("excluded artifact %q was included", excl)
			}
		}
	}
}

func TestRunBundleFilesWritten(t *testing.T) {
	repoRoot := buildMiniRepo(t)
	outDir := t.TempDir()

	artifacts, err := bundlemirror.Run(repoRoot, outDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	bundleDir := filepath.Join(outDir, "bundle")
	for _, a := range artifacts {
		onDisk := filepath.Join(bundleDir, filepath.FromSlash(a.Target))
		if _, err := os.Stat(onDisk); os.IsNotExist(err) {
			t.Errorf("bundle file missing: %s", onDisk)
		}
	}
}

func TestRunTargetPaths(t *testing.T) {
	repoRoot := buildMiniRepo(t)
	outDir := t.TempDir()

	artifacts, err := bundlemirror.Run(repoRoot, outDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	targetMap := map[string]bool{}
	for _, a := range artifacts {
		targetMap[a.Target] = true
	}

	expected := []string{
		"agents/atomic-builder.md",
		"agents/atomic-reviewer.md",
		"skills/atomic-tdd/SKILL.md",
		"skills/atomic-verify/SKILL.md",
		"output-styles/atomic.md",
		"commands/commit-only.md",
		"commands/atomic-plan.md",
		"rules/python/style.md",
		"rules/typescript/style.md",
		"CLAUDE.md",
	}
	for _, e := range expected {
		if !targetMap[e] {
			t.Errorf("expected target %q not found in artifacts", e)
		}
	}

	// rules are sourced from root, not .claude/.
	for _, a := range artifacts {
		if len(a.Target) >= 7 && a.Target[:7] == ".claude" {
			t.Errorf("artifact target %q must not have .claude prefix", a.Target)
		}
	}
}

func TestRunSHA256Correct(t *testing.T) {
	repoRoot := buildMiniRepo(t)
	outDir := t.TempDir()

	artifacts, err := bundlemirror.Run(repoRoot, outDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	bundleDir := filepath.Join(outDir, "bundle")
	for _, a := range artifacts {
		data, err := os.ReadFile(filepath.Join(bundleDir, filepath.FromSlash(a.Target)))
		if err != nil {
			t.Errorf("read %s: %v", a.Target, err)
			continue
		}
		actual := bundlemirror.SHA256Hex(data)
		if actual != a.SHA256 {
			t.Errorf("%s: SHA256 = %s, want %s", a.Target, actual, a.SHA256)
		}
	}
}

func TestRunDeterministic(t *testing.T) {
	repoRoot := buildMiniRepo(t)

	outDir1 := t.TempDir()
	outDir2 := t.TempDir()

	artifacts1, err := bundlemirror.Run(repoRoot, outDir1)
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}

	artifacts2, err := bundlemirror.Run(repoRoot, outDir2)
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	if len(artifacts1) != len(artifacts2) {
		t.Fatalf("artifact count differs: %d vs %d", len(artifacts1), len(artifacts2))
	}

	for i := range artifacts1 {
		a1 := artifacts1[i]
		a2 := artifacts2[i]
		if a1.Kind != a2.Kind || a1.Source != a2.Source || a1.Target != a2.Target || a1.SHA256 != a2.SHA256 {
			t.Errorf("artifacts differ at index %d: %+v vs %+v", i, a1, a2)
		}
	}
}

func TestRunStableSort(t *testing.T) {
	repoRoot := buildMiniRepo(t)
	outDir := t.TempDir()

	artifacts, err := bundlemirror.Run(repoRoot, outDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for i := 1; i < len(artifacts); i++ {
		prev := artifacts[i-1]
		curr := artifacts[i]
		less := prev.Kind < curr.Kind || (prev.Kind == curr.Kind && prev.Target <= curr.Target)
		if !less {
			t.Errorf("not sorted at %d: %q/%q vs %q/%q", i, prev.Kind, prev.Target, curr.Kind, curr.Target)
		}
	}
}
