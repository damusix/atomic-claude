package docs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/docs"
)

// makeDocRepo creates a temp directory with doc files for testing.
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

// TestScan_HeadingExtraction verifies that H1 title and first 3 H2 headings
// are extracted from doc files and written to the cache file.
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

	// H1 title must appear
	if !strings.Contains(out, "Getting Started") {
		t.Errorf("expected H1 'Getting Started' in output:\n%s", out)
	}
	// First 3 H2s must appear
	for _, h2 := range []string{"Installation", "Configuration", "Usage"} {
		if !strings.Contains(out, h2) {
			t.Errorf("expected H2 %q in output:\n%s", h2, out)
		}
	}
	// Fourth H2 must NOT appear
	if strings.Contains(out, "Extra Section") {
		t.Errorf("H2 'Extra Section' (4th) should not appear in output:\n%s", out)
	}
}

// TestScan_SignalsIgnoreExclusion verifies that files matching .signalsignore
// are excluded from the scan output.
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

// TestScan_CacheFileWritten verifies the cache file is created at the right
// path and contains the expected header and last-scanned timestamp.
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

	// Must contain last-scanned timestamp
	if !strings.Contains(out, "2024-01-15") {
		t.Errorf("expected timestamp '2024-01-15' in output:\n%s", out)
	}
	// Must have a header line
	if !strings.Contains(out, "# Doc surfaces") {
		t.Errorf("expected '# Doc surfaces' header in output:\n%s", out)
	}
}

// TestScan_NoDocs verifies that scanning a repo with no doc files produces
// a cache file with an empty surfaces list (not an error).
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

// TestStale_FreshCache verifies that Stale returns nil when the cache is newer
// than all doc files.
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

	// Cache was just written — should be fresh.
	if err := docs.Stale(root); err != nil {
		t.Errorf("expected Stale to return nil for fresh cache, got: %v", err)
	}
}

// TestStale_StaleAfterDocTouch verifies that Stale returns ErrStale after a
// doc file is modified after the cache was written.
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

	// Touch the doc file to make it newer than the cache.
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

// TestStale_MissingCache verifies that Stale returns an error when no cache
// file exists.
func TestStale_MissingCache(t *testing.T) {
	root := makeDocRepo(t, nil)

	err := docs.Stale(root)
	if err == nil {
		t.Error("expected error when cache does not exist, got nil")
	}
}
