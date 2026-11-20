package realmshare

import (
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSyncReceivesSharedDefinitionWithoutLinking(t *testing.T) {
	f, seed := newLinkFixture(t)
	binding := f.binding(t)
	ref := "refs/heads/" + binding.Branch
	definition := filepath.Join(seed, manifestName)
	writeRealmshareFile(t, definition, readRealmshareFile(t, definition)+
		manifestMapping("sops", "sops", string(KindDirectory))+"\n[[buckets]]\nname = \"sops\"\npath = \"sops\"\n")
	writeRealmshareFile(t, filepath.Join(seed, "sops", "index.md"), "# Team procedures\n")
	head := syncFixtureCommit(t, f, seed, "Publish procedures", manifestName, "sops")
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:"+ref)

	if got, err := Sync(t.Context(), f.realm); err != nil || got != (SyncResult{Head: head, Received: true}) {
		t.Fatalf("receive-only sync = %+v, %v; want fast-forward to %s", got, err, head)
	}
	if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != head {
		t.Errorf("clone HEAD = %s, want %s", got, head)
	}
	if got := readRealmshareFile(t, filepath.Join(binding.ClonePath, "sops", "index.md")); got != "# Team procedures\n" {
		t.Errorf("received procedures = %q", got)
	}
	if got := f.manifest(t, binding).Buckets; !slices.Equal(got, []Bucket{{Name: "sops", Path: "sops"}}) {
		t.Errorf("received buckets = %+v", got)
	}
	for _, path := range []string{"guide.md", "docs/notes", "sops"} {
		assertRealmshareAbsent(t, filepath.Join(f.realm, path))
	}
	f.assertOriginal(t, "docs/local.md", "", "# Local notes\n")
	if got := f.git(t, f.remote, "rev-parse", ref); got != head {
		t.Errorf("receiving changed remote tip to %s", got)
	}
}

func TestSyncRejectsUnmaterializedReceivedDirectories(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bucket bool
		link   bool
	}{
		{name: "empty mapped directory"},
		{name: "empty registered bucket", bucket: true},
		{name: "symlink to empty directory", link: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, seed := newLinkFixture(t)
			binding := f.binding(t)
			clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
			base := f.git(t, clone, "rev-parse", "HEAD")
			definition := readRealmshareFile(t, filepath.Join(clone, manifestName))
			incoming := definition + manifestMapping("sops", "sops", string(KindDirectory))
			paths := []string{manifestName}
			if tc.bucket {
				incoming += "\n[[buckets]]\nname = \"procedures\"\npath = \"sops/procedures\"\n"
			}
			if tc.bucket || tc.link {
				writeRealmshareFile(t, filepath.Join(seed, "sops", "index.md"), "# Team procedures\n")
				paths = append(paths, "sops/index.md")
			}
			if tc.link {
				if err := os.Symlink("procedures", filepath.Join(seed, "sops", "current")); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, "sops/current")
			}
			writeRealmshareFile(t, filepath.Join(seed, manifestName), incoming)
			staged := syncFixtureCommit(t, f, seed, "Prepare incoming definition", paths...)
			mktree := func(entries string) string {
				t.Helper()
				out, err := localGit(seed).run(t.Context(), strings.NewReader(entries), "mktree")
				if err != nil {
					t.Fatal(err)
				}
				return strings.TrimSpace(string(out))
			}
			sops := mktree("")
			if tc.bucket || tc.link {
				sops = mktree(f.git(t, seed, "ls-tree", staged+":sops") + "\n040000 tree " + sops + "\tprocedures\n")
			}
			root := mktree(f.git(t, seed, "ls-tree", staged, "--", manifestName, "shared") + "\n040000 tree " + sops + "\tsops\n")
			remoteHead := f.git(t, seed, "commit-tree", root, "--no-gpg-sign", "-p", base, "-m", "Publish unmaterialized directory")
			f.git(t, seed, "push", "--quiet", f.remote, remoteHead+":"+ref)

			if result, err := Sync(t.Context(), f.realm); err == nil || !strings.Contains(err.Error(), "invalid received snapshot") || result.Received {
				t.Errorf("sync = %+v, %v; want rejection before integration", result, err)
			}
			if got := f.git(t, clone, "rev-parse", "HEAD"); got != base {
				t.Errorf("clone HEAD = %s, want %s", got, base)
			}
			if got := readRealmshareFile(t, filepath.Join(clone, manifestName)); got != definition {
				t.Errorf("clone manifest changed to %q", got)
			}
			if got := f.git(t, clone, "status", "--porcelain=v1"); got != "" {
				t.Errorf("clone workspace changed: %q", got)
			}
			assertRealmshareAbsent(t, filepath.Join(clone, "sops"))
			assertRealmshareAbsent(t, filepath.Join(f.realm, "sops"))
			assertRealmshareAbsent(t, filepath.Join(clone, ".git", "MERGE_HEAD"))
			f.assertOriginal(t, "docs/local.md", "", "# Local notes\n")
			if got := f.git(t, f.remote, "rev-parse", ref); got != remoteHead {
				t.Errorf("remote tip = %s, want %s", got, remoteHead)
			}
		})
	}
}

