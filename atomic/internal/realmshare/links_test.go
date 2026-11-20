package realmshare

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func TestPreviewLinkIsReadOnly(t *testing.T) {
	f, _ := newLinkFixture(t)
	before := f.linkState(t, f.home)
	plan := previewRealmLink(t, f.realm, "guide.md")
	if after := f.linkState(t, f.home); after != before {
		t.Fatal("preview changed the local realm, user state, or Git remote")
	}
	requireShareSymlinks(t)
	if err := ApplyLink(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if got := f.assertLink(t, f.binding(t), "guide.md", "# Team guide\n"); got.Source != "shared/guide.md" {
		t.Errorf("linked the wrong clone source: %+v", got)
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, "docs", "notes"))
}

func TestApplyLinkRejectsChangedDefinition(t *testing.T) {
	f, seed := newLinkFixture(t)
	requireShareSymlinks(t)
	plan := previewRealmLink(t, f.realm, "guide.md")
	receiveLinkBucket(t, f, seed)
	clone := f.binding(t).ClonePath
	before := f.linkState(t, clone)
	if err := ApplyLink(t.Context(), plan); err == nil {
		t.Fatal("applied approval for the previous definition")
	}
	if after := f.linkState(t, clone); after != before {
		t.Fatal("stale approval changed the realm or shared storage")
	}
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md")); err != nil {
		t.Fatalf("apply after renewed review: %v", err)
	}
	f.assertLink(t, f.binding(t), "guide.md", "# Team guide\n")
	assertRealmshareAbsent(t, filepath.Join(f.realm, "sops"))
}

func TestApplyLinkRevalidatesAllSelectedPaths(t *testing.T) {
	for _, scenario := range []string{"private file", "same-target unowned link", "missing target", "escaping source", "escaping destination parent"} {
		t.Run(scenario, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			requireShareSymlinks(t)
			plan := previewRealmLink(t, f.realm, "guide.md", "docs/notes/")
			binding := f.binding(t)
			local := filepath.Join(f.realm, "docs", "notes")
			source := filepath.Join(binding.ClonePath, "shared", "notes")
			outside := t.TempDir()
			writeRealmshareFile(t, filepath.Join(outside, "private.md"), "never shared\n")
			switch scenario {
			case "private file":
				writeRealmshareFile(t, local, "private collision\n")
			case "same-target unowned link":
				symlinkRealmshare(t, source, local)
			case "missing target":
				if err := os.Rename(source, filepath.Join(outside, "notes")); err != nil {
					t.Fatal(err)
				}
			case "escaping source":
				symlinkRealmshare(t, filepath.Join(outside, "private.md"), filepath.Join(source, "escape.md"))
			case "escaping destination parent":
				if err := os.Rename(filepath.Dir(local), filepath.Join(outside, "old-docs")); err != nil {
					t.Fatal(err)
				}
				symlinkRealmshare(t, outside, filepath.Dir(local))
			}
			before, outsideBefore := f.linkState(t, binding.ClonePath), linkTreeDigest(t, outside)
			if err := ApplyLink(t.Context(), plan); err == nil {
				t.Fatal("applied a selection that became unsafe after review")
			}
			if f.linkState(t, binding.ClonePath) != before || linkTreeDigest(t, outside) != outsideBefore {
				t.Fatal("failed apply modified a selected path, its ownership, or outside content")
			}
			assertRealmshareAbsent(t, filepath.Join(f.realm, "guide.md"))
		})
	}
}

func TestLinkRepeatPreservesOwnedSymlink(t *testing.T) {
	f, _ := newLinkFixture(t)
	requireShareSymlinks(t)
	local := filepath.Join(f.realm, "guide.md")
	var original os.FileInfo
	for range 2 {
		if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md")); err != nil {
			t.Fatal(err)
		}
		f.assertLink(t, f.binding(t), "guide.md", "# Team guide\n")
		current := linkLstat(t, local)
		if original != nil && !os.SameFile(original, current) {
			t.Fatal("repeat replaced the owned symlink")
		}
		original = current
	}
}

func TestLinkAndUnlinkRefuseReplacedOwnedLinks(t *testing.T) {
	for _, scenario := range []struct{ operation, replacement string }{
		{"link", "same target"},
		{"unlink", "same target"},
		{"unlink", "regular file"},
	} {
		t.Run(scenario.operation+"/"+scenario.replacement, func(t *testing.T) {
			f := newShareFixture(t)
			binding := f.publishGuide(t)
			backup := f.restorationBackup(t, binding, "guide.md")
			local := filepath.Join(f.realm, "guide.md")
			target, err := os.Readlink(local)
			if err != nil {
				t.Fatal(err)
			}
			var plan LinkPlan
			if scenario.operation == "link" {
				plan = previewRealmLink(t, f.realm, "guide.md")
			}
			parked := filepath.Join(t.TempDir(), "original-link")
			if err := os.Rename(local, parked); err != nil {
				t.Fatal(err)
			}
			if scenario.replacement == "same target" {
				symlinkRealmshare(t, target, local)
			} else {
				f.write(t, "guide.md", "private replacement\n")
			}
			foreign := linkLstat(t, local)
			if os.SameFile(foreign, linkLstat(t, parked)) {
				t.Fatal("replacement fixture is not a distinct entry")
			}
			before := linkTreeDigest(t, f.realm)
			if scenario.operation == "link" {
				err = ApplyLink(t.Context(), plan)
			} else {
				err = Unlink(t.Context(), f.realm)
			}
			if err == nil {
				t.Fatal("treated the replacement as an Atomic-owned link")
			}
			if !os.SameFile(foreign, linkLstat(t, local)) || linkTreeDigest(t, f.realm) != before {
				t.Fatal("refusal changed the replacement or local ownership state")
			}
			if got := readRealmshareFile(t, backup); got != "# Guide\n" {
				t.Errorf("retained original = %q", got)
			}
		})
	}
}

