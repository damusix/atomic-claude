package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedNow is the deterministic clock value used across scaffold tests.
var fixedNow = time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

func TestValidateSlug_AcceptsKebabCase(t *testing.T) {
	for _, s := range []string{"seo", "vector-store-bench", "a1-b2", "123"} {
		if err := validateSlug(s); err != nil {
			t.Errorf("validateSlug(%q): unexpected error: %v", s, err)
		}
	}
}

func TestValidateSlug_RejectsNonConforming(t *testing.T) {
	cases := []string{"", "SEO", "has space", "snake_case", "trailing.", "with/slash"}
	for _, s := range cases {
		if err := validateSlug(s); err == nil {
			t.Errorf("validateSlug(%q): expected error, got nil", s)
		} else if !strings.Contains(err.Error(), "[a-z0-9-]+") {
			t.Errorf("validateSlug(%q): error %q does not name the pattern", s, err)
		}
	}
}

func TestScaffoldBucketDoc_WritesFrontmatterAndStampsCreated(t *testing.T) {
	bucketDir := t.TempDir()

	path, err := ScaffoldBucketDoc(bucketDir, "seo", false, fixedNow)
	if err != nil {
		t.Fatalf("ScaffoldBucketDoc: %v", err)
	}
	wantPath := filepath.Join(bucketDir, "seo.md")
	if path != wantPath {
		t.Errorf("got path %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read scaffolded doc: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "created: 2026-07-24") {
		t.Errorf("expected created stamped from injected clock, got:\n%s", content)
	}
	if !strings.Contains(content, "title: seo") {
		t.Errorf("expected title derived from slug, got:\n%s", content)
	}
	if !strings.Contains(content, "# seo") {
		t.Errorf("expected H1 derived from slug, got:\n%s", content)
	}
	// Model-owned placeholders stay untouched.
	if !strings.Contains(content, "<type") {
		t.Errorf("expected type placeholder left as-is, got:\n%s", content)
	}
}

func TestScaffoldBucketDoc_RejectsNonConformingSlug(t *testing.T) {
	bucketDir := t.TempDir()

	if _, err := ScaffoldBucketDoc(bucketDir, "Not Kebab", false, fixedNow); err == nil {
		t.Fatal("expected error for non-conforming slug, got nil")
	}

	entries, _ := os.ReadDir(bucketDir)
	if len(entries) != 0 {
		t.Errorf("expected no files written on slug rejection, found %d", len(entries))
	}
}

func TestScaffoldBucketDoc_RefusesCollisionAndWritesNothing(t *testing.T) {
	bucketDir := t.TempDir()
	target := filepath.Join(bucketDir, "seo.md")
	existing := "# existing content\n"
	if err := os.WriteFile(target, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ScaffoldBucketDoc(bucketDir, "seo", false, fixedNow)
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("expected error to name the path %q, got: %v", target, err)
	}

	data, _ := os.ReadFile(target)
	if string(data) != existing {
		t.Errorf("existing file was modified; got:\n%s", string(data))
	}
}

func TestScaffoldBucketDoc_RouterCreatesSubtreeAndClaudeMD(t *testing.T) {
	bucketDir := t.TempDir()

	_, err := ScaffoldBucketDoc(bucketDir, "bench", true, fixedNow)
	if err != nil {
		t.Fatalf("ScaffoldBucketDoc: %v", err)
	}

	subtreeDir := filepath.Join(bucketDir, "bench")
	if info, err := os.Stat(subtreeDir); err != nil || !info.IsDir() {
		t.Fatalf("expected subtree dir %s to exist", subtreeDir)
	}

	claudePath := filepath.Join(subtreeDir, "CLAUDE.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read router CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(data), "bench") {
		t.Errorf("expected slug substituted into router CLAUDE.md, got:\n%s", string(data))
	}
}

func TestScaffoldBucketDoc_RouterSkipsExistingSubtreeAndClaudeMD(t *testing.T) {
	bucketDir := t.TempDir()
	subtreeDir := filepath.Join(bucketDir, "bench")
	if err := os.MkdirAll(subtreeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(subtreeDir, "CLAUDE.md")
	existing := "# hand-authored notes\n"
	if err := os.WriteFile(claudePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ScaffoldBucketDoc(bucketDir, "bench", true, fixedNow)
	if err != nil {
		t.Fatalf("ScaffoldBucketDoc: %v", err)
	}

	data, _ := os.ReadFile(claudePath)
	if string(data) != existing {
		t.Errorf("existing router CLAUDE.md was overwritten; got:\n%s", string(data))
	}
}

func TestScaffoldBucketDoc_PreExistingDocStillAbortsEvenWithRouter(t *testing.T) {
	bucketDir := t.TempDir()
	target := filepath.Join(bucketDir, "bench.md")
	if err := os.WriteFile(target, []byte("# pre-existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ScaffoldBucketDoc(bucketDir, "bench", true, fixedNow)
	if err == nil {
		t.Fatal("expected collision error even with --router, got nil")
	}

	// The router subtree must never be created when the doc write aborts.
	if _, statErr := os.Stat(filepath.Join(bucketDir, "bench")); statErr == nil {
		t.Error("expected router subtree NOT to be created when the doc collision aborts")
	}
}

func TestScaffoldBucketSkill_WritesPrefilledSkillWhenAbsent(t *testing.T) {
	realmRoot := t.TempDir()
	bucketDir := filepath.Join(realmRoot, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexContent := "---\ndescription: Authored research reports.\n---\n\n# research\n"
	if err := os.WriteFile(filepath.Join(bucketDir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := ScaffoldBucketSkill(realmRoot, "research", bucketDir)
	if err != nil {
		t.Fatalf("ScaffoldBucketSkill: %v", err)
	}
	wantPath := filepath.Join(realmRoot, ".claude", "skills", "research-management", "SKILL.md")
	if path != wantPath {
		t.Errorf("got path %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read scaffolded skill: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "name: research-management") {
		t.Errorf("expected bucket name substituted into skill name, got:\n%s", content)
	}
	if !strings.Contains(content, "Authored research reports.") {
		t.Errorf("expected purpose line prefilled, got:\n%s", content)
	}
}

func TestScaffoldBucketSkill_NoOpWhenPresent(t *testing.T) {
	realmRoot := t.TempDir()
	bucketDir := filepath.Join(realmRoot, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(realmRoot, ".claude", "skills", "research-management")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# hand-authored skill\n"
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := ScaffoldBucketSkill(realmRoot, "research", bucketDir)
	if err != nil {
		t.Fatalf("ScaffoldBucketSkill: %v", err)
	}
	if path != skillPath {
		t.Errorf("got path %q, want %q", path, skillPath)
	}

	data, _ := os.ReadFile(skillPath)
	if string(data) != existing {
		t.Errorf("existing SKILL.md was overwritten; got:\n%s", string(data))
	}
}

func TestBucketPurposeLine_ReadsDescription(t *testing.T) {
	bucketDir := t.TempDir()
	content := "---\ndescription: Spikes and prototypes.\n---\n\n# experiments\n"
	if err := os.WriteFile(filepath.Join(bucketDir, "index.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := bucketPurposeLine(bucketDir); got != "Spikes and prototypes." {
		t.Errorf("got %q, want %q", got, "Spikes and prototypes.")
	}
}

func TestBucketPurposeLine_EmptyWhenNoDescription(t *testing.T) {
	bucketDir := t.TempDir()
	if got := bucketPurposeLine(bucketDir); got != "" {
		t.Errorf("got %q, want empty string when index.md is absent", got)
	}
}
