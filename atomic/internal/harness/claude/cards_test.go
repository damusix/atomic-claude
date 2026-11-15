package claude

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const cardBody = "---\npaths:\n  - \"src/**\"\n---\n\nDomain: core.\n"

// cardRepo is a Git repository whose canonical card lives at
// <stateDir>/rules/wiki/core.md, with Git reading no user configuration.
func cardRepo(t *testing.T, stateDir string) (repo, stateRoot string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	stateRoot = filepath.Join(repo, stateDir)
	writeTestFile(t, filepath.Join(stateRoot, "rules", "wiki", "core.md"), cardBody)
	return repo, stateRoot
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func linkRequest(home, repo, stateRoot string) CardLinkRequest {
	return CardLinkRequest{Home: home, RepoRoot: repo, StateRoot: stateRoot, ProjectKey: "repo-key", Targets: []string{"claude:default"}}
}

func TestEnsureCardLinkServesTheCards(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	home := t.TempDir()

	result, err := EnsureCardLink(linkRequest(home, repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	link := filepath.Join(repo, ".claude", "rules", "atomic")
	if got, err := os.Readlink(link); err != nil || got != "../../.atomic/rules" {
		t.Fatalf("link target = %q, %v; want ../../.atomic/rules", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(link, "wiki", "core.md")); err != nil || string(got) != cardBody {
		t.Errorf("card through the link = %q, %v", got, err)
	}
	if result.Action != CardLinkCreated {
		t.Errorf("action = %q, want created", result.Action)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find("claude:default", link)
	if !ok || row.Applied.Kind != managedfile.KindSymlink || row.Applied.Digest != managedfile.Digest([]byte("../../.atomic/rules")) {
		t.Errorf("ledger row = %+v, %v; want the link owned by its target", row, ok)
	}
}

// A custom state root nested deeper still gets a target relative to the link.
func TestEnsureCardLinkTargetIsRelativeToTheLink(t *testing.T) {
	repo, stateRoot := cardRepo(t, "tools/state")
	if _, err := EnsureCardLink(linkRequest(t.TempDir(), repo, stateRoot)); err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if got, _ := os.Readlink(filepath.Join(repo, ".claude", "rules", "atomic")); got != "../../tools/state/rules" {
		t.Errorf("link target = %q, want ../../tools/state/rules", got)
	}
}

func TestEnsureCardLinkWritesNothingForTheClaudeRoot(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".claude")
	result, err := EnsureCardLink(linkRequest(t.TempDir(), repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Action != CardLinkNone {
		t.Errorf("action = %q, want none", result.Action)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "rules", "atomic")); !os.IsNotExist(err) {
		t.Errorf("a link was written under a .claude state root (lstat err = %v)", err)
	}
}

// A teammate's clone carries the committed link with no ledger row.
func TestEnsureCardLinkAdoptsTheExpectedLink(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	home := t.TempDir()
	link := filepath.Join(repo, ".claude", "rules", "atomic")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.atomic/rules", link); err != nil {
		t.Fatal(err)
	}
	result, err := EnsureCardLink(linkRequest(home, repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Action != CardLinkAdopted {
		t.Errorf("action = %q, want adopted", result.Action)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find("claude:default", link); !ok {
		t.Error("the adopted link has no ledger row")
	}
}

func TestEnsureCardLinkRefusesAForeignEntry(t *testing.T) {
	for name, seed := range map[string]func(string) error{
		"other link": func(p string) error { return os.Symlink("../../elsewhere", p) },
		"real dir":   func(p string) error { return os.Mkdir(p, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			repo, stateRoot := cardRepo(t, ".atomic")
			link := filepath.Join(repo, ".claude", "rules", "atomic")
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := seed(link); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Lstat(link)
			_, err := EnsureCardLink(linkRequest(t.TempDir(), repo, stateRoot))
			if !errors.Is(err, ErrCardLinkConflict) || !strings.Contains(err.Error(), link) {
				t.Fatalf("error = %v, want a conflict naming %s", err, link)
			}
			after, _ := os.Lstat(link)
			if before.Mode() != after.Mode() {
				t.Error("the foreign entry was replaced")
			}
		})
	}
}

// An ignore file inside the repository that hides the link gets a negation,
// and the migration's staging adds both to the index.
func TestEnsureCardLinkNegatesARepositoryIgnore(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), ".claude/rules/atomic\n")
	req := linkRequest(t.TempDir(), repo, stateRoot)
	req.Stage = true

	result, err := EnsureCardLink(req)
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Negated != filepath.Join(repo, ".gitignore") {
		t.Errorf("negated = %q, want the root .gitignore", result.Negated)
	}
	if err := exec.Command("git", "-C", repo, "check-ignore", "-q", ".claude/rules/atomic").Run(); err == nil {
		t.Error("the link is still ignored")
	}
	staged := runGit(t, repo, "diff", "--cached", "--name-status")
	if !strings.Contains(staged, "A\t.claude/rules/atomic") || !strings.Contains(staged, "A\t.gitignore") {
		t.Errorf("staged =\n%s\nwant the link and the amended ignore file", staged)
	}
}

// A match only in a global excludes file is reported, never edited, and
// nothing is staged or force-added.
func TestEnsureCardLinkReportsAGlobalExclude(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	excludes := filepath.Join(t.TempDir(), "ignore")
	writeTestFile(t, excludes, "atomic\n")
	writeTestFile(t, os.Getenv("GIT_CONFIG_GLOBAL"), "[core]\n\texcludesFile = "+excludes+"\n")
	req := linkRequest(t.TempDir(), repo, stateRoot)
	req.Stage = true

	result, err := EnsureCardLink(req)
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.IgnoredBy != excludes {
		t.Errorf("ignored by = %q, want %s", result.IgnoredBy, excludes)
	}
	if got, _ := os.ReadFile(excludes); string(got) != "atomic\n" {
		t.Errorf("the global excludes file changed: %q", got)
	}
	if staged := runGit(t, repo, "diff", "--cached", "--name-status"); staged != "" {
		t.Errorf("staged =\n%s\nwant nothing", staged)
	}
}

// A state root outside the repository cannot be linked to with a committed
// relative path, so Claude reads an ignored generated copy instead.
func TestEnsureCardLinkCopiesAnOutsideStateRoot(t *testing.T) {
	repo, _ := cardRepo(t, ".atomic")
	outside := filepath.Join(t.TempDir(), "state")
	writeTestFile(t, filepath.Join(outside, "rules", "wiki", "core.md"), cardBody)

	result, err := EnsureCardLink(linkRequest(t.TempDir(), repo, outside))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	copied := filepath.Join(repo, ".claude", "rules", "atomic", "wiki", "core.md")
	if fi, err := os.Lstat(filepath.Dir(filepath.Dir(copied))); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copy root = %v, %v; want a real directory", fi, err)
	}
	if got, err := os.ReadFile(copied); err != nil || string(got) != cardBody {
		t.Errorf("copied card = %q, %v", got, err)
	}
	if result.Action != CardLinkCopied {
		t.Errorf("action = %q, want copied", result.Action)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, ".claude", ".gitignore")); string(got) != "/rules/atomic/\n" {
		t.Errorf(".claude/.gitignore = %q, want /rules/atomic/", got)
	}
}

// A card tree an earlier version projected at .claude/rules/atomic-wiki/ goes
// once its ledger row proves Atomic wrote it.
func TestEnsureCardLinkRetiresTheLegacyTree(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	home := t.TempDir()
	legacy := filepath.Join(repo, ".claude", "rules", "atomic-wiki")
	writeTestFile(t, filepath.Join(legacy, "core.md"), "old\n")
	digest, _, err := managedfile.TreeDigest(legacy)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(installstate.Row{Target: "claude:default", Resource: legacy,
		Applied: installstate.AppliedValue{Path: legacy, Kind: managedfile.KindTree, Digest: digest}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	result, err := EnsureCardLink(linkRequest(home, repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("the legacy tree survived (stat err = %v)", err)
	}
	if len(result.Retired) != 1 || result.Retired[0] != legacy {
		t.Errorf("retired = %v, want %s", result.Retired, legacy)
	}
}
