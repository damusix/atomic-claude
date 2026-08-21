package scratchpad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func bundleDir(root, slug string) string {
	return filepath.Join(config.ScratchpadDir(root), slug)
}

// New with --purpose plan must seed BRIEF/STATE/FOLLOWUPS in the bundle and
// docs/design + docs/spec outside it, per the purpose matrix.
func TestScratchpadNewPlanSeedsBundleAndDocs(t *testing.T) {
	root := t.TempDir()

	b, extended, err := New(root, "my-feature", "plan")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if extended {
		t.Fatalf("expected extended=false for a fresh bundle")
	}
	if b.Meta.Slug != "my-feature" {
		t.Errorf("Slug = %q, want my-feature", b.Meta.Slug)
	}
	if len(b.Meta.Purposes) != 1 || b.Meta.Purposes[0] != "plan" {
		t.Errorf("Purposes = %v, want [plan]", b.Meta.Purposes)
	}
	if b.Meta.Status != "active" {
		t.Errorf("Status = %q, want active", b.Meta.Status)
	}
	if b.Meta.Created == "" || b.Meta.Updated == "" {
		t.Errorf("Created/Updated must be set: %+v", b.Meta)
	}

	dir := bundleDir(root, "my-feature")
	for _, name := range []string{"meta.toml", "BRIEF.md", "STATE.md", "FOLLOWUPS.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}
	for _, missing := range []string{"CONTEXT.md", "lenses", "findings"} {
		if _, err := os.Stat(filepath.Join(dir, missing)); err == nil {
			t.Errorf("did not expect %s to exist for purpose plan", missing)
		}
	}

	for _, docPath := range []string{
		filepath.Join(root, "docs", "design", "my-feature.md"),
		filepath.Join(root, "docs", "spec", "my-feature.md"),
	} {
		if _, err := os.Stat(docPath); err != nil {
			t.Errorf("expected %s to exist: %v", docPath, err)
		}
	}
}

// diagnose seeds CONTEXT.md in addition to the trio implement/fix also get.
func TestScratchpadNewDiagnoseSeedsContext(t *testing.T) {
	root := t.TempDir()

	b, _, err := New(root, "some-bug", "diagnose")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := bundleDir(root, b.Meta.Slug)
	if _, err := os.Stat(filepath.Join(dir, "CONTEXT.md")); err != nil {
		t.Errorf("expected CONTEXT.md to exist: %v", err)
	}
}

// review seeds lenses/ and findings/ dirs, no BRIEF/STATE/FOLLOWUPS.
func TestScratchpadNewReviewSeedsLensesAndFindingsDirs(t *testing.T) {
	root := t.TempDir()

	b, _, err := New(root, "swarm-run", "review")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := bundleDir(root, b.Meta.Slug)
	for _, want := range []string{"lenses", "findings"} {
		info, err := os.Stat(filepath.Join(dir, want))
		if err != nil || !info.IsDir() {
			t.Errorf("expected dir %s to exist: %v", want, err)
		}
	}
	for _, missing := range []string{"BRIEF.md", "STATE.md", "FOLLOWUPS.md"} {
		if _, err := os.Stat(filepath.Join(dir, missing)); err == nil {
			t.Errorf("did not expect %s for purpose review", missing)
		}
	}
}

// Re-running New with a different purpose on an existing slug is additive:
// prior files are untouched, only what's missing is created, and the call
// reports extended=true.
func TestScratchpadNewExtendsExistingBundleAdditively(t *testing.T) {
	root := t.TempDir()

	if _, _, err := New(root, "my-feature", "plan"); err != nil {
		t.Fatalf("New(plan): %v", err)
	}
	dir := bundleDir(root, "my-feature")
	briefPath := filepath.Join(dir, "BRIEF.md")
	if err := os.WriteFile(briefPath, []byte("edited by hand"), 0o644); err != nil {
		t.Fatalf("seed edit: %v", err)
	}

	b, extended, err := New(root, "my-feature", "implement")
	if err != nil {
		t.Fatalf("New(implement): %v", err)
	}
	if !extended {
		t.Fatalf("expected extended=true when the slug already has a bundle")
	}

	raw, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("ReadFile BRIEF.md: %v", err)
	}
	if string(raw) != "edited by hand" {
		t.Errorf("BRIEF.md was overwritten: %q", string(raw))
	}

	if len(b.Meta.Purposes) != 2 || b.Meta.Purposes[0] != "plan" || b.Meta.Purposes[1] != "implement" {
		t.Errorf("Purposes = %v, want [plan implement]", b.Meta.Purposes)
	}

	// docs/design + docs/spec were created by the earlier plan call and stay untouched.
	if _, err := os.Stat(filepath.Join(root, "docs", "design", "my-feature.md")); err != nil {
		t.Errorf("expected docs/design/my-feature.md to still exist: %v", err)
	}
}

// Running New again with the same purpose is a no-op on Purposes (no duplicate).
func TestScratchpadNewSamePurposeTwiceDoesNotDuplicate(t *testing.T) {
	root := t.TempDir()

	if _, _, err := New(root, "my-feature", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	b, extended, err := New(root, "my-feature", "implement")
	if err != nil {
		t.Fatalf("New (rerun): %v", err)
	}
	if !extended {
		t.Fatalf("expected extended=true on rerun")
	}
	if len(b.Meta.Purposes) != 1 {
		t.Errorf("Purposes = %v, want a single implement entry", b.Meta.Purposes)
	}
}

func TestScratchpadNewUnknownPurposeErrors(t *testing.T) {
	root := t.TempDir()

	if _, _, err := New(root, "x", "bogus"); err == nil {
		t.Fatalf("expected an error for an unknown purpose")
	}
}

// meta.toml must parse tolerantly: unrecognized keys are ignored, not fatal.
// The struct carries no version field, so a fixture with one is exactly the
// unrecognized-key case this locks in.
func TestScratchpadLoadToleratesUnrecognizedKeys(t *testing.T) {
	root := t.TempDir()
	dir := bundleDir(root, "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fixture := `slug = "legacy"
purposes = ["plan"]
created = "2026-08-01T00:00:00Z"
updated = "2026-08-01T00:00:00Z"
status = "active"
schema_version = 3
`
	if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v (should tolerate schema_version)", err)
	}
	if m.Slug != "legacy" || m.Status != "active" {
		t.Errorf("Load result = %+v", m)
	}
}

// Save must not drop keys Load found but Meta doesn't model: New's Load ->
// mutate -> Save cycle on an existing bundle would otherwise destroy them.
func TestScratchpadSaveRoundTripsUnrecognizedKeys(t *testing.T) {
	root := t.TempDir()
	dir := bundleDir(root, "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fixture := `slug = "legacy"
purposes = ["plan"]
created = "2026-08-01T00:00:00Z"
updated = "2026-08-01T00:00:00Z"
status = "active"
schema_version = 3
`
	if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(dir, m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "meta.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "schema_version") {
		t.Errorf("Save dropped unrecognized key schema_version; got:\n%s", raw)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if reloaded.Slug != "legacy" || reloaded.Status != "active" {
		t.Errorf("reloaded Meta = %+v", reloaded)
	}
}

func TestScratchpadSaveUpdatesUpdatedField(t *testing.T) {
	root := t.TempDir()
	dir := bundleDir(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	m := &Meta{Slug: "s", Created: "2026-01-01T00:00:00Z", Status: "active"}
	if err := Save(dir, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if m.Updated == "" {
		t.Errorf("expected Save to stamp Updated")
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Updated != m.Updated {
		t.Errorf("Updated round-trip = %q, want %q", loaded.Updated, m.Updated)
	}
}
