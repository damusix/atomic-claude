package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
)

// gitCmd runs git in dir with a synthetic identity, never touching the real
// $HOME's global gitconfig for author info.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// setupMainRepo creates a real git repo at root/main with one commit.
func setupMainRepo(t *testing.T, root string) string {
	t.Helper()
	main := filepath.Join(root, "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatalf("mkdir main: %v", err)
	}
	gitCmd(t, main, "init", "-b", "main")
	gitCmd(t, main, "commit", "--allow-empty", "-m", "init")
	return main
}

// writeDoc writes docs/<kind>/<slug>.md under checkoutRoot with content, and
// backdates its mtime so a zero-quiet-window aggregator still excludes it
// from the "just written" bucket a nonzero production window would apply —
// tests use a zero quiet window (see newPlansAggregatorWithQuietWindow), so
// this is mostly for tests that need a specific relative ordering.
func writeDoc(t *testing.T, checkoutRoot, kind, slug, content string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(checkoutRoot, "docs", kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir docs/%s: %v", kind, err)
	}
	path := filepath.Join(dir, slug+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func newTestAggregator(root string) *plansAggregator {
	return newPlansAggregatorWithQuietWindow(root, 0)
}

// Three checkouts sharing one doc's bytes collapse into one version holding
// all three, labelled by the merged (main) checkout.
func TestPlansAggregator_ThreeCheckoutsSameBytesLabelledByMain(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	wtA := filepath.Join(main, ".claude", "worktrees", "wt-a")
	wtB := filepath.Join(main, ".claude", "worktrees", "wt-b")
	gitCmd(t, main, "worktree", "add", wtA, "-b", "feature-a")
	gitCmd(t, main, "worktree", "add", wtB, "-b", "feature-b")

	content := "# my-feature\n\n## Goal\n\nDo the thing.\n"
	now := time.Now()
	writeDoc(t, main, "spec", "my-feature", content, now.Add(-3*time.Minute))
	writeDoc(t, wtA, "spec", "my-feature", content, now.Add(-2*time.Minute))
	writeDoc(t, wtB, "spec", "my-feature", content, now.Add(-1*time.Minute))

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "my-feature")
	doc := findDoc(t, row, "docs/spec/my-feature.md")
	if len(doc.Versions) != 1 {
		t.Fatalf("versions = %+v, want exactly 1 (identical bytes must collapse)", doc.Versions)
	}
	v := doc.Versions[0]
	if len(v.Checkouts) != 3 {
		t.Fatalf("checkouts = %+v, want 3", v.Checkouts)
	}
	if !v.IsMain || v.Label != "main" {
		t.Errorf("version = %+v, want IsMain=true label=main", v)
	}
	for _, c := range v.Checkouts {
		if c.OutsideRoot {
			t.Errorf("checkout %+v OutsideRoot = true, want false (it sits under the served root)", c)
		}
	}
}

// When no checkout in the set is the main checkout, the version is labelled
// by the checkout with the newest file mtime.
func TestPlansAggregator_NoMainLabelledByNewestMtime(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	wtOld := filepath.Join(main, ".claude", "worktrees", "wt-old")
	wtNew := filepath.Join(main, ".claude", "worktrees", "wt-new")
	gitCmd(t, main, "worktree", "add", wtOld, "-b", "old-branch")
	gitCmd(t, main, "worktree", "add", wtNew, "-b", "new-branch")

	content := "# shared\n\nBody.\n"
	now := time.Now()
	writeDoc(t, wtOld, "design", "shared", content, now.Add(-10*time.Minute))
	writeDoc(t, wtNew, "design", "shared", content, now.Add(-1*time.Minute))

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "shared")
	doc := findDoc(t, row, "docs/design/shared.md")
	if len(doc.Versions) != 1 {
		t.Fatalf("versions = %+v, want 1", doc.Versions)
	}
	v := doc.Versions[0]
	if v.IsMain {
		t.Errorf("version.IsMain = true, want false (main checkout never wrote this doc)")
	}
	if v.Label != "new-branch" {
		t.Errorf("label = %q, want %q (newest mtime)", v.Label, "new-branch")
	}
}

