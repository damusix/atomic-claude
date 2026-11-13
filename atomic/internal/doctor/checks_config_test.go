package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
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

func TestCheckConfig_noTOML(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("detail is empty")
	}
	if !strings.Contains(r.Detail, "defaults") {
		t.Errorf("detail %q: want mention of 'defaults'", r.Detail)
	}
}

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

func TestCheckConfig_retiredHarnessTableIsPass(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[harness]\ndir = \".pi\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

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

func TestCheckConfig_stateDirPath(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  doctor.Severity
	}{
		{"state/atomic", doctor.PASS},
		{"../escape", doctor.FAIL},
		{"/abs/path", doctor.FAIL},
	} {
		t.Run(tc.value, func(t *testing.T) {
			root := t.TempDir()
			writeTOML(t, root, "[state]\ndir = \""+tc.value+"\"\n")

			r := doctor.RunCheckConfigWith(root)
			if r.Severity != tc.want {
				t.Errorf("severity = %q, want %q; detail: %s", r.Severity, tc.want, r.Detail)
			}
		})
	}
}

func TestRepairPlan_configFAIL_notFixable(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 0\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Fatalf("precondition: severity = %q, want FAIL", r.Severity)
	}

	// repairPlan keys off Name and is unexported, so drive it through Repair.
	r.Name = "config"

	var repairCalled bool
	rp := doctor.DefaultRepairer()

	nopPrompter := &alwaysYesPrompter{}
	var buf strings.Builder
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

func TestCheckConfig_noInstallTable(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 3\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (no [install] is valid pre-framework state); detail: %s", r.Severity, r.Detail)
	}
}

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

	if _, warns, err := config.Load(config.TOMLPath(root)); err != nil {
		t.Fatalf("Load: %v", err)
	} else if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for valid install.version; detail: %s", r.Severity, r.Detail)
	}
}

// --- [claude.agents] doctor tests ---

// effort is a strict enum; model is lenient.
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

// Claude Code, not atomic, resolves the model name, so any well-formed string
// has to pass here.
func TestCheckConfig_agents_arbitraryModelNotFail(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[claude.agents.atomic-implementer]\nmodel = \"turbo\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = %q, want not-FAIL for arbitrary (lenient) model value; detail: %s", r.Severity, r.Detail)
	}
}

func TestCheckConfig_agents_unknownAgent(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[claude.agents.made-up-agent]\nmodel = \"haiku\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN for unknown agent key; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "made-up-agent") {
		t.Errorf("detail %q: want mention of 'made-up-agent'", r.Detail)
	}
}

func TestCheckConfig_agents_valid(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, `[claude.agents.atomic-implementer]
model = "sonnet"

[claude.agents.atomic-investigator]
model = "haiku"

[claude.agents.atomic-strategist]
model = "opus"
`)

	if _, warns, err := config.Load(config.TOMLPath(root)); err != nil {
		t.Fatalf("Load: %v", err)
	} else if len(warns) != 0 {
		t.Fatalf("unexpected structural warnings: %v", warns)
	}

	clearClaudeEnv(t)
	enrollKinds(t, root, "claude")
	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for valid [claude.agents]; detail: %s", r.Severity, r.Detail)
	}
}

func clearClaudeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "")
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
}

func enrollKinds(t *testing.T, home string, kinds ...string) {
	t.Helper()
	ledger := &installstate.Ledger{}
	for _, kind := range kinds {
		ledger.UpsertTarget(installstate.TargetRecord{Harness: kind, Instance: filepath.Join(home, "."+kind), NativeRoot: filepath.Join(home, "."+kind)})
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
}

// A stored override on a harness nothing delivers to would fail silently, so
// doctor names it.
func TestCheckConfig_agents_unenrolledHarnessWarns(t *testing.T) {
	root := t.TempDir()
	clearClaudeEnv(t)
	writeTOML(t, root, "[claude.agents.atomic-implementer]\nmodel = \"opus\"\n\n[omp.agents.atomic-implementer]\nmodel = \"@slow\"\n")
	enrollKinds(t, root, "claude")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "omp.agents") || !strings.Contains(r.Detail, "not enrolled") {
		t.Errorf("detail %q: want the unenrolled omp overrides named", r.Detail)
	}
	if strings.Contains(r.Detail, "claude.agents") {
		t.Errorf("detail %q: the enrolled claude overrides were flagged", r.Detail)
	}
}