func TestSyncPublishesOnlyDeclaredWork(t *testing.T) {
	f, _ := newLinkFixture(t)
	requireShareSymlinks(t)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "docs/notes")); err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	base := f.git(t, clone, "rev-parse", "HEAD")
	owned := linkLstat(t, filepath.Join(f.realm, "docs", "notes"))
	f.write(t, "docs/notes/index.md", "# Edited notes\n")
	f.write(t, "docs/notes/new/topic.md", "# New topic\n")
	writeRealmshareFile(t, filepath.Join(clone, "shared", "adjacent.md"), "# Unselected clone draft\n")

	result, err := Sync(t.Context(), f.realm)
	if err != nil {
		t.Fatal(err)
	}
	head := f.git(t, clone, "rev-parse", "HEAD")
	if want := (SyncResult{Head: head, Committed: true, Published: true}); result != want || head == base {
		t.Fatalf("sync = %+v, want new published commit at %s (base %s)", result, head, base)
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != head {
		t.Fatalf("remote tip = %s, want %s", got, head)
	}
	if got := f.git(t, f.remote, "diff-tree", "--no-commit-id", "--name-only", "-r", base, head); got != "shared/notes/index.md\nshared/notes/new/topic.md" {
		t.Errorf("published paths = %q", got)
	}
	for path, want := range map[string]string{
		"shared/notes/index.md":     "# Edited notes",
		"shared/notes/new/topic.md": "# New topic",
	} {
		if got := f.git(t, f.remote, "show", ref+":"+path); got != want {
			t.Errorf("remote %s = %q, want %q", path, got, want)
		}
	}
	if got, err := Sync(t.Context(), f.realm); err != nil || got != (SyncResult{Head: head}) {
		t.Fatalf("no-op sync = %+v, %v; want unchanged head %s", got, err, head)
	}
	if got := f.git(t, clone, "rev-parse", "HEAD"); got != head {
		t.Errorf("no-op sync created commit %s", got)
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != head {
		t.Errorf("no-op sync changed remote tip to %s", got)
	}
	if got := f.git(t, clone, "status", "--porcelain=v1"); got != "?? shared/adjacent.md" {
		t.Errorf("clone status = %q, want only the unselected draft", got)
	}
	if got := readRealmshareFile(t, filepath.Join(clone, "shared", "adjacent.md")); got != "# Unselected clone draft\n" {
		t.Errorf("unselected clone draft = %q", got)
	}
	if !os.SameFile(owned, linkLstat(t, filepath.Join(f.realm, "docs", "notes"))) {
		t.Error("sync replaced the owned directory link")
	}
	f.assertOriginal(t, "docs/local.md", "", "# Local notes\n")
}

func TestSyncRefusesUnexpectedStagedPath(t *testing.T) {
	f, _ := newLinkFixture(t)
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	base := f.git(t, clone, "rev-parse", "HEAD")
	unexpected := filepath.Join(clone, "unexpected.md")
	writeRealmshareFile(t, unexpected, "# Staged private draft\n")
	f.git(t, clone, "add", "--", "unexpected.md")
	writeRealmshareFile(t, unexpected, "# Unstaged private edit\n")
	declared := filepath.Join(clone, "shared", "guide.md")
	writeRealmshareFile(t, declared, "# Team edit not staged\n")
	index := f.git(t, clone, "ls-files", "--stage")

	_, err := Sync(t.Context(), f.realm)
	assertSyncCloneError(t, err, clone, "unexpected.md", "stag")
	if got := f.git(t, clone, "ls-files", "--stage"); got != index {
		t.Error("refused sync changed the staged paths or blobs")
	}
	if got := readRealmshareFile(t, unexpected); got != "# Unstaged private edit\n" {
		t.Errorf("unstaged private edit = %q", got)
	}
	if got := readRealmshareFile(t, declared); got != "# Team edit not staged\n" {
		t.Errorf("declared edit = %q", got)
	}
	if got := f.git(t, clone, "rev-parse", "HEAD"); got != base {
		t.Errorf("refused sync advanced the clone to %s", got)
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != base {
		t.Errorf("refused sync advanced the remote to %s", got)
	}
}

func TestSyncRefusesUnsafeDeclaredPaths(t *testing.T) {
	for _, path := range []string{"escape.md", "nested"} {
		t.Run(path, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			binding := f.binding(t)
			clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
			base := f.git(t, clone, "rev-parse", "HEAD")
			unsafe := filepath.Join(clone, "shared", "notes", path)
			if path == "escape.md" {
				target := filepath.Join(t.TempDir(), "outside.md")
				writeRealmshareFile(t, target, "# Outside the clone\n")
				symlinkRealmshare(t, target, unsafe)
			} else {
				f.git(t, clone, "init", "--quiet", unsafe)
			}
			_, err := Sync(t.Context(), f.realm)
			assertSyncCloneError(t, err, clone, path)
			if got := f.git(t, clone, "diff", "--cached", "--name-only"); got != "" {
				t.Errorf("unsafe path left staged changes: %s", got)
			}
			if got := f.git(t, clone, "rev-parse", "HEAD"); got != base {
				t.Errorf("unsafe path advanced clone to %s", got)
			}
			if got := f.git(t, f.remote, "rev-parse", ref); got != base {
				t.Errorf("unsafe path advanced remote to %s", got)
			}
			linkLstat(t, unsafe)
		})
	}
}

func TestSyncPreservesConflictsForManualResolution(t *testing.T) {
	f, seed := newLinkFixture(t)
	requireShareSymlinks(t)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "docs/notes")); err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	const source, local = "shared/notes/index.md", "docs/notes/index.md"
	writeRealmshareFile(t, filepath.Join(seed, source), "# Remote edit\n")
	remoteHead := syncFixtureCommit(t, f, seed, "Remote notes edit", source)
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:"+ref)
	f.write(t, local, "# Local edit\n")

	_, err := Sync(t.Context(), f.realm)
	assertSyncCloneError(t, err, clone, "conflict", source, local, "git -C", "add", "commit")
	localHead := f.git(t, clone, "rev-parse", "HEAD")
	if got := f.git(t, clone, "rev-parse", "MERGE_HEAD"); got != remoteHead {
		t.Fatalf("MERGE_HEAD = %s, want %s", got, remoteHead)
	}
	stages := f.git(t, clone, "ls-files", "--unmerged", "--", source)
	if len(strings.Split(stages, "\n")) != 3 {
		t.Fatalf("expected three conflict stages, got %q", stages)
	}
	for rev, want := range map[string]string{":2:": "# Local edit", ":3:": "# Remote edit"} {
		if got := f.git(t, clone, "show", rev+source); got != want {
			t.Errorf("conflict stage %s = %q, want %q", rev, got, want)
		}
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != remoteHead {
		t.Errorf("conflicted sync moved remote to %s", got)
	}

	conflicted := readRealmshareFile(t, filepath.Join(f.realm, local))
	_, err = Sync(t.Context(), f.realm)
	assertSyncCloneError(t, err, clone, "merge")
	if got := f.git(t, clone, "ls-files", "--unmerged", "--", source); got != stages {
		t.Fatal("sync during unfinished merge changed the unmerged index")
	}
	if got := readRealmshareFile(t, filepath.Join(f.realm, local)); got != conflicted {
		t.Fatal("sync during unfinished merge changed conflict content")
	}

	writeRealmshareFile(t, filepath.Join(clone, source), "# Reconciled notes\n")
	f.git(t, f.realm, "-C", clone, "add", "--", source)
	f.git(t, f.realm, "-C", clone, "commit", "--quiet", "-m", "Resolve notes conflict")
	merge := f.git(t, clone, "rev-parse", "HEAD")
	if got, err := Sync(t.Context(), f.realm); err != nil || got != (SyncResult{Head: merge, Published: true}) {
		t.Fatalf("sync after manual resolution = %+v, %v; want publication of %s", got, err, merge)
	}
	if got := f.git(t, f.remote, "rev-list", "--parents", "-n1", ref); got != merge+" "+localHead+" "+remoteHead {
		t.Errorf("remote merge ancestry = %s", got)
	}
	if got := f.git(t, f.remote, "show", ref+":"+source); got != "# Reconciled notes" {
		t.Errorf("published resolution = %q", got)
	}
	if got := readRealmshareFile(t, filepath.Join(f.realm, local)); got != "# Reconciled notes\n" {
		t.Errorf("resolution through realm path = %q", got)
	}
}

