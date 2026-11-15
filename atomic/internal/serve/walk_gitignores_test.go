package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkRepo initialises a git repository at dir, creating it first if needed.
func mkRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", dir, err, out)
	}
}

// TestGitIgnores_SkipsBuildOutputButNotMemberRepos pins the distinction the
// whole type exists for. A realm gitignores its member repositories, so
// "ignored" cannot by itself mean "do not enumerate" — that would empty a
// realm's nav of the very repos it exists to serve. Only an ignored directory
// that is *not* its own repository is a second copy of content served
// elsewhere, and only that gets skipped.
func TestGitIgnores_SkipsBuildOutputButNotMemberRepos(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	write(t, filepath.Join(root, ".gitignore"), "/member/\n/dist/\n")

	// A member repo: ignored by the realm, but a repository in its own right.
	mkRepo(t, filepath.Join(root, "member"))
	write(t, filepath.Join(root, "member", "doc.md"), "member doc")

	// Build output: ignored, and no repository of its own.
	write(t, filepath.Join(root, "dist", "doc.md"), "generated")

	g := newGitIgnores(root)

	if g.skipDir(filepath.Join(root, "member")) {
		t.Error("member repo was skipped; a realm's members are gitignored yet must still be served")
	}
	if !g.skipDir(filepath.Join(root, "dist")) {
		t.Error("dist was not skipped; ignored build output must stay out of enumeration")
	}
}

// TestGitIgnores_SkipsLinkedWorktree covers the reported bug: a worktree under
// .claude/worktrees is a whole second checkout of the same repository, so
// enumerating it duplicates every file the main checkout already serves.
func TestGitIgnores_SkipsLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	write(t, filepath.Join(root, "readme.md"), "# root")

	// A commit is needed before a worktree can be added.
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	write(t, filepath.Join(root, ".claude", ".gitignore"), "/worktrees/\n")
	wt := filepath.Join(root, ".claude", "worktrees", "feature")
	cmd := exec.Command("git", "worktree", "add", "-q", "-b", "feature", wt)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree unavailable: %v: %s", err, out)
	}

	if info, err := os.Stat(filepath.Join(wt, ".git")); err != nil || info.IsDir() {
		t.Fatalf("expected a linked worktree with a .git file, got err=%v", err)
	}

	g := newGitIgnores(root)
	if !g.skipDir(filepath.Join(root, ".claude", "worktrees")) {
		t.Error("worktrees dir was not skipped; it duplicates the whole checkout into nav, search, and the graph")
	}
}

// TestWalkMarkdown_ExcludesWorktreeCopies is the end-to-end form of the
// reported bug, and the one that proves the walkers actually consult the
// filter rather than merely that the filter is correct. A worktree holds a
// second copy of every doc in the repo, so before this each file appeared once
// per worktree in nav, search, and the graph.
func TestWalkMarkdown_ExcludesWorktreeCopies(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	write(t, filepath.Join(root, "docs", "guide.md"), "# guide")
	write(t, filepath.Join(root, ".claude", ".gitignore"), "/worktrees/\n")

	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	wt := filepath.Join(root, ".claude", "worktrees", "feature")
	cmd := exec.Command("git", "worktree", "add", "-q", "-b", "feature", wt)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree unavailable: %v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(wt, "docs", "guide.md")); err != nil {
		t.Fatalf("worktree checkout missing its copy of the doc: %v", err)
	}

	got := walkMarkdownFilesRecursive(root)
	for _, p := range got {
		if strings.Contains(p, "worktrees/") {
			t.Errorf("walk returned %q; a worktree copy duplicates a doc already served from the main checkout", p)
		}
	}
	if len(got) != 1 || got[0] != "docs/guide.md" {
		t.Errorf("walk = %v, want exactly [docs/guide.md]", got)
	}
}

// commitAll makes the one commit `git worktree add` needs.
func commitAll(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func addWorktree(t *testing.T, repo, dir string) {
	t.Helper()
	cmd := exec.Command("git", "worktree", "add", "-q", "-b", filepath.Base(dir), dir)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree unavailable: %v: %s", err, out)
	}
}

