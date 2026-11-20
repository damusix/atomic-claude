package realmshare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInspectStatusReportsPathOwnership(t *testing.T) {
	requireShareSymlinks(t)
	f, _, head := joinedStatusFixture(t)
	f.write(t, "guide.md", "# Private guide\n")
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "wiki", "research")); err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)

	research := filepath.Join(f.realm, "research")
	target, err := os.Readlink(research)
	if err != nil {
		t.Fatal(err)
	}
	parked := filepath.Join(t.TempDir(), "original-link")
	if err := os.Rename(research, parked); err != nil {
		t.Fatal(err)
	}
	symlinkRealmshare(t, target, research)
	replacement := linkLstat(t, research)
	if os.SameFile(replacement, linkLstat(t, parked)) {
		t.Fatal("drift fixture did not replace the owned symlink")
	}

	report := inspectUnchangedStatus(t, f)
	if report.RealmRoot != f.realm || report.StateRoot != f.state || report.ClonePath != binding.ClonePath ||
		report.RealmID != binding.RealmID || report.Remote != f.remote || report.Branch != binding.Branch || report.Head != head {
		t.Errorf("status did not identify the local realm and its bound checkout: %+v", report)
	}
	for _, want := range []StatusPath{
		{Path: "wiki", Source: "shared/wiki", Kind: KindDirectory, State: "linked"},
		{Path: "guide.md", Source: "guide.md", Kind: KindFile, State: "conflicting"},
	} {
		got := statusPath(t, report, want.Path)
		if got.Source != want.Source || got.Kind != want.Kind || got.State != want.State || got.Drift {
			t.Errorf("status path %q = %+v; want source %q, kind %q, state %q, no drift", want.Path, got, want.Source, want.Kind, want.State)
		}
	}
	if got := statusPath(t, report, "research"); !got.Drift || got.State == "available" || got.Detail == "" {
		t.Errorf("same-target replacement should report ownership drift, not be available: %+v", got)
	}
	if !os.SameFile(replacement, linkLstat(t, research)) {
		t.Error("inspection replaced the altered symlink")
	}
	f.assertOriginal(t, "guide.md", "", "# Private guide\n")
}

func TestInspectStatusReportsDriftThroughReplacedLinkParent(t *testing.T) {
	for _, replacement := range []string{"file", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			requireShareSymlinks(t)
			f, _ := newLinkFixture(t)
			const destination = "docs/notes"
			if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, destination)); err != nil {
				t.Fatal(err)
			}
			original := linkLstat(t, filepath.Join(f.realm, destination))
			parent := filepath.Join(f.realm, "docs")
			parked := filepath.Join(f.realm, "parked-docs")
			if err := os.Rename(parent, parked); err != nil {
				t.Fatal(err)
			}
			if replacement == "symlink" {
				symlinkRealmshare(t, parked, parent)
			} else {
				writeRealmshareFile(t, parent, "# Private document\n")
			}
			replaced := linkLstat(t, parent)

			report := inspectUnchangedStatus(t, f)
			if got := statusPath(t, report, destination); !got.Drift || got.State != "conflicting" || got.Detail == "" {
				t.Errorf("owned link under a replaced parent should report drift: %+v", got)
			}
			if !os.SameFile(replaced, linkLstat(t, parent)) ||
				!os.SameFile(original, linkLstat(t, filepath.Join(parked, "notes"))) {
				t.Error("inspection changed the replacement parent or the parked link")
			}
		})
	}
}