// A linked worktree reports a creation time taken from its .git FILE; the
// main checkout, whose .git is a directory, reports none.
func TestPlansAggregator_CreatedTimeFromGitFileNotMainDir(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	wt := filepath.Join(main, ".claude", "worktrees", "wt-created")
	gitCmd(t, main, "worktree", "add", wt, "-b", "created-branch")

	content := "# doc\n\nBody.\n"
	writeDoc(t, main, "design", "doc", content, time.Now())
	writeDoc(t, wt, "design", "doc", "different body\n", time.Now())

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "doc")
	doc := findDoc(t, row, "docs/design/doc.md")
	if len(doc.Versions) != 2 {
		t.Fatalf("versions = %+v, want 2 (different bytes)", doc.Versions)
	}
	for _, v := range doc.Versions {
		for _, c := range v.Checkouts {
			if c.IsMain {
				if c.Created != nil {
					t.Errorf("main checkout Created = %v, want nil", c.Created)
				}
			} else {
				if c.Created == nil {
					t.Errorf("worktree checkout Created = nil, want the .git file's mtime")
				}
			}
		}
	}
}

// A worktree outside the served root is flagged and carries an absolute
// path rather than a relative one.
func TestPlansAggregator_WorktreeOutsideRootFlaggedAbsolute(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	outsideParent := t.TempDir()
	outsideWt := filepath.Join(outsideParent, "elsewhere")
	gitCmd(t, main, "worktree", "add", outsideWt, "-b", "outside-branch")

	writeDoc(t, outsideWt, "design", "far", "# far\n\nBody.\n", time.Now())

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "far")
	doc := findDoc(t, row, "docs/design/far.md")
	if len(doc.Versions) != 1 {
		t.Fatalf("versions = %+v, want 1", doc.Versions)
	}
	c := doc.Versions[0].Checkouts[0]
	if !c.OutsideRoot {
		t.Errorf("OutsideRoot = false, want true")
	}
	if !filepath.IsAbs(c.Path) {
		t.Errorf("Path = %q, want an absolute path for an out-of-root checkout", c.Path)
	}
}

// A row's description comes from the spec doc's "## Goal" paragraph, never
// from the bundle's meta.toml description field.
func TestPlansAggregator_DescriptionFromGoalNotMetaToml(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	bundle, _, err := scratchpad.New(main, "goal-slug", "plan")
	if err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	bundle.Meta.Description = "migrated"
	if err := scratchpad.Save(bundle.Root, bundle.Meta); err != nil {
		t.Fatalf("Save: %v", err)
	}

	specPath := filepath.Join(main, "docs", "spec", "goal-slug.md")
	specContent := "# Goal Slug\n\n## Goal\n\nThe real description lives here.\n"
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(specPath, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "goal-slug")
	if row.Description != "The real description lives here." {
		t.Errorf("Description = %q, want the ## Goal paragraph, not meta.toml's %q", row.Description, "migrated")
	}
}

// An archived bundle is excluded from the row set entirely — it has moved
// out of the repo's scratchpad root by the time the aggregator walks it.
func TestPlansAggregator_ArchivedBundleNeverAppears(t *testing.T) {
	root := t.TempDir()
	gitCmd(t, root, "init", "-b", "main")
	gitCmd(t, root, "commit", "--allow-empty", "-m", "init")

	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	if _, _, err := scratchpad.New(root, "archived-slug", "fix"); err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	if _, err := scratchpad.Archive(root, "archived-slug"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	a := newTestAggregator(root)
	rows, _, _ := a.rows()
	for _, r := range rows {
		if r.Slug == "archived-slug" {
			t.Fatalf("archived slug %q appeared in rows: %+v", r.Slug, r)
		}
	}
}

// A detached-HEAD worktree is labeled by its short commit SHA rather than
// erroring or reporting an empty branch.
func TestPlansAggregator_DetachedHeadLabelsByShortSHA(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	sha := gitCmd(t, main, "rev-parse", "HEAD")
	sha = trimNL(sha)
	shortSHAWant := sha[:7]

	detached := filepath.Join(main, ".claude", "worktrees", "detached-wt")
	gitCmd(t, main, "worktree", "add", "--detach", detached, sha)

	writeDoc(t, detached, "design", "det-doc", "# det\n\nBody.\n", time.Now())

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "det-doc")
	doc := findDoc(t, row, "docs/design/det-doc.md")
	if len(doc.Versions) != 1 || len(doc.Versions[0].Checkouts) != 1 {
		t.Fatalf("doc = %+v, want exactly 1 checkout", doc)
	}
	got := doc.Versions[0].Checkouts[0].Branch
	if got != shortSHAWant {
		t.Errorf("detached branch label = %q, want short SHA %q", got, shortSHAWant)
	}
}