func TestMergeSyncHeadRefusesBranchDriftBeforeInvokingMerge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires POSIX shebang support")
	}
	f, _ := newLinkFixture(t)
	b := f.binding(t)
	clonePath := b.ClonePath
	head := f.git(t, clonePath, "rev-parse", "HEAD")
	const source = "shared/guide.md"
	guide := filepath.Join(clonePath, source)
	original := readRealmshareFile(t, guide)
	f.git(t, clonePath, "checkout", "--quiet", "-b", "incoming")
	writeRealmshareFile(t, guide, "# Incoming guide edit\n")
	incomingHead := syncFixtureCommit(t, f, clonePath, "Edit incoming guide", source)
	f.git(t, clonePath, "checkout", "--quiet", b.Branch)
	f.git(t, clonePath, "branch", "other")

	loc, err := locateBinding(f.realm)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := os.OpenRoot(clonePath)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	ctx := t.Context()
	git := localGit(clonePath)
	local, err := syncTreeManifest(ctx, git, head, b.RealmID, ".atomic")
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := syncTreeManifest(ctx, git, incomingHead, b.RealmID, ".atomic")
	if err != nil {
		t.Fatal(err)
	}
	markerDir := t.TempDir()
	switched := filepath.Join(markerDir, "switched")
	mergeCalled := filepath.Join(markerDir, "merge-called")
	wrapSyncGit(t, "merge-base|merge", "if [ \"$arg\" = merge ]; then\n"+
		"  touch "+quoteShareShell(mergeCalled)+" || exit 2\n"+
		"  exit 2\nfi\n"+
		"\"$git\" \"$@\"\nstatus=$?\n"+
		"if [ ! -f "+quoteShareShell(switched)+" ]; then\n"+
		"  \"$git\" -C "+quoteShareShell(clonePath)+" checkout --quiet other || exit 2\n"+
		"  touch "+quoteShareShell(switched)+" || exit 2\nfi\n"+
		"exit \"$status\"\n")

	got, _, err := mergeSyncHead(ctx, clone, git, b, loc, head, incomingHead, local, incoming)
	if _, statErr := os.Stat(switched); statErr != nil {
		t.Fatalf("fixture did not switch branches during ancestry inspection: %v (merge error: %v)", statErr, err)
	}
	assertSyncCloneError(t, err, clonePath, "checkout", b.Branch)
	if got != head {
		t.Errorf("merge reported tip %s, want unchanged %s", got, head)
	}
	assertRealmshareAbsent(t, mergeCalled)
	assertRealmshareAbsent(t, filepath.Join(clonePath, ".git", "MERGE_HEAD"))
	if got := f.git(t, clonePath, "symbolic-ref", "--short", "HEAD"); got != "other" {
		t.Errorf("checked-out branch = %q, want other", got)
	}
	for _, branch := range []string{b.Branch, "other"} {
		if got := f.git(t, clonePath, "rev-parse", "refs/heads/"+branch); got != head {
			t.Errorf("branch %s = %s, want unchanged %s", branch, got, head)
		}
	}
	if got := readRealmshareFile(t, guide); got != original {
		t.Errorf("other branch's worktree guide = %q, want %q", got, original)
	}
	if got := f.git(t, clonePath, "status", "--porcelain=v1"); got != "" {
		t.Errorf("other branch's index or worktree changed: %q", got)
	}
}