func TestUnlinkRemovesOnlyOwnedLinksWithoutOriginals(t *testing.T) {
	f, _ := newLinkFixture(t)
	requireShareSymlinks(t)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md")); err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)
	writeRealmshareFile(t, filepath.Join(binding.ClonePath, "shared", "guide.md"), "unpublished guide edit\n")
	f.write(t, "private.md", "private realm content\n")
	privateLink := filepath.Join(f.realm, "private-link.md")
	symlinkRealmshare(t, filepath.Join(f.realm, "private.md"), privateLink)
	privateEntry := linkLstat(t, privateLink)
	if err := Unlink(t.Context(), f.realm); err != nil {
		t.Fatal(err)
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, "guide.md"))
	f.assertOriginal(t, "private.md", "", "private realm content\n")
	if !os.SameFile(privateEntry, linkLstat(t, privateLink)) {
		t.Error("unlink replaced a user-owned symlink")
	}
	current := f.binding(t)
	if len(current.Links) != 0 || current.RealmID != binding.RealmID || current.ClonePath != binding.ClonePath || current.Remote != binding.Remote || current.Branch != binding.Branch {
		t.Fatalf("unlink changed the binding instead of clearing link ownership: %+v", current)
	}
	if got := readRealmshareFile(t, filepath.Join(binding.ClonePath, "shared", "guide.md")); got != "unpublished guide edit\n" {
		t.Errorf("unlink discarded the unpublished edit: %q", got)
	}
	if got := f.git(t, f.remote, "show", "refs/heads/"+binding.Branch+":shared/guide.md"); got != "# Team guide" {
		t.Errorf("unlink changed the published guide: %q", got)
	}
}

func TestRelinkRestoredOriginalsAfterSharedWikiAdvances(t *testing.T) {
	f, wikiHead := publishLinkWiki(t)
	initial := f.binding(t)
	if err := Unlink(t.Context(), f.realm); err != nil {
		t.Fatal(err)
	}
	f.assertOriginal(t, "guide.md", "", "# Guide\n")
	f.assertOriginal(t, "wiki", "index.md", "# Original draft\n")
	assertLinkWikiHistory(t, f, filepath.Join(f.realm, "wiki"), wikiHead)
	unlinked := f.binding(t)
	if len(unlinked.Links) != 0 {
		t.Fatalf("unlink left live link receipts: %+v", unlinked.Links)
	}
	assertLinkWikiHistory(t, f, f.restorationBackup(t, unlinked, "wiki"), wikiHead)

	writeRealmshareFile(t, filepath.Join(initial.ClonePath, "wiki", "index.md"), "# Shared advance\n")
	f.git(t, initial.ClonePath, "add", "--", "wiki/index.md")
	f.git(t, initial.ClonePath, "commit", "--quiet", "-m", "advance shared wiki")
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md", "wiki/")); err != nil {
		t.Fatalf("relink unchanged originals against advanced shared content: %v", err)
	}
	relinked := f.binding(t)
	f.assertLink(t, relinked, "guide.md", "# Guide\n")
	f.assertLink(t, relinked, "wiki", "")
	if got := readRealmshareFile(t, filepath.Join(f.realm, "wiki", "index.md")); got != "# Shared advance\n" {
		t.Errorf("relinked wiki index = %q", got)
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, "wiki", ".git"))
	assertLinkWikiHistory(t, f, f.restorationBackup(t, relinked, "wiki"), wikiHead)

	if err := Unlink(t.Context(), f.realm); err != nil {
		t.Fatal(err)
	}
	f.assertOriginal(t, "guide.md", "", "# Guide\n")
	f.assertOriginal(t, "wiki", "index.md", "# Original draft\n")
	assertLinkWikiHistory(t, f, filepath.Join(f.realm, "wiki"), wikiHead)
	if got := readRealmshareFile(t, filepath.Join(initial.ClonePath, "wiki", "index.md")); got != "# Shared advance\n" {
		t.Errorf("second unlink changed the shared advance: %q", got)
	}
}

