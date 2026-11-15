package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// legacyClaudeTree seeds .claude/rules/atomic-wiki/ and, when owned, the
// ledger row that proves Atomic wrote it.
func legacyClaudeTree(t *testing.T, home, repo string, owned bool) string {
	t.Helper()
	legacy := filepath.Join(repo, ".claude", "rules", "atomic-wiki")
	writeTestFile(t, filepath.Join(legacy, "core.md"), "old\n")
	if !owned {
		return legacy
	}
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
	return legacy
}

// The old projection is retired even where no link is written.
func TestEnsureCardLinkRetiresTheLegacyTreeUnderAClaudeRoot(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".claude")
	home := t.TempDir()
	legacy := legacyClaudeTree(t, home, repo, true)

	result, err := EnsureCardLink(linkRequest(home, repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) || len(result.Retired) != 1 {
		t.Errorf("legacy tree stat err = %v, retired = %v; want it removed", err, result.Retired)
	}
}

func TestEnsureCardLinkKeepsAnUnprovenLegacyTree(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	home := t.TempDir()
	legacy := legacyClaudeTree(t, home, repo, false)

	result, err := EnsureCardLink(linkRequest(home, repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil || len(result.Kept) != 1 || result.Kept[0] != legacy {
		t.Errorf("legacy stat err = %v, kept = %v; want it kept and reported", err, result.Kept)
	}
}

// macOS temp paths resolve /var to /private/var; Git reports the resolved
// form, so an unresolved repository root must still find its ignore file.
func TestEnsureCardLinkNegatesUnderAnUnresolvedRoot(t *testing.T) {
	resolved, stateRoot := cardRepo(t, ".atomic")
	unresolved := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(resolved, unresolved); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(resolved, ".gitignore"), ".claude/rules/atomic\n")
	req := linkRequest(t.TempDir(), unresolved, filepath.Join(unresolved, filepath.Base(stateRoot)))

	result, err := EnsureCardLink(req)
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Negated == "" || result.IgnoredBy != "" {
		t.Errorf("negated = %q, ignored by = %q; want the repository ignore file negated", result.Negated, result.IgnoredBy)
	}
}

// A repository with no cards has nothing for the link to serve.
func TestEnsureCardLinkWritesNothingWithoutCards(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	if err := os.RemoveAll(filepath.Join(stateRoot, "rules")); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	result, err := EnsureCardLink(linkRequest(home, repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Action != CardLinkNone || pathExists(filepath.Join(repo, ".claude", "rules", "atomic")) {
		t.Errorf("action = %q; want no link without cards", result.Action)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) != 0 {
		t.Errorf("rows = %+v, want none", ledger.Rows)
	}
}

// The outside-repository copy is a Claude projection, so with no Claude target
// enrolled there is nothing to generate it for.
func TestEnsureCardLinkSkipsTheCopyWithoutATarget(t *testing.T) {
	repo, _ := cardRepo(t, ".atomic")
	outside := filepath.Join(t.TempDir(), "state")
	writeTestFile(t, filepath.Join(outside, "rules", "wiki", "core.md"), cardBody)
	req := linkRequest(t.TempDir(), repo, outside)
	req.Targets = nil

	result, err := EnsureCardLink(req)
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Action != CardLinkNone || pathExists(filepath.Join(repo, ".claude")) {
		t.Errorf("action = %q; want no copy without a Claude target", result.Action)
	}
}

// A negation cannot re-include a file whose parent directory is ignored, so
// the appended line is taken back and the blocking file is reported.
func TestEnsureCardLinkUndoesAnIneffectiveNegation(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	ignore := filepath.Join(repo, ".gitignore")
	writeTestFile(t, ignore, ".claude/\n")
	req := linkRequest(t.TempDir(), repo, stateRoot)
	req.Stage = true

	result, err := EnsureCardLink(req)
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if got, _ := os.ReadFile(ignore); string(got) != ".claude/\n" {
		t.Errorf(".gitignore = %q, want it restored", got)
	}
	if result.IgnoredBy != ignore || result.Negated != "" {
		t.Errorf("ignored by = %q, negated = %q; want the blocking file reported", result.IgnoredBy, result.Negated)
	}
	if staged := runGit(t, repo, "diff", "--cached", "--name-status"); staged != "" {
		t.Errorf("staged =\n%s\nwant nothing", staged)
	}
}

// No row is written without a target, so the link is not reported adopted.
func TestEnsureCardLinkDoesNotAdoptWithoutATarget(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	link := filepath.Join(repo, ".claude", "rules", "atomic")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.atomic/rules", link); err != nil {
		t.Fatal(err)
	}
	req := linkRequest(t.TempDir(), repo, stateRoot)
	req.Targets = nil
	result, err := EnsureCardLink(req)
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.Action != CardLinkPresent {
		t.Errorf("action = %q, want present", result.Action)
	}
}

// Ignore file paths may contain colons; check-ignore's verbose form uses colons
// as separators.
func TestEnsureCardLinkReportsAnExcludesPathWithAColon(t *testing.T) {
	repo, stateRoot := cardRepo(t, ".atomic")
	excludes := filepath.Join(t.TempDir(), "a:b", "ignore")
	writeTestFile(t, excludes, "atomic\n")
	writeTestFile(t, os.Getenv("GIT_CONFIG_GLOBAL"), "[core]\n\texcludesFile = "+excludes+"\n")

	result, err := EnsureCardLink(linkRequest(t.TempDir(), repo, stateRoot))
	if err != nil {
		t.Fatalf("EnsureCardLink: %v", err)
	}
	if result.IgnoredBy != excludes {
		t.Errorf("ignored by = %q, want %s", result.IgnoredBy, excludes)
	}
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
