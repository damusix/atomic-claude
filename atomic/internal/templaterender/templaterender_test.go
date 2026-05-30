package templaterender_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

// mkdirWrite creates a file at dir/name with the given content.
func mkdirWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestEmptyNoOp verifies SC 1: empty templates/ + empty commands/ is a no-op.
// Render must succeed and write nothing.
func TestEmptyNoOp(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	// Create empty templates/commands/ and templates/shared/ dirs.
	if err := os.MkdirAll(filepath.Join(root, "templates", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "templates", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	// outDir/commands/ doesn't exist yet — that's fine.

	if err := templaterender.Run(root, outDir); err != nil {
		t.Fatalf("Run on empty dirs: %v", err)
	}

	// No files should have been written.
	entries, _ := os.ReadDir(filepath.Join(outDir, "commands"))
	if len(entries) != 0 {
		t.Errorf("expected zero output files, got %d", len(entries))
	}
}

// TestSingleFileRender verifies SC 2: a single template produces the correct output.
func TestSingleFileRender(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	mkdirWrite(t, filepath.Join(root, "templates", "commands"), "foo.md", "# hello\n\nworld\n")
	if err := os.MkdirAll(filepath.Join(root, "templates", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := templaterender.Run(root, outDir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "commands", "foo.md"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := "# hello\n\nworld\n"
	if string(got) != want {
		t.Errorf("output mismatch\ngot:  %q\nwant: %q", string(got), want)
	}
}

// TestSharedPartialComposition verifies SC 3: shared partials are callable via
// {{ template "<name>" . }} from source templates.
func TestSharedPartialComposition(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	// Shared partial: "greeting"
	mkdirWrite(t, filepath.Join(root, "templates", "shared"), "greeting.md",
		`{{- define "greeting" -}}Hello, world!{{- end -}}`)

	// Source template that includes the shared partial.
	mkdirWrite(t, filepath.Join(root, "templates", "commands"), "bar.md",
		"# Bar\n\n{{ template \"greeting\" . }}\n")

	if err := templaterender.Run(root, outDir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "commands", "bar.md"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := "# Bar\n\nHello, world!\n"
	if string(got) != want {
		t.Errorf("partial composition mismatch\ngot:  %q\nwant: %q", string(got), want)
	}
}

// TestOrphanDetection verifies SC 4: an output file without a matching template
// causes a non-zero exit with an error message naming both remediation paths.
func TestOrphanDetection(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	// Empty template dirs (no template for "orphan.md").
	if err := os.MkdirAll(filepath.Join(root, "templates", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "templates", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}

	// But there's an existing output file with no corresponding template.
	mkdirWrite(t, filepath.Join(outDir, "commands"), "orphan.md", "# orphan\n")

	err := templaterender.Run(root, outDir)
	if err == nil {
		t.Fatal("expected error for orphan output file, got nil")
	}

	msg := err.Error()
	// Must name both remediation paths.
	if !strings.Contains(msg, "templates/commands/orphan.md") {
		t.Errorf("error message missing 'create template' remediation path (templates/commands/orphan.md): %s", msg)
	}
	if !strings.Contains(msg, "rm") {
		t.Errorf("error message missing 'rm' remediation path: %s", msg)
	}
	if !strings.Contains(msg, "commands/orphan.md") {
		t.Errorf("error message missing output file path: %s", msg)
	}
}

// TestAgentKindRender verifies that templates/agents/<name>.md renders to
// agents/<name>.md, sharing the same shared-partial pool as commands.
func TestAgentKindRender(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	// A shared partial usable by both kinds.
	mkdirWrite(t, filepath.Join(root, "templates", "shared"), "agent-rule.md",
		`{{- define "agent-rule" -}}Quote errors exactly.{{- end -}}`)

	// An agent template that pulls the shared partial.
	mkdirWrite(t, filepath.Join(root, "templates", "agents"), "atomic-builder.md",
		"---\nname: atomic-builder\n---\n\n## Rules\n\n- {{ template \"agent-rule\" . }}\n")

	// A command template alongside it, proving both kinds render in one pass.
	mkdirWrite(t, filepath.Join(root, "templates", "commands"), "foo.md", "# foo\n")

	if err := templaterender.Run(root, outDir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "agents", "atomic-builder.md"))
	if err != nil {
		t.Fatalf("read agent output: %v", err)
	}
	want := "---\nname: atomic-builder\n---\n\n## Rules\n\n- Quote errors exactly.\n"
	if string(got) != want {
		t.Errorf("agent render mismatch\ngot:  %q\nwant: %q", string(got), want)
	}

	// The command kind must still render in the same pass.
	if _, err := os.ReadFile(filepath.Join(outDir, "commands", "foo.md")); err != nil {
		t.Errorf("command kind did not render alongside agents: %v", err)
	}
}

// TestAgentOrphanDetection verifies the orphan rule applies to the agents kind:
// an agents/<name>.md with no matching template halts with an error naming the
// agent-specific remediation paths.
func TestAgentOrphanDetection(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "templates", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "templates", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Output agent with no corresponding template.
	mkdirWrite(t, filepath.Join(outDir, "agents"), "ghost.md", "---\nname: ghost\n---\n")

	err := templaterender.Run(root, outDir)
	if err == nil {
		t.Fatal("expected error for orphan agent output, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "templates/agents/ghost.md") {
		t.Errorf("error missing create-template remediation (templates/agents/ghost.md): %s", msg)
	}
	if !strings.Contains(msg, "agents/ghost.md") {
		t.Errorf("error missing orphan output path (agents/ghost.md): %s", msg)
	}
}

// TestDeterministicOutput verifies that Run produces the same result on repeated
// calls (no timestamps, env reads, or ordering non-determinism).
func TestDeterministicOutput(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "templates", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkdirWrite(t, filepath.Join(root, "templates", "commands"), "a.md", "alpha\n")
	mkdirWrite(t, filepath.Join(root, "templates", "commands"), "b.md", "beta\n")

	outDir1 := t.TempDir()
	outDir2 := t.TempDir()

	if err := templaterender.Run(root, outDir1); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := templaterender.Run(root, outDir2); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	for _, name := range []string{"a.md", "b.md"} {
		b1, err := os.ReadFile(filepath.Join(outDir1, "commands", name))
		if err != nil {
			t.Fatal(err)
		}
		b2, err := os.ReadFile(filepath.Join(outDir2, "commands", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b1) != string(b2) {
			t.Errorf("%s differs between runs: %q vs %q", name, b1, b2)
		}
	}
}
