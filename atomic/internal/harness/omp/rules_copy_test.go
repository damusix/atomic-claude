package omp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// OMP's rule scan skips any file a Git ignore source covers, so an ignored card
// is never listed; publication writes no ignore rule and leaves the user's own.
func TestPublishProjectCardsAddsNoIgnoreRule(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, ".omp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, ".omp", ".gitignore"), []byte("/mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !exists(cardFile(base, "typescript")) {
		t.Fatal("the copy did not land at .omp/rules/atomic-wiki-typescript.md")
	}
	if got := readFile(t, filepath.Join(base, ".omp", ".gitignore")); got != "/mine" {
		t.Errorf(".omp/.gitignore = %q, want the user's file untouched", got)
	}
}

func TestPublishProjectCardsCreatesNoIgnoreFile(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if exists(filepath.Join(base, ".omp", ".gitignore")) {
		t.Error("publication created .omp/.gitignore")
	}
}

// The ignore line goes only with the directory it ignored: a repository file in
// .omp/rules/atomic/ keeps the directory, so the line stays.
func TestPublishProjectCardsKeepsTheNestedIgnoreRuleWhileTheParentHoldsAFile(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(base, ".omp", "rules", "atomic", "wiki")
	writeFile(t, filepath.Join(nested, "typescript.md"), "old card\n")
	mine := filepath.Join(base, ".omp", "rules", "atomic", "mine.md")
	writeFile(t, mine, "mine\n")
	writeFile(t, filepath.Join(base, ".omp", ".gitignore"), "/rules/atomic/\n")
	digest, _, err := managedfile.TreeDigest(nested)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(installstate.Row{Target: ProjectRuleTarget("repo-key").Key(), Resource: nested, Consumer: "repo-key",
		Applied: installstate.AppliedValue{Path: nested, Kind: managedfile.KindTree, Digest: digest}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if exists(nested) || !exists(mine) {
		t.Errorf("nested exists = %v, repository file exists = %v; want the nested copy gone and the file kept", exists(nested), exists(mine))
	}
	if got := readFile(t, filepath.Join(base, ".omp", ".gitignore")); got != "/rules/atomic/\n" {
		t.Errorf(".omp/.gitignore = %q, want the rule kept", got)
	}
}

// Atomic created the ignore file for the nested copy, so it goes when its only
// rule does.
func TestPublishProjectCardsDeletesTheIgnoreFileItEmptied(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(base, ".omp", "rules", "atomic", "wiki")
	writeFile(t, filepath.Join(nested, "typescript.md"), "old card\n")
	writeFile(t, filepath.Join(base, ".omp", ".gitignore"), "/rules/atomic/\n")
	digest, _, err := managedfile.TreeDigest(nested)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(installstate.Row{Target: ProjectRuleTarget("repo-key").Key(), Resource: nested, Consumer: "repo-key",
		Applied: installstate.AppliedValue{Path: nested, Kind: managedfile.KindTree, Digest: digest}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if exists(filepath.Join(base, ".omp", ".gitignore")) {
		t.Error(".omp/.gitignore survived with no rule left")
	}
}

// A refused publication mutates nothing, the legacy copy included.
func TestPublishProjectCardsConflictLeavesTheNestedCopyAlone(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(base, ".omp", "rules", "atomic", "wiki")
	writeFile(t, filepath.Join(nested, "typescript.md"), "old card\n")
	digest, _, err := managedfile.TreeDigest(nested)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(installstate.Row{Target: ProjectRuleTarget("repo-key").Key(), Resource: nested, Consumer: "repo-key",
		Applied: installstate.AppliedValue{Path: nested, Kind: managedfile.KindTree, Digest: digest}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	ledgerBefore := readFile(t, config.LedgerPath(home))
	writeFile(t, cardFile(base, "typescript"), "# foreign\n")
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err == nil {
		t.Fatal("a foreign card file was overwritten")
	}
	if !exists(nested) {
		t.Error("the refused publication removed the nested copy")
	}
	if got := readFile(t, config.LedgerPath(home)); got != ledgerBefore {
		t.Error("the refused publication changed the ledger")
	}
}

// The nested copy is retired with its ignore rule when its ledger row matches
// its bytes, and kept and reported, rule included, otherwise.
func TestPublishProjectCardsMigratesTheNestedCopy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tamper  bool
		removed bool
	}{
		{"matching row removes it", false, true},
		{"changed copy is kept", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := newHome(t)
			stateRoot := filepath.Join(t.TempDir(), ".atomic")
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nested := filepath.Join(base, ".omp", "rules", "atomic", "wiki")
			card := filepath.Join(nested, "typescript.md")
			writeFile(t, card, "old card\n")
			writeFile(t, filepath.Join(base, ".omp", ".gitignore"), "/mine\n/rules/atomic/\n")
			digest, _, err := managedfile.TreeDigest(nested)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := installstate.LoadLedger(config.LedgerPath(home))
			if err != nil {
				t.Fatal(err)
			}
			key := ProjectRuleTarget("repo-key").Key()
			ledger.Upsert(installstate.Row{Target: key, Resource: nested, Consumer: "repo-key",
				Applied: installstate.AppliedValue{Path: nested, Kind: managedfile.KindTree, Digest: digest}})
			if err := ledger.Save(config.LedgerPath(home)); err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				writeFile(t, card, "hand edit\n")
			}
			writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

			a := newAdapter(t, map[string]string{})
			result, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base})
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			if !exists(cardFile(base, "typescript")) {
				t.Error("the flat card was not published beside the nested copy")
			}
			if exists(nested) == tc.removed {
				t.Errorf("nested copy exists = %v, want %v", exists(nested), !tc.removed)
			}
			if exists(filepath.Join(base, ".omp", "rules", "atomic")) == tc.removed {
				t.Errorf("empty atomic/ parent exists = %v, want %v", exists(filepath.Join(base, ".omp", "rules", "atomic")), !tc.removed)
			}
			wantIgnore := "/mine\n"
			if !tc.removed {
				wantIgnore = "/mine\n/rules/atomic/\n"
			}
			if got := readFile(t, filepath.Join(base, ".omp", ".gitignore")); got != wantIgnore {
				t.Errorf(".omp/.gitignore = %q, want %q", got, wantIgnore)
			}
			after, err := installstate.LoadLedger(config.LedgerPath(home))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := after.Find(key, nested); ok == tc.removed {
				t.Errorf("nested row kept = %v, want %v", ok, !tc.removed)
			}
			reported := result.Retired
			if !tc.removed {
				reported = result.Kept
			}
			if len(reported) != 1 || reported[0] != nested {
				t.Errorf("retired = %v, kept = %v; want %s reported", result.Retired, result.Kept, nested)
			}
		})
	}
}

