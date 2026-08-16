package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(entries []dirEntry, name string) (dirEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return dirEntry{}, false
}

// A listing of extensionless slugs says nothing about any of its entries, and
// cannot be told apart from a listing of folders.
func TestListDirEntries_DescribesEntries(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "docs", "titled.md"),
		"---\ntitle: Getting started\ndescription: How to set the thing up.\n---\n\n# Ignored heading\n")
	write(t, filepath.Join(root, "docs", "headed.md"),
		"# Heading wins\n\nThe opening prose becomes the summary.\n")
	write(t, filepath.Join(root, "docs", "sub", "index.md"),
		"---\ntitle: Subsection\n---\n\n# Sub\n")
	write(t, filepath.Join(root, "docs", "bare", "note.md"), "# Note\n")

	entries, ok := listDirEntries(root, "docs")
	if !ok {
		t.Fatal("listDirEntries failed")
	}

	titled, found := find(entries, "titled")
	if !found {
		t.Fatal("missing titled entry")
	}
	if titled.Title != "Getting started" {
		t.Errorf("Title = %q, want the frontmatter title", titled.Title)
	}
	if titled.Summary != "How to set the thing up." {
		t.Errorf("Summary = %q, want the frontmatter description", titled.Summary)
	}
	// The filename is how the entry is addressed; Name has it stripped.
	if titled.Filename != "titled.md" {
		t.Errorf("Filename = %q, want titled.md", titled.Filename)
	}

	headed, found := find(entries, "headed")
	if !found {
		t.Fatal("missing headed entry")
	}
	if headed.Title != "Heading wins" {
		t.Errorf("Title = %q, want the document's own heading when frontmatter has none", headed.Title)
	}
	if headed.Summary == "" {
		t.Error("Summary is empty; the opening prose should stand in for a missing description")
	}

	// A folder with an index opens a document — it is described by that
	// document, and says so.
	sub, found := find(entries, "sub")
	if !found {
		t.Fatal("missing sub entry")
	}
	if sub.Index != "docs/sub/index.md" {
		t.Errorf("Index = %q, want docs/sub/index.md", sub.Index)
	}
	if sub.Title != "Subsection" {
		t.Errorf("Title = %q, want the index file's title", sub.Title)
	}

	// A folder without one opens another listing and has nothing to describe.
	bare, found := find(entries, "bare")
	if !found {
		t.Fatal("missing bare entry")
	}
	if bare.Index != "" {
		t.Errorf("Index = %q, want empty for a folder with no index file", bare.Index)
	}
}

// A summary is a listing entry, not an excerpt — it stops at a word break so
// it never ends mid-word.
func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("short", 150); got != "short" {
		t.Errorf("short string was altered: %q", got)
	}

	long := strings.Repeat("alpha beta ", 40)
	got := truncateRunes(long, summaryCap)
	if len([]rune(got)) > summaryCap+1 {
		t.Errorf("length %d exceeds the cap", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated summary should be marked with an ellipsis: %q", got)
	}
	if strings.Contains(got, "alph…") {
		t.Errorf("cut mid-word: %q", got)
	}
}
