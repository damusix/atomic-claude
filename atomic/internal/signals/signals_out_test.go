package signals_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/signals"
)

// OutDir takes the substrate, and the scanned repo gains no docs/wiki/.
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

	wantPath := filepath.Join(outDir, "docs", "wiki", "scan.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("substrate not written to outDir: %v (expected %s)", err, wantPath)
	}

	repoWiki := filepath.Join(repo, "docs", "wiki")
	if _, err := os.Stat(repoWiki); err == nil {
		t.Errorf("scanned repo has docs/wiki/ created — it must not be written when OutDir is set")
	}
}

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

// Byte-identical output proves --out is a redirect, not a fork.
func TestScanWithOut_ContentMatchesDefault(t *testing.T) {
	files := map[string]string{"main.go": "package main\n"}
	repoA := makeRepo(t, files)
	repoB := makeRepo(t, files)

	if err := signals.ScanWithOptions(repoA, &signals.Options{}); err != nil {
		t.Fatalf("default scan: %v", err)
	}
	defaultPath := filepath.Join(repoA, "docs", "wiki", "scan.md")
	defaultBytes, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatalf("read default substrate: %v", err)
	}

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
