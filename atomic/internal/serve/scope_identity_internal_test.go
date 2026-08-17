package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T, dir, branch string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--initial-branch=" + branch},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// Serving a subdirectory must still name the repository. Naming the served
// directory instead reports "wiki" or "docs" as the project, which is what the
// header chip is specifically there not to do.
func TestResolveScopeIdentity_NamesRepoRootFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "my-project")
	sub := filepath.Join(repo, "docs", "wiki")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo, "trunk")

	got := resolveScopeIdentity(sub)

	if got.Name != "my-project" {
		t.Errorf("Name = %q, want the repo root's name %q", got.Name, "my-project")
	}
	if got.Branch != "trunk" {
		t.Errorf("Branch = %q, want %q", got.Branch, "trunk")
	}
}

// A directory name is a local accident — a worktree, or a clone into a
// renamed folder, misreports the project. owner/repo is what it is called
// everywhere else, so the remote wins when there is one.
func TestResolveScopeIdentity_PrefersRemoteOwnerRepo(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "some-local-folder-name")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo, "main")

	for _, remote := range []struct {
		url  string
		want string
	}{
		{"git@github.com:damusix/atomic-claude.git", "damusix/atomic-claude"},
		{"https://github.com/damusix/atomic-claude.git", "damusix/atomic-claude"},
		{"https://gitlab.com/group/sub/proj.git", "sub/proj"},
	} {
		cmd := exec.Command("git", "remote", "remove", "origin")
		cmd.Dir = repo
		_ = cmd.Run()

		cmd = exec.Command("git", "remote", "add", "origin", remote.url)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git remote add %s: %v: %s", remote.url, err, out)
		}

		if got := resolveScopeIdentity(repo).Name; got != remote.want {
			t.Errorf("remote %q → Name %q, want %q", remote.url, got, remote.want)
		}
	}
}

// A scope="repo" marker outranks the git root, so a directory can declare
// itself the project even when it sits inside a larger checkout.
func TestResolveScopeIdentity_ScopeMarkerOutranksGitRoot(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "outer-repo")
	inner := filepath.Join(repo, "packages", "inner")
	if err := os.MkdirAll(filepath.Join(inner, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo, "main")

	marker := filepath.Join(inner, ".claude", "atomic.toml")
	if err := os.WriteFile(marker, []byte("scope = \"repo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveScopeIdentity(inner)

	if got.Name != "inner" {
		t.Errorf("Name = %q, want the marker's directory %q", got.Name, "inner")
	}
}

// Not every served tree is a checkout — an unversioned folder still has a
// name, and the branch is simply absent rather than an error state.
func TestResolveScopeIdentity_NoGitRepoReportsNoBranch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "loose-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveScopeIdentity(dir)

	if got.Name != "loose-notes" {
		t.Errorf("Name = %q, want %q", got.Name, "loose-notes")
	}
	if got.Branch != "" {
		t.Errorf("Branch = %q, want empty for a non-repo", got.Branch)
	}
}