func TestRelinkRevalidatesCompleteRestoredIdentity(t *testing.T) {
	for _, scenario := range []string{"same-size file edit with original timestamp", "wiki Git history only"} {
		t.Run(scenario, func(t *testing.T) {
			f, wikiHead := publishLinkWiki(t)
			if err := Unlink(t.Context(), f.realm); err != nil {
				t.Fatal(err)
			}
			plan := previewRealmLink(t, f.realm, "guide.md", "wiki")
			wiki := filepath.Join(f.realm, "wiki")
			editedHead := wikiHead
			if scenario == "wiki Git history only" {
				f.git(t, wiki, "commit", "--quiet", "--allow-empty", "-m", "private wiki history")
				editedHead = f.git(t, wiki, "rev-parse", "HEAD")
				if editedHead == wikiHead {
					t.Fatal("fixture did not change retained Git history")
				}
			} else {
				local := filepath.Join(f.realm, "guide.md")
				stamp := linkLstat(t, local).ModTime()
				f.write(t, "guide.md", "# Local\n")
				if err := os.Chtimes(local, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			before := linkTreeDigest(t, f.realm)
			if err := ApplyLink(t.Context(), plan); err == nil {
				t.Fatal("relinked over an original edited after review")
			}
			if linkTreeDigest(t, f.realm) != before {
				t.Fatal("refusal changed a restored original or its receipts")
			}
			f.assertOriginal(t, "wiki", "index.md", "# Original draft\n")
			assertLinkWikiHistory(t, f, wiki, editedHead)
			current := f.binding(t)
			assertLinkWikiHistory(t, f, f.restorationBackup(t, current, "wiki"), wikiHead)
			if got := readRealmshareFile(t, filepath.Join(current.ClonePath, "guide.md")); got != "# Guide\n" {
				t.Errorf("refusal changed shared content: %q", got)
			}
		})
	}
}

func TestLinkSelectsReceivedBucketWithoutChangingOwnedLinks(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "available"
		if collision {
			name = "private collision"
		}
		t.Run(name, func(t *testing.T) {
			f, seed := newLinkFixture(t)
			requireShareSymlinks(t)
			if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md")); err != nil {
				t.Fatal(err)
			}
			guide := filepath.Join(f.realm, "guide.md")
			owned := linkLstat(t, guide)
			bucket := filepath.Join(f.realm, "sops")
			binding := f.binding(t)
			if collision {
				f.write(t, "sops/index.md", "# Private procedures\n")
				binding.PrivateBuckets = []Bucket{{Name: "sops", Path: bucket}}
				if err := SaveBinding(f.realm, binding); err != nil {
					t.Fatal(err)
				}
			}
			receiveLinkBucket(t, f, seed)
			if got := f.manifest(t, binding).Buckets; !slices.Equal(got, []Bucket{{Name: "sops", Path: "sops"}}) {
				t.Fatalf("received portable registration = %+v", got)
			}
			if !collision {
				assertRealmshareAbsent(t, bucket)
			}
			before := linkTreeDigest(t, f.realm)
			plan, err := PreviewLink(t.Context(), f.realm, []string{"sops/"})
			if linkTreeDigest(t, f.realm) != before {
				t.Fatal("review changed local paths or registrations")
			}
			if err == nil {
				err = ApplyLink(t.Context(), plan)
			}
			current := f.binding(t)
			if collision {
				if err == nil {
					t.Fatal("linked over a private bucket")
				}
				f.assertOriginal(t, "sops", "index.md", "# Private procedures\n")
				if !slices.Equal(current.PrivateBuckets, binding.PrivateBuckets) || !slices.Equal(current.Links, binding.Links) {
					t.Fatalf("collision changed private registration or link ownership: %+v", current)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				f.assertLink(t, current, "sops", "")
				if got := readRealmshareFile(t, filepath.Join(bucket, "index.md")); got != "# Team procedures\n" {
					t.Errorf("linked bucket index = %q", got)
				}
			}
			if !os.SameFile(owned, linkLstat(t, guide)) {
				t.Error("handling the received bucket replaced the existing owned guide link")
			}
			assertRealmshareAbsent(t, filepath.Join(f.realm, "docs", "notes"))
		})
	}
}

func TestApplyLinkFailsWithoutNativeSymlinks(t *testing.T) {
	f, _ := newLinkFixture(t)
	probe := t.TempDir()
	if err := os.Symlink(probe, filepath.Join(probe, "probe")); err == nil {
		t.Skip("fixture account can create native symlinks")
	}
	before := linkTreeDigest(t, f.realm)
	plan, err := PreviewLink(t.Context(), f.realm, []string{"guide.md"})
	if err == nil {
		err = ApplyLink(t.Context(), plan)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") || !strings.Contains(strings.ToLower(err.Error()), "enable") {
		t.Fatalf("missing actionable native symlink diagnostic: %v", err)
	}
	if linkTreeDigest(t, f.realm) != before {
		t.Fatal("unavailable symlinks changed the local realm")
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, "guide.md"))
}