// An enrolled OMP or Codex target receives the agent files, but no runtime
// probe has shown the harness honoring them, so a stored override is named.
func TestCheckConfig_agents_unsupportedDeliveryWarns(t *testing.T) {
	for _, kind := range []string{"omp", "codex"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			clearClaudeEnv(t)
			writeTOML(t, root, "["+kind+".agents.atomic-implementer]\nmodel = \"m\"\n")
			enrollKinds(t, root, kind)

			r := doctor.RunCheckConfigWith(root)
			want := kind + ".agents overrides are stored but " + kind + " agent delivery is unsupported"
			if r.Severity != doctor.WARN || !strings.Contains(r.Detail, want) {
				t.Errorf("severity %q detail %q: want a WARN saying %q", r.Severity, r.Detail, want)
			}
			if strings.Contains(r.Detail, "not enrolled") {
				t.Errorf("detail %q: the enrolled %s target was reported unenrolled", r.Detail, kind)
			}
		})
	}
}

// An unadopted legacy Claude install still applies [claude.agents] through
// the legacy installer, so it is not reported as undelivered.
func TestCheckConfig_agents_legacyClaudeInstallIsNotUnenrolled(t *testing.T) {
	root := t.TempDir()
	clearClaudeEnv(t)
	writeTOML(t, root, "[install]\nversion = \"7.0.0\"\n\n[claude.agents.atomic-implementer]\nmodel = \"opus\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// Claude Code's own variables outrank or fill in the agent file, so a set one
// beside a stored Claude override is named.
func TestCheckConfig_agents_claudeEnvironmentMaskWarns(t *testing.T) {
	for name, effect := range map[string]string{
		"CLAUDE_CODE_SUBAGENT_MODEL": "sets the model wherever claude.agents leaves it empty",
		"CLAUDE_CODE_EFFORT_LEVEL":   "outranks the stored claude.agents effort",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			clearClaudeEnv(t)
			t.Setenv(name, "high")
			writeTOML(t, root, "[claude.agents.atomic-implementer]\neffort = \"low\"\n")
			enrollKinds(t, root, "claude")

			r := doctor.RunCheckConfigWith(root)
			if r.Severity != doctor.WARN || !strings.Contains(r.Detail, name+" is set and "+effect) {
				t.Errorf("severity %q detail %q: want a WARN saying %s %s", r.Severity, r.Detail, name, effect)
			}
		})
	}
}

// `atomic config unset` refuses agent keys, so an agent warning points at
// `atomic config agents` and only an unknown key carries the unset hint.
func TestCheckConfig_agents_warningsPointAtConfigAgents(t *testing.T) {
	root := t.TempDir()
	clearClaudeEnv(t)
	writeTOML(t, root, "[claude.agents.made-up-agent]\nmodel = \"opus\"\n")
	enrollKinds(t, root, "claude")

	r := doctor.RunCheckConfigWith(root)
	if strings.Contains(r.Detail, "atomic config unset") || !strings.Contains(r.Detail, "atomic config agents") {
		t.Errorf("detail %q: want the agent warning pointed at `atomic config agents` only", r.Detail)
	}

	writeTOML(t, root, "[stray]\nx = 1\n")
	if r := doctor.RunCheckConfigWith(root); !strings.Contains(r.Detail, "atomic config unset") {
		t.Errorf("detail %q: an unknown key lost its unset hint", r.Detail)
	}
}

