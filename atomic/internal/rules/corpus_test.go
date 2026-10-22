package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shipped corpus parses cleanly with the identity, scope, and body each
// family carries today.
func TestLoadShipped_ShippedCorpus(t *testing.T) {
	records, err := LoadShipped("../../../context/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}

	want := []string{
		"shipped:rules/python/style.md",
		"shipped:rules/specs/spec-currency.md",
		"shipped:rules/typescript/style.md",
	}
	if len(records) != len(want) {
		t.Fatalf("records = %d, want %d: %+v", len(records), len(want), records)
	}
	for i, id := range want {
		if records[i].ID != id {
			t.Errorf("record %d ID = %q, want %q", i, records[i].ID, id)
		}
		if len(records[i].Include) == 0 || len(records[i].Body) == 0 {
			t.Errorf("record %s lost scope or body", id)
		}
		if records[i].Class != ClassShipped || records[i].BaseKind != BaseRepositoryRoot {
			t.Errorf("record %s class/base = %q/%q", id, records[i].Class, records[i].BaseKind)
		}
	}
}

// The produced set carries no checkout-specific path, so two loads yield
// identical digests even from different working directories.
func TestLoadShipped_Deterministic(t *testing.T) {
	first, err := LoadShipped("../../../context/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	abs, err := filepath.Abs("../../../context/rules")
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadShipped(abs)
	if err != nil {
		t.Fatalf("LoadShipped(abs): %v", err)
	}
	for i := range first {
		if first[i].ID != second[i].ID || first[i].SourceDigest != second[i].SourceDigest {
			t.Errorf("record %d not checkout-independent: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestLoadWiki_AbsentProducesNothing(t *testing.T) {
	records, err := LoadWiki("../../../context/rules", "proj")
	if err != nil {
		t.Fatalf("LoadWiki: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("records = %+v, want none", records)
	}
}

// A rename changes identity — the signal an adapter uses to detect a deleted
// projection still owned at the old name — while unchanged bytes keep the
// source digest, so the same content is recognizable across the rename.
func TestLoadShipped_RenameAndDeletion(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	writeRuleFile(t, filepath.Join(rulesDir, "typescript", "style.md"), "**/*.ts")

	before, err := LoadShipped(rulesDir)
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	if len(before) != 1 || before[0].ID != "shipped:rules/typescript/style.md" {
		t.Fatalf("before = %+v", before)
	}

	if err := os.Rename(filepath.Join(rulesDir, "typescript", "style.md"), filepath.Join(rulesDir, "typescript", "conventions.md")); err != nil {
		t.Fatal(err)
	}
	after, err := LoadShipped(rulesDir)
	if err != nil {
		t.Fatalf("LoadShipped after rename: %v", err)
	}
	if len(after) != 1 || after[0].ID != "shipped:rules/typescript/conventions.md" {
		t.Fatalf("after rename = %+v", after)
	}
	if after[0].SourceDigest != before[0].SourceDigest {
		t.Errorf("renamed rule lost its content digest")
	}

	if err := os.Remove(filepath.Join(rulesDir, "typescript", "conventions.md")); err != nil {
		t.Fatal(err)
	}
	gone, err := LoadShipped(rulesDir)
	if err != nil {
		t.Fatalf("LoadShipped after delete: %v", err)
	}
	if len(gone) != 0 {
		t.Errorf("deleted rule still present: %+v", gone)
	}
}

// A malformed source fails the whole load and names the offending file; no rule
// is dropped silently.
func TestLoadShipped_MalformedSourceFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, filepath.Join(dir, "rules", "ok.md"), "**/*.go")
	writeRuleFile(t, filepath.Join(dir, "rules", "broken.md"), "**/*.go")
	if err := os.WriteFile(filepath.Join(dir, "rules", "broken.md"), []byte("---\nglobs:\n  - x\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadShipped(filepath.Join(dir, "rules"))
	if err == nil {
		t.Fatal("LoadShipped accepted a malformed source")
	}
	if !strings.Contains(err.Error(), "rules/broken.md") || !strings.Contains(err.Error(), `unknown scope key "globs"`) {
		t.Errorf("error = %q, want the source path and cause", err)
	}
}

// Shipped and wiki records collide on their native name even though their
// producer-qualified IDs differ, so a combined set fails validation.
func TestLoad_CollisionAcrossProducers(t *testing.T) {
	shipped, err := LoadShipped("testdata/collision/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	wiki, err := LoadWiki("testdata/collision/rules", "proj")
	if err != nil {
		t.Fatalf("LoadWiki: %v", err)
	}
	if len(shipped) != 1 || len(wiki) != 1 {
		t.Fatalf("shipped=%d wiki=%d", len(shipped), len(wiki))
	}
	if shipped[0].ID == wiki[0].ID {
		t.Fatalf("fixture no longer distinguishes producers: %q", shipped[0].ID)
	}
	if err := Validate(append(shipped, wiki...)); err == nil || !strings.Contains(err.Error(), "native-name collision") {
		t.Fatalf("Validate error = %v, want a native-name collision", err)
	}
}

func writeRuleFile(t *testing.T, path, pattern string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := "---\npaths:\n  - \"" + pattern + "\"\n---\n\n# Rule\n\nBody.\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
