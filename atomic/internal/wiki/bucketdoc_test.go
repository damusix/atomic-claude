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

func TestValidateSlug_RejectsSkippedFolderNames(t *testing.T) {
	for _, s := range []string{"build", "dist", "tmp", "target", "vendor"} {
		if err := validateSlug(s); err == nil {
			t.Errorf("validateSlug(%q): expected error, got nil", s)
		} else if !strings.Contains(err.Error(), "skips") {
			t.Errorf("validateSlug(%q): error %q does not say the index skips it", s, err)
		}
	}
}

func TestScaffoldBucketDoc_WritesSlugIndexAndRawStage(t *testing.T) {
	bucketDir := t.TempDir()

	path, err := ScaffoldBucketDoc(bucketDir, "seo", fixedNow)
	if err != nil {
		t.Fatalf("ScaffoldBucketDoc: %v", err)
	}
	wantPath := filepath.Join(bucketDir, "seo", "index.md")
	if path != wantPath {
		t.Errorf("got path %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read slug index: %v", err)
	}
	content := string(data)
	for _, key := range []string{"title: seo", "type:", "description:", "tags:", "status:", "created: 2026-07-24"} {
		if !strings.Contains(content, key) {
			t.Errorf("slug index missing %q:\n%s", key, content)
		}
	}
	if !strings.Contains(content, "# seo") {
		t.Errorf("expected H1 derived from slug, got:\n%s", content)
	}
	if !strings.Contains(content, "01_raw") {
		t.Errorf("expected the lede to describe the stage layout, got:\n%s", content)
	}

	stage, err := os.ReadFile(filepath.Join(bucketDir, "seo", "01_raw", "index.md"))
	if err != nil {
		t.Fatalf("read 01_raw index: %v", err)
	}
	for _, key := range []string{"title:", "description:", "created: 2026-07-24"} {
		if !strings.Contains(string(stage), key) {
			t.Errorf("01_raw index missing %q:\n%s", key, stage)
		}
	}
}

func TestScaffoldBucketDoc_RejectsNonConformingSlug(t *testing.T) {
	bucketDir := t.TempDir()

	if _, err := ScaffoldBucketDoc(bucketDir, "Not Kebab", fixedNow); err == nil {
		t.Fatal("expected error for non-conforming slug, got nil")
	}

	entries, _ := os.ReadDir(bucketDir)
	if len(entries) != 0 {
		t.Errorf("expected no files written on slug rejection, found %d", len(entries))
	}
}

// An existing <slug> path of any kind may hold user material; the scaffold
// must not touch it.
func TestScaffoldBucketDoc_RefusesExistingSlugPathAndWritesNothing(t *testing.T) {
	cases := map[string]struct {
		setup  func(t *testing.T, bucketDir, target string)
		intact func(t *testing.T, bucketDir, target string)
	}{
		"directory": {
			setup: func(t *testing.T, _, target string) {
				if err := os.MkdirAll(target, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			intact: func(t *testing.T, _, target string) { assertEmptyDir(t, target) },
		},
		"file": {
			setup: func(t *testing.T, _, target string) {
				if err := os.WriteFile(target, []byte("notes\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			intact: func(t *testing.T, _, target string) {
				if data, err := os.ReadFile(target); err != nil || string(data) != "notes\n" {
					t.Errorf("existing file changed: %q, %v", data, err)
				}
			},
		},
		"dangling symlink": {
			setup: func(t *testing.T, bucketDir, target string) {
				if err := os.Symlink(filepath.Join(bucketDir, "missing"), target); err != nil {
					t.Fatal(err)
				}
			},
			intact: func(t *testing.T, bucketDir, target string) {
				assertSymlinkTo(t, target, filepath.Join(bucketDir, "missing"))
				if _, err := os.Lstat(filepath.Join(bucketDir, "missing")); err == nil {
					t.Error("scaffold created the dangling link's target")
				}
			},
		},
		"symlink to directory": {
			setup: func(t *testing.T, bucketDir, target string) {
				realDir := filepath.Join(bucketDir, "real")
				if err := os.Mkdir(realDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realDir, target); err != nil {
					t.Fatal(err)
				}
			},
			intact: func(t *testing.T, bucketDir, target string) {
				assertSymlinkTo(t, target, filepath.Join(bucketDir, "real"))
				assertEmptyDir(t, filepath.Join(bucketDir, "real"))
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			bucketDir := t.TempDir()
			target := filepath.Join(bucketDir, "seo")
			tc.setup(t, bucketDir, target)

			_, err := ScaffoldBucketDoc(bucketDir, "seo", fixedNow)
			if err == nil {
				t.Fatal("expected refusal, got nil")
			}
			if !strings.Contains(err.Error(), target+" already exists") {
				t.Errorf("expected error to name %q as existing, got: %v", target, err)
			}
			tc.intact(t, bucketDir, target)
		})
	}
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s: want empty, found %d entries", dir, len(entries))
	}
}

func assertSymlinkTo(t *testing.T, link, want string) {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %s: %v", link, err)
	}
	if got != want {
		t.Errorf("%s points at %q, want %q", link, got, want)
	}
}

// A flat topic seo.md and a seo/ folder can coexist in one bucket.
func TestScaffoldBucketDoc_FlatTopicFileDoesNotBlock(t *testing.T) {
	bucketDir := t.TempDir()
	flat := filepath.Join(bucketDir, "seo.md")
	if err := os.WriteFile(flat, []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ScaffoldBucketDoc(bucketDir, "seo", fixedNow); err != nil {
		t.Fatalf("seo.md must not block the seo/ scaffold: %v", err)
	}
	if data, _ := os.ReadFile(flat); string(data) != "notes\n" {
		t.Errorf("scaffold modified seo.md: %q", data)
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

func TestScaffoldBucketSkill_DescribesSlugFoldersAndStages(t *testing.T) {
	realmRoot := t.TempDir()
	bucketDir := filepath.Join(realmRoot, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := ScaffoldBucketSkill(realmRoot, "research", bucketDir)
	if err != nil {
		t.Fatalf("ScaffoldBucketSkill: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, want := range []string{"atomic wiki bucket doc research <slug>", "<slug>/index.md", "01_raw/"} {
		if !strings.Contains(content, want) {
			t.Errorf("skill missing %q:\n%s", want, content)
		}
	}
	for _, gone := range []string{"--router", "router", "<slug>.md"} {
		if strings.Contains(content, gone) {
			t.Errorf("skill still describes the single-file or router shape (%q):\n%s", gone, content)
		}
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