func TestSyncRefusesUnfinishedRebase(t *testing.T) {
	f, seed := newLinkFixture(t)
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	const source = "shared/guide.md"
	writeRealmshareFile(t, filepath.Join(clone, source), "# Local edit\n")
	localHead := syncFixtureCommit(t, f, clone, "Edit guide locally", source)
	writeRealmshareFile(t, filepath.Join(seed, source), "# Remote edit\n")
	remoteHead := syncFixtureCommit(t, f, seed, "Edit guide remotely", source)
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:"+ref)
	f.git(t, clone, "fetch", "--quiet", "origin", binding.Branch)
	cmd := exec.CommandContext(t.Context(), "git", "rebase", "FETCH_HEAD")
	cmd.Dir = clone
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "CONFLICT") {
		t.Fatalf("fixture expected a rebase conflict: %v\n%s", err, out)
	}
	stages := f.git(t, clone, "ls-files", "--unmerged", "--", source)
	conflict := readRealmshareFile(t, filepath.Join(clone, source))

	_, err := Sync(t.Context(), f.realm)
	assertSyncCloneError(t, err, clone, "rebase")
	if got := f.git(t, clone, "rev-parse", "REBASE_HEAD"); got != localHead {
		t.Errorf("REBASE_HEAD = %s, want %s", got, localHead)
	}
	if got := f.git(t, clone, "ls-files", "--unmerged", "--", source); got != stages {
		t.Error("refused sync changed the rebase index")
	}
	if got := readRealmshareFile(t, filepath.Join(clone, source)); got != conflict {
		t.Error("refused sync changed rebase conflict content")
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != remoteHead {
		t.Errorf("refused sync advanced the remote to %s", got)
	}
}

