package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
)

func TestRealmCLIExpandingBuckets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		collision bool
	}{
		{name: "reviewed link"},
		{name: "private collision", collision: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			realmInstructionTestPATH(t)
			for _, key := range []string{"CODEX_HOME", "PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
				t.Setenv(key, "")
			}
			a, aHome, remote := newRealmShareCLIFixture(t)
			const (
				wikiIndex = "wiki/index.md"
				wikiText  = "# Team knowledge\n"
				bucket    = "sops"
				selected  = "sops/"
				runbook   = "sops/runbook.md"
				teamPage  = "# Shared runbook\n\nReview the weekly checklist.\n"
				private   = "sops/personal.md"
				localPage = "# Personal checklist\n"
				capture   = "capture"
				ref       = "refs/heads/" + shareBranch
			)
			writeSteeringTestFile(t, filepath.Join(a, wikiIndex), wikiText)
			writeSteeringTestFile(t, filepath.Join(a, sharedRealmInstructions), teamRealmGuidance)
			args := []string{"wiki/", sharedRealmInstructions, "--remote", remote, "--branch", shareBranch}
			review := previewRealmReview(t, aHome, "share", args...)
			mustRunRealmCLI(t, aHome, "share", append(args, "--yes", "--review", review)...)
			runGit(t, remote, "symbolic-ref", "HEAD", ref)
			base := runGit(t, remote, "rev-parse", ref)
			assertALinks := watchExpandingBucketPaths(t, a, true, "wiki", sharedRealmInstructions)
			mustRunWikiBucketCLI(t, aHome, a, "add", capture)
			captureRow := capture + "\t" + filepath.Join(a, capture)
			assertPrivateBuckets := watchExpandingBucketPaths(t, a, false, capture)
			if tc.collision {
				mustRunWikiBucketCLI(t, aHome, a, "add", bucket)
				writeSteeringTestFile(t, filepath.Join(a, private), localPage)
				assertPrivateBuckets = watchExpandingBucketPaths(t, a, false, capture, bucket)
			}

			b, bHome := t.TempDir(), t.TempDir()
			writeSteeringTestFile(t, config.RepoConfigPath(b), "scope = \"realm\"\n")
			t.Chdir(b)
			mustRunRealmCLI(t, bHome, "join", remote)
			review = previewRealmReview(t, bHome, "link", "wiki/", sharedRealmInstructions)
			mustRunRealmCLI(t, bHome, "link", "wiki/", sharedRealmInstructions, "--yes", "--review", review)
			assertBLinks := watchExpandingBucketPaths(t, b, true, "wiki", sharedRealmInstructions)
			bClone := realmCLIManagedClone(t, bHome)
			sharedWiki := readSteeringTestFile(t, filepath.Join(b, wikiIndex))
			mustRunWikiBucketCLI(t, bHome, b, "add", bucket)
			writeSteeringTestFile(t, filepath.Join(b, runbook), teamPage)
			assertBPrivate := watchExpandingBucketPaths(t, b, false, bucket)
			assertWikiBucketCLIList(t, bHome, b, bucket+"\t"+filepath.Join(b, bucket))

			preview := mustRunRealmCLI(t, bHome, "share", selected, "--dry-run")
			if !strings.Contains(preview, `Register bucket "sops" at "sops"`) {
				t.Errorf("share preview lacks portable bucket registration:\n%s", preview)
			}
			assertBPrivate()
			assertBLinks()
			assertRealmShareCLIFile(t, b, wikiIndex, sharedWiki, false)
			assertRealmShareCLIFile(t, b, sharedRealmInstructions, teamRealmGuidance, true)
			if head, dirty, remoteHead := runGit(t, bClone, "rev-parse", "HEAD"), runGit(t, bClone, "status", "--porcelain", "--untracked-files=all"), runGit(t, remote, "rev-parse", ref); head != base || dirty != "" || remoteHead != base {
				t.Fatalf("private add and share preview changed Git: clone HEAD %q, status %q, remote HEAD %q; want base %q and clean status", head, dirty, remoteHead, base)
			}
			mustRunRealmCLI(t, bHome, "share", selected, "--yes", "--review", realmReviewDigest(t, preview))
			watchExpandingBucketPaths(t, b, true, bucket)
			assertBLinks()
			published := runGit(t, remote, "rev-parse", ref)
			if published == base {
				t.Fatal("approved B share did not advance the existing remote")
			}

			t.Chdir(a)
			assertRealmCLISyncSummary(t, mustRunRealmCLI(t, aHome, "sync"), false, true, false)
			raw := mustRunRealmCLI(t, aHome, "status", "--json")
			var status realmshare.StatusReport
			if err := json.Unmarshal([]byte(raw), &status); err != nil || status.Head != strings.TrimSpace(published) {
				t.Fatalf("A status must report B's published HEAD %q: %v\n%s", strings.TrimSpace(published), err, raw)
			}
			state := "available"
			if tc.collision {
				state = "conflicting"
			}
			found := false
			for _, path := range status.Paths {
				if path.Path == bucket && path.Source != "" && path.State == state && !path.Drift {
					found = true
				}
			}
			if !found {
				t.Fatalf("A status does not report sops as %s:\n%s", state, raw)
			}
			if tc.collision {
				out, diag, code := runRealmCLI(t, aHome, "link", selected, "--dry-run")
				if code == 0 || !strings.Contains(diag, bucket) || !(strings.Contains(diag, "private bucket") || strings.Contains(diag, "occupied")) {
					t.Errorf("link did not reject the private sops directory: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
				}
				assertRealmShareCLIFile(t, a, private, localPage, false)
				assertRealmCLIPathAbsent(t, a, runbook)
				mustRunWikiBucketCLI(t, aHome, a, "index", bucket)
				if index := readSteeringTestFile(t, filepath.Join(a, bucket, "index.md")); !strings.Contains(index, "](personal.md)") || strings.Contains(index, "](runbook.md)") {
					t.Errorf("private index did not stay private:\n%s", index)
				}
			} else {
				assertRealmCLIPathAbsent(t, a, bucket)
				assertWikiBucketCLIList(t, aHome, a, captureRow)
				preview = mustRunRealmCLI(t, aHome, "link", selected, "--dry-run")
				if !strings.Contains(preview, `Shared bucket "sops" at "sops"`) {
					t.Errorf("link preview lacks received registration:\n%s", preview)
				}
				assertRealmCLIPathAbsent(t, a, bucket)
				mustRunRealmCLI(t, aHome, "link", selected, "--yes", "--review", realmReviewDigest(t, preview))
				watchExpandingBucketPaths(t, a, true, bucket)
				assertRealmShareCLIFile(t, a, runbook, teamPage, false)
			}
			assertWikiBucketCLIList(t, aHome, a, captureRow, bucket+"\t"+filepath.Join(a, bucket))
			assertALinks()
			assertPrivateBuckets()
			if head := runGit(t, remote, "rev-parse", ref); head != published {
				t.Errorf("A's receive and local bucket operations advanced the remote: %s -> %s", published, head)
			}
		})
	}
}

func watchExpandingBucketPaths(t *testing.T, root string, linked bool, names ...string) func() {
	t.Helper()
	before := make([]os.FileInfo, len(names))
	for i, name := range names {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || (info.Mode()&os.ModeSymlink != 0) != linked || (!linked && !info.IsDir()) {
			t.Fatalf("unexpected entry %q in %s (want symlink: %t): %v (%v)", name, root, linked, info, err)
		}
		before[i] = info
	}
	return func() {
		t.Helper()
		for i, name := range names {
			after, err := os.Lstat(filepath.Join(root, name))
			if err != nil || !os.SameFile(before[i], after) {
				t.Errorf("existing entry %q in %s was replaced: %v", name, root, err)
			}
		}
	}
}
