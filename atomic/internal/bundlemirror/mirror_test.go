package bundlemirror

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

// setupMinimalRepo builds the smallest context/ tree enumerate can walk without
// erroring: every expected directory, the authored global steering source, and
// one agent file.
func setupMinimalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ctx := bundlespec.SourceRoot(root)
	for _, dir := range []string{"agents", "skills", "output-styles", "commands", "rules"} {
		if err := os.MkdirAll(filepath.Join(ctx, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	steering := []byte("# Atomic\n\n<atomic>\n\nContract.\n\n</atomic>\n")
	if err := os.WriteFile(filepath.Join(ctx, bundlespec.GlobalSteering.Source), steering, 0o644); err != nil {
		t.Fatalf("write %s: %v", bundlespec.GlobalSteering.Source, err)
	}
	agentContent := []byte("# atomic-test-agent\n")
	if err := os.WriteFile(filepath.Join(ctx, "agents", "atomic-test-agent.md"), agentContent, 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	return root
}

// Proves the projection contract: Data is the rendered body and the SHA is
// taken over those same bytes.
func TestEnumerate_DataAndDigest(t *testing.T) {
	root := setupMinimalRepo(t)

	items, err := enumerate(root)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("enumerate returned no artifacts")
	}

	for _, it := range items {
		if got := SHA256Hex(it.Data); got != it.SHA256 {
			t.Errorf("artifact %q: SHA256 %q does not match SHA256Hex(Data) %q", it.Target, it.SHA256, got)
		}
		if it.Source != "bundle/"+it.Target {
			t.Errorf("artifact %q: Source = %q, want %q", it.Target, it.Source, "bundle/"+it.Target)
		}
	}
}

func TestEnumerate_AgentPresent(t *testing.T) {
	root := setupMinimalRepo(t)

	items, err := enumerate(root)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}

	found := false
	for _, it := range items {
		if it.Target == "agents/atomic-test-agent.md" {
			found = true
			if it.Canonical != "agents/atomic-test-agent.md" {
				t.Errorf("Canonical = %q, want %q", it.Canonical, "agents/atomic-test-agent.md")
			}
			wantData := []byte("# atomic-test-agent\n")
			if string(it.Data) != string(wantData) {
				t.Errorf("Data = %q, want %q", it.Data, wantData)
			}
		}
	}
	if !found {
		t.Error("agent artifact atomic-test-agent.md not found in enumerate output")
	}
}

// The global steering source ships as Claude's user-level CLAUDE.md, and no
// artifact is sourced from a CLAUDE.md in context/.
func TestEnumerate_SteeringProjectsToClaudeGlobal(t *testing.T) {
	root := setupMinimalRepo(t)

	items, err := enumerate(root)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}

	var steering *enumeratedArtifact
	for i := range items {
		if items[i].Kind == "claude-md" {
			steering = &items[i]
		}
		if items[i].Canonical == "CLAUDE.md" {
			t.Errorf("artifact %q is still sourced from a context/CLAUDE.md", items[i].Target)
		}
	}
	if steering == nil {
		t.Fatal("no claude-md artifact in enumerate output")
	}
	if steering.Target != bundlespec.GlobalSteering.ClaudeTarget {
		t.Errorf("steering Target = %q, want %q", steering.Target, bundlespec.GlobalSteering.ClaudeTarget)
	}
	if steering.Canonical != bundlespec.GlobalSteering.Source {
		t.Errorf("steering Canonical = %q, want %q", steering.Canonical, bundlespec.GlobalSteering.Source)
	}
}

// An artifact deleted from context/ must leave the embedded bundle too, or the
// binary keeps shipping it.
func TestRun_RemovesStaleBundleFiles(t *testing.T) {
	root := setupMinimalRepo(t)
	outDir := t.TempDir()

	stale := filepath.Join(outDir, "bundle", "commands", "deleted.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(outDir, "manifest.go")
	if err := os.WriteFile(sibling, []byte("package embedded\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(root, outDir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale bundle file survived Run: stat err = %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("Run removed a file outside bundle/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "bundle", "agents", "atomic-test-agent.md")); err != nil {
		t.Errorf("current artifact missing after Run: %v", err)
	}
}

func TestRun_RefusesEmptyOutDir(t *testing.T) {
	root := setupMinimalRepo(t)
	if _, err := Run(root, ""); err == nil {
		t.Fatal("Run with empty outDir succeeded; want an error")
	}
}
