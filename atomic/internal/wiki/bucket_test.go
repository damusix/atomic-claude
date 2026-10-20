package wiki_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// wikiRoot builds a temporary wiki root with a wiki/ subdirectory.
func wikiRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatalf("mkdir wiki: %v", err)
	}
	return root
}

func assertWikiDirEmpty(t *testing.T, wikiDir string) {
	t.Helper()
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("RegisterBucket created wiki/%s", e.Name())
	}
}

func TestRegisterBucket_CreatesNothing(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}
	assertWikiDirEmpty(t, wikiDir)
}

func TestRegisterBucket_WikiNameRefused(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	err := wiki.RegisterBucket(wikiDir, "wiki")
	if err == nil {
		t.Fatal("expected error for bucket name 'wiki', got nil")
	}
}

func TestRegisterBucket_RegistryDuplicateRefused(t *testing.T) {
	root := wikiRoot(t)
	wikiDir := filepath.Join(root, "wiki")
	index := "<wiki-buckets>\n<bucket name=\"repos\" path=\"" + filepath.Join(root, "repos") + "\"/>\n</wiki-buckets>\n"
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := wiki.RegisterBucket(wikiDir, "repos"); err == nil {
		t.Fatal("expected error for a name already in the registry, got nil")
	}
	if err := wiki.RegisterBucket(wikiDir, "other"); err != nil {
		t.Fatalf("unregistered name refused: %v", err)
	}
}

// TestRegisterBucket_UnsafeNamesRejected covers the RegisterBucket-level
// backstop behind the CLI arg scanner: a caller that reaches
// RegisterBucket programmatically, bypassing the CLI parser entirely, must
// still be refused an unsafe name.
func TestRegisterBucket_UnsafeNamesRejected(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"-h",
		"-",
		"--help",
		"a/b",
		"a\\b",
		".",
		"..",
		"wiki",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			wikiDir := filepath.Join(wikiRoot(t), "wiki")
			if err := wiki.RegisterBucket(wikiDir, name); err == nil {
				t.Fatalf("RegisterBucket(%q): expected error, got nil", name)
			}
			assertWikiDirEmpty(t, wikiDir)
		})
	}
}

// TestRegisterBucket_OrdinaryNamesAccepted is the back-compat guard: name
// validation must reject only unsafe shapes, never ordinary bucket names
// already in use in the wild.
func TestRegisterBucket_OrdinaryNamesAccepted(t *testing.T) {
	names := []string{
		"content-ideas",
		"social-media",
		"facts",
		"raw",
		"research",
		"tickets",
		"under_scored",
		"has123digits",
		"MixedCase",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			wikiDir := filepath.Join(wikiRoot(t), "wiki")
			if err := wiki.RegisterBucket(wikiDir, name); err != nil {
				t.Fatalf("RegisterBucket(%q): unexpected error: %v", name, err)
			}
		})
	}
}
