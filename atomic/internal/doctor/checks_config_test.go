package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// makeAtomicDir creates <root>/.atomic/ and returns the dir path.
func makeAtomicDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".atomic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	return dir
}

// writeTOML writes content to <root>/.atomic/config.toml.
func writeTOML(t *testing.T, root, content string) {
	t.Helper()
	makeAtomicDir(t, root)
	path := config.TOMLPath(root)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

// writeResolved writes content to <root>/.atomic/config.resolved.md.
func writeResolved(t *testing.T, root, content string) {
	t.Helper()
	makeAtomicDir(t, root)
	path := config.ResolvedPath(root)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.resolved.md: %v", err)
	}
}

// TestCheckConfig_noTOML: no config.toml → PASS with "built-in defaults" detail.
func TestCheckConfig_noTOML(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("detail is empty")
	}
	// Must mention defaults
	if !strings.Contains(r.Detail, "defaults") {
		t.Errorf("detail %q: want mention of 'defaults'", r.Detail)
	}
}

// TestCheckConfig_validTOMLAndSyncedResolved: valid TOML + resolved.md in sync → PASS.
func TestCheckConfig_validTOMLAndSyncedResolved(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")

	// Pre-render the correct resolved.md.
	cfg, warns, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "ok") {
		t.Errorf("detail %q: want 'ok'", r.Detail)
	}
}

// TestCheckConfig_unparseableTOML: bad TOML → FAIL with parse error in detail.
func TestCheckConfig_unparseableTOML(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "invalid toml content [[[")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("detail is empty")
	}
}

// TestCheckConfig_unknownKey: TOML with unknown section → WARN listing the key.
func TestCheckConfig_unknownKey(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[unknown]\nfoo = \"bar\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "unknown") {
		t.Errorf("detail %q: want 'unknown'", r.Detail)
	}
}

// TestCheckConfig_invalidValue: known key, out-of-enum value → FAIL with Validate error.
func TestCheckConfig_invalidValue(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 0\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "max_depth") {
		t.Errorf("detail %q: want mention of 'max_depth'", r.Detail)
	}
}

// TestCheckConfig_resolvedMissing: valid TOML but no resolved.md → WARN about drift.
func TestCheckConfig_resolvedMissing(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")
	// deliberately do NOT write resolved.md

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "sync") {
		t.Errorf("detail %q: want mention of 'sync'", r.Detail)
	}
}

// TestCheckConfig_resolvedDrifted: valid TOML, resolved.md exists but has stale content → WARN.
func TestCheckConfig_resolvedDrifted(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")
	writeResolved(t, root, "# stale content that does not match rendered output\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "sync") {
		t.Errorf("detail %q: want mention of 'sync'", r.Detail)
	}
}

// TestCheckConfig_fixRerender: repair re-renders resolved.md and check goes PASS.
func TestCheckConfig_fixRerender(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")
	// resolved.md missing — should be WARN before fix
	before := doctor.RunCheckConfigWith(root)
	if before.Severity != doctor.WARN {
		t.Fatalf("pre-fix severity = %q, want WARN", before.Severity)
	}

	// Apply the repair.
	err := doctor.RunConfigRepairWith(root)
	if err != nil {
		t.Fatalf("repair returned error: %v", err)
	}

	// After repair, resolved.md must exist and check must PASS.
	after := doctor.RunCheckConfigWith(root)
	if after.Severity != doctor.PASS {
		t.Errorf("post-fix severity = %q, want PASS; detail: %s", after.Severity, after.Detail)
	}
}

// TestCheckConfig_fixRerenderDrifted: repair corrects drifted resolved.md.
func TestCheckConfig_fixRerenderDrifted(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")
	writeResolved(t, root, "# wrong content\n")

	before := doctor.RunCheckConfigWith(root)
	if before.Severity != doctor.WARN {
		t.Fatalf("pre-fix severity = %q, want WARN", before.Severity)
	}

	if err := doctor.RunConfigRepairWith(root); err != nil {
		t.Fatalf("repair: %v", err)
	}

	after := doctor.RunCheckConfigWith(root)
	if after.Severity != doctor.PASS {
		t.Errorf("post-fix severity = %q, want PASS; detail: %s", after.Severity, after.Detail)
	}
}

