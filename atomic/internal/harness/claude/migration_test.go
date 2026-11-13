package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	atomicartifacts "github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const (
	userProseBefore = "user prose before the block\n"
	userProseAfter  = "user prose after the block\n\n@~/.atomic/profile.md\n"
)

// installLegacy runs the real Claude installer so the fixture is the
// installer's own output rather than a hand-built copy.
func installLegacy(t *testing.T, home string) string {
	t.Helper()
	restore := claudeinstall.ProfileRefresh
	claudeinstall.ProfileRefresh = func(string, string, int) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.ProfileRefresh = restore })

	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"model":"sonnet"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := claudeinstall.Install(root, home, false, claudeinstall.RealClock); err != nil {
		t.Fatalf("legacy install: %v", err)
	}
	return root
}

// ageInstall turns the install into a complete prior-version install, wrapped
// in unowned prose, that the selected generation cannot prove.
func ageInstall(t *testing.T, home, root string) {
	t.Helper()
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(a.Target))
		if err := os.WriteFile(path, []byte("older "+a.Target+"\n"), 0o644); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}

	steeringPath := GlobalSteeringPath(root)
	data, err := os.ReadFile(steeringPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte(userProseBefore), data...)
	data = append(data, []byte(userProseAfter)...)
	aged, err := managedfile.ReplaceBlock(data, []byte(managedfile.BlockOpen+"\nolder atomic body\n"+managedfile.BlockClose+"\n"))
	if err != nil {
		t.Fatalf("age block: %v", err)
	}
	if err := os.WriteFile(steeringPath, aged, 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Install.Version = "0.1.0"
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("record older install version: %v", err)
	}
}

// embeddedSteering returns the selected generation's global CLAUDE.md bytes as
// Claude receives them, native-vocabulary paragraph included.
func embeddedSteering(t *testing.T) []byte {
	t.Helper()
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" {
			data, err := fs.ReadFile(embedded.FS, a.Source)
			if err != nil {
				t.Fatal(err)
			}
			steering, err := atomicartifacts.ClaudeSteering(data)
			if err != nil {
				t.Fatal(err)
			}
			return steering
		}
	}
	t.Fatal("embedded corpus has no global steering artifact")
	return nil
}

func migrationRequest(home, root string) MigrationRequest {
	return MigrationRequest{
		Home:        home,
		NativeRoot:  root,
		Target:      "claude:" + root,
		Consumer:    "claude:" + root,
		Generation:  "gen-1",
		Tier:        "native-scope",
		OperationID: "migrate-e2e",
	}
}

func ledgerRows(t *testing.T, home string) []installstate.Row {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	return ledger.Rows
}

