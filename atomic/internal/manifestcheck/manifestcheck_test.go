package manifestcheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlemirror"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/manifestcheck"
)

// makeRepo builds a minimal fake repo under dir with the given artifacts on disk.
// artifacts maps target path (e.g. "agents/atomic-foo.md") to file content.
func makeRepo(t *testing.T, artifacts map[string][]byte) string {
	t.Helper()
	root := t.TempDir()

	// Create required top-level dirs so bundlemirror.Enumerate doesn't fail.
	for _, dir := range []string{"agents", "skills", "output-styles", "commands", "rules"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	for target, content := range artifacts {
		dst := filepath.Join(root, filepath.FromSlash(target))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", target, err)
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}

	return root
}

// committedFrom converts a slice of disk artifacts (from Enumerate) into the
// embedded.Artifact format expected by Compare.
func committedFrom(arts []embedded.Artifact) []embedded.Artifact {
	out := make([]embedded.Artifact, len(arts))
	copy(out, arts)
	return out
}

func TestCompare_OK(t *testing.T) {
	content := []byte("# agent\nsome content\n")
	root := makeRepo(t, map[string][]byte{
		"agents/atomic-foo.md": content,
		"commands/bar.md":      []byte("# command\n"),
		"CLAUDE.md":            []byte("# claude\n"),
	})

	live, err := bundlemirror.Enumerate(root)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	result, err := manifestcheck.Compare(root, live)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if !result.OK {
		t.Errorf("OK = false, want true")
	}
	if len(result.Missing) != 0 {
		t.Errorf("Missing = %v, want []", result.Missing)
	}
	if len(result.Extra) != 0 {
		t.Errorf("Extra = %v, want []", result.Extra)
	}
	if len(result.Drifted) != 0 {
		t.Errorf("Drifted = %v, want []", result.Drifted)
	}
}

func TestCompare_Drift(t *testing.T) {
	original := []byte("# agent original\n")
	root := makeRepo(t, map[string][]byte{
		"agents/atomic-foo.md": original,
		"CLAUDE.md":            []byte("# claude\n"),
	})

	live, err := bundlemirror.Enumerate(root)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	// Mutate the on-disk file after we captured the committed state.
	if err := os.WriteFile(filepath.Join(root, "agents/atomic-foo.md"), []byte("# agent CHANGED\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := manifestcheck.Compare(root, live)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if result.OK {
		t.Errorf("OK = true, want false")
	}
	if len(result.Drifted) != 1 {
		t.Fatalf("Drifted len = %d, want 1", len(result.Drifted))
	}
	if result.Drifted[0].Target != "agents/atomic-foo.md" {
		t.Errorf("Drifted[0].Target = %q, want %q", result.Drifted[0].Target, "agents/atomic-foo.md")
	}
	if result.Drifted[0].CommittedSHA == result.Drifted[0].GeneratedSHA {
		t.Errorf("CommittedSHA == GeneratedSHA, expected them to differ")
	}
	if len(result.Missing) != 0 {
		t.Errorf("Missing = %v, want []", result.Missing)
	}
	if len(result.Extra) != 0 {
		t.Errorf("Extra = %v, want []", result.Extra)
	}
}

func TestCompare_Missing(t *testing.T) {
	root := makeRepo(t, map[string][]byte{
		"agents/atomic-foo.md": []byte("# agent\n"),
		"CLAUDE.md":            []byte("# claude\n"),
	})

	live, err := bundlemirror.Enumerate(root)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	// Add a phantom entry to committed that doesn't exist on disk.
	phantom := embedded.Artifact{
		Kind:   "agent",
		Source: "bundle/agents/atomic-phantom.md",
		Target: "agents/atomic-phantom.md",
		SHA256: "deadbeef",
	}
	committed := append(committedFrom(live), phantom)

	result, err := manifestcheck.Compare(root, committed)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if result.OK {
		t.Errorf("OK = true, want false")
	}
	if len(result.Missing) != 1 {
		t.Fatalf("Missing len = %d, want 1", len(result.Missing))
	}
	if result.Missing[0] != "agents/atomic-phantom.md" {
		t.Errorf("Missing[0] = %q, want %q", result.Missing[0], "agents/atomic-phantom.md")
	}
	if len(result.Extra) != 0 {
		t.Errorf("Extra = %v, want []", result.Extra)
	}
	if len(result.Drifted) != 0 {
		t.Errorf("Drifted = %v, want []", result.Drifted)
	}
}

func TestCompare_Extra(t *testing.T) {
	root := makeRepo(t, map[string][]byte{
		"agents/atomic-foo.md": []byte("# agent\n"),
		"CLAUDE.md":            []byte("# claude\n"),
	})

	live, err := bundlemirror.Enumerate(root)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	// Committed is a subset: drop the agent, so disk has it but committed doesn't.
	committed := make([]embedded.Artifact, 0, len(live))
	for _, a := range live {
		if a.Target == "agents/atomic-foo.md" {
			continue
		}
		committed = append(committed, a)
	}

	result, err := manifestcheck.Compare(root, committed)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if result.OK {
		t.Errorf("OK = true, want false")
	}
	if len(result.Extra) != 1 {
		t.Fatalf("Extra len = %d, want 1", len(result.Extra))
	}
	if result.Extra[0] != "agents/atomic-foo.md" {
		t.Errorf("Extra[0] = %q, want %q", result.Extra[0], "agents/atomic-foo.md")
	}
	if len(result.Missing) != 0 {
		t.Errorf("Missing = %v, want []", result.Missing)
	}
	if len(result.Drifted) != 0 {
		t.Errorf("Drifted = %v, want []", result.Drifted)
	}
}

func TestCompare_Combined(t *testing.T) {
	root := makeRepo(t, map[string][]byte{
		"agents/atomic-foo.md": []byte("# original\n"),
		"agents/atomic-bar.md": []byte("# bar\n"),
		"CLAUDE.md":            []byte("# claude\n"),
	})

	live, err := bundlemirror.Enumerate(root)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	// Build committed: mutate foo's SHA, drop bar, add phantom.
	committed := make([]embedded.Artifact, 0, len(live)+1)
	for _, a := range live {
		switch a.Target {
		case "agents/atomic-foo.md":
			// SHA drift: keep the target but alter the committed SHA.
			a.SHA256 = "aaaaaaaabbbbbbbbccccccccdddddddd00000000111111112222222233333333"
			committed = append(committed, a)
		case "agents/atomic-bar.md":
			// Drop bar → it will appear in Extra.
		default:
			committed = append(committed, a)
		}
	}
	// Add phantom → will appear in Missing.
	committed = append(committed, embedded.Artifact{
		Kind:   "agent",
		Source: "bundle/agents/atomic-phantom.md",
		Target: "agents/atomic-phantom.md",
		SHA256: "deadbeef",
	})

	result, err := manifestcheck.Compare(root, committed)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if result.OK {
		t.Errorf("OK = true, want false")
	}
	if len(result.Drifted) != 1 || result.Drifted[0].Target != "agents/atomic-foo.md" {
		t.Errorf("Drifted = %v, want [{agents/atomic-foo.md ...}]", result.Drifted)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "agents/atomic-phantom.md" {
		t.Errorf("Missing = %v, want [agents/atomic-phantom.md]", result.Missing)
	}
	if len(result.Extra) != 1 || result.Extra[0] != "agents/atomic-bar.md" {
		t.Errorf("Extra = %v, want [agents/atomic-bar.md]", result.Extra)
	}
}
