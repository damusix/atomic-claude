package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/docs"
)

// TestRunDocsScanDispatch verifies that docsAction("scan") writes the cache
// file to the repo root. Encodes the WHY: CLI wiring must reach the correct
// package function through the dispatch switch; a misconfigured import path
// or switch fall-through would silently produce no output.
func TestRunDocsScanDispatch(t *testing.T) {
	root := t.TempDir()
	// Create a docs/ dir so Scan has something to walk.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "index.md"), []byte("# Index\n\n## Intro\n"), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}

	// Exercise the dispatch switch, not docs.Scan directly.
	code := docsAction([]string{"scan"}, root)
	if code != 0 {
		t.Fatalf("docsAction(scan) returned exit code %d, want 0", code)
	}

	cachePath := filepath.Join(root, ".claude", "project", "doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not written by docsAction(scan): %v", err)
	}
	if !strings.Contains(string(data), "docs/index.md") {
		t.Errorf("cache missing 'docs/index.md'; got:\n%s", string(data))
	}
}

// TestRunDocsStaleDispatch verifies that docsAction("stale") returns the
// correct exit codes. Encodes the WHY: exit codes are the contract for CI
// consumers; the mapping nil→0, ErrStale→1, other error→2 must be exercised
// through the dispatch switch, not by calling docs.Stale directly.
func TestRunDocsStaleDispatch(t *testing.T) {
	root := t.TempDir()

	// No cache yet → non-ErrStale error (cache missing) → exit code 2.
	code := docsAction([]string{"stale"}, root)
	if code != 2 {
		t.Fatalf("docsAction(stale) with no cache: got exit code %d, want 2", code)
	}

	// Create a docs dir + file, scan to produce a fresh cache.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# Guide\n"), 0o644); err != nil {
		t.Fatalf("write guide.md: %v", err)
	}
	if err := docs.Scan(root); err != nil {
		t.Fatalf("docs.Scan: %v", err)
	}

	// After a fresh scan the cache is current → exit code 0.
	code = docsAction([]string{"stale"}, root)
	if code != 0 {
		t.Errorf("docsAction(stale) after fresh scan: got exit code %d, want 0", code)
	}
}

// TestRunDocsNoSubcommandUsage verifies that docsAction with no subcommand
// returns exit code 1. Encodes the WHY: every other dispatch function in
// main.go returns a non-zero code when called with no verb; docs must follow
// the same contract. A zero return here would silently succeed on `atomic docs`.
func TestRunDocsNoSubcommandUsage(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{}, root)
	if code != 1 {
		t.Errorf("docsAction with no args: got exit code %d, want 1", code)
	}
}

// TestRunDocsUnknownVerbDispatch verifies that docsAction with an unknown verb
// returns exit code 1. Encodes the WHY: unknown verbs must not silently
// succeed or fall through to a no-op.
func TestRunDocsUnknownVerbDispatch(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{"bogus"}, root)
	if code != 1 {
		t.Errorf("docsAction(bogus): got exit code %d, want 1", code)
	}
}