// TestCheckConfig_fixUnparseableCantFix: repair returns error for unparseable TOML.
func TestCheckConfig_fixUnparseableCantFix(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "invalid toml [[[")

	err := doctor.RunConfigRepairWith(root)
	if err == nil {
		t.Error("expected error repairing unparseable TOML, got nil")
	}
}

// TestRunConfigRepairWith_invalidValueRefuses: repair MUST NOT write to
// config.resolved.md when the TOML has an invalid value. The rendered content
// with an out-of-range max_depth would poison every Claude session via @-ref.
func TestRunConfigRepairWith_invalidValueRefuses(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 0\n")
	// Write a sentinel so we can verify the file was not overwritten.
	writeResolved(t, root, "original\n")

	err := doctor.RunConfigRepairWith(root)
	if err == nil {
		t.Error("expected error repairing invalid-value TOML, got nil")
	}

	// config.resolved.md must not have been touched.
	got, readErr := os.ReadFile(config.ResolvedPath(root))
	if readErr != nil {
		t.Fatalf("read config.resolved.md: %v", readErr)
	}
	if string(got) != "original\n" {
		t.Errorf("config.resolved.md was overwritten: got %q, want %q", string(got), "original\n")
	}
}

// TestCheckConfig_unknownKeyAndDrift_combinedWARN: when TOML has both unknown keys
// AND resolved.md is out of sync, RunCheckConfigWith must return a single WARN
// whose Detail mentions both issues — not bail after the first WARN.
func TestCheckConfig_unknownKeyAndDrift_combinedWARN(t *testing.T) {
	root := t.TempDir()
	// Unknown key in a known section + valid key, no resolved.md.
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\nfoo = \"bar\"\n")
	// Deliberately write a stale resolved.md so drift is also present.
	writeResolved(t, root, "# stale\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	// Detail must mention the unknown key.
	if !strings.Contains(r.Detail, "unknown") {
		t.Errorf("detail %q: want mention of 'unknown'", r.Detail)
	}
	// Detail must also mention sync/drift.
	if !strings.Contains(r.Detail, "sync") {
		t.Errorf("detail %q: want mention of 'sync'", r.Detail)
	}
}

// TestRepairPlan_configFAIL_notFixable: a config FAIL result must not be auto-fixable.
// Parse errors and invalid values cannot be repaired by the binary.
func TestRepairPlan_configFAIL_notFixable(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 0\n")

	// RunCheckConfigWith returns FAIL for invalid values.
	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Fatalf("precondition: severity = %q, want FAIL", r.Severity)
	}

	// Inject the name so repairPlan can look it up.
	r.Name = "config"

	// repairPlan is not exported; drive it through Repair with a fake prompter
	// that should never be called (because non-fixable items skip the prompt).
	var repairCalled bool
	rp := doctor.DefaultRepairer()
	rp.ConfigFn = func(_ string) error {
		repairCalled = true
		return nil
	}

	nopPrompter := &alwaysYesPrompter{}
	var buf strings.Builder
	// Pass the single FAIL result — Repair should report it as NonFixable.
	summary := rp.Repair([]doctor.Result{r}, doctor.Opts{}, nopPrompter, &buf)
	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1; output:\n%s", summary.NonFixable, buf.String())
	}
	if summary.Applied != 0 {
		t.Errorf("Applied = %d, want 0", summary.Applied)
	}
	if repairCalled {
		t.Error("repair function was called despite FAIL severity — must not attempt")
	}
}

// TestRepairPlan_configWARN_fixable: a config WARN result (drift) must be auto-fixable.
func TestRepairPlan_configWARN_fixable(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")
	// No resolved.md → WARN (drift).
	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Fatalf("precondition: severity = %q, want WARN", r.Severity)
	}
	r.Name = "config"

	var repairCalled bool
	rp := doctor.DefaultRepairer()
	// HomeFn must be overridden too: applyConfigRepair resolves home itself
	// before calling ConfigFn, so without this the test still hits the real
	// $HOME regardless of what ConfigFn does with the home arg it receives.
	rp.HomeFn = func() (string, error) { return root, nil }
	rp.ConfigFn = func(home string) error {
		repairCalled = true
		// Actually do the repair so the summary says Applied=1.
		return doctor.RunConfigRepairWith(home)
	}

	nopPrompter := &alwaysYesPrompter{}
	var buf strings.Builder
	summary := rp.Repair([]doctor.Result{r}, doctor.Opts{}, nopPrompter, &buf)
	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1; output:\n%s", summary.Applied, buf.String())
	}
	if !repairCalled {
		t.Error("repair function was not called for WARN severity — should be fixable")
	}
}