// The tree earlier versions published at .omp/rules/atomic-wiki/ is retired
// when its ledger row matches the bytes, and kept and reported otherwise.
func TestPublishProjectCardsRetiresLegacyTree(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tamper  bool
		removed bool
	}{
		{"matching row removes it", false, true},
		{"changed tree is kept", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := newHome(t)
			stateRoot := filepath.Join(t.TempDir(), ".atomic")
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			legacy := filepath.Join(base, ".omp", "rules", "atomic-wiki")
			if err := os.MkdirAll(legacy, 0o755); err != nil {
				t.Fatal(err)
			}
			card := filepath.Join(legacy, "typescript.md")
			if err := os.WriteFile(card, []byte("old card\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			digest, _, err := managedfile.TreeDigest(legacy)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := installstate.LoadLedger(config.LedgerPath(home))
			if err != nil {
				t.Fatal(err)
			}
			key := ProjectRuleTarget("repo-key").Key()
			ledger.Upsert(installstate.Row{Target: key, Resource: legacy, Consumer: "repo-key",
				Applied: installstate.AppliedValue{Path: legacy, Kind: managedfile.KindTree, Digest: digest}})
			if err := ledger.Save(config.LedgerPath(home)); err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				if err := os.WriteFile(card, []byte("hand edit\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

			a := newAdapter(t, map[string]string{})
			result, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base})
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			if exists(legacy) == tc.removed {
				t.Errorf("legacy tree exists = %v, want %v", exists(legacy), !tc.removed)
			}
			after, err := installstate.LoadLedger(config.LedgerPath(home))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := after.Find(key, legacy); ok == tc.removed {
				t.Errorf("legacy row kept = %v, want %v", ok, !tc.removed)
			}
			reported := result.Retired
			if !tc.removed {
				reported = result.Kept
			}
			if len(reported) != 1 || reported[0] != legacy {
				t.Errorf("retired = %v, kept = %v; want %s reported", result.Retired, result.Kept, legacy)
			}
		})
	}
}
