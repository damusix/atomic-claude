package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// sandboxHome returns a temporary home with a Claude native root and the ledger
// describing it, plus the file the ledger row owns.
func claudeLedgerHome(t *testing.T) (home, root, owned string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	owned = filepath.Join(root, "CLAUDE.md")
	return home, root, owned
}

func saveLedger(t *testing.T, home string, targets []installstate.TargetRecord, rows []installstate.Row) {
	t.Helper()
	ledger := &installstate.Ledger{}
	for _, rec := range targets {
		ledger.UpsertTarget(rec)
	}
	for _, row := range rows {
		ledger.Upsert(row)
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
}

func claudeTarget(home, root string) installstate.TargetRecord {
	return installstate.TargetRecord{Harness: string(harness.KindClaude), Instance: root, NativeRoot: root, Status: string(harness.StatusConverged)}
}

func claudeInstance(home, root string) harness.Instance {
	return harness.Instance{Kind: harness.KindClaude, ID: root, NativeRoot: root, Home: home, Exists: true}
}

func fileRow(target, resource, path, digest string) installstate.Row {
	return installstate.Row{
		Target: target, Resource: resource, Consumer: target, Generation: "gen-1", Tier: "unsupported",
		Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: digest},
	}
}

// Every multi-harness category reports on its own: a direct call returns a
// result with the registered index and name and does not depend on another
// category having run.
func TestMultiHarnessCategoriesReportIndependently(t *testing.T) {
	home, root, owned := claudeLedgerHome(t)
	writeRootFile(t, owned, "projected")
	target := claudeTarget(home, root)
	key := target.Key()
	saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
		fileRow(key, "steering", owned, fileDigest(t, owned)),
	})

	withStatusSteps(t, func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
	})

	checks := []struct {
		index int
		name  string
		run   func(Opts) Result
	}{
		{15, "targets", checkTargets},
		{16, "resources", checkResources},
		{18, "capabilities", checkCapabilities},
		{19, "rules", checkRules},
		{21, "staleness", checkStaleness},
		{22, "conflicts", checkConflicts},
		{23, "shadowing", checkShadowing},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			r := c.run(Opts{Home: home})
			if r.Detail == "" {
				t.Errorf("%s reported no detail", c.name)
			}
		})
	}

	// The registry is the only place index and name are assigned, and an --only
	// selection runs exactly the named category.
	results, err := RunWith(Opts{Only: []int{15}, Home: home, RepoRoot: home}, false)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(results) != 1 || results[0].Index != 15 || results[0].Name != "targets" {
		t.Fatalf("only-selection = %+v, want one targets result", results)
	}
}

func TestCheckTargets(t *testing.T) {
	t.Run("enrolled and registered", func(t *testing.T) {
		home, root, _ := claudeLedgerHome(t)
		saveLedger(t, home, []installstate.TargetRecord{claudeTarget(home, root)}, nil)
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		if r := checkTargets(Opts{Home: home}); r.Severity != PASS {
			t.Fatalf("targets = %+v, want PASS", r)
		}
	})

	t.Run("native root missing", func(t *testing.T) {
		home := t.TempDir()
		root := filepath.Join(home, ".claude")
		saveLedger(t, home, []installstate.TargetRecord{claudeTarget(home, root)}, nil)
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude})
		})
		r := checkTargets(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "not present") {
			t.Fatalf("targets = %+v, want WARN naming the missing native root", r)
		}
	})
}

func TestCheckResources(t *testing.T) {
	t.Run("enrolled consumer", func(t *testing.T) {
		home, root, owned := claudeLedgerHome(t)
		writeRootFile(t, owned, "projected")
		target := claudeTarget(home, root)
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
			fileRow(target.Key(), "steering", owned, fileDigest(t, owned)),
		})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		if r := checkResources(Opts{Home: home}); r.Severity != PASS {
			t.Fatalf("resources = %+v, want PASS", r)
		}
	})

	t.Run("dangling consumer", func(t *testing.T) {
		home, root, owned := claudeLedgerHome(t)
		writeRootFile(t, owned, "projected")
		target := claudeTarget(home, root)
		row := fileRow("claude:/elsewhere", "steering", owned, fileDigest(t, owned))
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{row})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		r := checkResources(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "does not enroll") {
			t.Fatalf("resources = %+v, want WARN about the dangling consumer", r)
		}
	})
}

