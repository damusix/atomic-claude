package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

const wantRealmSyncReminder = "Shared realm changes are pending. Run `atomic realm sync` to publish them."

func TestRealmSyncReminderStartup(t *testing.T) {
	setRealmStartupPath(t, "")
	root, home, remote := newRealmShareCLIFixture(t)
	const bucket = "sops/"
	writeSteeringTestFile(t, filepath.Join(root, bucket, "review.md"), "# Review\n")
	clone := shareReminderFixture(t, home, remote, bucket)
	remoteHead := runGit(t, remote, "rev-parse", "refs/heads/"+shareBranch)
	native := func(t *testing.T, dir, kind, source string, flags ...string) string {
		t.Helper()
		out, _ := runReminderStartup(t, home, kind, realmStartupInputForSource(t, dir, source), flags...)
		return readReminderSystemMessage(t, out)
	}
	assertNotice := func(t *testing.T, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("startup systemMessage = %q, want %q", got, want)
		}
	}

	assertNotice(t, native(t, root, "claude", "startup"), "")
	writeSteeringTestFile(t, filepath.Join(clone, "unrelated.md"), "Not a selection\n")
	assertNotice(t, native(t, root, "claude", "startup"), "")
	if err := os.Remove(filepath.Join(clone, "unrelated.md")); err != nil {
		t.Fatal(err)
	}

	writeSteeringTestFile(t, filepath.Join(root, bucket, "pending.md"), "# Unpublished\n")
	writeSteeringTestFile(t, filepath.Join(clone, "unrelated.md"), "Unexpected staged path\n")
	runGit(t, clone, "add", "--", "unrelated.md")
	assertNotice(t, native(t, root, "claude", "startup"), "")
	runGit(t, clone, "restore", "--staged", "--", "unrelated.md")
	if err := os.Remove(filepath.Join(clone, "unrelated.md")); err != nil {
		t.Fatal(err)
	}
	assertNotice(t, native(t, root, "claude", "resume"), "")
	assertNotice(t, native(t, root, "codex", "resume"), "")
	subagent := strings.TrimSuffix(realmStartupInput(t, root), "}") + `,"agent_id":"worker-id"}`
	subagentOut, _ := runReminderStartup(t, home, "claude", subagent)
	assertNotice(t, readReminderSystemMessage(t, subagentOut), "")
	member := filepath.Join(root, "member")
	if err := os.Mkdir(member, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, member, "init")
	assertNotice(t, native(t, member, "claude", "startup"), "")
	out, diag := runReminderStartup(t, home, "omp", "", "--format", "text", "--cwd", root)
	if strings.Contains(out+diag, wantRealmSyncReminder) {
		t.Fatal("non-primary OMP startup received a reminder")
	}

	unchanged := unchangedRealmCLIStartupWithReminder(t, root, home, remote)
	assertNotice(t, native(t, root, "claude", "startup"), wantRealmSyncReminder)
	unchanged(t)
	assertNotice(t, native(t, root, "claude", "startup", "--realm-instructions"), "")
	assertNotice(t, native(t, root, "codex", "startup"), "")
	out, diag = runReminderStartup(t, home, "omp", "", "--format", "text", "--cwd", root, "--realm-primary")
	if strings.Contains(out+diag, wantRealmSyncReminder) {
		t.Fatal("OMP repeated a reminder emitted by Claude")
	}

	if got := runGit(t, clone, "rev-parse", "HEAD"); got != remoteHead {
		t.Errorf("startup committed %q; local HEAD was %q", got, remoteHead)
	}
	if got := runGit(t, remote, "rev-parse", "refs/heads/"+shareBranch); got != remoteHead {
		t.Errorf("startup published %q; remote was %q", got, remoteHead)
	}
}

func TestRealmSyncReminderAheadStartup(t *testing.T) {
	setRealmStartupPath(t, "")
	root, home, remote := newRealmShareCLIFixture(t)
	clone := shareReminderFixture(t, home, remote, shareSelected)
	remoteHead := runGit(t, remote, "rev-parse", "refs/heads/"+shareBranch)
	writeSteeringTestFile(t, filepath.Join(clone, shareSelected), "# Unpublished\n")
	runGit(t, clone, "add", "--", shareSelected)
	runGit(t, clone, "commit", "-m", "Local unpublished commit")
	if got := runGit(t, clone, "status", "--porcelain"); got != "" {
		t.Fatalf("ahead fixture is not clean: %s", got)
	}
	out, _ := runReminderStartup(t, home, "codex", realmStartupInput(t, root))
	if got := readReminderSystemMessage(t, out); got != wantRealmSyncReminder {
		t.Errorf("Codex startup systemMessage = %q, want %q", got, wantRealmSyncReminder)
	}
	if got := runGit(t, remote, "rev-parse", "refs/heads/"+shareBranch); got != remoteHead {
		t.Errorf("startup published %q; remote was %q", got, remoteHead)
	}
}