// A realm member checked out as a linked worktree of a repository kept
// elsewhere is still a member: it is walked and its state root is listed.
func TestGitIgnores_ServesMemberThatIsLinkedWorktree(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin")
	mkRepo(t, origin)
	write(t, filepath.Join(origin, "readme.md"), "# origin")
	commitAll(t, origin)

	realm := t.TempDir()
	mkRepo(t, realm)
	write(t, filepath.Join(realm, ".gitignore"), "/member/\n")
	member := filepath.Join(realm, "member")
	addWorktree(t, origin, member)
	write(t, filepath.Join(member, ".atomic", "rules", "wiki", "c.md"), "# Card\n")

	if newGitIgnores(realm).skipDir(member) {
		t.Error("linked-worktree member was skipped")
	}
	if !BuildLinkGraph(realm).Has("member/.atomic/rules/wiki/c.md") {
		t.Error("linked-worktree member's state root is missing from the graph")
	}
	entries, ok := listDirEntries(realm, "member")
	if !ok {
		t.Fatal("listDirEntries(member) failed")
	}
	found := false
	for _, e := range entries {
		found = found || (e.Folder && e.Name == ".atomic")
	}
	if !found {
		t.Errorf("member listing lacks .atomic: %+v", entries)
	}
}

// Serving from a linked worktree, a worktree nested inside it shares its
// repository and is a second copy.
func TestGitIgnores_ServedWorktreeSkipsNestedWorktree(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin")
	mkRepo(t, origin)
	write(t, filepath.Join(origin, "readme.md"), "# origin")
	commitAll(t, origin)
	served := filepath.Join(t.TempDir(), "served")
	addWorktree(t, origin, served)
	nested := filepath.Join(served, ".claude", "worktrees", "x")
	addWorktree(t, origin, nested)

	if !newGitIgnores(served).skipDir(nested) {
		t.Error("a worktree nested in the served worktree was walked")
	}
}

// Sibling realm members that are a checkout and its linked worktree are both
// members, whichever the walk reaches first.
func TestGitIgnores_WalksSiblingWorktreeMembersInEitherOrder(t *testing.T) {
	for _, names := range [][2]string{{"a", "b"}, {"b", "a"}} {
		mainName, wtName := names[0], names[1]
		t.Run(mainName+"-main", func(t *testing.T) {
			realm := t.TempDir()
			main := filepath.Join(realm, mainName)
			mkRepo(t, main)
			write(t, filepath.Join(main, "main.md"), "# main")
			commitAll(t, main)
			addWorktree(t, main, filepath.Join(realm, wtName))
			write(t, filepath.Join(realm, wtName, "wt.md"), "# wt")

			g := BuildLinkGraph(realm)
			for _, want := range []string{mainName + "/main.md", wtName + "/wt.md"} {
				if !g.Has(want) {
					t.Errorf("%s missing from the graph", want)
				}
			}
		})
	}
}

// A linked worktree inside a repository the walk already serves is a second
// copy of it, whatever ignores it.
func TestGitIgnores_SkipsUnignoredWorktreeOfServedRepo(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	mkRepo(t, root)
	write(t, filepath.Join(root, "readme.md"), "# root")
	commitAll(t, root)
	wt := filepath.Join(root, "copies", "feature")
	addWorktree(t, root, wt)

	if !newGitIgnores(root).skipDir(wt) {
		t.Error("a worktree copy of the served repository was walked")
	}
}

// A submodule's .git file also points under the parent's .git/, but it is
// distinct content, not a second checkout, so it is walked.
func TestGitIgnores_WalksSubmodule(t *testing.T) {
	sub := filepath.Join(t.TempDir(), "sub")
	mkRepo(t, sub)
	write(t, filepath.Join(sub, "doc.md"), "# Sub doc\n")
	commitAll(t, sub)

	parent := filepath.Join(t.TempDir(), "parent")
	mkRepo(t, parent)
	cmd := exec.Command("git", "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "lib")
	cmd.Dir = parent
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git submodule unavailable: %v: %s", err, out)
	}

	if newGitIgnores(parent).skipDir(filepath.Join(parent, "lib")) {
		t.Error("submodule was skipped as a worktree copy")
	}
	if !BuildLinkGraph(parent).Has("lib/doc.md") {
		t.Error("submodule doc is missing from the graph")
	}
}