func TestSyncRetriesPushOnlyOnceAfterRemoteAdvance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires POSIX shebang support")
	}
	for _, name := range []string{"retry succeeds", "remote advances again"} {
		t.Run(name, func(t *testing.T) {
			f, seed := newLinkFixture(t)
			binding := f.binding(t)
			clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
			base := f.git(t, clone, "rev-parse", "HEAD")
			writeRealmshareFile(t, filepath.Join(seed, "shared", "notes", "first.md"), "# First remote advance\n")
			first := syncFixtureCommit(t, f, seed, "First remote advance", "shared/notes/first.md")
			writeRealmshareFile(t, filepath.Join(seed, "shared", "notes", "second.md"), "# Second remote advance\n")
			second := syncFixtureCommit(t, f, seed, "Second remote advance", "shared/notes/second.md")
			const fixtureRef = "refs/heads/fixture-push-race"
			f.git(t, seed, "push", "--quiet", f.remote, "HEAD:"+fixtureRef)
			f.git(t, f.remote, "update-ref", "-d", fixtureRef)
			writeRealmshareFile(t, filepath.Join(clone, "shared", "guide.md"), "# Local snapshot\n")

			countFile := filepath.Join(t.TempDir(), "push-count")
			count := quoteShareShell(countFile)
			advance := `"$git" -C ` + quoteShareShell(f.remote) + " update-ref " + quoteShareShell(ref)
			push := "n=0\nif [ -f " + count + " ]; then read -r n < " + count + "; fi\n" +
				"n=$((n + 1))\nprintf '%s\\n' \"$n\" > " + count + " || exit 1\n" +
				"case \"$n\" in\n1) (unset GIT_OBJECT_DIRECTORY && " + advance + " " + first + " " + base + ") || exit 1 ;;\n"
			if name == "remote advances again" {
				push += "2) (unset GIT_OBJECT_DIRECTORY && " + advance + " " + second + " " + first + ") || exit 1 ;;\n"
			}
			wrapSyncGit(t, "push", push+"esac\n")

			result, err := Sync(t.Context(), f.realm)
			if got := readRealmshareFile(t, countFile); got != "2\n" {
				t.Fatalf("push count = %q, want exactly two attempts (sync error: %v)", got, err)
			}
			head := f.git(t, clone, "rev-parse", "HEAD")
			if got := f.git(t, clone, "rev-parse", "HEAD^2"); got != first {
				t.Errorf("merge's remote parent = %s, want first advance %s", got, first)
			}
			if got := f.git(t, clone, "show", "HEAD:shared/guide.md"); got != "# Local snapshot" {
				t.Errorf("local snapshot in merged commit = %q", got)
			}
			wantRemote := head
			if name == "remote advances again" {
				assertSyncCloneError(t, err, clone)
				msg := strings.ToLower(err.Error())
				if !strings.Contains(msg, "again") && !strings.Contains(msg, "retry") && !strings.Contains(msg, "rerun") && !strings.Contains(msg, "repeat") {
					t.Errorf("second advance did not indicate a retry: %v", err)
				}
				wantRemote = second
			} else if err != nil || result != (SyncResult{Head: head, Committed: true, Received: true, Published: true}) {
				t.Errorf("sync after first advance = %+v, %v; want published merge %s", result, err, head)
			}
			if got := f.git(t, f.remote, "rev-parse", ref); got != wantRemote {
				t.Errorf("remote tip = %s, want %s", got, wantRemote)
			}
		})
	}
}