// Adoption must replace only the Atomic block, leaving a legacy relative import
// in place.
func TestMigrateAdoptsGlobalLegacyBlock(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	fixture, err := os.ReadFile(filepath.Join("testdata", "migration", "global", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalSteeringPath(root), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	req := migrationRequest(home, root)
	req.AcknowledgeSnapshot = true
	result, err := Migrate(req)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	applied := false
	for _, path := range result.Adoption.Applied {
		if path == GlobalSteeringPath(root) {
			applied = true
		}
	}
	if !applied {
		t.Fatalf("applied = %v, want the global steering path", result.Adoption.Applied)
	}

	steering := readScopeFile(t, GlobalSteeringPath(root))
	if !managedfile.BlocksEqual([]byte(steering), embeddedSteering(t)) {
		t.Errorf("global block is not the selected generation's:\n%s", steering)
	}
	for _, line := range []string{"# My own Claude notes", "Personal notes that must survive any rewrite of the block above.", "@~/.atomic/profile.md"} {
		if !strings.Contains(steering, line) {
			t.Errorf("unowned global prose lost %q:\n%s", line, steering)
		}
	}
	if strings.Contains(steering, "older generation") {
		t.Error("legacy block body survived adoption")
	}
	if _, err := os.Stat(GlobalLoaderPath(root)); !os.IsNotExist(err) {
		t.Errorf("adoption created a user-level AGENTS.md: %v", err)
	}
}

// Migrating a real prior-version install must keep user prose and the
// write-once snapshot, and commit ownership only for verified bytes.
func TestMigrateLegacyCompleteIntoCleanHome(t *testing.T) {
	home := t.TempDir()
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	snapshotBefore := treeDigest(t, config.PreInstallDir(home))

	req := migrationRequest(home, root)
	req.BatchDecision = installstate.DecisionReplace
	result, err := Migrate(req)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(result.Adoption.Applied) == 0 {
		t.Fatal("migration applied nothing")
	}

	// The global block is the selected generation's; the unowned prose and the
	// @-import around it are byte-identical.
	steering := readScopeFile(t, GlobalSteeringPath(root))
	if !managedfile.BlocksEqual([]byte(steering), embeddedSteering(t)) {
		t.Errorf("global block is not the selected generation's:\n%s", steering)
	}
	if !strings.Contains(steering, userProseBefore) || !strings.Contains(steering, userProseAfter) {
		t.Errorf("unowned prose lost:\n%s", steering)
	}
	if _, err := os.Stat(GlobalLoaderPath(root)); !os.IsNotExist(err) {
		t.Errorf("migration created a user-level AGENTS.md: %v", err)
	}

	// Every committed row's digest matches the bytes on disk.
	rows := ledgerRows(t, home)
	if len(rows) == 0 {
		t.Fatal("ledger recorded no ownership")
	}
	for _, row := range rows {
		obs, err := managedfile.Observe(row.Applied.Path, row.Applied.Kind)
		if err != nil {
			t.Fatalf("observe %s: %v", row.Applied.Path, err)
		}
		if obs.Digest != row.Applied.Digest {
			t.Errorf("%s: ledger %s != observed %s", row.Applied.Path, row.Applied.Digest, obs.Digest)
		}
	}

	if after := treeDigest(t, config.PreInstallDir(home)); after != snapshotBefore {
		t.Error("migration modified the write-once pre-install snapshot")
	}
}

// ApplyOwnedSettings refuses this shape after Migrate has rewritten the tree, so
// the plan every caller reads first must block it.
func TestPlanMigrationBlocksBadSettingsShape(t *testing.T) {
	home := t.TempDir()
	root := installLegacy(t, home)
	settings := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"model":"sonnet","hooks":[]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanMigration(migrationRequest(home, root))
	if err != nil {
		t.Fatalf("PlanMigration: %v", err)
	}
	if plan.Status != installstate.StatusBlocked {
		t.Errorf("status = %s, want %s", plan.Status, installstate.StatusBlocked)
	}
	want := settings + `: "hooks" must be an object; found array`
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), want) {
		t.Errorf("blockers %q do not name %q", plan.Blockers, want)
	}
}