func TestLinkAndUnlinkShareWriterLock(t *testing.T) {
	const rootEnv = "ATOMIC_TEST_REALM_LINK_LOCK_ROOT"
	const opEnv = "ATOMIC_TEST_REALM_LINK_LOCK_OPERATION"
	if realm := os.Getenv(rootEnv); realm != "" {
		if os.Getenv(opEnv) == "unlink" {
			if err := Unlink(t.Context(), realm); err != nil {
				t.Fatal(err)
			}
		} else if err := ApplyLink(t.Context(), previewRealmLink(t, realm, "guide.md")); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, op := range []string{"link", "unlink"} {
		t.Run(op, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			requireShareSymlinks(t)
			if op == "unlink" {
				if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md")); err != nil {
					t.Fatal(err)
				}
			}
			before := linkTreeDigest(t, f.realm)
			loc, err := locateBinding(f.realm)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := loc.lockShare()
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinkAndUnlinkShareWriterLock$")
			cmd.Env = append(os.Environ(), rootEnv+"="+f.realm, opEnv+"="+op)
			var stdout strings.Builder
			cmd.Stdout = &stdout
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			var notices []string
			scan := bufio.NewScanner(stderr)
			var waiting bool
			for scan.Scan() {
				line := scan.Text()
				notices = append(notices, line)
				if strings.Contains(line, "waiting on "+lock.Path()) {
					waiting = true
					break
				}
			}
			if !waiting {
				err := cmd.Wait()
				t.Fatalf("%s did not wait for the existing sharing lock: %v; stderr=%q; stdout=%q", op, err, notices, stdout.String())
			}
			if linkTreeDigest(t, f.realm) != before {
				t.Fatal("mutation changed the realm before acquiring the lock")
			}
			if op == "unlink" {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				if err := cmd.Wait(); err == nil {
					t.Fatal("interrupted unlink reported success")
				}
				if linkTreeDigest(t, f.realm) != before {
					t.Fatal("interrupted waiter changed links or receipts")
				}
			}
			if err := lock.Release(); err != nil {
				t.Fatal(err)
			}
			if op == "link" {
				if err := cmd.Wait(); err != nil {
					t.Fatalf("link after lock release: %v; stdout=%q", err, stdout.String())
				}
				f.assertLink(t, f.binding(t), "guide.md", "# Team guide\n")
			} else {
				if err := Unlink(t.Context(), f.realm); err != nil {
					t.Fatalf("retry interrupted unlink: %v", err)
				}
				assertRealmshareAbsent(t, filepath.Join(f.realm, "guide.md"))
			}
		})
	}
}

func TestRelinkRestartsBeforeFinalBindingWrite(t *testing.T) {
	for _, stage := range []string{"journaled", "original-retained", "link-installed"} {
		t.Run(stage, func(t *testing.T) {
			f, head := publishLinkWiki(t)
			if err := Unlink(t.Context(), f.realm); err != nil {
				t.Fatal(err)
			}
			plan := previewRealmLink(t, f.realm, "wiki")
			p := recordPendingLink(t, f, plan)
			change := p.Changes[0]
			wiki := filepath.Join(f.realm, "wiki")
			if stage != "journaled" {
				withPendingLinkDirs(t, f, change, func(l *localLinks, backups *os.Root, dir, parent *os.File) error {
					staged := change.Token + ".stage"
					if _, err := stageShareLink(backups, dir, filepath.Base(change.Link.Witness), staged, shareTarget(l.binding, plan.Selections[0].Mapping)); err != nil {
						return err
					}
					if err := os.Rename(wiki, filepath.Join(backups.Name(), change.Token)); err != nil || stage == "original-retained" {
						return err
					}
					if err := syncShareDirectories(parent, dir); err != nil {
						return err
					}
					if err := os.Rename(filepath.Join(backups.Name(), staged), wiki); err != nil {
						return err
					}
					assertPendingSyncError(t, func(pipe *os.File) error {
						return l.installLocalLink(t.Context(), backups, pipe, pipe, change)
					})
					return nil
				})
			}
			var installed os.FileInfo
			if stage == "link-installed" {
				installed = linkLstat(t, wiki)
			}
			if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "wiki")); err != nil {
				t.Fatal(err)
			}
			b := f.binding(t)
			if b.PendingLocal != nil || len(b.Links) != 1 || b.Links[0].Path != "wiki" {
				t.Fatalf("restarted relink did not finalize only wiki: %+v", b)
			}
			f.assertLink(t, b, "wiki", "")
			if installed != nil && !os.SameFile(installed, linkLstat(t, wiki)) {
				t.Error("restart replaced the installed symlink")
			}
			assertLinkWikiHistory(t, f, f.restorationBackup(t, b, "wiki"), head)
		})
	}
}