// TestCheckConfig_noInstallTable: config.toml without [install] is valid (pre-framework state).
func TestCheckConfig_noInstallTable(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 3\n")

	cfg, _, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (no [install] is valid pre-framework state); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckConfig_invalidInstallVersion: non-semver install.version → FAIL.
func TestCheckConfig_invalidInstallVersion(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[install]\nversion = \"not-a-semver\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL for invalid install.version; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "install.version") {
		t.Errorf("detail %q: want mention of 'install.version'", r.Detail)
	}
}

// TestCheckConfig_validInstallVersion: parseable install.version + in-sync resolved → PASS.
func TestCheckConfig_validInstallVersion(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, `[install]
version = "1.2.3"
[install.artifacts]
agents = ["atomic-implementer.md"]
commands = ["commit.md"]
skills = []
output-styles = []
rules = []
`)

	cfg, warns, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for valid install.version; detail: %s", r.Severity, r.Detail)
	}
}

// --- [claude.agents] doctor tests (CP2) ---

// TestCheckConfig_agents_invalidEffort: [claude.agents.<name>] with an invalid
// effort value → FAIL. effort is validated against a strict enum; model is
// lenient (see TestCheckConfig_agents_arbitraryModelNotFail).
func TestCheckConfig_agents_invalidEffort(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[claude.agents.atomic-implementer]\neffort = \"turbo\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL for invalid agent effort; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "atomic-implementer") {
		t.Errorf("detail %q: want mention of agent name 'atomic-implementer'", r.Detail)
	}
	if !strings.Contains(r.Detail, "effort") {
		t.Errorf("detail %q: want mention of 'effort'", r.Detail)
	}
}

// TestCheckConfig_agents_arbitraryModelNotFail: [claude.agents] model value
// is lenient — any well-formed string passes doctor's config check, since
// Claude Code (not atomic) resolves the model name.
func TestCheckConfig_agents_arbitraryModelNotFail(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[claude.agents.atomic-implementer]\nmodel = \"turbo\"\n")

	// Pre-render resolved.md so drift doesn't confound the severity.
	cfg, warns, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = warns
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = %q, want not-FAIL for arbitrary (lenient) model value; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckConfig_agents_unknownAgent: [claude.agents] with an unknown agent key → WARN, not FAIL.
func TestCheckConfig_agents_unknownAgent(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[claude.agents.made-up-agent]\nmodel = \"haiku\"\n")

	// Pre-render resolved.md so drift doesn't confound the severity.
	cfg, warns, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = warns
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN for unknown agent key; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "made-up-agent") {
		t.Errorf("detail %q: want mention of 'made-up-agent'", r.Detail)
	}
}

// TestCheckConfig_agents_valid: [claude.agents] with known agents + valid model/effort → PASS (when resolved synced).
func TestCheckConfig_agents_valid(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, `[claude.agents.atomic-implementer]
model = "sonnet"

[claude.agents.atomic-investigator]
model = "haiku"

[claude.agents.atomic-strategist]
model = "opus"
`)

	cfg, warns, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected structural warnings: %v", warns)
	}
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for valid [claude.agents]; detail: %s", r.Severity, r.Detail)
	}
}

// --- [repl] idle_timeout (CP4: atomic-repl) ---

// TestCheckConfig_invalidIdleTimeout: an unparseable repl.idle_timeout → FAIL
// naming the offending value.
func TestCheckConfig_invalidIdleTimeout(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[repl]\nidle_timeout = \"turbo\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL for invalid repl.idle_timeout; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "turbo") {
		t.Errorf("detail %q: want mention of the offending value 'turbo'", r.Detail)
	}
}