func TestInspectStatusUsesOnlyCachedRemote(t *testing.T) {
	f, publisher, initial := joinedStatusFixture(t)
	cache := inspectUnchangedStatus(t, f).CachedRemote
	if cache == nil || cache.Head != initial || !cache.LastSuccess.IsZero() {
		t.Fatalf("joined remote observation should not claim a successful sync: %+v", cache)
	}

	binding := f.binding(t)
	if result, err := Sync(t.Context(), f.realm); err != nil || result.Head != initial {
		t.Fatalf("initial sync = %+v, %v; want %s", result, err, initial)
	}
	cached := f.binding(t).Sync
	if cached == nil || cached.LastSuccess.IsZero() {
		t.Fatalf("successful sync did not persist an observation: %+v", cached)
	}

	definition := filepath.Join(publisher.realm, manifestName)
	writeRealmshareFile(t, definition, readRealmshareFile(t, definition)+
		manifestMapping("team/sops", "sops", string(KindDirectory)))
	publisher.write(t, "team/sops/index.md", "# Team procedures\n")
	advanced := syncFixtureCommit(t, publisher, publisher.realm, "Publish procedures", manifestName, "team/sops")
	publisher.git(t, publisher.realm, "push", "--quiet", f.remote, "HEAD:refs/heads/"+binding.Branch)

	stale := inspectUnchangedStatus(t, f)
	if stale.Head != initial || stale.CachedRemote == nil || stale.CachedRemote.Head != initial ||
		!stale.CachedRemote.DivergenceKnown || stale.CachedRemote.Ahead != 0 || stale.CachedRemote.Behind != 0 ||
		!stale.CachedRemote.LastSuccess.Equal(cached.LastSuccess) {
		t.Errorf("remote-only advance changed local or cached status: head=%q cache=%+v", stale.Head, stale.CachedRemote)
	}
	for _, p := range stale.Paths {
		if p.Path == "sops" {
			t.Errorf("reported remote-only path before sync: %+v", p)
		}
	}

	trace := os.Getenv("GIT_TRACE_PACKET")
	if result, err := Sync(t.Context(), f.realm); err != nil || result != (SyncResult{Head: advanced, Received: true}) {
		t.Fatalf("receive-only sync = %+v, %v; want %s", result, err, advanced)
	}
	if packets := readRealmshareFile(t, trace); packets == "" {
		t.Fatal("Git packet trace did not observe fixture transport during sync")
	}
	fresh := inspectUnchangedStatus(t, f)
	if fresh.Head != advanced || fresh.CachedRemote == nil || fresh.CachedRemote.Head != advanced ||
		!fresh.CachedRemote.DivergenceKnown || fresh.CachedRemote.Ahead != 0 || fresh.CachedRemote.Behind != 0 ||
		!fresh.CachedRemote.LastSuccess.Equal(f.binding(t).Sync.LastSuccess) {
		t.Errorf("status did not reflect received sync: head=%q cache=%+v", fresh.Head, fresh.CachedRemote)
	}
	if got := statusPath(t, fresh, "sops"); got.Source != "team/sops" || got.Kind != KindDirectory || got.State != "available" || got.Drift {
		t.Errorf("received path should be available but unlinked: %+v", got)
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, "sops"))
}

func TestInspectStatusReportsCloneIndexAndWorktreeSeparately(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const staged, unstaged, untracked = "shared/guide.md", "shared/notes/index.md", "shared/notes/draft notes.md"
	writeRealmshareFile(t, filepath.Join(clone, staged), "# Staged guide\n")
	f.git(t, clone, "add", "--", staged)
	writeRealmshareFile(t, filepath.Join(clone, unstaged), "# Worktree notes\n")
	writeRealmshareFile(t, filepath.Join(clone, untracked), "# Draft\n")
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(filepath.Join(clone, manifestName), old, old); err != nil {
		t.Fatal(err)
	}

	report := inspectUnchangedStatus(t, f)
	if report.CachedRemote != nil {
		t.Errorf("status without a persisted observation invented remote knowledge: %+v", report.CachedRemote)
	}
	changes := make(map[string]StatusChange, len(report.Changes))
	for _, change := range report.Changes {
		changes[change.Path] = change
	}
	if len(report.Changes) != 3 || len(changes) != 3 {
		t.Errorf("changes = %+v; want exactly the three changed clone paths", report.Changes)
	}
	if got := changes[staged]; got.Index == "" || got.Worktree != "" {
		t.Errorf("staged-only change = %+v", got)
	}
	if got := changes[unstaged]; got.Index != "" || got.Worktree == "" {
		t.Errorf("unstaged-only change = %+v", got)
	}
	if got, ok := changes[untracked]; !ok || got.Index == "" && got.Worktree == "" ||
		(got.Index == changes[staged].Index && got.Worktree == changes[staged].Worktree) ||
		(got.Index == changes[unstaged].Index && got.Worktree == changes[unstaged].Worktree) {
		t.Errorf("untracked path is missing or not distinguished from tracked changes: %+v", got)
	}
}