// A prunable worktree (its directory removed without `git worktree remove`)
// is dropped from enumeration entirely.
func TestPlansAggregator_PrunableWorktreeDropped(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	gone := filepath.Join(main, ".claude", "worktrees", "gone-wt")
	gitCmd(t, main, "worktree", "add", gone, "-b", "gone-branch")
	writeDoc(t, gone, "design", "gone-doc", "# gone\n\nBody.\n", time.Now())

	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	a := newTestAggregator(main)
	rows, _, _ := a.rows()
	for _, r := range rows {
		if r.Slug == "gone-doc" {
			t.Fatalf("prunable worktree's doc %q still appeared: %+v", r.Slug, r)
		}
	}
}

// A worktree added after the first build is visible on the next fingerprint
// check, without any content in it yet — worktree enumeration alone must
// change the fingerprint.
func TestPlansAggregator_NewWorktreeVisibleOnNextCheck(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	a := newTestAggregator(main)
	rowsBefore, _, _ := a.rows()
	for _, r := range rowsBefore {
		if r.Slug == "late-doc" {
			t.Fatalf("late-doc present before the worktree that holds it was created")
		}
	}

	late := filepath.Join(main, ".claude", "worktrees", "late-wt")
	gitCmd(t, main, "worktree", "add", late, "-b", "late-branch")
	writeDoc(t, late, "design", "late-doc", "# late\n\nBody.\n", time.Now())

	rowsAfter, _, _ := a.rows()
	found := false
	for _, r := range rowsAfter {
		if r.Slug == "late-doc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("late-doc missing after the new worktree was added; rows = %+v", rowsAfter)
	}
}

