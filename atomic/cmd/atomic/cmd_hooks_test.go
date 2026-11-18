package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// User scope writes no repository state, so an ambiguous state root must not
// refuse it; project scope still must.
func TestResolveHooksRootUserScopeIgnoresAmbiguousState(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "ref: refs/heads/main\n")
	cfgHome := t.TempDir()
	defer config.SetHomeDirForTest(cfgHome)()
	cfg := config.Default()
	if err := config.Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatal(err)
	}
	if err := config.WritePersist(config.TOMLPath(cfgHome), cfg); err != nil {
		t.Fatal(err)
	}
	for _, seg := range []string{".claude", ".atomic"} {
		if err := os.MkdirAll(filepath.Join(root, seg, "project"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)

	if _, err := resolveHooksRoot("project", ""); err == nil {
		t.Error("project scope resolved inside an ambiguous repository, want the refusal")
	}
	got, err := resolveHooksRoot("user", "")
	if err != nil {
		t.Fatalf("user scope refused: %v", err)
	}
	if got != root {
		t.Errorf("user scope root = %q, want %q", got, root)
	}
}

func TestSessionStartCwdPrecedence(t *testing.T) {
	stdin := strings.NewReader(`{"cwd":"/from/stdin","session_id":"s"}`)
	if got := sessionStartCwd("/from/flag", stdin, time.Second); got != "/from/flag" {
		t.Errorf("flag lost to stdin: got %q", got)
	}
	stdin = strings.NewReader(`{"cwd":"/from/stdin"}`)
	if got := sessionStartCwd("", stdin, time.Second); got != "/from/stdin" {
		t.Errorf("stdin cwd = %q, want /from/stdin", got)
	}
}

func TestSessionStartCwdIgnoresUnusableStdin(t *testing.T) {
	for _, in := range []string{"", "not json", `{"cwd":5}`, `{}`} {
		if got := sessionStartCwd("", strings.NewReader(in), time.Second); got != "" {
			t.Errorf("stdin %q gave %q, want empty", in, got)
		}
	}
}

func TestSessionStartCwdDoesNotBlockOnOpenStdin(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	done := make(chan string, 1)
	go func() { done <- sessionStartCwd("", pr, 50*time.Millisecond) }()
	select {
	case got := <-done:
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sessionStartCwd blocked on a stdin that never closes")
	}
}

func sessionStartFixture(t *testing.T) (home, repo, sub string) {
	t.Helper()
	home = t.TempDir()
	repo = t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	sub = filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, repo, sub
}

func TestResolveSessionRootWalksUpFromSubdirectory(t *testing.T) {
	t.Run("scope marker", func(t *testing.T) {
		repo := t.TempDir()
		marker := config.RepoConfigPath(repo)
		if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("scope = \"repo\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(repo, "sub", "dir")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := resolveSessionRoot(sub, nil, "")
		if err != nil || got != repo {
			t.Errorf("root = %q, %v; want marker root %q", got, err, repo)
		}
	})
	t.Run("git", func(t *testing.T) {
		repo := t.TempDir()
		if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		sub := filepath.Join(repo, "sub", "dir")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(repo)
		if err != nil {
			t.Fatal(err)
		}
		got, err := resolveSessionRoot(sub, nil, "")
		if err != nil || got != want {
			t.Errorf("root = %q, %v; want git root %q", got, err, want)
		}
	})
}

func TestPipedStdinSkipsTerminalsAndDevices(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if pipedStdin(null) != nil {
		t.Error("a character device (terminal or /dev/null) must not be read")
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	if pipedStdin(pr) == nil {
		t.Error("a pipe must be read")
	}
}

func TestSessionStartCwdNilStdin(t *testing.T) {
	if got := sessionStartCwd("", nil, time.Second); got != "" {
		t.Errorf("nil stdin gave %q, want empty", got)
	}
}

func TestSessionStartUnflaggedMatchesClaude(t *testing.T) {
	home, repo, _ := sessionStartFixture(t)
	bare, bareCode := runAtomicCLIEnv(t, home, nil, "root", "hooks", "session-start", "--repo", repo)
	flagged, flaggedCode := runAtomicCLIEnv(t, home, nil, "root", "hooks", "session-start", "--harness", "claude", "--repo", repo)
	if bareCode != 0 || flaggedCode != 0 || bare == "" || bare != flagged {
		t.Errorf("unflagged (%d) %q != --harness claude (%d) %q", bareCode, bare, flaggedCode, flagged)
	}
}

// OMP reads each card through a committed link, so a session start has no
// card copy to refresh and writes none, whatever state the card surface is in.
func TestSessionStartWritesNoCardFile(t *testing.T) {
	repo := cardStateRepo(t)
	home := enrollTargets(t, "omp")
	for _, harnessKind := range []string{"claude", "omp", "codex"} {
		if _, diag, code := runAtomicCLIFull(t, home, gitOnlyPath(t), "root", "hooks", "session-start", "--format", "text", "--harness", harnessKind, "--cwd", repo); code != 0 {
			t.Fatalf("session-start --harness %s exited %d:\n%s", harnessKind, code, diag)
		}
	}
	if pathExists(filepath.Join(repo, ".omp")) {
		t.Error("a session start wrote under .omp/")
	}
}

// gitOnlyPath lets the child resolve the repository through Git without
// reaching any other tool on the developer's PATH.
func gitOnlyPath(t *testing.T) []string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	return []string{"PATH=" + filepath.Dir(git)}
}

func TestSessionStartRejectsUnknownHarness(t *testing.T) {
	home, repo, _ := sessionStartFixture(t)
	if _, code := runAtomicCLIEnv(t, home, nil, "root", "hooks", "session-start", "--harness", "bogus", "--repo", repo); code != 2 {
		t.Errorf("--harness bogus exit %d, want 2", code)
	}
}