// A folder listing reads only the state-root exemption, so it must not pay
// for listing every ignored path in the repository on each click.
func TestStateRoots_ExemptsStateRootWithoutIgnoreListing(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	write(t, filepath.Join(root, ".gitignore"), "/dist/\n")
	write(t, filepath.Join(root, "dist", "doc.md"), "generated")
	write(t, filepath.Join(root, ".atomic", "rules", "wiki", "x.md"), "# Card\n")

	g := newStateRoots(root)
	if len(g.paths) != 0 {
		t.Errorf("state-root filter loaded the ignore listing: %v", g.paths)
	}
	if g.hiddenDir(filepath.Join(root, ".atomic")) {
		t.Error("state root is hidden")
	}
	if !g.hiddenDir(filepath.Join(root, ".foo")) {
		t.Error("an unrelated dot directory is shown")
	}
}

// TestGitIgnores_LoadsRulesOfAnUnignoredNestedRepo covers the realm this
// package must not assume: one whose root does not gitignore its members (or
// is not a repository at all). The member is walked either way, but only a
// loaded ignore set filters the member's *own* build output — otherwise the
// member is walked under the parent's rules, which say nothing about it.
func TestGitIgnores_LoadsRulesOfAnUnignoredNestedRepo(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	// Deliberately no .gitignore entry for member/: it is untracked, not ignored.

	member := filepath.Join(root, "member")
	mkRepo(t, member)
	write(t, filepath.Join(member, ".gitignore"), "/dist/\n")
	write(t, filepath.Join(member, "doc.md"), "member doc")
	write(t, filepath.Join(member, "dist", "generated.md"), "generated")

	g := newGitIgnores(root)

	if g.skipDir(member) {
		t.Fatal("member repo was skipped")
	}
	if !g.skipDir(filepath.Join(member, "dist")) {
		t.Error("the member's own ignored build output was not skipped; its rules were never loaded")
	}
}

// TestGitIgnores_SkipsIgnoredFile checks that an ignored file is excluded even
// when its directory is otherwise enumerated.
func TestGitIgnores_SkipsIgnoredFile(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	write(t, filepath.Join(root, ".gitignore"), "secret.md\n")
	write(t, filepath.Join(root, "secret.md"), "hidden")
	write(t, filepath.Join(root, "public.md"), "shown")

	g := newGitIgnores(root)
	if !g.skipFile(filepath.Join(root, "secret.md")) {
		t.Error("ignored file was not skipped")
	}
	if g.skipFile(filepath.Join(root, "public.md")) {
		t.Error("tracked file was skipped")
	}
}

// TestGitIgnores_NonRepoRootSkipsNothing keeps the change inert outside git,
// where serve still has to work.
func TestGitIgnores_NonRepoRootSkipsNothing(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sub", "doc.md"), "doc")

	g := newGitIgnores(root)
	if g.skipDir(filepath.Join(root, "sub")) || g.skipFile(filepath.Join(root, "sub", "doc.md")) {
		t.Error("a root outside any repository must exclude nothing")
	}
}

// TestGitIgnores_RelativeRootResolves guards the prefix rewrite. WalkDir
// echoes whatever root it was handed, so a relative root produces relative
// paths while git answers with absolute real ones; without the rewrite every
// lookup misses and the filter silently does nothing.
func TestGitIgnores_RelativeRootResolves(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	write(t, filepath.Join(root, ".gitignore"), "/dist/\n")
	write(t, filepath.Join(root, "dist", "doc.md"), "generated")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	g := newGitIgnores(".")
	if !g.skipDir(filepath.Join(".", "dist")) {
		t.Error("relative walk path did not match the ignore set")
	}
}
