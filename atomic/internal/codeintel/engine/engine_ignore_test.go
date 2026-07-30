// Package engine tests — graphignore config wiring (checkpoint 2).
//
// ensureIndexer loads .claude/atomic.toml once per indexer boot and wires the
// resulting matcher onto the orchestrator. These tests exercise that wiring
// end-to-end through the public Engine API (New/Init/IndexAll), which is the
// only way to reach ensureIndexer — each test pays the tree-sitter pool boot
// once (see ensureIndexer's doc comment on cost), consistent with every other
// engine-level test in this package.
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
)

// TestIndexAll_AbsentIgnoreConfig_OutputUnchanged is the checkpoint 2
// "absent config" success criterion: with no .claude/atomic.toml present,
// discovery output is identical to pre-graphignore behavior — every file is
// indexed and no warnings are recorded.
func TestIndexAll_AbsentIgnoreConfig_OutputUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "func Main() {}")
	writeGoFile(t, dir, "other.go", "func Other() {}")

	ctx := context.Background()
	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}
	if err := e.IndexAll(ctx); err != nil {
		t.Fatal("IndexAll:", err)
	}

	if warns := e.IgnoreWarnings(); len(warns) != 0 {
		t.Errorf("no .claude/atomic.toml present: expected no ignore warnings, got %v", warns)
	}

	files, err := e.GetFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 indexed files (no ignore config present), got %d", len(files))
	}

	if count, _ := e.IgnorePatternInfo(); count != 0 {
		t.Errorf("IgnorePatternInfo count = %d, want 0 (no config file)", count)
	}
}

// TestIndexAll_IgnoreConfigFiltersAndWarns proves the full config→matcher
// wiring: a valid pattern excludes the matching file from the index, an
// invalid pattern mixed into the same array degrades to a warning (not a
// hard failure), and IgnorePatternInfo reports only the successfully
// compiled pattern.
func TestIndexAll_IgnoreConfigFiltersAndWarns(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, dir, "main.go", "func Main() {}")
	// "external", not "vendor": walkDirFallback (used here — this fixture has
	// no git repo) already skips a literal "vendor" directory by name, which
	// would make the test pass for the wrong reason (structural skip, not the
	// ignore matcher under test).
	writeGoFile(t, dir, filepath.Join("external", "lib.go"), "func Lib() {}")

	tomlContent := "[code]\nignore = [\"external/**\", \"vendor[/**\"]\n"
	if err := os.WriteFile(filepath.Join(dir, ".claude", "atomic.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}
	if err := e.IndexAll(ctx); err != nil {
		t.Fatal("IndexAll:", err)
	}

	warns := e.IgnoreWarnings()
	if len(warns) == 0 {
		t.Fatal("expected at least one warning for the invalid pattern")
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "vendor[/**") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings %v do not mention the invalid pattern", warns)
	}

	if _, err := e.GetFile(ctx, filepath.Join("external", "lib.go")); err == nil {
		t.Error("external/lib.go should be excluded from the index")
	}
	if _, err := e.GetFile(ctx, "main.go"); err != nil {
		t.Errorf("main.go should be indexed: %v", err)
	}

	count, path := e.IgnorePatternInfo()
	if count != 1 {
		t.Errorf("IgnorePatternInfo count = %d, want 1 (only external/** compiles)", count)
	}
	wantPath := filepath.Join(dir, ".claude", "atomic.toml")
	if path != wantPath {
		t.Errorf("IgnorePatternInfo path = %q, want %q", path, wantPath)
	}
}

// writeGoFile writes a minimal .go file with body inside a package.
func writeGoFile(t *testing.T, dir, relPath, body string) {
	t.Helper()
	abs := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "package p\n\n" + body + "\n"
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