// A non-markdown bundle file is classified with kind "file", not "markdown"
// or "html".
func TestPlansAggregator_BundleFileKindClassification(t *testing.T) {
	root := t.TempDir()
	gitCmd(t, root, "init", "-b", "main")
	gitCmd(t, root, "commit", "--allow-empty", "-m", "init")

	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	bundle, _, err := scratchpad.New(root, "kinds-slug", "review")
	if err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	writeExtra := func(rel, content string) {
		path := filepath.Join(bundle.Root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	writeExtra("findings/lens-a.md", "# finding\n")
	writeExtra("options.html", "<html></html>")
	writeExtra("data.bin", "binary")

	a := newTestAggregator(root)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "kinds-slug")
	if len(row.Bundles) != 1 {
		t.Fatalf("bundles = %+v, want 1", row.Bundles)
	}
	// RelPath is worktree-relative so /api/plans/page can resolve it under
	// the checkout root; key on the bundle-local suffix to keep this test
	// about classification.
	kinds := map[string]string{}
	for _, f := range row.Bundles[0].Files {
		if !strings.HasPrefix(f.RelPath, ".claude/.scratchpad/") {
			t.Errorf("RelPath %q is not worktree-relative", f.RelPath)
		}
		for _, name := range []string{"findings/lens-a.md", "options.html", "data.bin", "meta.toml"} {
			if strings.HasSuffix(f.RelPath, "/"+name) {
				kinds[name] = f.Kind
			}
		}
	}
	if kinds["findings/lens-a.md"] != "markdown" {
		t.Errorf("findings/lens-a.md kind = %q, want markdown", kinds["findings/lens-a.md"])
	}
	if kinds["options.html"] != "html" {
		t.Errorf("options.html kind = %q, want html", kinds["options.html"])
	}
	if kinds["data.bin"] != "file" {
		t.Errorf("data.bin kind = %q, want file", kinds["data.bin"])
	}
	if _, ok := kinds["meta.toml"]; ok {
		t.Errorf("meta.toml listed as a bundle file, want it excluded")
	}
}

// scratchpad.List's warnings (F-3) surface through the aggregator rather
// than being swallowed.
func TestPlansAggregator_SurfacesScratchpadListWarnings(t *testing.T) {
	root := t.TempDir()
	gitCmd(t, root, "init", "-b", "main")
	gitCmd(t, root, "commit", "--allow-empty", "-m", "init")

	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	if _, _, err := scratchpad.New(root, "healthy-slug", "fix"); err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	corruptDir := filepath.Join(config.ScratchpadDir(root), "corrupt")
	if err := os.MkdirAll(filepath.Join(corruptDir, "meta.toml"), 0o755); err != nil {
		t.Fatalf("mkdir corrupt meta.toml dir: %v", err)
	}

	a := newTestAggregator(root)
	_, _, warnings := a.rows()
	if len(warnings) == 0 {
		t.Fatalf("warnings = %v, want at least one for the corrupt bundle", warnings)
	}
}

// The merged checkout is the one on the repository's default branch, not the
// one whose .git happens to be a directory. Primary checkout sits on a
// feature branch; a linked worktree holds "main" — the filled dot must land
// on the worktree.
func TestPlansAggregator_MergedCheckoutIsDefaultBranchNotMainDir(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	gitCmd(t, main, "checkout", "-b", "feature-wip")

	wt := filepath.Join(main, ".claude", "worktrees", "wt-main")
	gitCmd(t, main, "worktree", "add", wt, "main")

	content := "# doc\n\nBody.\n"
	writeDoc(t, main, "design", "merge-doc", content, time.Now())
	writeDoc(t, wt, "design", "merge-doc", content, time.Now())

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "merge-doc")
	doc := findDoc(t, row, "docs/design/merge-doc.md")
	if len(doc.Versions) != 1 {
		t.Fatalf("versions = %+v, want 1 (identical bytes)", doc.Versions)
	}
	v := doc.Versions[0]
	if !v.IsMain || v.Label != "main" {
		t.Errorf("version = %+v, want IsMain=true label=main (the worktree on main, not the primary on feature-wip)", v)
	}
	for _, c := range v.Checkouts {
		wantMain := c.Branch == "main"
		if c.IsMain != wantMain {
			t.Errorf("checkout %+v IsMain = %v, want %v", c, c.IsMain, wantMain)
		}
	}
}

// A bare-repository hub with two linked worktrees, one of which holds
// "main", marks that one merged — a bare hub has no primary checkout whose
// .git is ever a directory, so the old dir-vs-file signal could never mark
// anything merged there.
func TestPlansAggregator_BareHubMarksWorktreeOnDefaultBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	bare := filepath.Join(root, "hub.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	gitCmd(t, bare, "init", "--bare", "-b", "main")

	wtMain := filepath.Join(root, "wt-main")
	wtOther := filepath.Join(root, "wt-other")
	gitCmd(t, bare, "worktree", "add", "--orphan", "-b", "main", wtMain)
	gitCmd(t, wtMain, "commit", "--allow-empty", "-m", "init")
	gitCmd(t, bare, "worktree", "add", "--orphan", "-b", "other-branch", wtOther)
	gitCmd(t, wtOther, "commit", "--allow-empty", "-m", "init")

	content := "# hub-doc\n\nBody.\n"
	writeDoc(t, wtMain, "design", "hub-doc", content, time.Now())
	writeDoc(t, wtOther, "design", "hub-doc", content, time.Now())

	a := newTestAggregator(bare)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "hub-doc")
	doc := findDoc(t, row, "docs/design/hub-doc.md")
	if len(doc.Versions) != 1 {
		t.Fatalf("versions = %+v, want 1 (identical bytes)", doc.Versions)
	}
	v := doc.Versions[0]
	if !v.IsMain || v.Label != "main" {
		t.Errorf("version = %+v, want IsMain=true label=main", v)
	}
}

