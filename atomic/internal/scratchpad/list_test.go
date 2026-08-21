package scratchpad

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func slugs(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Slug
	}
	sort.Strings(out)
	return out
}

// List must skip a directory with no meta.toml of its own — descending into
// it and stopping only at the first directory that has one. A pre-migration
// reminders/ dir sitting beside real bundles is exactly this shape.
func TestListDescendsThroughDirWithNoMetaThenStopsAtFirstBundle(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "a", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}

	scratchpadRoot := bundleDir(root, "a")
	scratchpadRoot = filepath.Dir(scratchpadRoot) // <root's scratchpad dir>

	// A legacy reminders/ dir has no meta.toml itself, but its own child does
	// (simulating a nested bundle-shaped dir under a non-bundle container).
	nested := filepath.Join(scratchpadRoot, "reminders", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "meta.toml"), []byte(
		"slug = \"b\"\npurposes = [\"implement\"]\ncreated = \"2026-08-01T00:00:00Z\"\nupdated = \"2026-08-01T00:00:00Z\"\nstatus = \"active\"\n",
	), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, _, err := List(scratchpadRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := slugs(entries)
	want := []string{"a", "b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("List slugs = %v, want %v", got, want)
	}
}

// A bundle's own subdirectories (e.g. lenses/, findings/) are never walked
// further once its meta.toml is found — even when they hold files.
func TestListStopsAtBundleAndDoesNotWalkItsSubdirs(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "reviewed", "review"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := bundleDir(root, "reviewed")
	if err := os.WriteFile(filepath.Join(dir, "findings", "lens-a.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, _, err := List(filepath.Dir(dir))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "reviewed" {
		t.Fatalf("List = %+v, want exactly one entry for 'reviewed'", entries)
	}
}

// A dot-prefixed directory is never walked, even if something meta.toml-shaped
// lives inside it.
func TestListNeverWalksDotPrefixedDirs(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "visible", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	scratchpadRoot := filepath.Dir(bundleDir(root, "visible"))

	hidden := filepath.Join(scratchpadRoot, ".archive", "ghost")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "meta.toml"), []byte(
		"slug = \"ghost\"\npurposes = [\"implement\"]\ncreated = \"2026-08-01T00:00:00Z\"\nupdated = \"2026-08-01T00:00:00Z\"\nstatus = \"active\"\n",
	), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, _, err := List(scratchpadRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "visible" {
		t.Fatalf("List = %+v, want exactly one entry for 'visible' (dot dir must be skipped)", entries)
	}
}

// The archive root is two levels (<slug>/<created>/meta.toml) but resolves
// through the exact same List rule as the one-level live root.
func TestListResolvesTwoLevelArchiveShapeWithSameRule(t *testing.T) {
	archiveRoot := t.TempDir()

	first := filepath.Join(archiveRoot, "my-feature", "2026-08-01")
	second := filepath.Join(archiveRoot, "my-feature", "2026-08-15")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte(
			"slug = \"my-feature\"\npurposes = [\"implement\"]\ncreated = \"2026-08-01T00:00:00Z\"\nupdated = \"2026-08-01T00:00:00Z\"\nstatus = \"archived\"\n",
		), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	entries, _, err := List(archiveRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List = %+v, want 2 entries (one per dated archive)", entries)
	}
	for _, e := range entries {
		if e.Slug != "my-feature" {
			t.Errorf("entry slug = %q, want my-feature", e.Slug)
		}
		if e.Path != first && e.Path != second {
			t.Errorf("entry path = %q, want one of %q/%q", e.Path, first, second)
		}
	}
}

// A meta.toml that is itself a directory must not abort the walk — the
// healthy sibling bundle still lists, and the corruption is reported as a
// warning the caller can surface, rather than written through the stdlib
// global logger (F-3: the only non-test use of `log` in the codebase, which
// left a long-running `atomic serve` process with no route to surface it and
// no way for a test to assert on it).
func TestListSkipsMetaTomlThatIsADirectory(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "healthy", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	scratchpadRoot := filepath.Dir(bundleDir(root, "healthy"))

	brokenMeta := filepath.Join(scratchpadRoot, "broken", "meta.toml")
	if err := os.MkdirAll(brokenMeta, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	entries, warnings, err := List(scratchpadRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "healthy" {
		t.Fatalf("List = %+v, want exactly one entry for 'healthy'", entries)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one warning naming the corrupt entry", warnings)
	}
	if !strings.Contains(warnings[0], brokenMeta) && !strings.Contains(warnings[0], filepath.Dir(brokenMeta)) {
		t.Errorf("warning %q does not name the corrupt path %q", warnings[0], filepath.Dir(brokenMeta))
	}
}

// A meta.toml with unparseable TOML must not abort the walk — the healthy
// sibling bundle still lists.
func TestListSkipsMetaTomlWithUnparseableTOML(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "healthy", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	scratchpadRoot := filepath.Dir(bundleDir(root, "healthy"))

	brokenDir := filepath.Join(scratchpadRoot, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "meta.toml"), []byte("not [ valid toml"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, _, err := List(scratchpadRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "healthy" {
		t.Fatalf("List = %+v, want exactly one entry for 'healthy'", entries)
	}
}

// A 0-byte meta.toml parses without error (toml.Unmarshal on empty input
// returns a zero-value struct) but its Slug is empty — structurally
// incomplete, not just corrupt-syntax. It must be skipped the same way a
// malformed or directory-shaped meta.toml is, not listed as a phantom entry.
func TestListSkipsMetaTomlWithEmptySlug(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "healthy", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	scratchpadRoot := filepath.Dir(bundleDir(root, "healthy"))

	emptyDir := filepath.Join(scratchpadRoot, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(emptyDir, "meta.toml"), nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, _, err := List(scratchpadRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "healthy" {
		t.Fatalf("List = %+v, want exactly one entry for 'healthy'", entries)
	}
}

// A missing root (e.g. no archive ever written) is not an error — it lists
// as empty.
func TestListOnMissingRootReturnsEmpty(t *testing.T) {
	entries, _, err := List(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("List = %+v, want empty", entries)
	}
}