// TestCheckConfig_zeroIdleTimeout: a zero-duration repl.idle_timeout → FAIL —
// zero means invalid, never "disable".
func TestCheckConfig_zeroIdleTimeout(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[repl]\nidle_timeout = \"0s\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL for zero repl.idle_timeout; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckConfig_validIdleTimeout: a well-formed repl.idle_timeout with a
// synced resolved.md → PASS.
func TestCheckConfig_validIdleTimeout(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[repl]\nidle_timeout = \"45m\"\n")

	cfg, warns, err := config.Load(config.TOMLPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	writeResolved(t, root, config.Render(cfg))

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for valid repl.idle_timeout; detail: %s", r.Severity, r.Detail)
	}
}

// --- Chronic background-update-check failure (CP7: selfupdate-state) ---

// writeUpdateState writes a selfupdate.State to <root>/.atomic/state.json.
func writeUpdateState(t *testing.T, root string, s selfupdate.State) {
	t.Helper()
	makeAtomicDir(t, root)
	if err := selfupdate.WriteState(config.StatePath(root), s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
}

// TestCheckConfig_chronicUpdateFailure_WARN: a non-empty update.last_result
// (runUpdateCheck writes LastResult="" on success, lookupErr.Error() or
// stageErr.Error() on failure) surfaces as WARN naming the recorded failure
// text, even though config.toml itself is absent (otherwise PASS).
func TestCheckConfig_chronicUpdateFailure_WARN(t *testing.T) {
	root := t.TempDir()
	lastCheck := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	writeUpdateState(t, root, selfupdate.State{
		Update: selfupdate.UpdateState{
			LastCheck:  lastCheck,
			LastResult: "GET https://api.github.com/repos/x/y/releases/latest: 500 Internal Server Error",
		},
	})

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "500 Internal Server Error") {
		t.Errorf("detail %q: want mention of the recorded last_result error", r.Detail)
	}
}

// TestCheckConfig_chronicUpdateFailure_emptySuccess_noFinding: an empty
// update.last_result (runUpdateCheck writes LastResult="" on a successful
// check) is healthy and must not produce a finding; PASS is preserved.
func TestCheckConfig_chronicUpdateFailure_emptySuccess_noFinding(t *testing.T) {
	root := t.TempDir()
	writeUpdateState(t, root, selfupdate.State{
		Update: selfupdate.UpdateState{
			LastCheck:     time.Now(),
			LatestVersion: "1.2.3",
			LastResult:    "",
		},
	})

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (healthy update state); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckConfig_chronicUpdateFailure_missingStateFile_noFinding: no
// state.json at all (LoadState's zero-value contract) must not produce a
// finding — absence is not itself a failure signal.
func TestCheckConfig_chronicUpdateFailure_missingStateFile_noFinding(t *testing.T) {
	root := t.TempDir()
	// Deliberately do NOT write state.json.

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (no state.json); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckConfig_chronicUpdateFailure_neverEscalatesToFAIL: category
// severity ceiling for this finding is WARN, never FAIL — a chronic update
// failure alone (independent of config.toml validity) must not FAIL the
// category.
func TestCheckConfig_chronicUpdateFailure_neverEscalatesToFAIL(t *testing.T) {
	root := t.TempDir()
	writeUpdateState(t, root, selfupdate.State{
		Update: selfupdate.UpdateState{LastResult: "dial tcp: no such host"},
	})

	r := doctor.RunCheckConfigWith(root)
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = %q, want not-FAIL for a chronic update failure alone", r.Severity)
	}
}

// TestCheckConfig_chronicUpdateFailure_combinedWithDrift: a chronic update
// failure alongside an unrelated config.toml drift finding combines into a
// single WARN mentioning both, mirroring the existing unknown-key + drift
// combination behavior.
func TestCheckConfig_chronicUpdateFailure_combinedWithDrift(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 5\n")
	// deliberately do NOT write resolved.md -> drift WARN
	writeUpdateState(t, root, selfupdate.State{
		Update: selfupdate.UpdateState{LastResult: "connection refused"},
	})

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "sync") {
		t.Errorf("detail %q: want mention of config drift ('sync')", r.Detail)
	}
	if !strings.Contains(r.Detail, "connection refused") {
		t.Errorf("detail %q: want mention of the chronic update failure", r.Detail)
	}
}

// alwaysYesPrompter always returns DecisionYes for testing.
type alwaysYesPrompter struct{}

func (a *alwaysYesPrompter) Confirm(_ string) doctor.Decision { return doctor.DecisionYes }
func (a *alwaysYesPrompter) Indexed(_ []string) int           { return 1 }