// A worktree id is stable across rebuilds and vanishes with its checkout —
// it never gets reassigned to a neighbour.
func TestPlansAggregator_WorktreeIDStableAcrossRemoval(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	wtA := filepath.Join(main, ".claude", "worktrees", "wt-a")
	wtB := filepath.Join(main, ".claude", "worktrees", "wt-b")
	wtC := filepath.Join(main, ".claude", "worktrees", "wt-c")
	gitCmd(t, main, "worktree", "add", wtA, "-b", "branch-a")
	gitCmd(t, main, "worktree", "add", wtB, "-b", "branch-b")
	gitCmd(t, main, "worktree", "add", wtC, "-b", "branch-c")

	a := newTestAggregator(main)
	_, resolverBefore, _ := a.rows()

	idFor := func(resolver map[string]string, path string) string {
		resolved := resolveDir(path)
		for id, p := range resolver {
			if resolveDir(p) == resolved {
				return id
			}
		}
		return ""
	}
	idA := idFor(resolverBefore, wtA)
	idB := idFor(resolverBefore, wtB)
	idC := idFor(resolverBefore, wtC)
	if idA == "" || idB == "" || idC == "" {
		t.Fatalf("resolver missing an id: %+v", resolverBefore)
	}

	gitCmd(t, main, "worktree", "remove", wtA)

	_, resolverAfter, _ := a.rows()
	if _, ok := resolverAfter[idA]; ok {
		t.Errorf("removed worktree's id %q still resolves: %+v", idA, resolverAfter)
	}
	if resolverAfter[idB] != resolverBefore[idB] {
		t.Errorf("idB root changed: before %q after %q", resolverBefore[idB], resolverAfter[idB])
	}
	if resolverAfter[idC] != resolverBefore[idC] {
		t.Errorf("idC root changed: before %q after %q", resolverBefore[idC], resolverAfter[idC])
	}
}

// A symlinked entry under docs/design/ is never followed — no row is
// produced for it.
func TestPlansAggregator_SymlinkedDocSkipped(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	outsideTarget := filepath.Join(t.TempDir(), "outside-secret.md")
	if err := os.WriteFile(outsideTarget, []byte("# outside\n\nSecret body.\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	designDir := filepath.Join(main, "docs", "design")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatalf("mkdir docs/design: %v", err)
	}
	symlinkPath := filepath.Join(designDir, "evil-symlink.md")
	if err := os.Symlink(outsideTarget, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	a := newTestAggregator(main)
	rows, _, _ := a.rows()
	for _, r := range rows {
		if r.Slug == "evil-symlink" {
			t.Fatalf("symlinked doc produced a row: %+v", r)
		}
	}
}

// A doc filename stem that fails path-segment validation (e.g. "...md",
// which strips to "..") is skipped with a warning rather than becoming a
// Slug of "..".
func TestPlansAggregator_InvalidSlugSkippedWithWarning(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	designDir := filepath.Join(main, "docs", "design")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatalf("mkdir docs/design: %v", err)
	}
	badPath := filepath.Join(designDir, "...md")
	if err := os.WriteFile(badPath, []byte("# bad\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("write bad doc: %v", err)
	}

	a := newTestAggregator(main)
	rows, _, warnings := a.rows()
	for _, r := range rows {
		if r.Slug == ".." {
			t.Fatalf("row with Slug %q produced: %+v", "..", r)
		}
	}
	if len(warnings) == 0 {
		t.Errorf("warnings = %v, want at least one for the invalid slug", warnings)
	}
}

// A row's dots count the spec document's versions when one exists, else the
// design document's — never a union of the two.
func TestPlansAggregator_RowDotsCountSpecVersionsNotDesign(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	wtA := filepath.Join(main, ".claude", "worktrees", "wt-a")
	wtB := filepath.Join(main, ".claude", "worktrees", "wt-b")
	gitCmd(t, main, "worktree", "add", wtA, "-b", "branch-a")
	gitCmd(t, main, "worktree", "add", wtB, "-b", "branch-b")

	writeDoc(t, main, "design", "dots-slug", "# design v1\n\nBody.\n", time.Now())

	writeDoc(t, main, "spec", "dots-slug", "# spec v1\n\nBody.\n", time.Now())
	writeDoc(t, wtA, "spec", "dots-slug", "# spec v2\n\nBody.\n", time.Now())
	writeDoc(t, wtB, "spec", "dots-slug", "# spec v3\n\nBody.\n", time.Now())

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "dots-slug")
	specDoc := findDoc(t, row, "docs/spec/dots-slug.md")
	designDoc := findDoc(t, row, "docs/design/dots-slug.md")
	if len(specDoc.Versions) != 3 {
		t.Fatalf("spec versions = %d, want 3", len(specDoc.Versions))
	}
	if len(designDoc.Versions) != 1 {
		t.Fatalf("design versions = %d, want 1", len(designDoc.Versions))
	}
	if row.DotCount != 3 {
		t.Errorf("row.DotCount = %d, want 3 (spec's version count)", row.DotCount)
	}
}

// A row's UpdatedAt is the max mtime across everything it holds. When the
// newest touch is a bundle file rather than a committed doc, UpdatedAt
// reflects that file's mtime, not the doc's.
func TestPlansAggregator_UpdatedAtFromNewestBundleFile(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	now := time.Now()
	writeDoc(t, main, "spec", "bundle-newest", "# bundle-newest\n\nBody.\n", now.Add(-1*time.Hour))

	bundle, _, err := scratchpad.New(main, "bundle-newest", "plan")
	if err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	stateFile := filepath.Join(bundle.Root, "STATE.md")
	if err := os.WriteFile(stateFile, []byte("state\n"), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}
	// Newer than every file scratchpad.New itself created, so STATE.md is
	// unambiguously the newest touch in the bundle.
	bundleMtime := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(stateFile, bundleMtime, bundleMtime); err != nil {
		t.Fatalf("chtimes STATE.md: %v", err)
	}

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "bundle-newest")
	if !row.UpdatedAt.Equal(bundleMtime) {
		t.Errorf("UpdatedAt = %v, want the bundle file's mtime %v", row.UpdatedAt, bundleMtime)
	}
}