func TestUnlinkRecoversPartialLinkAfterReviewChanges(t *testing.T) {
	for _, changed := range []string{"definition", "instructions"} {
		t.Run(changed, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			requireShareSymlinks(t)
			clone := f.binding(t).ClonePath
			const team = ".agents/realm.md"
			manifest := filepath.Join(clone, manifestName)
			instructions := filepath.Join(clone, team)
			writeRealmshareFile(t, manifest, readRealmshareFile(t, manifest)+manifestMapping(team, team, string(KindInstructions)))
			writeRealmshareFile(t, instructions, "# Reviewed\n")
			plan := previewRealmLink(t, f.realm, "guide.md", team)
			p := recordPendingLink(t, f, plan)
			for _, change := range p.Changes {
				if change.Link.Path == "guide.md" {
					withPendingLinkDirs(t, f, change, func(l *localLinks, backups *os.Root, dir, parent *os.File) error {
						return l.installLocalLink(t.Context(), backups, dir, parent, change)
					})
				}
			}
			guide := filepath.Join(f.realm, "guide.md")
			installed := linkLstat(t, guide)
			if changed == "definition" {
				writeRealmshareFile(t, manifest, readRealmshareFile(t, manifest)+"\n# New definition\n")
			} else {
				writeRealmshareFile(t, instructions, "# Modified\n")
			}
			if err := ApplyLink(t.Context(), plan); err == nil {
				t.Fatal("repeated link accepted the stale review")
			}
			assertRealmshareAbsent(t, filepath.Join(f.realm, team))
			if !os.SameFile(installed, linkLstat(t, guide)) {
				t.Fatal("refused link altered the installed symlink")
			}
			before := linkTreeDigest(t, clone)
			if err := Unlink(t.Context(), f.realm); err != nil {
				t.Fatalf("unlink could not unwind a partially installed stale review: %v", err)
			}
			assertRealmshareAbsent(t, guide)
			assertRealmshareAbsent(t, filepath.Join(f.realm, team))
			b := f.binding(t)
			if b.PendingLocal != nil || len(b.Links) != 0 || b.TrustedDigest != "" {
				t.Fatalf("recovery finalized or retained the stale approval: %+v", b)
			}
			if after := linkTreeDigest(t, clone); after != before {
				t.Fatal("unlink rewrote the changed shared checkout")
			}
		})
	}
}

