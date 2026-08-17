package docs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
)

// files maps relative path → content.
func makeDocRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("makeDocRepo: mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("makeDocRepo: write %s: %v", rel, err)
		}
	}
	return root
}

func TestScan_HeadingExtraction(t *testing.T) {
	root := makeDocRepo(t, map[string]string{
		"docs/guide.md": `# Getting Started

## Installation

Follow these steps to install.

## Configuration

Set up your config file.

## Usage

Run the binary.

## Extra Section

This fourth section should not appear.
`,
	})

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	cachePath := filepath.Join(root, ".claude/project/doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	out := string(data)

	if !strings.Contains(out, "Getting Started") {
		t.Errorf("expected H1 'Getting Started' in output:\n%s", out)
	}
	for _, h2 := range []string{"Installation", "Configuration", "Usage"} {
		if !strings.Contains(out, h2) {
			t.Errorf("expected H2 %q in output:\n%s", h2, out)
		}
	}
	if strings.Contains(out, "Extra Section") {
		t.Errorf("H2 'Extra Section' (4th) should not appear in output:\n%s", out)
	}
}

func TestScan_SignalsIgnoreExclusion(t *testing.T) {
	root := makeDocRepo(t, map[string]string{
		"docs/included.md": `# Included

## Section A

Content here.
`,
		"docs/excluded.md": `# Excluded

## Should Not Appear

This file is excluded.
`,
		".signalsignore": "excluded.md\n",
	})

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	cachePath := filepath.Join(root, ".claude/project/doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	out := string(data)

	if !strings.Contains(out, "Included") {
		t.Errorf("expected 'Included' in output:\n%s", out)
	}
	if strings.Contains(out, "Excluded") {
		t.Errorf("excluded file content should not appear in output:\n%s", out)
	}
}

func TestScan_CacheFileWritten(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	root := makeDocRepo(t, map[string]string{
		"docs/simple.md": `# Simple Doc

## Overview

A simple overview.
`,
	})

	opts := &docs.Options{
		Clock: func() time.Time { return fixedTime },
	}
	if err := docs.ScanWithOptions(root, opts); err != nil {
		t.Fatalf("ScanWithOptions: %v", err)
	}

	cachePath := filepath.Join(root, ".claude/project/doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	out := string(data)

	if !strings.Contains(out, "2024-01-15") {
		t.Errorf("expected timestamp '2024-01-15' in output:\n%s", out)
	}
	if !strings.Contains(out, "# Doc surfaces") {
		t.Errorf("expected '# Doc surfaces' header in output:\n%s", out)
	}
}

func TestScan_NoDocs(t *testing.T) {
	root := makeDocRepo(t, nil)

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan on empty repo: %v", err)
	}

	cachePath := filepath.Join(root, ".claude/project/doc-surfaces.md")
	_, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file should exist even with no docs: %v", err)
	}
}

// Scans the committed testdata/ fixtures, copied to a temp workspace so the
// cache write never touches the committed tree.
func TestScan_GoldenFixtures(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata")); err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".claude/project/doc-surfaces.md"))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	out := string(data)

	for _, want := range []string{"Project README", "Getting Started", "API Reference"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected title %q in scan output:\n%s", want, out)
		}
	}
	for _, want := range []string{"Overview", "Installation", "Authentication"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected H2 %q in scan output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Excluded Doc") {
		t.Errorf("excluded.md should be filtered by .signalsignore:\n%s", out)
	}
}

func TestStale_FreshCache(t *testing.T) {
	root := makeDocRepo(t, map[string]string{
		"docs/guide.md": `# Guide

## Setup

Install here.
`,
	})

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if err := docs.Stale(root); err != nil {
		t.Errorf("expected Stale to return nil for fresh cache, got: %v", err)
	}
}

func TestStale_StaleAfterDocTouch(t *testing.T) {
	root := makeDocRepo(t, map[string]string{
		"docs/guide.md": `# Guide

## Setup

Install here.
`,
	})

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	docPath := filepath.Join(root, "docs/guide.md")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(docPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := docs.Stale(root); err == nil {
		t.Error("expected Stale to return error after doc file was touched")
	} else if err != docs.ErrStale {
		t.Errorf("expected ErrStale, got: %v", err)
	}
}

// A deletion bumps no surviving file's mtime, so only the set-drift check
// catches it.
func TestStale_StaleAfterDocDeleted(t *testing.T) {
	root := makeDocRepo(t, map[string]string{
		"docs/keep.md": `# Keep

## Setup

Stays.
`,
		"docs/remove.md": `# Remove

## Setup

Goes away.
`,
	})

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// The cache stays the newest mtime after this, so mtime alone says "fresh".
	if err := os.Remove(filepath.Join(root, "docs/remove.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := docs.Stale(root); err == nil {
		t.Error("expected Stale to return error after a cached doc was deleted")
	} else if err != docs.ErrStale {
		t.Errorf("expected ErrStale, got: %v", err)
	}
}

func TestStale_MissingCache(t *testing.T) {
	root := makeDocRepo(t, nil)

	err := docs.Stale(root)
	if err == nil {
		t.Error("expected error when cache does not exist, got nil")
	}
}

// The cache path follows the resolved harness dir, not a hardcoded .claude/.
func TestScan_HarnessAware(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := makeDocRepo(t, map[string]string{
		"docs/guide.md": `# Getting Started

## Installation

Follow these steps to install.
`,
	})

	if err := docs.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	cachePath := filepath.Join(root, ".pi/project/doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache under .pi harness dir: %v", err)
	}
	if !strings.Contains(string(data), "Getting Started") {
		t.Errorf("expected H1 'Getting Started' in output:\n%s", string(data))
	}

	if _, err := os.Stat(filepath.Join(root, ".claude/project/doc-surfaces.md")); err == nil {
		t.Error("cache must not be written under the default .claude harness dir when harness.dir=.pi")
	}
}
