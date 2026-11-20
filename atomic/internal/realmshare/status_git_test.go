package realmshare

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestInspectStatusPreservesSplitIndexMtime(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const staged, unstaged, untracked = "shared/guide.md", "shared/notes/index.md", "shared/notes/draft notes.md"
	writeRealmshareFile(t, filepath.Join(clone, staged), "# Staged guide\n")
	f.git(t, clone, "add", "--", staged)
	f.git(t, clone, "update-index", "--split-index")
	shared := filepath.FromSlash(f.git(t, clone, "rev-parse", "--shared-index-path"))
	if shared == "" {
		t.Fatal("Git did not create a shared index")
	}
	if !filepath.IsAbs(shared) {
		shared = filepath.Join(clone, shared)
	}
	writeRealmshareFile(t, filepath.Join(clone, unstaged), "# Worktree notes\n")
	writeRealmshareFile(t, filepath.Join(clone, untracked), "# Draft\n")

	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(shared, old, old); err != nil {
		t.Fatal(err)
	}
	before := linkLstat(t, shared).ModTime()
	report := inspectUnchangedStatus(t, f)
	if after := linkLstat(t, shared).ModTime(); !after.Equal(before) {
		t.Errorf("status changed shared index mtime: before=%v after=%v", before, after)
	}

	want := []StatusChange{
		{Path: staged, Index: "M"},
		{Path: unstaged, Worktree: "M"},
		{Path: untracked, Worktree: "?"},
	}
	if len(report.Changes) != len(want) {
		t.Fatalf("changes = %+v; want exactly %+v", report.Changes, want)
	}
	for _, change := range want {
		if !slices.Contains(report.Changes, change) {
			t.Errorf("changes = %+v; missing %+v", report.Changes, change)
		}
	}
}

func TestInspectStatusCountsCachedDivergenceOnlyWithCompleteAncestry(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	base := publishJoinFixture(t, publisher)
	publisher.git(t, publisher.realm, "commit", "--quiet", "--allow-empty", "-m", "Advance published branch")
	published := publisher.git(t, publisher.realm, "rev-parse", "HEAD")
	publisher.git(t, publisher.realm, "push", "--quiet", publisher.remote, "HEAD:refs/heads/"+joinFixtureBranch)
	f := newJoinReceiver(t, publisher.remote)
	if _, err := Join(t.Context(), f.realm, f.remote); err != nil {
		t.Fatal(err)
	}
	clone := f.binding(t).ClonePath

	const alternate = "refs/heads/status-alternate"
	publisher.git(t, publisher.realm, "checkout", "--quiet", "--detach", base)
	publisher.git(t, publisher.realm, "commit", "--quiet", "--allow-empty", "-m", "Alternate parent")
	publisher.git(t, publisher.realm, "commit", "--quiet", "--allow-empty", "-m", "Alternate tip")
	local := publisher.git(t, publisher.realm, "rev-parse", "HEAD")
	publisher.git(t, publisher.realm, "push", "--quiet", publisher.remote, "HEAD:"+alternate)
	f.git(t, clone, "fetch", "--quiet", "--depth=1", "origin", alternate)
	f.git(t, clone, "reset", "--quiet", "--hard", "FETCH_HEAD")
	if got := f.git(t, clone, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("shallow fixture: rev-parse = %q; want true", got)
	}
	if got := f.git(t, clone, "rev-list", "--left-right", "--count", local+"..."+published); got != "1\t2" {
		t.Fatalf("shallow fixture: rev-list = %q; want the truncated counts 1 and 2", got)
	}

	t.Run("shallow", func(t *testing.T) {
		report := inspectUnchangedStatus(t, f)
		cache := report.CachedRemote
		if report.Head != local || cache == nil || cache.Head != published || cache.DivergenceKnown || cache.Ahead != 0 || cache.Behind != 0 {
			t.Errorf("head=%q cache=%+v; want head %s and cached %s with unknown divergence", report.Head, cache, local, published)
		}
	})
	t.Run("complete", func(t *testing.T) {
		f.git(t, clone, "fetch", "--quiet", "--unshallow", "origin", alternate)
		report := inspectUnchangedStatus(t, f)
		cache := report.CachedRemote
		if report.Head != local || cache == nil || cache.Head != published || !cache.DivergenceKnown || cache.Ahead != 2 || cache.Behind != 1 {
			t.Errorf("head=%q cache=%+v; want head %s, cached %s, two ahead and one behind", report.Head, cache, local, published)
		}
	})
}