func TestInspectStatusReportsMappedMergeConflict(t *testing.T) {
	requireShareSymlinks(t)
	f, publisher, _ := joinedStatusFixture(t)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "wiki")); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(t.Context(), f.realm); err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)
	const source, local = "shared/wiki/index.md", "wiki/index.md"
	publisher.write(t, source, "# Remote wiki\n")
	remoteHead := syncFixtureCommit(t, publisher, publisher.realm, "Edit team wiki", source)
	publisher.git(t, publisher.realm, "push", "--quiet", f.remote, "HEAD:refs/heads/"+binding.Branch)
	f.write(t, local, "# Local wiki\n")
	_, err := Sync(t.Context(), f.realm)
	assertSyncCloneError(t, err, binding.ClonePath, "merge conflict")
	localHead := f.git(t, binding.ClonePath, "rev-parse", "HEAD")

	report := inspectUnchangedStatus(t, f)
	if report.Head != localHead || len(report.Conflicts) != 1 || report.Conflicts[0].Source != source || report.Conflicts[0].Path != local {
		t.Errorf("head=%q conflicts=%+v; want %s and %s mapped to %s", report.Head, report.Conflicts, localHead, source, local)
	}
	if cached := report.CachedRemote; cached == nil || cached.Head != remoteHead || !cached.DivergenceKnown ||
		cached.Ahead != 1 || cached.Behind != 1 || !cached.LastSuccess.Equal(binding.Sync.LastSuccess) {
		t.Errorf("conflicted sync should report observed divergence and retain last success: %+v", cached)
	}

	if !ClaimSyncReminder(t.Context(), f.realm, time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local)) {
		t.Error("merge conflict suppressed a known unpublished commit")
	}
	if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != localHead {
		t.Errorf("reminder changed conflicted HEAD to %q, want %q", got, localHead)
	}
}

func TestInspectStatusUsesCommittedDefinitionWhenWorktreeDefinitionUnavailable(t *testing.T) {
	for _, state := range []string{"merge conflict", "missing"} {
		t.Run(state, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			b := f.binding(t)
			definitionPath := filepath.Join(b.ClonePath, manifestName)
			definition := readRealmshareFile(t, definitionPath)
			const original = `destination = "guide.md"`
			if state == "merge conflict" {
				f.git(t, b.ClonePath, "checkout", "--quiet", "-b", "other-definition")
				writeRealmshareFile(t, definitionPath, strings.Replace(definition, original, `destination = "other-guide.md"`, 1))
				syncFixtureCommit(t, f, b.ClonePath, "Change guide destination on other branch", manifestName)
				f.git(t, b.ClonePath, "checkout", "--quiet", b.Branch)
			}
			writeRealmshareFile(t, definitionPath, strings.Replace(definition, original, `destination = "local-guide.md"`, 1))
			head := syncFixtureCommit(t, f, b.ClonePath, "Change guide destination locally", manifestName)
			if state == "merge conflict" {
				cmd := exec.CommandContext(t.Context(), "git", "merge", "--no-edit", "other-definition")
				cmd.Dir = b.ClonePath
				if out, err := cmd.CombinedOutput(); !gitExit(err, 1) {
					t.Fatalf("fixture expected a merge conflict: %v\n%s", err, out)
				}
				if stages := f.git(t, b.ClonePath, "ls-files", "--unmerged", "--", manifestName); stages == "" {
					t.Fatal("merge did not leave the definition unmerged")
				}
			} else if err := os.Remove(definitionPath); err != nil {
				t.Fatal(err)
			}

			report := inspectUnchangedStatus(t, f)
			if report.RealmRoot != f.realm || report.StateRoot != f.state || report.ClonePath != b.ClonePath ||
				report.RealmID != b.RealmID || report.Remote != b.Remote || report.Branch != b.Branch || report.Head != head {
				t.Errorf("status did not identify the bound checkout at %s: %+v", head, report)
			}
			if got := statusPath(t, report, "local-guide.md"); got.Source != "shared/guide.md" ||
				got.Kind != KindFile || got.State != "available" || got.Drift {
				t.Errorf("status should report the committed definition's mapping: %+v", got)
			}
			if state == "merge conflict" {
				if len(report.Conflicts) != 1 || report.Conflicts[0].Source != manifestName {
					t.Errorf("conflicts = %+v; want the unmerged %s", report.Conflicts, manifestName)
				}
			} else if len(report.Conflicts) != 0 {
				t.Errorf("a missing definition is not a merge conflict: %+v", report.Conflicts)
			}
		})
	}
}