// Each harness has its own form, so the hint opens the form of the harness the
// warning is about.
func TestCheckConfig_agents_hintNamesTheWarningsHarness(t *testing.T) {
	root := t.TempDir()
	clearClaudeEnv(t)
	writeTOML(t, root, "[claude.agents.made-up-agent]\nmodel = \"opus\"\n\n[omp.agents.atomic-reviewer]\neffort = \"turbo\"\n")
	enrollKinds(t, root, "claude", "omp")

	r := doctor.RunCheckConfigWith(root)
	for _, want := range []string{
		"config: claude.agents.made-up-agent: unknown agent (not in installed set); override stored but agent must exist at apply time — run `atomic config agents --harness claude` to edit them",
		"config: omp.agents.atomic-reviewer.effort: unrecognized effort \"turbo\"; passed through as-is — run `atomic config agents --harness omp` to edit them",
	} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail %q\nwant %q", r.Detail, want)
		}
	}
}

// `atomic config unset` refuses agent keys, so its hint must not read as the
// fix for an inert override.
func TestCheckConfig_agents_inertDetailPrecedesTheUnsetHint(t *testing.T) {
	root := t.TempDir()
	clearClaudeEnv(t)
	writeTOML(t, root, "[omp.agents.atomic-implementer]\nmodel = \"@slow\"\n\n[stray]\nx = 1\n")

	r := doctor.RunCheckConfigWith(root)
	inert := strings.Index(r.Detail, "omp.agents overrides are stored")
	hint := strings.Index(r.Detail, "atomic config unset")
	if inert < 0 || hint < 0 || inert > hint {
		t.Errorf("detail %q: want the inert override before the unset hint", r.Detail)
	}
}

func TestCheckConfig_agents_claudeEnvironmentWithoutOverrideIsPass(t *testing.T) {
	root := t.TempDir()
	clearClaudeEnv(t)
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "opus")
	writeTOML(t, root, "[update]\ncheck = false\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// --- [repl] idle_timeout ---

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

// Zero means invalid, never "disable".
func TestCheckConfig_zeroIdleTimeout(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[repl]\nidle_timeout = \"0s\"\n")

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL for zero repl.idle_timeout; detail: %s", r.Severity, r.Detail)
	}
}

func TestCheckConfig_validIdleTimeout(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[repl]\nidle_timeout = \"45m\"\n")

	if _, warns, err := config.Load(config.TOMLPath(root)); err != nil {
		t.Fatalf("Load: %v", err)
	} else if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for valid repl.idle_timeout; detail: %s", r.Severity, r.Detail)
	}
}

// --- Chronic background-update-check failure ---

// writeUpdateState writes a selfupdate.State to <root>/.atomic/state.json.
func writeUpdateState(t *testing.T, root string, s selfupdate.State) {
	t.Helper()
	makeAtomicDir(t, root)
	if err := selfupdate.WriteState(config.StatePath(root), s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
}

// A non-empty last_result is how the background check records a failure, and
// it must surface even with config.toml absent (which alone would PASS).
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

// An empty last_result is what a successful check writes, so it is healthy.
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

// An absent state file is not itself a failure signal.
func TestCheckConfig_chronicUpdateFailure_missingStateFile_noFinding(t *testing.T) {
	root := t.TempDir()
	// Deliberately no state.json.

	r := doctor.RunCheckConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (no state.json); detail: %s", r.Severity, r.Detail)
	}
}

// This finding's severity ceiling is WARN: a failing background check is not
// a config validity problem.
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

// alwaysYesPrompter approves every prompt, so a test that reaches the prompt
// path fails loudly on the assertion rather than on an unanswered question.
type alwaysYesPrompter struct{}

func (a *alwaysYesPrompter) Confirm(_ string) doctor.Decision { return doctor.DecisionYes }
func (a *alwaysYesPrompter) Indexed(_ []string) int           { return 1 }