func TestInspectStatusRefusesTempInsideLiveStorage(t *testing.T) {
	f := newShareFixture(t)
	stateParent := t.TempDir()
	f.state = filepath.Join(stateParent, "caf\u00e9")
	if err := os.Mkdir(f.state, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.StateDirEnvVar, f.state)
	seedLinkFixture(t, f)
	cloneDir := filepath.Join(f.binding(t).ClonePath, "shared", "notes")

	t.Run("outside", func(t *testing.T) {
		inspectUnchangedStatus(t, f)
	})
	for _, tc := range []struct {
		name, parent string
		symlink      bool
		unicodeAlias bool
		caseSibling  bool
	}{
		{name: "clone descendant", parent: cloneDir},
		{name: "realm root", parent: f.realm},
		{name: "external state root", parent: f.state},
		{name: "symlink to clone descendant", parent: cloneDir, symlink: true},
		{name: "Unicode alias of state root", parent: f.state, unicodeAlias: true},
		{name: "distinct case-only state sibling", parent: f.state, caseSibling: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			temp := tc.parent
			switch {
			case tc.symlink:
				temp = filepath.Join(t.TempDir(), "temp-alias")
				symlinkRealmshare(t, tc.parent, temp)
			case tc.unicodeAlias:
				temp = filepath.Join(stateParent, "cafe\u0301")
				alias, err := os.Stat(temp)
				if os.IsNotExist(err) {
					t.Skip("filesystem does not resolve Unicode normalization aliases")
				}
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(linkLstat(t, f.state), alias) {
					t.Fatal("Unicode alias does not identify the state directory")
				}
			case tc.caseSibling:
				temp = filepath.Join(stateParent, "Caf\u00e9")
				if err := os.Mkdir(temp, 0o700); err != nil {
					if os.IsExist(err) && os.SameFile(linkLstat(t, f.state), linkLstat(t, temp)) {
						t.Skip("filesystem aliases case-only directory names")
					}
					t.Fatal(err)
				}
				if os.SameFile(linkLstat(t, f.state), linkLstat(t, temp)) {
					t.Fatal("case-only sibling is not a distinct directory")
				}
			}
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, temp)
			}

			old := time.Unix(1_000_000_000, 0)
			if err := os.Chtimes(tc.parent, old, old); err != nil {
				t.Fatal(err)
			}
			before := f.linkState(t, f.home)
			stateBefore := linkTreeDigest(t, f.state)
			mtimeBefore := linkLstat(t, tc.parent).ModTime()
			_, err := InspectStatus(t.Context(), f.realm)
			if after := f.linkState(t, f.home); after != before {
				t.Errorf("status changed realm, home/clone, or remote contents: before=%v after=%v", before, after)
			}
			if after := linkTreeDigest(t, f.state); after != stateBefore {
				t.Errorf("status changed state contents: before=%s after=%s", stateBefore, after)
			}
			if after := linkLstat(t, tc.parent).ModTime(); !after.Equal(mtimeBefore) {
				t.Errorf("status touched protected directory %q: mtime before=%v after=%v", tc.parent, mtimeBefore, after)
			}
			if tc.caseSibling {
				if err != nil {
					t.Fatalf("status refused a temporary directory outside live storage: %v", err)
				}
				entries, err := os.ReadDir(temp)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Errorf("status left scratch entries in %q: %v", temp, entries)
				}
				return
			}
			if err == nil {
				t.Fatal("status accepted a temporary directory inside live storage")
			}
			detail := strings.ToLower(err.Error())
			if !strings.Contains(detail, "outside") || (!strings.Contains(detail, "temp") && !strings.Contains(detail, "tmpdir")) {
				t.Errorf("status error = %q; want guidance to use a temporary directory outside live storage", err)
			}
		})
	}
}

func TestInspectStatusUsesLiveGitConfig(t *testing.T) {
	for _, scope := range []string{"gitdir", "onbranch", "worktree"} {
		t.Run(scope, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			binding := f.binding(t)
			clone := binding.ClonePath
			const staged, modeOnly = "shared/guide.md", "shared/notes/index.md"
			const visible, ignored, infoIgnored = "shared/notes/draft.md", "shared/notes/config-ignored.md", "shared/notes/info-ignored.md"

			excludes := filepath.Join(f.home, "status-excludes")
			writeRealmshareFile(t, excludes, "/"+ignored+"\n")
			fallback := filepath.Join(f.home, "fallback-excludes")
			writeRealmshareFile(t, fallback, "/"+visible+"\n")
			f.git(t, clone, "config", "--local", "core.excludesFile", fallback)
			writeRealmshareFile(t, filepath.Join(clone, ".git", "info", "exclude"), "/"+infoIgnored+"\n")

			include := filepath.Join(f.home, "status.gitconfig")
			configArgs := []string{"config", "--file", include}
			switch scope {
			case "gitdir":
				f.git(t, clone, "config", "--local", "includeIf.gitdir:**/realms/team/.git.path", include)
			case "onbranch":
				f.git(t, clone, "config", "--local", "includeIf.onbranch:"+binding.Branch+".path", include)
			case "worktree":
				f.git(t, clone, "config", "--local", "extensions.worktreeConfig", "true")
				configArgs = []string{"config", "--worktree"}
			}
			f.git(t, clone, append(configArgs, "core.excludesFile", excludes)...)

			writeRealmshareFile(t, filepath.Join(clone, staged), "# Staged guide\n")
			f.git(t, clone, "add", "--", staged)
			for _, path := range []string{visible, ignored, infoIgnored} {
				writeRealmshareFile(t, filepath.Join(clone, path), "# Draft\n")
			}
			wantGit := "M  " + staged + "\x00"
			want := []StatusChange{{Path: staged, Index: "M"}}
			if f.git(t, clone, "config", "--bool", "--get", "core.fileMode") == "true" {
				f.git(t, clone, "config", "--local", "core.fileMode", "false")
				f.git(t, clone, append(configArgs, "core.fileMode", "true")...)
				if err := os.Chmod(filepath.Join(clone, modeOnly), 0o755); err != nil {
					t.Fatal(err)
				}
				wantGit += " M " + modeOnly + "\x00"
				want = append(want, StatusChange{Path: modeOnly, Worktree: "M"})
			}
			wantGit += "?? " + visible + "\x00"
			want = append(want, StatusChange{Path: visible, Worktree: "?"})
			if got := f.git(t, clone, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--ignore-submodules=all"); got != wantGit {
				t.Fatalf("live Git fixture status = %q; want %q", got, wantGit)
			}

			report := inspectUnchangedStatus(t, f)
			if len(report.Changes) != len(want) {
				t.Fatalf("changes = %+v; want exactly %+v", report.Changes, want)
			}
			for _, change := range want {
				if !slices.Contains(report.Changes, change) {
					t.Errorf("changes = %+v; missing %+v", report.Changes, change)
				}
			}
		})
	}
}