func TestStatusDefinitionFallbackRequiresManifestIndexConflict(t *testing.T) {
	const source, destination = "shared/notes", "docs/notes"
	for _, conflict := range []string{manifestName, source + "/index.md"} {
		t.Run(conflict, func(t *testing.T) {
			requireShareSymlinks(t)
			f, _ := newLinkFixture(t)
			if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, destination)); err != nil {
				t.Fatal(err)
			}
			b := f.binding(t)
			definitionPath := filepath.Join(b.ClonePath, manifestName)
			definition := readRealmshareFile(t, definitionPath)
			const mapping = `destination = "docs/notes"`
			otherDefinition := strings.Replace(definition, mapping, `destination = "other-notes"`, 1)
			local, other := "# Local notes\n", "# Other notes\n"
			if conflict == manifestName {
				local = strings.Replace(definition, mapping, mapping+" # Local notes", 1)
				other = otherDefinition
			}

			conflictPath := filepath.Join(b.ClonePath, conflict)
			f.git(t, b.ClonePath, "checkout", "--quiet", "-b", "other-status")
			writeRealmshareFile(t, conflictPath, other)
			syncFixtureCommit(t, f, b.ClonePath, "Edit other status branch", conflict)
			f.git(t, b.ClonePath, "checkout", "--quiet", b.Branch)
			writeRealmshareFile(t, conflictPath, local)
			syncFixtureCommit(t, f, b.ClonePath, "Edit local status branch", conflict)
			cmd := exec.CommandContext(t.Context(), "git", "merge", "--no-edit", "other-status")
			cmd.Dir = b.ClonePath
			if out, err := cmd.CombinedOutput(); !gitExit(err, 1) {
				t.Fatalf("fixture expected a merge conflict: %v\n%s", err, out)
			}

			if conflict == manifestName {
				writeRealmshareFile(t, definitionPath, otherDefinition)
			} else {
				writeRealmshareFile(t, definitionPath, "version = [\n")
			}
			if stages := f.git(t, b.ClonePath, "diff", "--name-only", "--diff-filter=U"); stages != conflict {
				t.Fatalf("unmerged paths after editing the definition = %q; want only %q", stages, conflict)
			}

			if conflict != manifestName {
				if report, err := readUnchangedStatus(t, f); err == nil {
					t.Errorf("unrelated conflict allowed fallback for malformed definition: %+v", report)
				}
				if roots, err := LinkedRoots(f.realm); err == nil {
					t.Errorf("unrelated conflict allowed logical roots for malformed definition: %v", roots)
				}
				return
			}

			report := inspectUnchangedStatus(t, f)
			if len(report.Conflicts) != 1 || report.Conflicts[0].Source != manifestName {
				t.Errorf("conflicts = %+v; want the unmerged %s", report.Conflicts, manifestName)
			}
			if got := statusPath(t, report, destination); got.Source != source || got.State != "linked" || got.Drift {
				t.Errorf("unmerged definition should retain the committed mapping: %+v", got)
			}
			wantRoot := filepath.Join(report.RealmRoot, destination)
			if roots, err := LinkedRoots(f.realm); err != nil || len(roots) != 1 || roots[0] != wantRoot {
				t.Errorf("logical roots = %v, %v; want [%s]", roots, err, wantRoot)
			}
		})
	}
}