func TestUnlinkRestartsBeforeFinalBindingWrite(t *testing.T) {
	for _, stage := range []string{"journaled", "link-parked", "original-restored", "identical-replacement"} {
		t.Run(stage, func(t *testing.T) {
			f, head := publishLinkWiki(t)
			b := f.binding(t)
			originalBackup := f.restorationBackup(t, b, "wiki")
			p := &pendingLocal{Unlink: true}
			for i, link := range b.Links {
				p.Changes = append(p.Changes, localChange{Token: "unlink-" + strconv.Itoa(i), Link: link})
			}
			b.PendingLocal = p
			if err := SaveBinding(f.realm, b); err != nil {
				t.Fatal(err)
			}
			wiki := filepath.Join(f.realm, "wiki")
			var parked string
			if stage != "journaled" {
				for _, change := range p.Changes {
					if change.Link.Path != "wiki" {
						continue
					}
					withPendingLinkDirs(t, f, change, func(l *localLinks, backups *os.Root, dir, parent *os.File) error {
						parked = filepath.Join(backups.Name(), change.Token+".park")
						if stage == "identical-replacement" {
							return l.removeLocalLink(t.Context(), backups, dir, parent, change)
						}
						r, _ := restorationFor(l.binding, "wiki")
						prepared, err := l.prepareUnlinkRestoration(t.Context(), backups, dir, change, r)
						if err != nil {
							return err
						}
						if stage == "link-parked" {
							return os.Rename(wiki, parked)
						}
						if err := l.parkLocalLink(backups, dir, parent, prepared); err != nil {
							return err
						}
						if err := os.Rename(filepath.Join(backups.Name(), prepared.RestoreStage), wiki); err != nil {
							return err
						}
						assertPendingSyncError(t, func(pipe *os.File) error {
							return l.removeLocalLink(t.Context(), backups, pipe, pipe, prepared)
						})
						return nil
					})
				}
			}
			if stage == "identical-replacement" {
				genuine := filepath.Join(f.realm, "genuine-wiki")
				if err := os.Rename(wiki, genuine); err != nil {
					t.Fatal(err)
				}
				root, err := os.OpenRoot(f.realm)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				if err := copyLocalOriginal(t.Context(), root, root, "genuine-wiki"); err != nil {
					t.Fatal(err)
				}
				if err := root.Rename("original", "wiki"); err != nil {
					t.Fatal(err)
				}
				if linkTreeDigest(t, wiki) != linkTreeDigest(t, genuine) || os.SameFile(linkLstat(t, wiki), linkLstat(t, genuine)) {
					t.Fatal("fixture must be byte-identical but a different filesystem entry")
				}
			}
			var occupied os.FileInfo
			if stage == "original-restored" || stage == "identical-replacement" {
				occupied = linkLstat(t, wiki)
			}
			err := Unlink(t.Context(), f.realm)
			current := f.binding(t)
			if occupied != nil && !os.SameFile(occupied, linkLstat(t, wiki)) {
				t.Fatal("restart replaced the existing wiki")
			}
			assertLinkWikiHistory(t, f, wiki, head)
			assertLinkWikiHistory(t, f, originalBackup, head)
			if stage == "identical-replacement" {
				r, ok := restorationFor(current, "wiki")
				if err == nil || !ok || r.Restored {
					t.Fatalf("an external copy gained restoration ownership: err=%v, receipt=%+v", err, r)
				}
				linkLstat(t, parked)
				f.assertLink(t, current, "guide.md", "# Guide\n")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, ok := restorationFor(current, "wiki")
			if current.PendingLocal != nil || len(current.Links) != 0 || !ok || !r.Restored {
				t.Fatalf("restarted unlink did not finalize the restored original: %+v", current)
			}
			f.assertOriginal(t, "guide.md", "", "# Guide\n")
		})
	}
}

func TestUnlinkRestartsAfterRenameBeforeSync(t *testing.T) {
	for _, stage := range []string{"unlink-parked", "cancel-parked", "cancel-restored"} {
		t.Run(stage, func(t *testing.T) {
			var f shareFixture
			if stage == "cancel-restored" {
				f, _ = publishLinkWiki(t)
				if err := Unlink(t.Context(), f.realm); err != nil {
					t.Fatal(err)
				}
			} else {
				f, _ = newLinkFixture(t)
				requireShareSymlinks(t)
			}
			guide := filepath.Join(f.realm, "guide.md")
			var original os.FileInfo
			if stage == "cancel-restored" {
				original = linkLstat(t, guide)
			}
			plan := previewRealmLink(t, f.realm, "guide.md")
			var p *pendingLocal
			if stage == "unlink-parked" {
				if err := ApplyLink(t.Context(), plan); err != nil {
					t.Fatal(err)
				}
				p = &pendingLocal{Unlink: true, Changes: []localChange{{Token: "unlink-0", Link: f.binding(t).Links[0]}}}
			} else {
				p = recordPendingLink(t, f, plan)
				change := p.Changes[0]
				withPendingLinkDirs(t, f, change, func(l *localLinks, backups *os.Root, dir, parent *os.File) error {
					return l.installLocalLink(t.Context(), backups, dir, parent, change)
				})
				p.Cancel = true
			}
			b := f.binding(t)
			b.PendingLocal = p
			if err := SaveBinding(f.realm, b); err != nil {
				t.Fatal(err)
			}
			change := p.Changes[0]
			var parked string
			withPendingLinkDirs(t, f, change, func(l *localLinks, backups *os.Root, dir, parent *os.File) error {
				parked = filepath.Join(backups.Name(), change.Token+".park")
				if err := os.Rename(guide, parked); err != nil {
					return err
				}
				if original != nil {
					if err := syncShareDirectories(parent, dir); err != nil {
						return err
					}
					if err := os.Rename(filepath.Join(backups.Name(), change.Token), guide); err != nil {
						return err
					}
				}
				assertPendingSyncError(t, func(pipe *os.File) error {
					if p.Cancel {
						return l.cancelLocalChange(t.Context(), backups, pipe, change)
					}
					return l.removeLocalLink(t.Context(), backups, pipe, pipe, change)
				})
				return nil
			})
			if err := Unlink(t.Context(), f.realm); err != nil {
				t.Fatal(err)
			}
			if original == nil {
				assertRealmshareAbsent(t, guide)
			} else {
				f.assertOriginal(t, "guide.md", "", "# Guide\n")
				if !os.SameFile(original, linkLstat(t, guide)) {
					t.Error("restart replaced the restored original")
				}
			}
			assertRealmshareAbsent(t, parked)
			current := f.binding(t)
			if current.PendingLocal != nil || len(current.Links) != 0 || current.TrustedDigest != b.TrustedDigest {
				t.Fatalf("restarted unlink did not clear the journal and ownership while preserving trust: %+v", current)
			}
		})
	}
}

func TestLocalRetryAfterFinalBindingRenameBeforeSync(t *testing.T) {
	for _, verb := range []string{"link", "unlink"} {
		t.Run(verb, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			requireShareSymlinks(t)
			ctx := t.Context()
			plan := previewRealmLink(t, f.realm, "guide.md")
			if verb == "unlink" {
				if err := ApplyLink(ctx, plan); err != nil {
					t.Fatal(err)
				}
			}
			b := f.binding(t)
			var pending *pendingLocal
			if verb == "link" {
				pending = recordPendingLink(t, f, plan)
			} else {
				pending = &pendingLocal{Unlink: true, Changes: []localChange{{Token: "unlink-0", Link: b.Links[0]}}}
				b.PendingLocal = pending
				if err := SaveBinding(f.realm, b); err != nil {
					t.Fatal(err)
				}
			}
			change := pending.Changes[0]
			withPendingLinkDirs(t, f, change, func(l *localLinks, backups *os.Root, dir, parent *os.File) error {
				if pending.Unlink {
					return l.removeLocalLink(ctx, backups, dir, parent, change)
				}
				return l.installLocalLink(ctx, backups, dir, parent, change)
			})
			b.PendingLocal = nil
			if pending.Unlink {
				b.Links = nil
			} else {
				b.Links = []LinkReceipt{change.Link}
				b.TrustedDigest = pending.Definition
			}
			loc, err := locateBinding(f.realm)
			if err != nil {
				t.Fatal(err)
			}
			staged, err := os.CreateTemp(loc.physicalState, "."+bindingName+".")
			if err != nil {
				t.Fatal(err)
			}
			err = toml.NewEncoder(staged).Encode(b)
			if err == nil {
				err = staged.Sync()
			}
			if err := errors.Join(err, staged.Close()); err != nil {
				t.Fatal(err)
			}
			bindingPath := filepath.Join(loc.physicalState, bindingName)
			// Stop the simulated writer before the directory sync, not before the rename.
			if err := os.Rename(staged.Name(), bindingPath); err != nil {
				t.Fatal(err)
			}
			before := linkLstat(t, bindingPath)
			raw, err := os.ReadFile(bindingPath)
			if err != nil {
				t.Fatal(err)
			}
			current := f.binding(t)
			if current.PendingLocal != nil || !slices.Equal(current.Links, b.Links) || current.TrustedDigest != b.TrustedDigest {
				t.Fatalf("retry did not start with the final binding: %+v", current)
			}
			guide := filepath.Join(f.realm, "guide.md")
			if pending.Unlink {
				assertRealmshareAbsent(t, guide)
				if err := Unlink(ctx, f.realm); err != nil {
					t.Fatal(err)
				}
				assertRealmshareAbsent(t, guide)
			} else {
				link := linkLstat(t, guide)
				retry := previewRealmLink(t, f.realm, "guide.md")
				if !retry.Selections[0].AlreadyLinked {
					t.Fatal("retry did not recognize the owned link")
				}
				if err := ApplyLink(ctx, retry); err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(link, linkLstat(t, guide)) {
					t.Error("retry replaced the owned symlink")
				}
				f.assertLink(t, b, "guide.md", "# Team guide\n")
			}
			after, err := os.ReadFile(bindingPath)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, linkLstat(t, bindingPath)) || !slices.Equal(raw, after) {
				t.Fatal("retry rewrote the finalized binding")
			}
		})
	}
}