func TestCheckJournals(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		home := t.TempDir()
		if r := checkJournals(Opts{Home: home}); r.Severity != PASS {
			t.Fatalf("journals = %+v, want PASS", r)
		}
	})

	t.Run("recoverable journal", func(t *testing.T) {
		home := t.TempDir()
		journal := installstate.NewJournal("doctor-journal", time.Unix(0, 0).UTC())
		journal.Mutations = []installstate.Mutation{{
			Unit: "steering", Resource: "steering", Target: "claude:x", Consumer: "claude:x",
			Kind: managedfile.KindFile, Path: filepath.Join(home, "steering.md"),
			Stage:    filepath.Join(config.TransactionStageDir(home, "doctor-journal"), "steering"),
			Intended: "deadbeef", PriorObserved: false,
		}}
		if err := installstate.WriteJournal(config.JournalPath(home, "doctor-journal"), journal); err != nil {
			t.Fatal(err)
		}
		r := checkJournals(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "unfinished journal") {
			t.Fatalf("journals = %+v, want WARN naming the unfinished journal", r)
		}
	})
}

func TestCheckCapabilities(t *testing.T) {
	home := t.TempDir()
	r := checkCapabilities(Opts{Home: home})
	if r.Severity != PASS {
		t.Fatalf("capabilities = %+v, want PASS", r)
	}
	if !hasFinding(r.Detail, "2 harness capability record(s)") {
		t.Errorf("capabilities detail = %q, want both registered harnesses counted", r.Detail)
	}
}

func TestCheckRules(t *testing.T) {
	t.Run("applied rule resource", func(t *testing.T) {
		home, root, _ := claudeLedgerHome(t)
		rulePath := filepath.Join(root, "rules", "atomic", "style.md")
		if err := os.MkdirAll(filepath.Dir(rulePath), 0o755); err != nil {
			t.Fatal(err)
		}
		writeRootFile(t, rulePath, "rule body")
		target := claudeTarget(home, root)
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
			fileRow(target.Key(), "rules/atomic/style.md", rulePath, fileDigest(t, rulePath)),
		})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		if r := checkRules(Opts{Home: home}); r.Severity != PASS {
			t.Fatalf("rules = %+v, want PASS", r)
		}
	})

	t.Run("missing rule resource", func(t *testing.T) {
		home, root, _ := claudeLedgerHome(t)
		rulePath := filepath.Join(root, "rules", "atomic", "gone.md")
		target := claudeTarget(home, root)
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
			fileRow(target.Key(), "rules/atomic/gone.md", rulePath, "abc123"),
		})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		r := checkRules(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "is missing") {
			t.Fatalf("rules = %+v, want WARN naming the missing rule resource", r)
		}
	})
}

func TestCheckTrust(t *testing.T) {
	restore := hooksInstalledFn
	t.Cleanup(func() { hooksInstalledFn = restore })

	home := t.TempDir()
	hooksInstalledFn = func(string) (bool, bool, error) { return true, false, nil }
	if r := checkTrust(Opts{Home: home}); r.Severity != PASS {
		t.Fatalf("trust = %+v, want PASS for a registered hook", r)
	}

	hooksInstalledFn = func(string) (bool, bool, error) { return true, true, nil }
	r := checkTrust(Opts{Home: home})
	if r.Severity != WARN || !hasFinding(r.Detail, "drifted") {
		t.Fatalf("trust = %+v, want WARN for a drifted hook", r)
	}
}

func TestCheckStaleness(t *testing.T) {
	t.Run("applied", func(t *testing.T) {
		home, root, owned := claudeLedgerHome(t)
		writeRootFile(t, owned, "projected")
		target := claudeTarget(home, root)
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
			fileRow(target.Key(), "steering", owned, fileDigest(t, owned)),
		})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		if r := checkStaleness(Opts{Home: home}); r.Severity != PASS {
			t.Fatalf("staleness = %+v, want PASS", r)
		}
	})

	t.Run("unverifiable generation", func(t *testing.T) {
		home, root, owned := claudeLedgerHome(t)
		writeRootFile(t, owned, "projected")
		target := claudeTarget(home, root)
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
			fileRow(target.Key(), "steering", owned, ""),
		})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		r := checkStaleness(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "no applied digest") {
			t.Fatalf("staleness = %+v, want WARN for an unverifiable generation", r)
		}
	})
}

