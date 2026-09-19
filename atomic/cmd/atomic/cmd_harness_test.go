package main

import (
	"encoding/json"
	"os"
	"os/exec"
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