func TestApplyLinkRejectsCrossDeviceSelectionBeforeJournaling(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	state, err := os.MkdirTemp("/dev/shm", "atomic-realm-link-")
	if err != nil {
		t.Skipf("no writable alternate filesystem at /dev/shm: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(state); err != nil {
			t.Error(err)
		}
	})
	probe, err := os.MkdirTemp(f.realm, "rename-probe-")
	if err != nil {
		t.Fatal(err)
	}
	switch err := os.Rename(probe, filepath.Join(state, filepath.Base(probe))); {
	case err == nil:
		t.Skip("alternate state shares the realm filesystem")
	case !errors.Is(err, syscall.EXDEV):
		t.Fatalf("could not establish cross-device layout: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	f.state = state
	t.Setenv(config.StateDirEnvVar, state)
	seedLinkFixture(t, f)
	beforeBinding := readRealmshareFile(t, filepath.Join(state, bindingName))
	plan, err := PreviewLink(t.Context(), f.realm, []string{"guide.md", "docs/notes"})
	if err == nil {
		err = ApplyLink(t.Context(), plan)
	}
	if err == nil {
		t.Fatal("accepted cross-device link selection")
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "device") && !strings.Contains(message, "filesystem") {
		t.Errorf("failure did not identify the filesystem boundary: %v", err)
	}
	if after := readRealmshareFile(t, filepath.Join(state, bindingName)); after != beforeBinding {
		t.Error("cross-device failure changed the binding or journaled the selection")
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, "guide.md"))
	assertRealmshareAbsent(t, filepath.Join(f.realm, "docs", "notes"))
}

func recordPendingLink(t *testing.T, f shareFixture, plan LinkPlan) *pendingLocal {
	t.Helper()
	p := &pendingLocal{Definition: plan.review.approval.Definition, Paths: plan.review.paths}
	for i, selection := range plan.review.approval.Selections {
		p.Behavior = append(p.Behavior, selection.Behavior)
		if selection.Owned.Path != "" {
			continue
		}
		m := selection.Mapping
		token := "link-" + strconv.Itoa(i)
		change := localChange{Token: token, Link: LinkReceipt{
			Path: m.Destination, Source: m.Source, Witness: filepath.ToSlash(filepath.Join(shareBackupDir, token+".link")),
		}}
		change.RestoreID = selection.Original.Identity
		p.Changes = append(p.Changes, change)
	}
	b := f.binding(t)
	b.PendingLocal = p
	if err := SaveBinding(f.realm, b); err != nil {
		t.Fatal(err)
	}
	return p
}

func withPendingLinkDirs(t *testing.T, f shareFixture, change localChange, step func(*localLinks, *os.Root, *os.File, *os.File) error) {
	t.Helper()
	loc, err := locateBinding(f.realm)
	if err != nil {
		t.Fatal(err)
	}
	l, err := openLocalLinks(loc)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close()
	backups, err := openShareBackups(l.state)
	if err != nil {
		t.Fatal(err)
	}
	defer backups.Close()
	dir, err := openShareDirectory(backups, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	parent, err := openShareDirectory(l.realm, filepath.ToSlash(filepath.Dir(change.Link.Path)))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := step(l, backups, dir, parent); err != nil {
		t.Fatal(err)
	}
}

func assertPendingSyncError(t *testing.T, resume func(*os.File) error) {
	t.Helper()
	t.Run("sync-error", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		defer writer.Close()
		syncErr := reader.Sync()
		if syncErr == nil {
			t.Skip("this platform does not report a sync error on a pipe")
		}
		var want *os.PathError
		if !errors.As(syncErr, &want) || want.Op != "sync" {
			t.Fatalf("unexpected pipe sync error: %v", syncErr)
		}
		err = resume(reader)
		var got *os.PathError
		if !errors.As(err, &got) || got.Op != want.Op || got.Path != want.Path || !errors.Is(got.Err, want.Err) {
			t.Fatalf("restart returned %v, want %v", err, syncErr)
		}
	})
}