func TestSyncPreservesEditMadeAfterStaging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires POSIX shebang support")
	}
	f, _ := newLinkFixture(t)
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	const source = "shared/guide.md"
	path := filepath.Join(clone, source)
	writeRealmshareFile(t, path, "# Staged snapshot\n")
	marker := filepath.Join(t.TempDir(), "edited")
	wrapSyncGit(t, "commit|commit-tree", "if [ ! -f "+quoteShareShell(marker)+" ]; then\n"+
		"  printf '# Later edit\\n' > "+quoteShareShell(path)+" || exit 1\n"+
		"  touch "+quoteShareShell(marker)+" || exit 1\nfi\n")

	result, err := Sync(t.Context(), f.realm)
	if _, markerErr := os.Stat(marker); markerErr != nil {
		t.Fatalf("fixture never edited the file at commit time: %v (sync error: %v)", markerErr, err)
	}
	if got := readRealmshareFile(t, path); got != "# Later edit\n" {
		t.Errorf("worktree replaced post-staging edit with %q", got)
	}
	if got := f.git(t, clone, "show", "HEAD:"+source); got != "# Staged snapshot" {
		t.Errorf("commit used %q instead of the staged snapshot", got)
	}
	assertSyncCloneError(t, err, clone, "work", source, "retry")
	head := f.git(t, clone, "rev-parse", "HEAD")
	if !result.Committed || result.Received || result.Head != head {
		t.Errorf("first sync = %+v, want retained snapshot at %s", result, head)
	}
	if remote := f.git(t, f.remote, "rev-parse", ref); result.Published && remote != head {
		t.Errorf("published snapshot at %s, want %s", remote, head)
	}
	result, err = Sync(t.Context(), f.realm)
	if err != nil || !result.Committed || !result.Published {
		t.Fatalf("sync of preserved edit = %+v, %v", result, err)
	}
	if got := f.git(t, f.remote, "show", ref+":"+source); got != "# Later edit" {
		t.Errorf("remote was not updated with the preserved edit: %q", got)
	}
}