// A user edit to the owned block after adoption is reported drift, never
// rewritten.
func TestMigrateVerifyReportsDerivativeDrift(t *testing.T) {
	home := t.TempDir()
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	req := migrationRequest(home, root)
	req.BatchDecision = installstate.DecisionReplace
	if _, err := Migrate(req); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A derivative edit to the owned block: the old binary wrote over it.
	steeringPath := GlobalSteeringPath(root)
	edited, err := managedfile.ReplaceBlock([]byte(readScopeFile(t, steeringPath)), []byte(managedfile.BlockOpen+"\ndivergent body\n"+managedfile.BlockClose+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(steeringPath, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(verification.Drift) == 0 {
		t.Fatalf("derivative edit not reported: %+v", verification)
	}
	if verification.Classification.State == installstate.StateMixed {
		t.Errorf("derivative edit classified the install mixed: %+v", verification.Classification)
	}
	if got := readScopeFile(t, steeringPath); got != string(edited) {
		t.Error("Verify rewrote a derivative edit")
	}
}

// A ledger-owned artifact rewritten outside Atomic surfaces as drift, the way
// an older binary's later write does.
func TestMigrateOldBinaryDrift(t *testing.T) {
	home := t.TempDir()
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	req := migrationRequest(home, root)
	req.BatchDecision = installstate.DecisionReplace
	if _, err := Migrate(req); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	drifted := filepath.Join(root, "commands", "commit.md")
	if err := os.WriteFile(drifted, []byte("written by an older binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	joined := strings.Join(verification.Drift, "\n")
	if !strings.Contains(joined, drifted) {
		t.Fatalf("drift at %s not reported: %+v", drifted, verification)
	}
	if verification.Classification.State == installstate.StateMixed {
		t.Errorf("old-binary drift classified the install mixed: %+v", verification.Classification)
	}
}

// The one-time import moves a legacy native profile copy into the authority and
// never runs again.
func TestMigrateImportsMutableOnce(t *testing.T) {
	home := t.TempDir()
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	// Remove the authority the installer created and leave a legacy native copy.
	authority := config.ProfilePath(home)
	if err := os.Remove(authority); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, ".atomic", "profile.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("# legacy profile\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := migrationRequest(home, root)
	req.BatchDecision = installstate.DecisionReplace
	first, err := Migrate(req)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(first.Import.Imported) != 1 || first.Import.Imported[0] != authority {
		t.Fatalf("import = %+v, want the profile authority", first.Import)
	}
	if got := readScopeFile(t, authority); got != "# legacy profile\n" {
		t.Errorf("authority = %q", got)
	}

	// A later divergent native copy is a conflict, never imported.
	if err := os.WriteFile(legacy, []byte("# divergent legacy profile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.OperationID = "migrate-second"
	second, err := Migrate(req)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(second.Import.Imported) != 0 {
		t.Errorf("second migration imported again: %+v", second.Import)
	}
	if got := readScopeFile(t, authority); got != "# legacy profile\n" {
		t.Errorf("authority was overwritten by a later native copy: %q", got)
	}
	verification, err := Verify(req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(verification.Conflicts) == 0 {
		t.Error("divergent native profile copy not reported as a conflict")
	}
}

// An interruption mid-apply leaves a journal and unused staging; a rerun
// recovers it and converges without clobbering the change that blocked it.
func TestMigrateResumesInterruptedAdoption(t *testing.T) {
	home := t.TempDir()
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	// The file stays readable, so classification and backup succeed, but the
	// publish fails mid-operation.
	blocked := filepath.Join(root, "commands", "commit.md")
	if err := os.Chmod(blocked, 0o444); err != nil {
		t.Fatal(err)
	}

	req := migrationRequest(home, root)
	req.BatchDecision = installstate.DecisionReplace
	if _, err := Migrate(req); err == nil {
		t.Fatal("migration succeeded despite an unwritable artifact")
	}
	if journalCount(t, home) == 0 {
		t.Fatal("interrupted migration wrote no journal")
	}
	// Nothing was ledger-committed: the operation never reached Complete.
	if rows := ledgerRows(t, home); len(rows) != 0 {
		t.Errorf("failed migration committed ledger rows: %+v", rows)
	}

	if err := os.Chmod(blocked, 0o644); err != nil {
		t.Fatal(err)
	}
	req.OperationID = "migrate-resume"
	if _, err := Migrate(req); err != nil {
		t.Fatalf("resumed Migrate: %v", err)
	}

	steering := readScopeFile(t, GlobalSteeringPath(root))
	if !managedfile.BlocksEqual([]byte(steering), embeddedSteering(t)) {
		t.Errorf("resumed migration left the wrong block:\n%s", steering)
	}
	rows := ledgerRows(t, home)
	if len(rows) == 0 {
		t.Fatal("resumed migration committed no ledger rows")
	}
	for _, row := range rows {
		obs, err := managedfile.Observe(row.Applied.Path, row.Applied.Kind)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Digest != row.Applied.Digest {
			t.Errorf("%s: ledger %s != observed %s", row.Applied.Path, row.Applied.Digest, obs.Digest)
		}
	}
	for _, path := range journalPaths(t, home) {
		j, err := installstate.LoadJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		if !j.Completed {
			t.Errorf("unresolved journal left behind: %s", path)
		}
	}
}

// A scope whose directory is the global native root is refused before any
// write, so no second loader pair lands beside the global steering.
func TestMigrateRefusesNativeRootScope(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	req := migrationRequest(home, root)
	req.AcknowledgeSnapshot = true
	req.Scopes = []Scope{{Dir: root, Guidance: []byte(scopeGuidance)}}

	if _, err := Migrate(req); err == nil {
		t.Fatal("migration accepted a scope equal to the global native root")
	}
	if _, err := os.Stat(GlobalLoaderPath(root)); !os.IsNotExist(err) {
		t.Errorf("refused migration wrote the native-root AGENTS.md: %v", err)
	}
	if _, err := os.Stat(GlobalSteeringPath(root)); !os.IsNotExist(err) {
		t.Errorf("refusal happened after the global adoption wrote steering: %v", err)
	}
}

// A shared operation id once let a later scope overwrite an earlier scope's
// journal before its rows committed.
func TestMigrateScopesKeepDistinctOperationJournals(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	repos := []string{t.TempDir(), t.TempDir()}

	req := migrationRequest(home, root)
	req.AcknowledgeSnapshot = true
	for _, dir := range repos {
		req.Scopes = append(req.Scopes, Scope{Dir: dir, Guidance: []byte(scopeGuidance)})
	}

	result, err := Migrate(req)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(result.Scopes) != 2 {
		t.Fatalf("scopes migrated = %d, want 2", len(result.Scopes))
	}
	first, second := result.Scopes[0], result.Scopes[1]
	if first.JournalPath == "" || second.JournalPath == "" {
		t.Fatalf("scope journals = %q, %q, want both recorded", first.JournalPath, second.JournalPath)
	}
	if first.JournalPath == second.JournalPath {
		t.Fatalf("scopes share one journal: %s", first.JournalPath)
	}
	if _, err := os.Stat(config.JournalsDir(home)); !os.IsNotExist(err) {
		entries, readErr := os.ReadDir(config.JournalsDir(home))
		if readErr == nil && len(entries) != 0 {
			t.Errorf("completed scope migrations left %d journal(s) behind", len(entries))
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for i, sr := range result.Scopes {
		if sr.Status != ScopeCreated {
			t.Errorf("scope %d status = %s, want %s", i, sr.Status, ScopeCreated)
		}
		for _, path := range []string{sr.AgentsPath, sr.ClaudePath} {
			if !strings.HasPrefix(path, repos[i]+string(os.PathSeparator)) {
				t.Errorf("scope %d wrote outside its directory: %s", i, path)
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("scope %d did not publish %s: %v", i, path, err)
			}
			if _, ok := ledger.Find(req.Target, path); !ok {
				t.Errorf("scope %d recorded no ownership row for %s", i, path)
			}
		}
	}
}

func journalPaths(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(config.JournalsDir(home))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, filepath.Join(config.JournalsDir(home), e.Name()))
	}
	return out
}

func journalCount(t *testing.T, home string) int {
	t.Helper()
	return len(journalPaths(t, home))
}

// treeDigest hashes every regular file under root, path-qualified and sorted, so
// a byte-identical tree has an identical digest.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			lines = append(lines, "d "+path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, "f "+path+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// Legacy guidance migration writes pairs in the configured mode: agents-md
// leaves no CLAUDE.md beside AGENTS.md, or Claude would stop reading AGENTS.md.
func TestMigrateScopesFollowSteeringMode(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	agentsRepo, claudeRepo := t.TempDir(), t.TempDir()
	for dir, mode := range map[string]bundlespec.SteeringMode{
		agentsRepo: bundlespec.SteeringAgentsMD,
		claudeRepo: bundlespec.SteeringClaudeMD,
	} {
		cfg := config.RepoConfigPath(dir)
		if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg, []byte("[steering]\nclaude = \""+string(mode)+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	req := migrationRequest(home, root)
	req.AcknowledgeSnapshot = true
	req.Scopes = []Scope{
		{Dir: agentsRepo, Guidance: []byte(scopeGuidance)},
		{Dir: claudeRepo, Guidance: []byte(scopeGuidance)},
	}

	if _, err := Migrate(req); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	exists := func(dir, name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if !exists(agentsRepo, "AGENTS.md") || exists(agentsRepo, "CLAUDE.md") {
		t.Errorf("agents-md scope: AGENTS.md=%v CLAUDE.md=%v, want AGENTS.md only", exists(agentsRepo, "AGENTS.md"), exists(agentsRepo, "CLAUDE.md"))
	}
	if exists(claudeRepo, "AGENTS.md") || !exists(claudeRepo, "CLAUDE.md") {
		t.Errorf("claude-md scope: AGENTS.md=%v CLAUDE.md=%v, want CLAUDE.md only", exists(claudeRepo, "AGENTS.md"), exists(claudeRepo, "CLAUDE.md"))
	}
}
