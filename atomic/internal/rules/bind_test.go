package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadOne(t *testing.T) RuleRecord {
	t.Helper()
	records, err := LoadShipped("testdata/corpus/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("fixture corpus is empty")
	}
	return records[0]
}

func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	return want
}

// Two checkouts bind the same record — identity and digest survive — while each
// resolves its own runtime base.
func TestBind_TwoRepositoriesShareIdentity(t *testing.T) {
	rec := loadOne(t)
	repoA, repoB := t.TempDir(), t.TempDir()

	a, err := Bind(rec, "proj", repoA)
	if err != nil {
		t.Fatalf("Bind repoA: %v", err)
	}
	b, err := Bind(rec, "proj", repoB)
	if err != nil {
		t.Fatalf("Bind repoB: %v", err)
	}

	if a.Record.ID != rec.ID || a.Record.SourceDigest != rec.SourceDigest {
		t.Errorf("binding changed record identity or digest")
	}
	if b.Record.ID != rec.ID || b.Record.SourceDigest != rec.SourceDigest {
		t.Errorf("binding changed record identity or digest")
	}
	if a.Base == b.Base {
		t.Errorf("distinct repositories share base %q", a.Base)
	}
	if a.Base != resolvedDir(t, repoA) || b.Base != resolvedDir(t, repoB) {
		t.Errorf("bases = %q, %q", a.Base, b.Base)
	}
}

// Two worktrees of one clone are distinct bases too; sharing is a state-key
// property, not a rule-binding one.
func TestBind_WorktreesKeepDistinctBases(t *testing.T) {
	rec := loadOne(t)
	main := filepath.Join(t.TempDir(), "main")
	worktree := filepath.Join(t.TempDir(), "worktree")
	for _, dir := range []string{main, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m, err := Bind(rec, "proj", main)
	if err != nil {
		t.Fatal(err)
	}
	w, err := Bind(rec, "proj", worktree)
	if err != nil {
		t.Fatal(err)
	}
	if m.Base == w.Base {
		t.Errorf("main and worktree share base %q", m.Base)
	}
	if m.Record.ID != w.Record.ID {
		t.Errorf("worktree binding changed identity: %q vs %q", m.Record.ID, w.Record.ID)
	}
}

// A base reached through a symlink is stored resolved, so a matcher compares
// against the real runtime location.
func TestBind_ResolvesSymlinkedBase(t *testing.T) {
	rec := loadOne(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	inst, err := Bind(rec, "proj", link)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if inst.Base != resolvedDir(t, real) {
		t.Errorf("Base = %q, want resolved %q", inst.Base, resolvedDir(t, real))
	}
}

func TestBind_Rejects(t *testing.T) {
	rec := loadOne(t)
	cases := []struct {
		name    string
		project string
		base    string
		want    string
	}{
		{"empty project key", "", t.TempDir(), "empty project key"},
		{"relative base", "proj", "relative/dir", "not absolute"},
		{"unresolvable base", "proj", filepath.Join(t.TempDir(), "missing"), "resolve base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Bind(rec, tc.project, tc.base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