func TestSyncRetainsSnapshotWhenTransportFails(t *testing.T) {
	for _, failure := range []string{"authorization", "network"} {
		t.Run(failure, func(t *testing.T) {
			publisher := newJoinPublisher(t, portableManifest)
			base := publishJoinFixture(t, publisher)
			publisher.git(t, publisher.remote, "config", "--local", "http.receivepack", "true")
			git, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			backend := &cgi.Handler{Path: git, Args: []string{"http-backend"}, Env: []string{
				"GIT_PROJECT_ROOT=" + filepath.Dir(publisher.remote),
				"GIT_HTTP_EXPORT_ALL=1",
				"GIT_CONFIG_NOSYSTEM=1",
				"GIT_CONFIG_GLOBAL=" + os.DevNull,
			}}
			var denyPush atomic.Bool
			var uploadPackRequests atomic.Int64
			server := newJoinTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("service") == "git-upload-pack" || strings.HasSuffix(r.URL.Path, "/git-upload-pack") {
					uploadPackRequests.Add(1)
				}
				if denyPush.Load() && (r.URL.Query().Get("service") == "git-receive-pack" || strings.HasSuffix(r.URL.Path, "/git-receive-pack")) {
					http.Error(w, "fixture authorization denied", http.StatusForbidden)
					return
				}
				backend.ServeHTTP(w, r)
			}))
			f := newJoinReceiver(t, server.URL+"/team.git")
			if _, err := Join(t.Context(), f.realm, f.remote); err != nil {
				t.Fatal(err)
			}
			uploadPackRequests.Store(0)
			binding := f.binding(t)
			clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
			writeRealmshareFile(t, filepath.Join(clone, "guide.md"), "# Retained snapshot\n")
			writeRealmshareFile(t, filepath.Join(publisher.realm, "shared", "wiki", "index.md"), "# Remote work\n")
			remoteHead := syncFixtureCommit(t, publisher, publisher.realm, "Advance remote wiki", "shared/wiki/index.md")
			publisher.git(t, publisher.realm, "push", "--quiet", publisher.remote, "HEAD:"+ref)
			cause := "403"
			if failure == "authorization" {
				denyPush.Store(true)
			} else {
				server.Close()
				cause = "connect"
			}

			_, err = Sync(t.Context(), f.realm)
			assertSyncCloneError(t, err, clone, cause)
			if failure == "authorization" {
				if got := uploadPackRequests.Load(); got != 0 {
					t.Errorf("upload-pack requests after authorization failure = %d, want 0", got)
				}
			}
			head := f.git(t, clone, "rev-parse", "HEAD")
			if head == base || f.git(t, clone, "rev-list", "--parents", "-n1", "HEAD") != head+" "+base {
				t.Errorf("transport failure did not retain a snapshot directly on %s: HEAD %s", base, head)
			}
			if got := f.git(t, clone, "show", "HEAD:guide.md"); got != "# Retained snapshot" {
				t.Errorf("retained snapshot = %q", got)
			}
			if got := publisher.git(t, publisher.remote, "rev-parse", ref); got != remoteHead {
				t.Errorf("transport failure advanced remote to %s", got)
			}
			assertRealmshareAbsent(t, filepath.Join(clone, ".git", "MERGE_HEAD"))
		})
	}
}

func TestSyncPublishesPendingShareWithoutCutover(t *testing.T) {
	f := newShareFixture(t)
	binding := f.publishGuide(t)
	f.write(t, "sops/index.md", "# Reviewed procedures\n")
	bucket := Bucket{Name: "sops", Path: filepath.Join(f.realm, "sops")}
	binding.PrivateBuckets = []Bucket{bucket}
	if err := SaveBinding(f.realm, binding); err != nil {
		t.Fatal(err)
	}
	opts := ShareOptions{Paths: []string{"sops"}}
	plan := f.preview(t, opts)
	hook := f.hook(t, "pre-receive", "echo realm-fixture-rejected >&2\nexit 1\n")
	if err := ApplyShare(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "realm-fixture-rejected") {
		t.Fatalf("pending share setup: %v", err)
	}
	pending := f.binding(t).PendingShare
	if pending == nil || pending.Published || pending.Commit == "" {
		t.Fatalf("expected retained unpublished share, got %+v", pending)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}

	if got, err := Sync(t.Context(), f.realm); err != nil || got != (SyncResult{Head: pending.Commit, Published: true}) {
		t.Fatalf("sync of retained share = %+v, %v; want publication of %s", got, err, pending.Commit)
	}
	if got := f.git(t, f.remote, "rev-parse", "refs/heads/"+binding.Branch); got != pending.Commit {
		t.Errorf("retained share was not published: %s", got)
	}
	f.assertOriginal(t, "sops", "index.md", "# Reviewed procedures\n")
	after := f.binding(t)
	if after.PendingShare == nil || after.PendingShare.Commit != pending.Commit || !slices.Equal(after.PrivateBuckets, []Bucket{bucket}) {
		t.Fatalf("sync lost pending share or private registration: %+v", after)
	}
	if !slices.Equal(after.Links, binding.Links) {
		t.Errorf("sync changed link ownership: %+v", after.Links)
	}

	f.apply(t, f.preview(t, opts))
	completed := f.binding(t)
	if completed.PendingShare != nil {
		t.Errorf("explicit share did not finish cutover: %+v", completed.PendingShare)
	}
	f.assertLink(t, completed, "sops", "")
}

