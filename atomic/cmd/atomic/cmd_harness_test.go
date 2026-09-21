package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// TestAtomicCLIHelper is the subprocess entry point for the lifecycle verbs,
// which exit the process on failure and therefore cannot be called in-process.
// A normal run skips it.
func TestAtomicCLIHelper(t *testing.T) {
	args := os.Getenv("ATOMIC_TEST_CLI_ARGS")
	if args == "" {
		t.Skip("subprocess helper")
	}
	// The verb writes to os.Stdout directly, so it is redirected to a file the
	// parent reads; the test framework's own report would otherwise land in the
	// same stream and corrupt the JSON assertions.
	if outPath := os.Getenv("ATOMIC_TEST_CLI_OUT"); outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			t.Fatalf("create capture file: %v", err)
		}
		original := os.Stdout
		os.Stdout = f
		defer func() { os.Stdout = original }()
	}
	fields := strings.Split(args, "\x1f")
	switch fields[0] {
	case "install":
		runInstall(fields[1:])
	case "harness":
		runHarness(fields[1:])
	case "claude":
		runClaude(fields[1:])
	case "update":
		os.Exit(runUpdateWith(fields[1:], defaultUpdateDeps()))
	default:
		t.Fatalf("unknown helper verb %q", fields[0])
	}
}

// runAtomicCLI runs one verb in a subprocess with HOME redirected, which is the
// only form that catches scope-root resolution bugs — seam stubs cannot.
func runAtomicCLI(t *testing.T, home string, args ...string) (string, int) {
	t.Helper()
	return runAtomicCLIEnv(t, home, nil, args...)
}

