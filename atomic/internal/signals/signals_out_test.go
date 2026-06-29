package signals_test

// CP3: tests for signals scan --out redirect.
// These verify:
//   1. With OutDir set, substrate writes to OutDir; scanned repo is never written.
//   2. Without OutDir, behavior is identical to before (default path).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/signals"
)

// TestScanWithOut_WritesToOutDir asserts that when OutDir is set, the
// deterministic substrate is written under OutDir, and the scanned repo
// has no docs/wiki/ directory created.
func TestScanWithOut_WritesToOutDir(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	outDir := t.TempDir()

	opts := &signals.Options{
		OutDir: outDir,
	}
	if err := signals.ScanWithOptions(repo, opts); err != nil {
		t.Fatalf("ScanWithOptions: %v", err)
	}

	// Substrate must exist under outDir.
	wantPath := filepath.Join(outDir, "docs", "wiki", "scan.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("substrate not written to outDir: %v (expected %s)", err, wantPath)
	}

	// The scanned repo must NOT have docs/wiki/ created.
	repoWiki := filepath.Join(repo, "docs", "wiki")
	if _, err := os.Stat(repoWiki); err == nil {
		t.Errorf("scanned repo has docs/wiki/ created — it must not be written when OutDir is set")
	}
}

// TestScanWithOut_DefaultUnchanged asserts that without OutDir the substrate
// still lands in the repo's docs/wiki/scan.md path (default behavior unchanged).
func TestScanWithOut_DefaultUnchanged(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	if err := signals.ScanWithOptions(repo, nil); err != nil {
		t.Fatalf("ScanWithOptions default: %v", err)
	}

	wantPath := filepath.Join(repo, "docs", "wiki", "scan.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("default substrate not at expected path: %v", err)
	}
}

// TestScanWithOut_ContentMatchesDefault verifies that the substrate content
// written to OutDir is byte-identical to what a default scan would produce
// for the same repo. This proves --out is an additive redirect, not a fork.
func TestScanWithOut_ContentMatchesDefault(t *testing.T) {
	// Build two identical repos.
	files := map[string]string{"main.go": "package main\n"}
	repoA := makeRepo(t, files)
	repoB := makeRepo(t, files)

	// Default scan on repoA.
	if err := signals.ScanWithOptions(repoA, &signals.Options{}); err != nil {
		t.Fatalf("default scan: %v", err)
	}
	defaultPath := filepath.Join(repoA, "docs", "wiki", "scan.md")
	defaultBytes, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatalf("read default substrate: %v", err)
	}

	// --out scan on repoB.
	outDir := t.TempDir()
	if err := signals.ScanWithOptions(repoB, &signals.Options{OutDir: outDir}); err != nil {
		t.Fatalf("out scan: %v", err)
	}
	outPath := filepath.Join(outDir, "docs", "wiki", "scan.md")
	outBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out substrate: %v", err)
	}

	if string(defaultBytes) != string(outBytes) {
		t.Errorf("--out content differs from default content\ndefault:\n%s\n\nout:\n%s", defaultBytes, outBytes)
	}
}