func TestSyncRefusesUnrelatedCommitAfterPendingShare(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires POSIX shebang support")
	}
	f := newShareFixture(t)
	b := f.publishGuide(t)
	clone, ref := b.ClonePath, "refs/heads/"+b.Branch
	base := f.git(t, f.remote, "rev-parse", ref)
	f.write(t, "procedures.md", "# Reviewed procedures\n")
	plan := f.preview(t, ShareOptions{Paths: []string{"procedures.md"}})
	transportCalled := filepath.Join(t.TempDir(), "transport-called")
	const withheld = "realm-fixture-publication-withheld"
	wrapSyncGit(t, "push|fetch", "printf '%s\\n' \"$arg\" > "+quoteShareShell(transportCalled)+" || exit 1\n"+
		"echo "+withheld+" >&2\nexit 1\n")
	if err := ApplyShare(t.Context(), plan); err == nil || !strings.Contains(err.Error(), withheld) {
		t.Fatalf("pending share setup: %v", err)
	}
	if got := readRealmshareFile(t, transportCalled); got != "push\n" {
		t.Fatalf("pending share setup stopped at %q, want push", got)
	}
	if err := os.Remove(transportCalled); err != nil {
		t.Fatal(err)
	}
	pending := f.binding(t).PendingShare
	if pending == nil || pending.Commit == "" || pending.Published {
		t.Fatalf("expected retained unpublished share, got %+v", pending)
	}
	const source = "guide.md"
	guide := filepath.Join(clone, source)
	const edit = "# Independently edited guide\n"
	writeRealmshareFile(t, guide, edit)
	head := syncFixtureCommit(t, f, clone, "Edit the existing guide", source)
	if got := f.git(t, clone, "rev-parse", "HEAD^"); got != pending.Commit {
		t.Fatalf("unrelated edit parent = %s, want pending commit %s", got, pending.Commit)
	}

	result, err := Sync(t.Context(), f.realm)
	assertSyncCloneError(t, err, clone, "pending share")
	if result.Committed || result.Received || result.Published {
		t.Errorf("refused sync reported changes: %+v", result)
	}
	assertRealmshareAbsent(t, transportCalled)
	if got := f.git(t, f.remote, "rev-parse", ref); got != base {
		t.Errorf("remote tip = %s, want unchanged %s", got, base)
	}
	if got := f.git(t, clone, "rev-parse", "HEAD"); got != head {
		t.Errorf("clone HEAD = %s, want preserved %s", got, head)
	}
	if got := readRealmshareFile(t, guide); got != edit {
		t.Errorf("unrelated guide edit = %q, want %q", got, edit)
	}
	if got := f.git(t, clone, "status", "--porcelain=v1"); got != "" {
		t.Errorf("clone index or worktree changed: %q", got)
	}
	if after := f.binding(t).PendingShare; after == nil || after.Published || after.Commit != pending.Commit {
		t.Errorf("pending share receipt changed: %+v", after)
	}
	f.assertOriginal(t, "procedures.md", "", "# Reviewed procedures\n")
}

func syncFixtureCommit(t *testing.T, f shareFixture, dir, message string, paths ...string) string {
	t.Helper()
	f.git(t, dir, append([]string{"add", "--"}, paths...)...)
	f.git(t, dir, "commit", "--quiet", "-m", message)
	return f.git(t, dir, "rev-parse", "HEAD")
}

func assertSyncCloneError(t *testing.T, err error, clone string, details ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("sync succeeded; want an error with clone recovery context")
	}
	msg := err.Error()
	if !strings.Contains(msg, clone) && !strings.Contains(msg, strconv.Quote(clone)) {
		t.Errorf("sync error does not name clone %q: %v", clone, err)
	}
	msg = strings.ToLower(strings.NewReplacer(`\\`, "/", `\`, "/").Replace(msg))
	for _, detail := range details {
		if !strings.Contains(msg, strings.ToLower(detail)) {
			t.Errorf("sync error does not mention %q: %v", detail, err)
		}
	}
}

func wrapSyncGit(t *testing.T, commands, body string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	wrapper := filepath.Join(shim, "git")
	writeRealmshareFile(t, wrapper, "#!/bin/sh\ngit="+quoteShareShell(git)+"\n"+
		"for arg do\ncase \"$arg\" in\n"+commands+")\n"+body+"break ;;\nesac\ndone\nexec \"$git\" \"$@\"\n")
	if err := os.Chmod(wrapper, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
}
