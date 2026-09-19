package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const scopeGuidance = "## Scope guidance\n\n- one fact\n"

// copyScopeFixture copies a committed fixture tree into a fresh temp directory
// and returns that directory, so a test migrates a real scope shape rather than
// one it fabricated in place.
func copyScopeFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", "migration", name)
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dst
}

func readScopeFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestMigrateScopeCreatesLoaderPair(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()

	result, err := MigrateScope(ScopeRequest{
		Home:        home,
		Target:      "claude:" + filepath.Join(home, ".claude"),
		OperationID: "scope-create",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
	})
	if err != nil {
		t.Fatalf("MigrateScope: %v", err)
	}
	if result.Status != ScopeCreated {
		t.Errorf("status = %s, want %s", result.Status, ScopeCreated)
	}

	agents := readScopeFile(t, filepath.Join(repo, "AGENTS.md"))
	if !managedfile.BlocksEqual([]byte(agents), blockDocument([]byte(scopeGuidance))) {
		t.Errorf("AGENTS.md block = %q", agents)
	}
	claude := readScopeFile(t, filepath.Join(repo, "CLAUDE.md"))
	if !managedfile.BlocksEqual([]byte(claude), blockDocument([]byte(bundlespec.ScopeSteering.LoaderBody()))) {
		t.Errorf("CLAUDE.md block = %q, want the adjacent loader", claude)
	}
	if !strings.Contains(claude, "@AGENTS.md") {
		t.Errorf("CLAUDE.md does not import the adjacent AGENTS.md: %q", claude)
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	want := map[string]bool{
		filepath.Join(repo, "AGENTS.md"): false,
		filepath.Join(repo, "CLAUDE.md"): false,
	}
	for _, row := range ledger.Rows {
		if _, ok := want[row.Applied.Path]; ok {
			want[row.Applied.Path] = true
			if row.Applied.Digest == "" {
				t.Errorf("row %s recorded no applied digest", row.Resource)
			}
		}
	}
	for path, seen := range want {
		if !seen {
			t.Errorf("ledger has no row for %s", path)
		}
	}
}

// The default migration adds the loader block without moving a byte of the
// user's prose, so every relative reference keeps resolving from the same
// directory.
func TestMigrateScopePreservesUnownedProseAndRelativeRefs(t *testing.T) {
	home := t.TempDir()
	repo := copyScopeFixture(t, "repo")
	before := readScopeFile(t, filepath.Join(repo, "CLAUDE.md"))

	result, err := MigrateScope(ScopeRequest{
		Home:        home,
		Target:      "claude:" + filepath.Join(home, ".claude"),
		OperationID: "scope-preserve",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
	})
	if err != nil {
		t.Fatalf("MigrateScope: %v", err)
	}
	if result.Status != ScopeUpdated {
		t.Errorf("status = %s, want %s", result.Status, ScopeUpdated)
	}

	claude := readScopeFile(t, filepath.Join(repo, "CLAUDE.md"))
	for _, line := range []string{"# Repo notes", "Project-specific guidance kept by the maintainer.", "See @docs/wiki/index.md for the domain map."} {
		if !strings.Contains(claude, line) {
			t.Errorf("CLAUDE.md lost %q:\n%s", line, claude)
		}
	}
	if strings.Contains(before, managedfile.BlockOpen) {
		t.Fatalf("fixture unexpectedly carries a block")
	}
	if !managedfile.BlocksEqual([]byte(claude), blockDocument([]byte(bundlespec.ScopeSteering.LoaderBody()))) {
		t.Errorf("CLAUDE.md block is not the loader: %q", claude)
	}
	// The reference resolves from the directory it always did.
	if _, err := os.Stat(filepath.Join(repo, "docs", "wiki", "index.md")); err != nil {
		t.Errorf("relative reference target missing: %v", err)
	}

	agents := readScopeFile(t, filepath.Join(repo, "AGENTS.md"))
	if !strings.Contains(agents, "# Shared agent guidance") {
		t.Errorf("AGENTS.md lost its pre-existing shared prose:\n%s", agents)
	}
	if !managedfile.BlocksEqual([]byte(agents), blockDocument([]byte(scopeGuidance))) {
		t.Errorf("AGENTS.md block is not the guidance: %q", agents)
	}
}

// Approved relocation moves the unowned prose into the shared document, which
// sits in the same directory, so the relative reference still resolves.
func TestMigrateScopeRelocatesApprovedProse(t *testing.T) {
	home := t.TempDir()
	repo := copyScopeFixture(t, "repo")

	if _, err := MigrateScope(ScopeRequest{
		Home:        home,
		Target:      "claude:" + filepath.Join(home, ".claude"),
		OperationID: "scope-relocate",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
		Relocate:    true,
	}); err != nil {
		t.Fatalf("MigrateScope: %v", err)
	}

	claude := readScopeFile(t, filepath.Join(repo, "CLAUDE.md"))
	if !managedfile.BlocksEqual([]byte(claude), blockDocument([]byte(bundlespec.ScopeSteering.LoaderBody()))) {
		t.Errorf("relocated CLAUDE.md should be the loader alone: %q", claude)
	}
	if strings.Contains(claude, "Repo notes") {
		t.Errorf("relocated prose stayed in CLAUDE.md: %q", claude)
	}
	agents := readScopeFile(t, filepath.Join(repo, "AGENTS.md"))
	for _, line := range []string{"# Repo notes", "See @docs/wiki/index.md for the domain map."} {
		if !strings.Contains(agents, line) {
			t.Errorf("AGENTS.md is missing relocated %q:\n%s", line, agents)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "docs", "wiki", "index.md")); err != nil {
		t.Errorf("relative reference target missing after relocation: %v", err)
	}
}

// A scope CLAUDE.md with no block gains one without taking whole-file
// ownership: an edit to the user's prose after migration is not a derivative
// edit, so Verify reports no conflict and the ledger row stays block-scoped.
func TestMigrateScopeBlockRowSurvivesProseEdit(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	repo := copyScopeFixture(t, "repo")
	claudePath := filepath.Join(repo, "CLAUDE.md")
	if before := readScopeFile(t, claudePath); strings.Contains(before, managedfile.BlockOpen) {
		t.Fatalf("fixture unexpectedly carries a block")
	}

	if _, err := MigrateScope(ScopeRequest{
		Home:        home,
		NativeRoot:  root,
		Target:      "claude:" + root,
		OperationID: "scope-block-row",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
	}); err != nil {
		t.Fatalf("MigrateScope: %v", err)
	}

	var row installstate.Row
	found := false
	for _, r := range ledgerRows(t, home) {
		if r.Applied.Path == claudePath {
			row, found = r, true
		}
	}
	if !found {
		t.Fatalf("ledger has no row for %s", claudePath)
	}
	if row.Applied.Kind != managedfile.KindBlock {
		t.Fatalf("row kind = %s, want %s", row.Applied.Kind, managedfile.KindBlock)
	}

	// The user edits their own prose, outside the managed block.
	edited := readScopeFile(t, claudePath) + "\nA later user note.\n"
	if err := os.WriteFile(claudePath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(MigrationRequest{Home: home, NativeRoot: root})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(verification.Conflicts) != 0 {
		t.Errorf("prose edit reported as a conflict: %+v", verification.Conflicts)
	}
	if verification.Classification.State == installstate.StateMixed {
		t.Errorf("prose edit classified mixed: %+v", verification.Classification)
	}
	if got := readScopeFile(t, claudePath); got != edited {
		t.Error("Verify rewrote the user's prose")
	}
}

func TestMigrateScopeRefusesAmbiguousBlock(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	claudePath := filepath.Join(repo, "CLAUDE.md")
	original := "# user notes\n<atomic>\nunclosed body\n"
	if err := os.WriteFile(claudePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateScope(ScopeRequest{
		Home:        home,
		Target:      "claude:" + filepath.Join(home, ".claude"),
		OperationID: "scope-ambiguous",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
	}); err == nil {
		t.Fatal("ambiguous managed block accepted")
	}
	if got := readScopeFile(t, claudePath); got != original {
		t.Errorf("ambiguous file was rewritten:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("refused scope created AGENTS.md: %v", err)
	}
}

func TestMigrateScopeConvergesJournalFree(t *testing.T) {
	home := t.TempDir()
	repo := copyScopeFixture(t, "repo")

	first, err := MigrateScope(ScopeRequest{
		Home:        home,
		Target:      "claude:" + filepath.Join(home, ".claude"),
		OperationID: "scope-first",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
	})
	if err != nil {
		t.Fatalf("first MigrateScope: %v", err)
	}
	if first.JournalPath == "" {
		t.Fatal("first migration wrote no journal")
	}
	before, err := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}

	second, err := MigrateScope(ScopeRequest{
		Home:        home,
		Target:      "claude:" + filepath.Join(home, ".claude"),
		OperationID: "scope-second",
		Scope:       Scope{Dir: repo, Guidance: []byte(scopeGuidance)},
	})
	if err != nil {
		t.Fatalf("second MigrateScope: %v", err)
	}
	if second.Status != ScopeConverged || second.JournalPath != "" {
		t.Errorf("second migration = %+v, want a journal-free no-op", second)
	}
	after, err := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("converged migration rewrote CLAUDE.md")
	}
	entries, err := os.ReadDir(config.JournalsDir(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("journals = %d, want only the first migration's", len(entries))
	}
}

// A realm owns two loader pairs: the realm root for capture and member guidance,
// and the nested wiki repository. Both converge independently.
func TestMigrateScopeRealmPairs(t *testing.T) {
	home := t.TempDir()
	realm := copyScopeFixture(t, "realm")

	scopes := []Scope{
		{Dir: realm, Guidance: []byte("## Realm capture\n")},
		{Dir: filepath.Join(realm, "wiki"), Guidance: []byte("## Wiki steering\n")},
	}
	for _, s := range scopes {
		if _, err := MigrateScope(ScopeRequest{
			Home:   home,
			Target: "claude:" + filepath.Join(home, ".claude"),
			Scope:  s,
		}); err != nil {
			t.Fatalf("MigrateScope %s: %v", s.Dir, err)
		}
	}

	for _, dir := range []string{realm, filepath.Join(realm, "wiki")} {
		claude := readScopeFile(t, filepath.Join(dir, "CLAUDE.md"))
		if !managedfile.BlocksEqual([]byte(claude), blockDocument([]byte(bundlespec.ScopeSteering.LoaderBody()))) {
			t.Errorf("%s CLAUDE.md is not a loader: %q", dir, claude)
		}
		agents := readScopeFile(t, filepath.Join(dir, "AGENTS.md"))
		if !managedfile.HasBlock([]byte(agents)) {
			t.Errorf("%s AGENTS.md carries no Atomic block: %q", dir, agents)
		}
	}
	// The realm wiki's legacy `@index.md` prose is unowned and survives.
	if wikiClaude := readScopeFile(t, filepath.Join(realm, "wiki", "CLAUDE.md")); !strings.Contains(wikiClaude, "@index.md") {
		t.Errorf("realm wiki loader dropped the legacy import: %q", wikiClaude)
	}
}
