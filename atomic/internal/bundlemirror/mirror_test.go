package bundlemirror

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

// setupMinimalRepo builds the smallest context/ tree enumerate can walk without
// erroring: every expected directory, a CLAUDE.md, and one agent file.
func setupMinimalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ctx := bundlespec.SourceRoot(root)
	for _, dir := range []string{"agents", "skills", "output-styles", "commands", "rules"} {
		if err := os.MkdirAll(filepath.Join(ctx, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(ctx, "CLAUDE.md"), []byte("# CLAUDE\n"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	agentContent := []byte("# atomic-test-agent\n")
	if err := os.WriteFile(filepath.Join(ctx, "agents", "atomic-test-agent.md"), agentContent, 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	return root
}

// Proves the single-read contract: Data matches disk and the SHA is taken over
// those same bytes.
func TestEnumerate_SrcPathAndData(t *testing.T) {
	root := setupMinimalRepo(t)

	items, err := enumerate(root)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("enumerate returned no artifacts")
	}

	for _, it := range items {
		wantSrc := filepath.Join(bundlespec.SourceRoot(root), filepath.FromSlash(it.Target))
		if it.SrcPath != wantSrc {
			t.Errorf("artifact %q: SrcPath = %q, want %q", it.Target, it.SrcPath, wantSrc)
		}

		diskBytes, err := os.ReadFile(wantSrc)
		if err != nil {
			t.Fatalf("read %s for comparison: %v", wantSrc, err)
		}
		if string(it.Data) != string(diskBytes) {
			t.Errorf("artifact %q: Data does not match disk content", it.Target)
		}

		if got := SHA256Hex(it.Data); got != it.SHA256 {
			t.Errorf("artifact %q: SHA256 %q does not match SHA256Hex(Data) %q", it.Target, it.SHA256, got)
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
			wantSrc := filepath.Join(bundlespec.SourceRoot(root), "agents", "atomic-test-agent.md")
			if it.SrcPath != wantSrc {
				t.Errorf("SrcPath = %q, want %q", it.SrcPath, wantSrc)
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