func newLinkFixture(t *testing.T) (shareFixture, string) {
	t.Helper()
	f := newShareFixture(t)
	return f, seedLinkFixture(t, f)
}

func seedLinkFixture(t *testing.T, f shareFixture) string {
	t.Helper()
	seed := t.TempDir()
	const branch = "team"
	f.git(t, seed, "init", "--quiet", "--initial-branch="+branch)
	writeRealmshareFile(t, filepath.Join(seed, "shared", "guide.md"), "# Team guide\n")
	writeRealmshareFile(t, filepath.Join(seed, "shared", "notes", "index.md"), "# Team notes\n")
	f.write(t, "docs/local.md", "# Local notes\n")
	definition := "version = 1\nid = \"" + fixtureRealmID + "\"\n" +
		manifestMapping("shared/guide.md", "guide.md", string(KindFile)) +
		manifestMapping("shared/notes", "docs/notes", string(KindDirectory))
	writeRealmshareFile(t, filepath.Join(seed, manifestName), definition)
	f.git(t, seed, "add", "--all")
	f.git(t, seed, "commit", "--quiet", "-m", "publish shared realm fixture")
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:refs/heads/"+branch)
	binding := Binding{
		RealmID:   fixtureRealmID,
		ClonePath: filepath.Join(config.Dir(f.home), "realms", "team"),
		Remote:    f.remote,
		Branch:    branch,
	}
	if err := PrepareManagedClone(binding.ClonePath, func(staged string) error {
		f.git(t, "", "clone", "--quiet", "--branch", branch, "--", f.remote, staged)
		f.git(t, staged, "remote", "set-url", "origin", f.remote)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveBinding(f.realm, binding); err != nil {
		t.Fatal(err)
	}
	return seed
}

func receiveLinkBucket(t *testing.T, f shareFixture, seed string) {
	t.Helper()
	binding := f.binding(t)
	manifest := filepath.Join(seed, manifestName)
	definition := readRealmshareFile(t, manifest) + manifestMapping("sops", "sops", string(KindDirectory)) +
		"\n[[buckets]]\nname = \"sops\"\npath = \"sops\"\n"
	writeRealmshareFile(t, manifest, definition)
	writeRealmshareFile(t, filepath.Join(seed, "sops", "index.md"), "# Team procedures\n")
	f.git(t, seed, "add", "--", manifestName, "sops")
	f.git(t, seed, "commit", "--quiet", "-m", "add shared procedures bucket")
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:refs/heads/"+binding.Branch)
	f.git(t, binding.ClonePath, "fetch", "--quiet", "origin", binding.Branch)
	f.git(t, binding.ClonePath, "merge", "--quiet", "--ff-only", "FETCH_HEAD")
}

func publishLinkWiki(t *testing.T) (shareFixture, string) {
	t.Helper()
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Guide\n")
	f.write(t, "wiki/index.md", "# Committed wiki\n")
	wiki := filepath.Join(f.realm, "wiki")
	f.git(t, wiki, "init", "--quiet")
	f.git(t, wiki, "add", "--", "index.md")
	f.git(t, wiki, "commit", "--quiet", "-m", "original wiki history")
	head := f.git(t, wiki, "rev-parse", "HEAD")
	f.write(t, "wiki/index.md", "# Original draft\n")
	f.write(t, "wiki/untracked.md", "# Untracked wiki draft\n")
	f.apply(t, f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"guide.md", "wiki"}}))
	return f, head
}

func assertLinkWikiHistory(t *testing.T, f shareFixture, wiki, wantHead string) {
	t.Helper()
	if got := f.git(t, wiki, "rev-parse", "HEAD"); got != wantHead {
		t.Errorf("%s: wiki HEAD = %s, want %s", wiki, got, wantHead)
	}
	if got := f.git(t, wiki, "show", "HEAD:index.md"); got != "# Committed wiki" {
		t.Errorf("%s: committed wiki index = %q", wiki, got)
	}
	for name, want := range map[string]string{"index.md": "# Original draft\n", "untracked.md": "# Untracked wiki draft\n"} {
		if got := readRealmshareFile(t, filepath.Join(wiki, name)); got != want {
			t.Errorf("%s: %s = %q, want %q", wiki, name, got, want)
		}
	}
}

func previewRealmLink(t *testing.T, realm string, paths ...string) LinkPlan {
	t.Helper()
	plan, err := PreviewLink(t.Context(), realm, paths)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func (f shareFixture) linkState(t *testing.T, storage string) [3]string {
	t.Helper()
	return [3]string{linkTreeDigest(t, f.realm), linkTreeDigest(t, storage), linkTreeDigest(t, f.remote)}
}

func linkTreeDigest(t *testing.T, name string) string {
	t.Helper()
	root, err := os.OpenRoot(name)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	digest, err := digestSharePath(t.Context(), root, ".", nil)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func linkLstat(t *testing.T, name string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