// When the newest touch is a doc in a non-main checkout, UpdatedAt is that
// checkout's mtime, not the main checkout's older one.
func TestPlansAggregator_UpdatedAtFromNewestNonMainDoc(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	wt := filepath.Join(main, ".claude", "worktrees", "wt-fresh")
	gitCmd(t, main, "worktree", "add", wt, "-b", "fresh-branch")

	now := time.Now()
	writeDoc(t, main, "design", "doc-newest", "# main body\n\nBody.\n", now.Add(-1*time.Hour))
	nonMainMtime := now
	writeDoc(t, wt, "design", "doc-newest", "# fresh body\n\nBody.\n", nonMainMtime)

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	row := findRow(t, rows, "doc-newest")
	if !row.UpdatedAt.Equal(nonMainMtime) {
		t.Errorf("UpdatedAt = %v, want the non-main checkout's mtime %v", row.UpdatedAt, nonMainMtime)
	}
}

// Rows come back sorted by UpdatedAt DESC, ties broken by slug ASC.
func TestPlansAggregator_RowsSortedByUpdatedAtDescSlugTiebreak(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	now := time.Now()
	writeDoc(t, main, "design", "older-plan", "# older\n\nBody.\n", now.Add(-10*time.Minute))
	writeDoc(t, main, "design", "newest-plan", "# newest\n\nBody.\n", now.Add(-1*time.Minute))
	tieMtime := now.Add(-5 * time.Minute)
	writeDoc(t, main, "design", "tie-b", "# tie b\n\nBody.\n", tieMtime)
	writeDoc(t, main, "design", "tie-a", "# tie a\n\nBody.\n", tieMtime)

	a := newTestAggregator(main)
	rows, _, _ := a.rows()

	var order []string
	for _, r := range rows {
		order = append(order, r.Slug)
	}
	want := []string{"newest-plan", "tie-a", "tie-b", "older-plan"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func findRow(t *testing.T, rows []planRow, slug string) planRow {
	t.Helper()
	for _, r := range rows {
		if r.Slug == slug {
			return r
		}
	}
	t.Fatalf("no row for slug %q in %+v", slug, rows)
	return planRow{}
}

func findDoc(t *testing.T, row planRow, path string) planDoc {
	t.Helper()
	for _, d := range row.Docs {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("no doc %q in row %+v", path, row)
	return planDoc{}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
