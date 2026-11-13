package omp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

const linkTarget = "../../.atomic/rules/wiki/core.md"

// linkRepo is a Git repository whose state root .atomic holds the given domains.
func linkRepo(t *testing.T, domains ...string) (base, stateRoot string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	stateRoot = filepath.Join(dir, ".atomic")
	var cards []wiki.PointerCard
	for _, d := range domains {
		cards = append(cards, pointerCard(d, []string{"src/**"}, "# "+d+" card\n"))
	}
	writeCards(t, stateRoot, "repo-key", cards...)
	return dir, stateRoot
}

func publishLinks(t *testing.T, home, base, stateRoot string) (CardPublishResult, error) {
	t.Helper()
	return newAdapter(t, map[string]string{}).PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base})
}

func linkRow(t *testing.T, home, path string) (installstate.Row, bool) {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	return ledger.Find(ProjectRuleTarget("repo-key").Key(), path)
}

// OMP follows per-file links and needs no second copy of the card, so an
// in-repository state root publishes a tracked relative link that Atomic owns.
func TestPublishProjectCardsLinksCardsInsideTheRepository(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	result, err := publishLinks(t, home, base, state)
	if err != nil || !result.Applied {
		t.Fatalf("publish = %+v, %v", result, err)
	}
	link := cardFile(base, "core")
	if got, err := os.Readlink(link); err != nil || got != linkTarget {
		t.Fatalf("link = %q, %v; want %s", got, err, linkTarget)
	}
	if got := readFile(t, link); !strings.Contains(got, "description: \"core card\"") || !strings.Contains(got, "globs:") {
		t.Errorf("card through the link = %q", got)
	}
	row, ok := linkRow(t, home, link)
	if !ok || row.Applied.Kind != managedfile.KindSymlink || row.Applied.Digest != managedfile.Digest([]byte(linkTarget)) {
		t.Errorf("row = %+v, %v; want a symlink row over the target text", row, ok)
	}
	rerun, err := publishLinks(t, home, base, state)
	if err != nil || rerun.Applied {
		t.Errorf("rerun = %+v, %v; want a no-op", rerun, err)
	}
}

// A teammate's clone carries the committed link but no ledger row.
func TestPublishProjectCardsAdoptsAnExpectedLink(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	link := cardFile(base, "core")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, link); err != nil {
		t.Fatal(err)
	}
	if _, err := publishLinks(t, home, base, state); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, ok := linkRow(t, home, link); !ok {
		t.Error("the expected link was not adopted into the ledger")
	}
}

// A refused card must leave every other card unwritten: conflicts are checked
// before any mutation.
func TestPublishProjectCardsConflictOnForeignFileWritesNoLink(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "alpha", "core")
	writeFile(t, cardFile(base, "core"), "my own rule\n")
	_, err := publishLinks(t, home, base, state)
	if err == nil || !strings.Contains(err.Error(), "atomic-wiki-core.md") {
		t.Fatalf("err = %v, want a conflict naming atomic-wiki-core.md", err)
	}
	if got := readFile(t, cardFile(base, "core")); got != "my own rule\n" {
		t.Errorf("foreign file = %q", got)
	}
	if _, err := os.Lstat(cardFile(base, "alpha")); err == nil {
		t.Error("alpha link written despite the conflict")
	}
}

// `atomic update` converts earlier generated flat copies into links.
func TestPublishProjectCardsMigratesAGeneratedCopyToALink(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	copyPath := cardFile(base, "core")
	// Publish a copy the way earlier versions did: state root outside the repo.
	outside := filepath.Join(t.TempDir(), ".atomic")
	writeCards(t, outside, "repo-key", pointerCard("core", []string{"src/**"}, "# core card\n"))
	if _, err := publishLinks(t, home, base, outside); err != nil {
		t.Fatalf("copy publish: %v", err)
	}
	if fi, err := os.Lstat(copyPath); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("fixture copy missing or a link: %v", err)
	}
	if _, err := publishLinks(t, home, base, state); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if got, err := os.Readlink(copyPath); err != nil || got != linkTarget {
		t.Errorf("after migration %q, %v; want the link", got, err)
	}
}

func TestPublishProjectCardsKeepsAnEditedCopyAndMutatesNothing(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "alpha", "core")
	outside := filepath.Join(t.TempDir(), ".atomic")
	writeCards(t, outside, "repo-key", pointerCard("core", []string{"src/**"}, "# core card\n"))
	if _, err := publishLinks(t, home, base, outside); err != nil {
		t.Fatal(err)
	}
	copyPath := cardFile(base, "core")
	writeFile(t, copyPath, readFile(t, copyPath)+"edited\n")
	if _, err := publishLinks(t, home, base, state); err == nil {
		t.Fatal("an edited copy was migrated")
	}
	if !strings.HasSuffix(readFile(t, copyPath), "edited\n") {
		t.Error("the edited copy was not kept")
	}
	if _, err := os.Lstat(cardFile(base, "alpha")); err == nil {
		t.Error("alpha link written before the conflict was reported")
	}
}