func TestRealmSyncReminderOMPPrimaryStartup(t *testing.T) {
	setRealmStartupPath(t, "")
	root, home, remote := newRealmShareCLIFixture(t)
	clone := shareReminderFixture(t, home, remote, shareSelected)
	writeSteeringTestFile(t, filepath.Join(clone, shareSelected), "# Staged\n")
	runGit(t, clone, "add", "--", shareSelected)

	out, diag := runReminderStartup(t, home, "omp", "", "--format", "text", "--cwd", root, "--realm-primary")
	if strings.Contains(out, wantRealmSyncReminder) || !strings.Contains(out, "## Workflow preferences") {
		t.Errorf("OMP text context = %q", out)
	}
	if !strings.Contains("\n"+diag, "\natomic-realm-sync-reminder: "+wantRealmSyncReminder+"\n") {
		t.Errorf("OMP did not receive a separate startup notice: %q", diag)
	}
	out, _ = runReminderStartup(t, home, "claude", realmStartupInput(t, root), "--realm-instructions")
	if got := readReminderSystemMessage(t, out); got != "" {
		t.Errorf("realm-local Claude hook repeated OMP reminder: %q", got)
	}
}

func unchangedRealmCLIStartupWithReminder(t *testing.T, root string, otherRoots ...string) func(*testing.T) {
	t.Helper()
	reminders := filepath.Join(filepath.Dir(config.RepoConfigPath(root)), "realm-sync-reminders")
	if err := os.Mkdir(reminders, 0o700); err != nil {
		t.Fatal(err)
	}
	before := snapshotRealmCLITree(t, root)
	delete(before, reminders)
	unchanged := unchangedRealmCLITrees(t, otherRoots...)
	dayBefore := time.Now().Format(time.DateOnly)
	return func(t *testing.T) {
		t.Helper()
		dayAfter := time.Now().Format(time.DateOnly)
		entries, err := os.ReadDir(reminders)
		if err != nil || len(entries) != 1 || (entries[0].Name() != dayBefore && entries[0].Name() != dayAfter) {
			t.Fatalf("expected one daily reminder marker in %s, got %v: %v", reminders, entries, err)
		}
		after := snapshotRealmCLITree(t, root)
		delete(after, reminders)
		delete(after, filepath.Join(reminders, entries[0].Name()))
		for path, entry := range after {
			if old, ok := before[path]; !ok || old != entry {
				t.Errorf("startup created or changed %s", path)
			}
		}
		if len(after) != len(before) {
			t.Errorf("realm tree entries: got %d, want %d", len(after), len(before))
		}
		unchanged(t)
	}
}

func shareReminderFixture(t *testing.T, home, remote, selection string) string {
	t.Helper()
	args := []string{selection, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, home, "share", args...)
	mustRunRealmCLI(t, home, "share", append(args, "--yes", "--review", review)...)
	return realmCLIManagedClone(t, home)
}

func runReminderStartup(t *testing.T, home, kind, input string, flags ...string) (string, string) {
	t.Helper()
	inPath := os.DevNull
	if input != "" {
		inPath = filepath.Join(t.TempDir(), "stdin.json")
		writeSteeringTestFile(t, inPath, input)
	}
	args := append([]string{"root", "hooks", "session-start", "--harness", kind}, flags...)
	env := []string{"PATH=" + os.Getenv("PATH"), "ATOMIC_TEST_CLI_IN=" + inPath, "CLAUDE_PROJECT_DIR="}
	out, diag, code := runAtomicCLIFull(t, home, env, args...)
	if code != 0 {
		t.Fatalf("%s SessionStart: exit %d\nstdout: %s\nstderr: %s", kind, code, out, diag)
	}
	return out, diag
}

func readReminderSystemMessage(t *testing.T, out string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	var payload struct {
		Message string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid SessionStart output: %v\n%s", err, out)
	}
	return payload.Message
}