// runAtomicCLIEnv is runAtomicCLI with extra environment entries, for a verb
// whose adapter resolves its native root from a variable (Codex reads
// CODEX_HOME). An inherited entry under the same name is dropped, so the
// injected value is the only one the child sees.
func runAtomicCLIEnv(t *testing.T, home string, extra []string, args ...string) (string, int) {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "stdout")
	overridden := map[string]bool{}
	for _, kv := range extra {
		if name, _, ok := strings.Cut(kv, "="); ok {
			overridden[name] = true
		}
	}
	env := make([]string, 0, len(os.Environ())+len(extra)+4)
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && overridden[name] {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"ATOMIC_TEST_CLI_ARGS="+strings.Join(args, "\x1f"),
		"ATOMIC_TEST_CLI_OUT="+outPath,
		"HOME="+home,
		"CLAUDE_CONFIG_DIR=",
	)
	env = append(env, extra...)

	cmd := exec.Command(os.Args[0], "-test.run=^TestAtomicCLIHelper$")
	cmd.Env = env
	diag, err := cmd.CombinedOutput()
	out, readErr := os.ReadFile(outPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read helper output: %v", readErr)
	}
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) {
		t.Fatalf("run %v: %v\n%s", args, err, diag)
	}
	return string(out), exitErr.ExitCode()
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// TestLifecycleCLIInstallStatusUninstall exercises the real Claude adapter end to
// end: install enrolls and writes native bytes, status and diff report them, and
// a full uninstall removes them while preserving the mutable authorities.
func TestLifecycleCLIInstallStatusUninstall(t *testing.T) {
	home := t.TempDir()

	out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--yes")
	if code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "applied") {
		t.Errorf("install output = %q, want an applied target", out)
	}
	steering := filepath.Join(home, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(steering)
	if err != nil {
		t.Fatalf("global steering: %v", err)
	}
	if !strings.Contains(string(data), "<atomic>") {
		t.Error("global steering carries no Atomic block")
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic", "install", "ledger.json")); err != nil {
		t.Errorf("ledger was not written: %v", err)
	}

	// The mutable authorities a full uninstall must preserve.
	for name, content := range map[string]string{
		filepath.Join(home, ".atomic", "config.toml"): "state.dir=\".claude\"\n",
		filepath.Join(home, ".atomic", "profile.md"):  "profile\n",
		filepath.Join(home, ".atomic", "wikis.md"):    "wikis\n",
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	status, code := runAtomicCLI(t, home, "harness", "status", "--json")
	if code != 0 {
		t.Fatalf("status exited %d:\n%s", code, status)
	}
	var report struct {
		Instances []struct {
			Enrolled bool `json:"Enrolled"`
		} `json:"instances"`
		Targets []struct {
			Target struct {
				Harness  string `json:"harness"`
				Instance string `json:"instance"`
			} `json:"target"`
			Resources []struct {
				State string `json:"state"`
			} `json:"resources"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(status), &report); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, status)
	}
	if len(report.Targets) != 1 || report.Targets[0].Target.Harness != "claude" {
		t.Fatalf("status targets = %+v, want one enrolled claude target", report.Targets)
	}
	for _, resource := range report.Targets[0].Resources {
		if resource.State != string(harness.RowApplied) && resource.State != string(harness.RowStale) {
			t.Errorf("resource state = %q, want applied or stale", resource.State)
		}
	}

	diff, code := runAtomicCLI(t, home, "harness", "diff", "--json")
	if code != 0 {
		t.Fatalf("diff exited %d:\n%s", code, diff)
	}
	if !strings.Contains(diff, "rules/python/style.md") {
		t.Errorf("diff does not report a shipped rule:\n%s", diff)
	}

	rules, code := runAtomicCLI(t, home, "harness", "rules", "status", "--json")
	if code != 0 {
		t.Fatalf("rules status exited %d:\n%s", code, rules)
	}
	if !strings.Contains(rules, "rules/python/style.md") {
		t.Errorf("rules status does not report the shipped rule:\n%s", rules)
	}

	out, code = runAtomicCLI(t, home, "harness", "uninstall", "--all", "--yes")
	if code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "rules", "python", "style.md")); !os.IsNotExist(err) {
		t.Errorf("target uninstall left an owned rule file (stat err = %v)", err)
	}
	for name := range map[string]string{
		filepath.Join(home, ".atomic", "config.toml"): "",
		filepath.Join(home, ".atomic", "profile.md"):  "",
		filepath.Join(home, ".atomic", "wikis.md"):    "",
	} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("full uninstall removed %s: %v", name, err)
		}
	}
}

// TestLifecycleCLIAdoptLegacyInstallEnrolls proves a legacy install the selected
// generation verifies is adopted into an enrolled target with ownership rows,
// and that a repeated adopt or repair does not fall back to legacy handling.
func TestLifecycleCLIAdoptLegacyInstallEnrolls(t *testing.T) {
	home := t.TempDir()

	out, code := runAtomicCLI(t, home, "claude", "install", "--no-hooks")
	if code != 0 {
		t.Fatalf("legacy install exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic", "install", "ledger.json")); !os.IsNotExist(err) {
		t.Fatalf("the legacy installer wrote a v2 ledger (stat err = %v)", err)
	}

	out, code = runAtomicCLI(t, home, "harness", "adopt", "claude", "--yes")
	if code != 0 {
		t.Fatalf("adopt exited %d:\n%s", code, out)
	}
	ledger, err := installstate.LoadLedger(filepath.Join(home, ".atomic", "install", "ledger.json"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if _, ok := ledger.FindTarget("claude", filepath.Join(home, ".claude")); !ok {
		t.Errorf("adopt did not enroll the target: %+v", ledger.Targets)
	}
	if len(ledger.Rows) == 0 {
		t.Error("adopt recorded no ownership rows")
	}

	out, code = runAtomicCLI(t, home, "harness", "repair", "--yes")
	if code != 0 {
		t.Fatalf("repair exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "converged") {
		t.Errorf("repair after adopt = %q, want a converged target", out)
	}
}

// TestLifecycleCLIDryRunWritesNothing proves the planning form leaves the
// Atomic-owned paths byte-identical.
func TestLifecycleCLIDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()

	out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run install exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("dry-run output = %q", out)
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "CLAUDE.md"),
		filepath.Join(home, ".atomic", "install", "ledger.json"),
		filepath.Join(home, ".atomic", "install", "operation.lock"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("dry run created %s (stat err = %v)", path, err)
		}
	}
}

// TestLifecycleCLIUninstallDryRunWritesNothing proves both uninstall dry-run
// forms are read-only: a stateless --all creates no install directory, and after
// a real install neither form rewrites native bytes or the lifecycle lock.
func TestLifecycleCLIUninstallDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()

	out, code := runAtomicCLI(t, home, "harness", "uninstall", "--all", "--dry-run")
	if code != 0 {
		t.Fatalf("stateless dry-run uninstall exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("dry-run uninstall created state (stat err = %v)", err)
	}

	out, code = runAtomicCLI(t, home, "install", "--harness", "claude", "--yes")
	if code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	before := treeDigestCLI(t, home)
	target := "claude:" + filepath.Join(home, ".claude")

	out, code = runAtomicCLI(t, home, "harness", "uninstall", target, "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run uninstall exited %d:\n%s", code, out)
	}
	if after := treeDigestCLI(t, home); after != before {
		t.Errorf("dry-run target uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}

	out, code = runAtomicCLI(t, home, "harness", "uninstall", "--all", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run full uninstall exited %d:\n%s", code, out)
	}
	if after := treeDigestCLI(t, home); after != before {
		t.Errorf("dry-run full uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
}

// treeDigestCLI digests every path under root, so a read-only CLI assertion
// compares the whole home and not only the files a test happened to name.
func treeDigestCLI(t *testing.T, root string) string {
	t.Helper()
	digest, _, err := managedfile.TreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// seedInterruptedInstall writes the state an install leaves when it stops after
// its native bytes landed but before the ledger row committed: the steering block
// already holds the journal's intended digest, the unit is recorded applied, the
// enrollment record is written, and no ownership row exists.
func seedInterruptedInstall(t *testing.T, home string) (key, path string) {
	t.Helper()
	root := filepath.Join(home, ".omp", "agent")
	path = filepath.Join(root, "AGENTS.md")
	content := []byte("<atomic>\ncontract\n</atomic>\n")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := managedfile.DigestResourceBytes(content, managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	key = "omp:" + root
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	operationID := "interrupted-install-op"
	journal := installstate.NewJournal(operationID, time.Unix(0, 0).UTC())
	journal.Mutations = []installstate.Mutation{{
		Unit: "steering", Resource: "steering", Target: key, Consumer: key,
		Generation: "gen-1", Tier: "unsupported",
		Kind: managedfile.KindBlock, Path: path, Intended: digest,
	}}
	journal.Record("steering", installstate.StateApplied, digest, time.Unix(0, 0).UTC())
	if err := installstate.WriteJournal(config.JournalPath(home, operationID), journal); err != nil {
		t.Fatal(err)
	}
	return key, path
}

// TestLifecycleCLIUninstallDryRunPlansPostRecovery proves both CLI dry-run forms
// of an interrupted install report the advisory post-recovery removal plan — the
// applied-but-unrecorded resource included, never a false not-enrolled failure —
// leave the home byte-identical, and that the real run then recovers and removes.
func TestLifecycleCLIUninstallDryRunPlansPostRecovery(t *testing.T) {
	home := t.TempDir()
	key, path := seedInterruptedInstall(t, home)
	before := treeDigestCLI(t, home)

	out, code := runAtomicCLI(t, home, "harness", "uninstall", key, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry-run target uninstall exited %d:\n%s", code, out)
	}
	var removal harness.Removal
	if err := json.Unmarshal([]byte(out), &removal); err != nil {
		t.Fatalf("dry-run removal JSON: %v\n%s", err, out)
	}
	if len(removal.Blockers) != 0 {
		t.Errorf("dry run reported blockers: %v", removal.Blockers)
	}
	if len(removal.Removed) != 1 || removal.Removed[0] != "steering" {
		t.Errorf("Removed = %v, want the applied-but-unrecorded resource", removal.Removed)
	}

	out, code = runAtomicCLI(t, home, "harness", "uninstall", "--all", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry-run full uninstall exited %d:\n%s", code, out)
	}
	var report install.FullUninstallReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("dry-run full report JSON: %v\n%s", err, out)
	}
	if len(report.Targets) != 1 || len(report.Targets[0].Removed) != 1 || report.Targets[0].Removed[0] != "steering" {
		t.Errorf("full dry run targets = %+v, want the applied resource in the advisory plan", report.Targets)
	}

	if after := treeDigestCLI(t, home); after != before {
		t.Errorf("dry-run uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}

	out, code = runAtomicCLI(t, home, "harness", "uninstall", key, "--yes")
	if code != 0 {
		t.Fatalf("real uninstall exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("real uninstall left the recovered resource (stat err = %v)", err)
	}
}

// TestLifecycleCLIUninstallNamesDiscardFlagAndClearsChangedResource proves the
// CLI's skip line names the exact next step and that the flag it names finishes
// the job: the default run preserves the changed bytes and the claim, and a
// confirmed discard removes the resource, its row, and the enrollment.
func TestLifecycleCLIUninstallNamesDiscardFlagAndClearsChangedResource(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	path := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "omp:" + root
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"})
	ledger.Upsert(installstate.Row{Target: key, Resource: "steering", Consumer: key,
		Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	out, code := runAtomicCLI(t, home, "harness", "uninstall", key, "--yes")
	if code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "--discard-changed") {
		t.Errorf("the skip line does not name the resolution:\n%s", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the skipped resource's bytes were removed: %v", err)
	}
	kept, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kept.Find(key, "steering"); !ok {
		t.Fatal("the skipped claim was dropped")
	}

	out, code = runAtomicCLI(t, home, "harness", "uninstall", key, "--discard-changed", "--yes")
	if code != 0 {
		t.Fatalf("discard uninstall exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the discarded resource survived (stat err = %v)", err)
	}
	cleared, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cleared.Find(key, "steering"); ok {
		t.Error("the discard left the ownership row")
	}
	if _, ok := cleared.FindTarget("omp", root); ok {
		t.Error("the discard left the enrollment")
	}
}

// TestLifecycleCLIUninstallClearsUnparseableRow proves a ledger row whose target
// key does not parse is clearable through the CLI: the key it is recorded under
// is still a valid positional, so no stale row is unremovable state.
func TestLifecycleCLIUninstallClearsUnparseableRow(t *testing.T) {
	home := t.TempDir()
	missing := filepath.Join(home, ".omp", "agent", "extensions", "atomic.ts")
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(installstate.Row{Target: ":stale", Resource: missing, Consumer: ":stale",
		Applied: installstate.AppliedValue{Path: missing, Kind: managedfile.KindFile, Digest: "0000"}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	out, code := runAtomicCLI(t, home, "harness", "uninstall", ":stale", "--yes")
	if code != 0 {
		t.Fatalf("raw-key uninstall exited %d:\n%s", code, out)
	}
	cleared, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cleared.Find(":stale", missing); ok {
		t.Error("the raw-key removal left the stale row behind")
	}
}

// TestLifecycleCLIListsInstances proves discovery reports candidates without
// enrolling them.
func TestLifecycleCLIListsInstances(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := runAtomicCLI(t, home, "harness", "list", "--json")
	if code != 0 {
		t.Fatalf("list exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "\"claude\"") {
		t.Errorf("list output = %q, want a claude instance", out)
	}
	var rows []struct {
		Enrolled bool `json:"enrolled"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("list JSON: %v\n%s", err, out)
	}
	for _, row := range rows {
		if row.Enrolled {
			t.Errorf("discovery reported an enrollment: %s", out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("discovery created state (stat err = %v)", err)
	}
}

// TestSplitBoolFlags proves a positional target key may precede or follow the
// flags, which stdlib flag parsing alone would misread.
func TestSplitBoolFlags(t *testing.T) {
	flags, positional := splitBoolFlags([]string{"claude:/root", "--yes", "--json"})
	if len(flags) != 2 || len(positional) != 1 || positional[0] != "claude:/root" {
		t.Fatalf("split = %v, %v", flags, positional)
	}
	flags, positional = splitBoolFlags([]string{"--yes", "claude:/root"})
	if len(flags) != 1 || len(positional) != 1 {
		t.Fatalf("split = %v, %v", flags, positional)
	}
}

// TestHarnessSelectionParsesTargetKey proves a full target key drives kind and
// instance resolution.
func TestHarnessSelectionParsesTargetKey(t *testing.T) {
	sel, err := harnessSelection([]string{"claude:/home/u/.claude"}, "", "")
	if err != nil {
		t.Fatalf("harnessSelection: %v", err)
	}
	if sel.Kind != harness.KindClaude || sel.Instance != "/home/u/.claude" {
		t.Errorf("selection = %+v", sel)
	}
	if _, err := harnessSelection([]string{"not-a-key"}, "", ""); err == nil {
		t.Error("a malformed target key was accepted")
	}
}

// TestInstallFlagsRejectConflictingDecisions proves --replace and
// --leave-unowned cannot both choose how an unowned artifact is treated.
func TestInstallFlagsRejectConflictingDecisions(t *testing.T) {
	flags := installFlags{replace: true, leave: true}
	if _, err := flags.batchDecision(); err == nil {
		t.Error("conflicting decisions were accepted")
	}
	if decision, err := (&installFlags{replace: true}).batchDecision(); err != nil || decision != "replace" {
		t.Errorf("replace decision = %q, %v", decision, err)
	}
}

// TestRepairSelectionIsEnrolledOnly proves repair never widens to discovered
// instances.
func TestRepairSelectionIsEnrolledOnly(t *testing.T) {
	sel, err := repairSelection(&installFlags{harness: "omp"})
	if err != nil {
		t.Fatalf("repairSelection: %v", err)
	}
	if !sel.EnrolledOnly || sel.Kind != harness.KindOMP {
		t.Errorf("selection = %+v", sel)
	}
	if sel.All {
		t.Error("repair selection must not select every discovered instance")
	}
}

// TestLifecycleCLIOMPTarget drives the generic lifecycle verbs against a real
// OMP profile through the production registry: install delivers the generated
// extension module under the profile's agent root — the path CP0 proved OMP
// discovers — and install, status, and rules status report the package surfaces
// Atomic cannot prove instead of reading the enrollment as a full delivery.
func TestLifecycleCLIOMPTarget(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// The isolated child must not inherit an ambient profile selector: discovery
	// reports the ambient profile too, and two instances make --harness omp
	// ambiguous rather than wrong.
	env := []string{"OMP_PROFILE="}
	key := "omp:" + root

	out, code := runAtomicCLIEnv(t, home, env, "install", "--harness", "omp", "--yes")
	if code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, key) {
		t.Errorf("install output = %q, want the omp target reported", out)
	}
	if !strings.Contains(out, "unsupported") {
		t.Errorf("install output = %q, want the unproven package surfaces reported rather than a silent success", out)
	}

	// What OMP loads is the enrolled profile's module, at the agent-root path CP0
	// observed discovery under; the package tree is Atomic's corpus store.
	extension := filepath.Join(root, "extensions", "atomic.ts")
	if data, err := os.ReadFile(extension); err != nil {
		t.Fatalf("enrollment did not deliver the discovered extension at %s: %v", extension, err)
	} else if !strings.Contains(string(data), string(omp.RuntimeMarker)) {
		t.Errorf("the delivered extension does not carry the generated runtime delivery")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Errorf("enrollment did not write the profile steering: %v", err)
	}

	status, code := runAtomicCLIEnv(t, home, env, "harness", "status")
	if code != 0 {
		t.Fatalf("status exited %d:\n%s", code, status)
	}
	if !strings.Contains(status, "unsupported") || !strings.Contains(status, "package install") {
		t.Errorf("status output = %q, want the unproven package surfaces named", status)
	}

	rules, code := runAtomicCLIEnv(t, home, env, "harness", "rules", "status")
	if code != 0 {
		t.Fatalf("rules status exited %d:\n%s", code, rules)
	}
	if !strings.Contains(rules, "unsupported") {
		t.Errorf("rules status output = %q, want the unproven package surfaces named", rules)
	}

	// Repair converges the enrolled profile and reports the same unproven rows.
	repair, code := runAtomicCLIEnv(t, home, env, "harness", "repair", "--yes")
	if code != 0 {
		t.Fatalf("repair exited %d:\n%s", code, repair)
	}
	if !strings.Contains(repair, key) || !strings.Contains(repair, "unsupported") {
		t.Errorf("repair output = %q, want the converged target and its unproven surfaces", repair)
	}
}

// TestLifecycleCLICodexTarget drives the generic lifecycle verbs against a real
// Codex home through the production registry: discovery resolves CODEX_HOME,
// enrollment publishes only the Atomic plugin package, status, rules, diff, and
// repair report it, and a target uninstall removes only the owned package.
func TestLifecycleCLICodexTarget(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "user-notes.toml")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{"CODEX_HOME=" + root}
	key := "codex:" + root

	list, code := runAtomicCLIEnv(t, home, env, "harness", "list", "--json")
	if code != 0 {
		t.Fatalf("list exited %d:\n%s", code, list)
	}
	if !strings.Contains(list, root) || !strings.Contains(list, "\"codex\"") {
		t.Errorf("list output = %q, want the configured CODEX_HOME instance", list)
	}

	out, code := runAtomicCLIEnv(t, home, env, "install", "--harness", "codex", "--yes")
	if code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, key) || !strings.Contains(out, "applied") {
		t.Errorf("install output = %q, want the codex target applied", out)
	}

	manifest := filepath.Join(home, ".atomic", "packages", "codex", "atomic", ".agents", "plugins", "marketplace.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("enrollment did not publish the plugin package: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "config.toml")); !os.IsNotExist(err) {
		t.Errorf("enrollment wrote the Codex native registry (stat err = %v); registration is Codex's own surface", err)
	}

	status, code := runAtomicCLIEnv(t, home, env, "harness", "status", "--json")
	if code != 0 {
		t.Fatalf("status exited %d:\n%s", code, status)
	}
	var report struct {
		Targets []struct {
			Target struct {
				Harness  string `json:"harness"`
				Instance string `json:"instance"`
			} `json:"target"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(status), &report); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, status)
	}
	if len(report.Targets) != 1 || report.Targets[0].Target.Harness != "codex" || report.Targets[0].Target.Instance != root {
		t.Fatalf("status targets = %+v, want the enrolled codex target", report.Targets)
	}

	rules, code := runAtomicCLIEnv(t, home, env, "harness", "rules", "status", "--json")
	if code != 0 {
		t.Fatalf("rules status exited %d:\n%s", code, rules)
	}
	if !strings.Contains(rules, string(harness.RoleStaticScope)) {
		t.Errorf("rules status = %q, want the unproven Codex rule-delivery roles reported", rules)
	}

	diff, code := runAtomicCLIEnv(t, home, env, "harness", "diff", "--json")
	if code != 0 {
		t.Fatalf("diff exited %d:\n%s", code, diff)
	}
	if !strings.Contains(diff, filepath.Join(home, ".atomic", "packages", "codex", "atomic")) {
		t.Errorf("diff = %q, want the owned plugin package reported", diff)
	}

	repair, code := runAtomicCLIEnv(t, home, env, "harness", "repair", "--yes")
	if code != 0 {
		t.Fatalf("repair exited %d:\n%s", code, repair)
	}
	if !strings.Contains(repair, key) {
		t.Errorf("repair output = %q, want the enrolled codex target converged", repair)
	}

	removal, code := runAtomicCLIEnv(t, home, env, "harness", "uninstall", key, "--yes")
	if code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, removal)
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic", "packages", "codex", "atomic")); !os.IsNotExist(err) {
		t.Errorf("target uninstall left the owned plugin package (stat err = %v)", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("target uninstall removed unowned bytes in the native root: %v", err)
	}
	after, code := runAtomicCLIEnv(t, home, env, "harness", "status", "--json")
	if code != 0 {
		t.Fatalf("status after uninstall exited %d:\n%s", code, after)
	}
	if strings.Contains(after, key) {
		t.Errorf("status still reports the removed target:\n%s", after)
	}
}

// TestLifecycleCLICodexRequiresHome proves an unset CODEX_HOME is an explicit
// refusal naming the variable, never a guessed default root.
func TestLifecycleCLICodexRequiresHome(t *testing.T) {
	home := t.TempDir()
	out, code := runAtomicCLIEnv(t, home, []string{"CODEX_HOME="}, "harness", "list", "--json")
	if code != 0 {
		t.Fatalf("list exited %d:\n%s", code, out)
	}
	if strings.Contains(out, "\"codex\"") {
		t.Errorf("list reported a codex instance with CODEX_HOME unset:\n%s", out)
	}

	out, code = runAtomicCLIEnv(t, home, []string{"CODEX_HOME="}, "harness", "enroll", "codex")
	if code == 0 {
		t.Fatalf("enroll accepted an unset CODEX_HOME:\n%s", out)
	}
	// The refusal text itself — CODEX_HOME named, wrapped in ErrUnsupported — is
	// pinned by the adapter's own discovery test; here the dispatch is the claim.
}

// seedAppliedClaudeJournal writes an applied-but-unrecorded block publication:
// the native file holds Atomic's new block, the transaction backup holds the
// user's original bytes, and the journal is unresolved. It returns the journal
// path and the file path.
func seedAppliedClaudeJournal(t *testing.T, home string) (string, string) {
	t.Helper()
	target := filepath.Join(home, ".claude", "commands", "commit.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original user bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged := "user prose\n" + managedfile.BlockOpen + "\nnew body\n" + managedfile.BlockClose + "\n"
	digest, err := managedfile.DigestResourceBytes([]byte(staged), managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := installstate.NewTransaction(home, "op-cli-recover", installstate.Plan{Mutations: []installstate.Mutation{{
		Unit:       "global-claude",
		Resource:   "global-claude",
		Target:     "claude:default",
		Kind:       managedfile.KindBlock,
		Path:       target,
		Intended:   digest,
		Generation: "gen-1",
		Tier:       "unsupported",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(staged), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return tx.JournalPath, target
}

// TestHarnessRecoverCLIForwardAndRollback proves the `harness recover` verb is
// reachable: a dry run is read-only, the default run rolls forward and records
// the applied bytes, and --rollback restores the pre-mutation bytes.
func TestHarnessRecoverCLIForwardAndRollback(t *testing.T) {
	t.Run("dry run is read-only", func(t *testing.T) {
		home := t.TempDir()
		journalPath, target := seedAppliedClaudeJournal(t, home)
		before, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		out, code := runAtomicCLI(t, home, "harness", "recover", "--dry-run", "--json")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, out)
		}
		if _, err := os.Stat(journalPath); err != nil {
			t.Errorf("dry run consumed the journal: %v", err)
		}
		if after, err := os.ReadFile(target); err != nil || string(after) != string(before) {
			t.Errorf("dry run changed the native bytes: %q (%v)", after, err)
		}
	})

	t.Run("rolls forward by default", func(t *testing.T) {
		home := t.TempDir()
		journalPath, target := seedAppliedClaudeJournal(t, home)
		out, code := runAtomicCLI(t, home, "harness", "recover")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, out)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !managedfile.HasBlock(got) {
			t.Errorf("forward recovery did not keep the applied block: %q", got)
		}
		if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
			t.Errorf("forward recovery left the consumed journal behind (stat err = %v)", err)
		}
	})

	t.Run("rollback restores user bytes", func(t *testing.T) {
		home := t.TempDir()
		journalPath, target := seedAppliedClaudeJournal(t, home)
		out, code := runAtomicCLI(t, home, "harness", "recover", "--rollback")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, out)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "original user bytes\n" {
			t.Errorf("rollback did not restore the user's bytes: %q", got)
		}
		if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
			t.Errorf("rollback left the consumed journal behind (stat err = %v)", err)
		}
	})
}

// TestHarnessRecoverCLIReportsConflict proves a journal whose bytes no longer
// match anything recovery may touch is reported as a conflict with a non-zero
// exit, and the conflict message names the resolving verb.
func TestHarnessRecoverCLIReportsConflict(t *testing.T) {
	home := t.TempDir()
	journalPath, target := seedAppliedClaudeJournal(t, home)
	// A later user edit matches neither the published bytes nor the backup.
	if err := os.WriteFile(target, []byte("edited after the crash\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runAtomicCLI(t, home, "harness", "recover")
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero for an unresolvable journal\n%s", out)
	}
	if !strings.Contains(out, string(installstate.DecisionConflict)) {
		t.Errorf("output = %q, want a conflict decision", out)
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Errorf("a conflicted recovery consumed the journal: %v", err)
	}

	// The adapter-level conflict error names the verb that resolves it.
	_, err := installstate.Adopt(installstate.AdoptionRequest{Home: home, NativeRoot: filepath.Join(home, ".claude"), Target: "claude:default"})
	if err == nil || !strings.Contains(err.Error(), "atomic harness recover") {
		t.Errorf("adoption conflict error = %v, want a `harness recover` instruction", err)
	}
}

// atomicStateDigest digests Atomic's whole state root, so a read-only assertion
// covers the journal, the transaction backup, and the ledger at once.
func atomicStateDigest(t *testing.T, home string) string {
	t.Helper()
	return treeDigestCLI(t, filepath.Join(home, ".atomic"))
}

// seedInterruptedTreeJournal writes the state an interrupted generated-tree
// publication leaves: the tree already at the destination is displaced into the
// transaction backup, the staged tree is still in place, and the destination is
// absent — the window between PublishDir's two renames. It returns the
// destination, the staged tree, and the transaction backup.
func seedInterruptedTreeJournal(t *testing.T, home string) (dest, stage, backup string) {
	t.Helper()
	dest = config.PackageRoot(home, "omp")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "package.json"), []byte("{\"version\":\"old\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The staged tree is written twice: once to learn the digest the journal must
	// record, then through the real staging path.
	staged := []byte("{\"version\":\"new\"}\n")
	scratch := filepath.Join(t.TempDir(), "staged")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "package.json"), staged, 0o644); err != nil {
		t.Fatal(err)
	}
	intended, _, err := managedfile.TreeDigest(scratch)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := installstate.NewTransaction(home, "op-tree-crash", installstate.Plan{Mutations: []installstate.Mutation{{
		Unit: "omp-package", Resource: "omp-package", Target: "omp:default", Consumer: "omp:default",
		Generation: "gen-1", Tier: "unsupported",
		Kind: managedfile.KindTree, Path: dest, Intended: intended,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	stage, err = tx.StageTree("omp-package", func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "package.json"), staged, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	backup = filepath.Join(tx.BackupRoot(), filepath.Base(dest))
	tx.PublishDirFn = func(_, destDir, backupDir string) (managedfile.Publication, error) {
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return managedfile.Publication{}, err
		}
		if err := os.Rename(destDir, filepath.Join(backupDir, filepath.Base(destDir))); err != nil {
			return managedfile.Publication{}, err
		}
		return managedfile.Publication{}, errors.New("simulated crash between the two renames")
	}
	if err := tx.PublishTree("omp-package"); err == nil {
		t.Fatal("the crash stub published the tree")
	}
	return dest, stage, backup
}

// assertTreeUnchanged proves one read-only verb left the interrupted tree state
// and Atomic's own state byte-identical: the destination is still absent, the
// staged tree and the transaction backup are still in place, and nothing under
// ~/.atomic moved. The whole home is deliberately not compared — enumerating
// harness instances runs the installed OMP path query, which bootstraps its own
// state outside Atomic's.
func assertTreeUnchanged(t *testing.T, verb, home, before, dest, stage, backup string) {
	t.Helper()
	if after := atomicStateDigest(t, home); after != before {
		t.Errorf("%s changed Atomic's state:\nbefore %s\nafter  %s", verb, before, after)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("%s performed the interrupted tree publication (stat %s = %v)", verb, dest, err)
	}
	if _, err := os.Stat(stage); err != nil {
		t.Errorf("%s consumed the staged tree: %v", verb, err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("%s consumed the transaction backup: %v", verb, err)
	}
}

// TestHarnessDryRunsLeaveInterruptedTreeUntouched proves every read-only caller
// that previews recovery — the recover, status, and uninstall dry runs, install
// --dry-run through the projection, and doctor's journal check — reports the
// interrupted tree publication without performing it. Before the simulation
// neutralized the tree seams, each of these moved the staged tree into the
// destination and consumed the tree backup the crash left behind.
func TestHarnessDryRunsLeaveInterruptedTreeUntouched(t *testing.T) {
	home := t.TempDir()
	// An empty PATH keeps the OMP adapter's read-only path query from spawning a
	// locally installed OMP binary, whose own bootstrap would move bytes in the
	// home under test.
	noPath := []string{"PATH=" + t.TempDir()}
	// A real claude install gives the home an enrolled target, which is what makes
	// status and install project through the adoption planner.
	if out, code := runAtomicCLIEnv(t, home, noPath, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	dest, stage, backup := seedInterruptedTreeJournal(t, home)
	before := atomicStateDigest(t, home)

	dryRuns := []struct {
		verb string
		args []string
	}{
		{"harness recover --dry-run", []string{"harness", "recover", "--dry-run"}},
		{"harness status", []string{"harness", "status"}},
		{"harness uninstall --dry-run", []string{"harness", "uninstall", "claude:" + filepath.Join(home, ".claude"), "--dry-run"}},
		{"install --dry-run", []string{"install", "--harness", "claude", "--dry-run", "--yes"}},
	}
	for _, dry := range dryRuns {
		out, code := runAtomicCLIEnv(t, home, noPath, dry.args...)
		if code != 0 {
			t.Fatalf("%s exited %d:\n%s", dry.verb, code, out)
		}
		if dry.verb == "harness recover --dry-run" && !strings.Contains(out, "op-tree-crash") {
			t.Errorf("the preview did not report the interrupted journal:\n%s", out)
		}
		assertTreeUnchanged(t, dry.verb, home, before, dest, stage, backup)
	}

	results, err := doctor.RunWith(doctor.Opts{Home: home, Only: []int{17}, RepoRoot: t.TempDir()}, false)
	if err != nil {
		t.Fatalf("doctor journals check: %v", err)
	}
	if len(results) != 1 || results[0].Index != 17 {
		t.Fatalf("doctor results = %+v, want category 17", results)
	}
	if len(results[0].Findings) != 1 {
		t.Errorf("doctor findings = %v, want the interrupted journal reported", results[0].Findings)
	}
	assertTreeUnchanged(t, "doctor journals", home, before, dest, stage, backup)
}

// hasDecision reports whether one recovery result carries a decision.
func hasDecision(actions []installstate.RecoveryAction, want installstate.RecoveryDecision) bool {
	for _, a := range actions {
		if a.Decision == want {
			return true
		}
	}
	return false
}

// TestHarnessRecoverDryRunPreviewsRollback proves --rollback --dry-run previews
// the rollback the same command performs for real, instead of the roll-forward
// decision, and writes nothing: the native bytes, the transaction backup, and the
// journal are untouched.
func TestHarnessRecoverDryRunPreviewsRollback(t *testing.T) {
	home := t.TempDir()
	journalPath, target := seedAppliedClaudeJournal(t, home)
	applied, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := installstate.LoadJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.Backups) == 0 {
		t.Fatal("the seeded publication captured no transaction backup")
	}
	before := treeDigestCLI(t, home)

	out, code := runAtomicCLI(t, home, "harness", "recover", "--rollback", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("rollback dry run exited %d:\n%s", code, out)
	}
	var sims []installstate.RecoverySimulation
	if err := json.Unmarshal([]byte(out), &sims); err != nil {
		t.Fatalf("rollback dry-run JSON: %v\n%s", err, out)
	}
	if len(sims) != 1 || !hasDecision(sims[0].Actions, installstate.DecisionRestoreBackup) {
		t.Fatalf("preview = %+v, want the rollback decision", sims)
	}
	if hasDecision(sims[0].Actions, installstate.DecisionCommitApplied) {
		t.Errorf("the rollback preview reported the roll-forward decision: %+v", sims[0].Actions)
	}
	if after, err := os.ReadFile(target); err != nil || string(after) != string(applied) {
		t.Errorf("the rollback preview changed the native bytes: %q (%v)", after, err)
	}
	if _, err := os.Stat(journal.Backups[0].Path); err != nil {
		t.Errorf("the rollback preview consumed the transaction backup: %v", err)
	}
	if after := treeDigestCLI(t, home); after != before {
		t.Errorf("the rollback preview changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
}

// seedConflictedClaudeJournal writes an applied-but-unrecorded publication of two
// block resources that a later edit changed, so recovery can reconcile neither.
// It returns the journal path and both native paths.
func seedConflictedClaudeJournal(t *testing.T, home string) (string, []string) {
	t.Helper()
	units := []struct{ file, unit string }{{"commit.md", "global-claude"}, {"plan.md", "global-claude-plan"}}
	dir := filepath.Join(home, ".claude", "commands")
	mutations := make([]installstate.Mutation, 0, len(units))
	staged := make(map[string][]byte, len(units))
	paths := make([]string, 0, len(units))
	for _, u := range units {
		path := filepath.Join(dir, u.file)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original bytes for "+u.unit+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		body := "user prose\n" + managedfile.BlockOpen + "\nnew body for " + u.unit + "\n" + managedfile.BlockClose + "\n"
		digest, err := managedfile.DigestResourceBytes([]byte(body), managedfile.KindBlock)
		if err != nil {
			t.Fatal(err)
		}
		mutations = append(mutations, installstate.Mutation{
			Unit: u.unit, Resource: u.unit, Target: "claude:default",
			Kind: managedfile.KindBlock, Path: path, Intended: digest,
			Generation: "gen-1", Tier: "unsupported",
		})
		staged[u.unit] = []byte(body)
		paths = append(paths, path)
	}
	tx, err := installstate.NewTransaction(home, "op-cli-conflict", installstate.Plan{Mutations: mutations})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range units {
		if _, err := tx.StageFile(u.unit, staged[u.unit], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range units {
		if err := tx.PublishFile(u.unit); err != nil {
			t.Fatalf("publish %s: %v", u.unit, err)
		}
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("edited after the crash\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return tx.JournalPath, paths
}

// TestHarnessRecoverCLIJSONReportsEveryConflict proves a scripted recovery reads
// a refusal as a refusal — a non-zero exit in both output forms — and that every
// unreconciled unit is reported, not the first.
func TestHarnessRecoverCLIJSONReportsEveryConflict(t *testing.T) {
	home := t.TempDir()
	journalPath, paths := seedConflictedClaudeJournal(t, home)

	out, code := runAtomicCLI(t, home, "harness", "recover", "--dry-run")
	if code == 0 {
		t.Errorf("text dry-run exit = 0, want non-zero\n%s", out)
	}
	for _, path := range paths {
		if !strings.Contains(out, path) {
			t.Errorf("the text dry run omitted the conflict at %s:\n%s", path, out)
		}
	}

	out, code = runAtomicCLI(t, home, "harness", "recover", "--dry-run", "--json")
	if code == 0 {
		t.Errorf("dry-run exit = 0, want non-zero\n%s", out)
	}
	var sims []installstate.RecoverySimulation
	if err := json.Unmarshal([]byte(out), &sims); err != nil {
		t.Fatalf("dry-run JSON: %v\n%s", err, out)
	}
	if len(sims) != 1 || len(sims[0].Conflicts) != len(paths) {
		t.Errorf("dry-run conflicts = %+v, want both units", sims)
	}

	out, code = runAtomicCLI(t, home, "harness", "recover", "--json")
	if code == 0 {
		t.Errorf("exit = 0, want non-zero for an unresolvable journal\n%s", out)
	}
	var actions []installstate.RecoveryAction
	if err := json.Unmarshal([]byte(out), &actions); err != nil {
		t.Fatalf("recovery JSON: %v\n%s", err, out)
	}
	conflicted := map[string]bool{}
	for _, a := range actions {
		if a.Decision == installstate.DecisionConflict {
			conflicted[a.Path] = true
		}
	}
	for _, path := range paths {
		if !conflicted[path] {
			t.Errorf("the JSON report omitted the conflict at %s: %+v", path, actions)
		}
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Errorf("a conflicted recovery consumed the journal: %v", err)
	}
}