// A domain that lost its card leaves a dangling link OMP would list as broken.
func TestPublishProjectCardsPrunesDanglingLinks(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "alpha", "core")
	if _, err := publishLinks(t, home, base, state); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(state, "rules", "wiki", "core.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := publishLinks(t, home, base, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(cardFile(base, "core")); err == nil {
		t.Error("dangling link survived")
	}
	if _, ok := linkRow(t, home, cardFile(base, "core")); ok {
		t.Error("ledger row survived")
	}
	if _, err := os.Lstat(cardFile(base, "alpha")); err != nil {
		t.Errorf("live link lost: %v", err)
	}
}

// Without a ledger row a dangling link is ours to prune only when it points at
// exactly the card path Atomic would have written.
func TestPublishProjectCardsPrunesUnrecordedLinksOnlyAtTheirExactTarget(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "alpha")
	ours := filepath.Join(base, ".omp", "rules", "atomic-wiki-gone.md")
	theirs := filepath.Join(base, ".omp", "rules", "atomic-wiki-theirs.md")
	otherRoot := filepath.Join(base, ".omp", "rules", "atomic-wiki-old.md")
	if err := os.MkdirAll(filepath.Dir(ours), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.atomic/rules/wiki/gone.md", ours); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../elsewhere/theirs.md", theirs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../team/rules/wiki/old.md", otherRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := publishLinks(t, home, base, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(ours); err == nil {
		t.Error("dangling link at its exact card target kept")
	}
	if _, err := os.Lstat(theirs); err != nil {
		t.Errorf("foreign link removed: %v", err)
	}
	if _, err := os.Lstat(otherRoot); err != nil {
		t.Errorf("link into another state root removed: %v", err)
	}
}

// The link is created under a temporary name beside its final name, which must
// not cost the user a file that happens to sit there.
func TestPublishProjectCardsKeepsAUserFileAtTheStagingName(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	mine := cardFile(base, "core") + ".new"
	writeFile(t, mine, "my notes\n")
	if _, err := publishLinks(t, home, base, state); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, mine); got != "my notes\n" {
		t.Errorf("user file = %q", got)
	}
	if got, err := os.Readlink(cardFile(base, "core")); err != nil || got != linkTarget {
		t.Errorf("link = %q, %v; want %s", got, err, linkTarget)
	}
}

// OMP checks ignore status on the link path, so an ignored link is invisible
// to it and the user has to hear about that.
func TestPublishProjectCardsReportsAnIgnoredLink(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	writeFile(t, filepath.Join(base, ".gitignore"), ".omp/\n")
	result, err := publishLinks(t, home, base, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ignored) != 1 || !strings.HasSuffix(result.Ignored[0].Path, "atomic-wiki-core.md") || result.Ignored[0].Source != filepath.Join(base, ".gitignore") {
		t.Errorf("ignored = %+v, want the link reported with its ignore source", result.Ignored)
	}
}

// A state move changes the link target; the old link is Atomic's to repoint,
// and with Stage the new link joins the migration's index changes.
func TestPublishProjectCardsRelinksAfterAStateMoveAndStages(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	oldState := filepath.Join(base, ".claude")
	if err := os.Rename(state, oldState); err != nil {
		t.Fatal(err)
	}
	if _, err := publishLinks(t, home, base, oldState); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldState, state); err != nil {
		t.Fatal(err)
	}
	_, err := newAdapter(t, map[string]string{}).PublishProjectCards(CardPublishRequest{Home: home, StateRoot: state, ProjectKey: "repo-key", Base: base, Stage: true})
	if err != nil {
		t.Fatalf("relink: %v", err)
	}
	if got, _ := os.Readlink(cardFile(base, "core")); got != linkTarget {
		t.Errorf("link = %q, want %s", got, linkTarget)
	}
	out, _ := exec.Command("git", "-C", base, "diff", "--cached", "--name-only").Output()
	if !strings.Contains(string(out), ".omp/rules/atomic-wiki-core.md") {
		t.Errorf("staged = %q, want the link", out)
	}
	// A staged link to an unstaged card would commit a dangling link.
	if !strings.Contains(string(out), ".atomic/rules/wiki/core.md") {
		t.Errorf("staged = %q, want the card the link points to", out)
	}
}

// Outside a Git repository there is nothing to ignore or stage, and publishing
// still succeeds.
func TestPublishProjectCardsLinksOutsideAGitRepository(t *testing.T) {
	home := newHome(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, ".atomic")
	writeCards(t, state, "repo-key", pointerCard("core", []string{"src/**"}, "# core card\n"))
	result, err := newAdapter(t, map[string]string{}).PublishProjectCards(CardPublishRequest{Home: home, StateRoot: state, ProjectKey: "repo-key", Base: base, Stage: true})
	if err != nil || len(result.Ignored) != 0 {
		t.Fatalf("publish = %+v, %v", result, err)
	}
	if got, _ := os.Readlink(cardFile(base, "core")); got != linkTarget {
		t.Errorf("link = %q", got)
	}
}

// The links are tracked repository files: target uninstall reports them and
// deletes none.
func TestUninstallLeavesTheCardLinks(t *testing.T) {
	home := newHome(t)
	base, state := linkRepo(t, "core")
	result, err := publishLinks(t, home, base, state)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := harness.RemoveTargetRows(home, result.Target.Key())
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Lstat(cardFile(base, "core")); err != nil {
		t.Errorf("uninstall deleted the link: %v", err)
	}
	if len(removal.Left) != 1 {
		t.Errorf("removal = %+v, want the link reported as left", removal)
	}
}