func TestInspectStatusReportsPublishedSharePendingCutover(t *testing.T) {
	f := newShareFixture(t)
	binding := f.publishGuide(t)
	f.write(t, "sops/index.md", "# Reviewed procedures\n")
	plan := f.preview(t, ShareOptions{Paths: []string{"sops"}})
	hook := f.hook(t, "pre-receive", "echo realm-fixture-rejected >&2\nexit 1\n")
	if err := ApplyShare(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "realm-fixture-rejected") {
		t.Fatalf("pending share setup: %v", err)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	pending := f.binding(t).PendingShare
	if pending == nil || pending.Commit == "" {
		t.Fatalf("share did not retain a pending publication: %+v", pending)
	}
	if result, err := Sync(t.Context(), f.realm); err != nil || result != (SyncResult{Head: pending.Commit, Published: true}) {
		t.Fatalf("publishing retained share = %+v, %v; want %s", result, err, pending.Commit)
	}
	if head := f.git(t, f.remote, "rev-parse", "refs/heads/"+binding.Branch); head != pending.Commit {
		t.Fatalf("remote head = %s; want the retained publication %s", head, pending.Commit)
	}

	report := inspectUnchangedStatus(t, f)
	if got := statusPath(t, report, "sops"); got.Kind != KindDirectory || got.State != "pending" || got.Drift {
		t.Errorf("published share without cutover should remain pending: %+v", got)
	}
	f.assertOriginal(t, "sops", "index.md", "# Reviewed procedures\n")
}

func TestInspectStatusReportsParkedPendingUnlinkWithoutDrift(t *testing.T) {
	requireShareSymlinks(t)
	f, _ := newLinkFixture(t)
	const destination, source = "guide.md", "shared/guide.md"
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, destination)); err != nil {
		t.Fatal(err)
	}
	b := f.binding(t)
	change := localChange{Token: "unlink-0", Link: b.Links[0]}
	b.PendingLocal = &pendingLocal{Unlink: true, Changes: []localChange{change}}
	if err := SaveBinding(f.realm, b); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(f.realm, destination)
	owned := linkLstat(t, live)
	var parked string
	withPendingLinkDirs(t, f, change, func(_ *localLinks, backups *os.Root, _, _ *os.File) error {
		parked = filepath.Join(backups.Name(), change.Token+".park")
		return os.Rename(live, parked)
	})

	report := inspectUnchangedStatus(t, f)
	if got := statusPath(t, report, destination); got.Source != source || got.State != "pending" || got.Drift {
		t.Errorf("parking the owned link should be pending without drift: %+v", got)
	}
	assertRealmshareAbsent(t, live)
	if !os.SameFile(owned, linkLstat(t, parked)) {
		t.Error("inspection replaced the parked symlink")
	}
}

func TestInspectStatusPreservesSourceDriftDuringPendingUnlink(t *testing.T) {
	requireShareSymlinks(t)
	f, _ := newLinkFixture(t)
	const destination, source = "guide.md", "shared/guide.md"
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, destination)); err != nil {
		t.Fatal(err)
	}
	b := f.binding(t)
	if len(b.Links) != 1 || b.Links[0].Path != destination || b.Links[0].Source != source {
		t.Fatalf("fixture expected one owned link for %s: %+v", destination, b.Links)
	}
	if err := os.Remove(filepath.Join(b.ClonePath, source)); err != nil {
		t.Fatal(err)
	}
	if got := statusPath(t, inspectUnchangedStatus(t, f), destination); !got.Drift || got.Detail == "" {
		t.Fatalf("fixture expected the missing source to report drift: %+v", got)
	}
	b.PendingLocal = &pendingLocal{Unlink: true, Changes: []localChange{{Token: "unlink-0", Link: b.Links[0]}}}
	if err := SaveBinding(f.realm, b); err != nil {
		t.Fatal(err)
	}

	report := inspectUnchangedStatus(t, f)
	if got := statusPath(t, report, destination); got.State != "pending" || !got.Drift || got.Detail == "" {
		t.Errorf("pending unlink should not hide the missing source drift: %+v", got)
	}
}

func joinedStatusFixture(t *testing.T) (receiver, publisher shareFixture, head string) {
	t.Helper()
	publisher = newJoinPublisher(t, portableManifest)
	head = publishJoinFixture(t, publisher)
	receiver = newJoinReceiver(t, publisher.remote)
	if _, err := Join(t.Context(), receiver.realm, receiver.remote); err != nil {
		t.Fatal(err)
	}
	return receiver, publisher, head
}

func inspectUnchangedStatus(t *testing.T, f shareFixture) StatusReport {
	t.Helper()
	report, err := readUnchangedStatus(t, f)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func readUnchangedStatus(t *testing.T, f shareFixture) (StatusReport, error) {
	t.Helper()
	trace := filepath.Join(t.TempDir(), "packets.log")
	t.Setenv("GIT_TRACE_PACKET", trace)
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	before := f.linkState(t, f.home)
	report, err := InspectStatus(t.Context(), f.realm)
	if after := f.linkState(t, f.home); after != before {
		t.Errorf("inspection changed the fixture (realm/state, home/clone, remote): before=%v after=%v", before, after)
	}
	if packets, err := os.ReadFile(trace); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if len(packets) != 0 {
		t.Errorf("inspection used Git transport:\n%s", packets)
	}
	return report, err
}

func statusPath(t *testing.T, report StatusReport, path string) StatusPath {
	t.Helper()
	for _, item := range report.Paths {
		if item.Path == path {
			return item
		}
	}
	t.Fatalf("status missing %q in %+v", path, report.Paths)
	return StatusPath{}
}