func TestCheckConflicts(t *testing.T) {
	home, root, owned := claudeLedgerHome(t)
	writeRootFile(t, owned, "tampered by the user")
	target := claudeTarget(home, root)
	original := managedfile.Digest([]byte("original projected bytes"))
	saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
		fileRow(target.Key(), "steering", owned, original),
	})
	withStatusSteps(t, func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
	})
	r := checkConflicts(Opts{Home: home})
	if r.Severity != WARN || !hasFinding(r.Detail, "did not record") {
		t.Fatalf("conflicts = %+v, want WARN naming the derivative edit", r)
	}
}

func TestCheckShadowing(t *testing.T) {
	t.Run("duplicate file name", func(t *testing.T) {
		home, root, _ := claudeLedgerHome(t)
		first := filepath.Join(root, "rules", "a", "style.md")
		second := filepath.Join(root, "rules", "b", "style.md")
		for _, p := range []string{first, second} {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			writeRootFile(t, p, "same bytes")
		}
		target := claudeTarget(home, root)
		digest := fileDigest(t, first)
		saveLedger(t, home, []installstate.TargetRecord{target}, []installstate.Row{
			fileRow(target.Key(), "rules/a/style.md", first, digest),
			fileRow(target.Key(), "rules/b/style.md", second, digest),
		})
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		r := checkShadowing(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "delivered at both") {
			t.Fatalf("shadowing = %+v, want WARN naming the duplicate delivery", r)
		}
	})

	t.Run("native global AGENTS.md", func(t *testing.T) {
		home, root, _ := claudeLedgerHome(t)
		writeRootFile(t, filepath.Join(root, "AGENTS.md"), "native copy")
		target := claudeTarget(home, root)
		saveLedger(t, home, []installstate.TargetRecord{target}, nil)
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})
		r := checkShadowing(Opts{Home: home})
		if r.Severity != WARN || !hasFinding(r.Detail, "AGENTS.md") {
			t.Fatalf("shadowing = %+v, want WARN naming the native AGENTS.md", r)
		}
	})

	// Duplicate groups are collected in a map, so their order is randomized.
	// Several groups make the pre-sort order vary across runs; the Detail must
	// nevertheless be byte-identical every time.
	t.Run("detail is deterministic across runs", func(t *testing.T) {
		home, root, _ := claudeLedgerHome(t)
		target := claudeTarget(home, root)
		var rows []installstate.Row
		for _, name := range []string{"style", "voice", "notes"} {
			first := filepath.Join(root, "rules", name+"-a", name+".md")
			second := filepath.Join(root, "rules", name+"-b", name+".md")
			for _, p := range []string{first, second} {
				writeRootFile(t, p, "same bytes")
			}
			digest := fileDigest(t, first)
			rows = append(rows,
				fileRow(target.Key(), "rules/"+name+"-a/"+name+".md", first, digest),
				fileRow(target.Key(), "rules/"+name+"-b/"+name+".md", second, digest),
			)
		}
		saveLedger(t, home, []installstate.TargetRecord{target}, rows)
		withStatusSteps(t, func(h string) install.Steps {
			return stepsWith(h, fakeAdapter{kind: harness.KindClaude, instances: []harness.Instance{claudeInstance(home, root)}})
		})

		var want string
		for i := range 30 {
			r := checkShadowing(Opts{Home: home})
			if r.Severity != WARN {
				t.Fatalf("run %d: severity = %v (%s), want WARN", i, r.Severity, r.Detail)
			}
			if i == 0 {
				want = r.Detail
				continue
			}
			if r.Detail != want {
				t.Fatalf("run %d: Detail = %q, want the deterministic %q", i, r.Detail, want)
			}
		}
	})
}

// writeRootFile writes content, creating parents.
func writeRootFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fileDigest is the digest the ownership row records for a written file.
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return managedfile.Digest(data)
}

// hasFinding reports whether detail contains sub.
func hasFinding(detail, sub string) bool {
	return strings.Contains(detail, sub)
}
