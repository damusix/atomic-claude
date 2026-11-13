package doctor_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// fakePrompter records prompts received and returns canned decisions.
type fakePrompter struct {
	decisions   []doctor.Decision
	nextIdx     int
	indexInputs []int // returned from Indexed calls in order
	nextIdxIdx  int
}

func (f *fakePrompter) Confirm(prompt string) doctor.Decision {
	if f.nextIdx >= len(f.decisions) {
		return doctor.DecisionNo
	}
	d := f.decisions[f.nextIdx]
	f.nextIdx++
	return d
}

func (f *fakePrompter) Indexed(items []string) int {
	if f.nextIdxIdx >= len(f.indexInputs) {
		return 0
	}
	i := f.indexInputs[f.nextIdxIdx]
	f.nextIdxIdx++
	return i
}

func makeResult(idx int, name string, sev doctor.Severity, detail string) doctor.Result {
	return doctor.Result{Index: idx, Name: name, Severity: sev, Detail: detail}
}

// nopRepairer stubs every injectable function; tests override what they need.
func nopRepairer() doctor.Repairer {
	return doctor.Repairer{
		InstallFn:         func(io.Writer) error { return nil },
		HooksFn:           func(io.Writer) error { return nil },
		ManifestFn:        func(io.Writer) error { return nil },
		FollowupsRenderFn: func(io.Writer) error { return nil },
		OutputStyleFn:     func(io.Writer) error { return nil },
		ConvergeFn:        func(string, io.Writer) error { return nil },
		HomeFn:            func() (string, error) { return os.TempDir(), nil },
		IsRepoDevFn:       func() (bool, error) { return true, nil },
		RepoRootFn:        func() string { return os.TempDir() },
	}
}

// -- tests for the RepairSummary type --

func TestRepairSummaryFields(t *testing.T) {
	s := doctor.RepairSummary{Applied: 2, Skipped: 1, NonFixable: 3}
	if s.Applied != 2 || s.Skipped != 1 || s.NonFixable != 3 {
		t.Errorf("RepairSummary fields: got %+v", s)
	}
}

// -- prompter parsing tests --

func TestStdinPrompterParsing(t *testing.T) {
	cases := []struct {
		input string
		want  doctor.Decision
	}{
		{"y", doctor.DecisionYes},
		{"Y", doctor.DecisionYes},
		{"n", doctor.DecisionNo},
		{"N", doctor.DecisionNo},
		{"", doctor.DecisionNo},
		{"a", doctor.DecisionAll},
		{"A", doctor.DecisionAll},
		{"q", doctor.DecisionQuit},
		{"Q", doctor.DecisionQuit},
		{"garbage", doctor.DecisionNo},
	}
	for _, tc := range cases {
		r := strings.NewReader(tc.input + "\n")
		p := doctor.NewStdinPrompter(r, io.Discard)
		got := p.Confirm("prompt?")
		if got != tc.want {
			t.Errorf("input=%q: got %v, want %v", tc.input, got, tc.want)
		}
	}
}

// -- non-fixable repairs: signals, followups, memory, binary --

func TestRepair_Signals_NonFixable(t *testing.T) {
	results := []doctor.Result{
		makeResult(3, "signals", doctor.WARN, "signals stale"),
	}
	var out strings.Builder
	p := &fakePrompter{}
	rp := nopRepairer()
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	if summary.Applied != 0 {
		t.Errorf("Applied = %d, want 0", summary.Applied)
	}
	output := out.String()
	if !strings.Contains(output, "/refresh-wiki") {
		t.Errorf("expected /refresh-wiki instruction in output, got:\n%s", output)
	}
	if p.nextIdx != 0 {
		t.Errorf("Confirm called %d times, want 0", p.nextIdx)
	}
}

func TestRepair_Followups_NonFixable(t *testing.T) {
	results := []doctor.Result{
		makeResult(6, "followups", doctor.WARN, "1 entries malformed: F-1"),
	}
	var out strings.Builder
	p := &fakePrompter{}
	rp := nopRepairer()
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	output := out.String()
	if !strings.Contains(output, "cannot auto-fix") {
		t.Errorf("expected 'cannot auto-fix' in output, got:\n%s", output)
	}
}

func TestRepair_Memory_NonFixable(t *testing.T) {
	results := []doctor.Result{
		makeResult(7, "memory", doctor.WARN, "1 orphan refs: foo.md"),
	}
	var out strings.Builder
	p := &fakePrompter{}
	rp := nopRepairer()
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	output := out.String()
	if !strings.Contains(output, "cannot auto-fix") {
		t.Errorf("expected 'cannot auto-fix' in output, got:\n%s", output)
	}
}

func TestRepair_Binary_NonFixable(t *testing.T) {
	results := []doctor.Result{
		makeResult(8, "binary", doctor.WARN, "v0.4.1 < v0.4.2 available"),
	}
	var out strings.Builder
	p := &fakePrompter{}
	rp := nopRepairer()
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	output := out.String()
	if !strings.Contains(output, "atomic update") {
		t.Errorf("expected 'atomic update' instruction in output, got:\n%s", output)
	}
}

// -- output-style repair --

func TestRepair_OutputStyle_Yes(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.OutputStyleFn = func(out io.Writer) error { called = true; return nil }

	results := []doctor.Result{
		makeResult(14, "output-style", doctor.WARN, "output style not set at user level"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if !called {
		t.Error("output-style repair fn not called on Yes")
	}
	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
}

func TestRepair_OutputStyle_SeedDisabled_NonFixable(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.OutputStyleFn = func(out io.Writer) error { called = true; return nil }

	results := []doctor.Result{
		makeResult(14, "output-style", doctor.WARN,
			"output style not set at user level; seeding is disabled (output_style.seed = false)"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if called {
		t.Error("output-style repair fn called despite a non-fixable WARN")
	}
	if summary.Applied != 0 {
		t.Errorf("Applied = %d, want 0", summary.Applied)
	}
	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	if strings.Contains(sb.String(), "✓ fixed") {
		t.Errorf("output claims fixed, got:\n%s", sb.String())
	}
}

func TestRepair_OutputStyle_StyleFileMissing_NonFixable(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.OutputStyleFn = func(out io.Writer) error { called = true; return nil }

	results := []doctor.Result{
		makeResult(14, "output-style", doctor.WARN,
			`output style set to "Atomic" but output-styles/atomic.md is not installed`),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if called {
		t.Error("output-style repair fn called for a missing-style-file WARN")
	}
	if summary.Applied != 0 {
		t.Errorf("Applied = %d, want 0", summary.Applied)
	}
	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	if strings.Contains(sb.String(), "✓ fixed") {
		t.Errorf("output claims fixed, got:\n%s", sb.String())
	}
}

// A converge cannot seed a style the user disabled, so a scoped seed-disabled
// WARN must show the config guidance instead of reporting a repair.
func TestRepair_OutputStyle_SeedDisabledScoped_NonFixable(t *testing.T) {
	converged := false
	rp := nopRepairer()
	rp.ConvergeFn = func(string, io.Writer) error { converged = true; return nil }

	results := []doctor.Result{{
		Index:    14,
		Name:     "output-style",
		Severity: doctor.WARN,
		Detail:   "output style not set at user level; seeding is disabled (output_style.seed = false)",
		Scopes:   []string{"/relocated/.claude"},
	}}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if converged {
		t.Error("scoped converge ran for a seed-disabled output-style WARN")
	}
	if summary.Applied != 0 || summary.NonFixable != 1 {
		t.Errorf("Applied/NonFixable = %d/%d, want 0/1", summary.Applied, summary.NonFixable)
	}
	if !strings.Contains(sb.String(), "atomic config set output_style.seed true") {
		t.Errorf("the seed-disabled config guidance is missing, got:\n%s", sb.String())
	}
}

// -- PASS results are skipped entirely --

func TestRepair_SkipsPassResults(t *testing.T) {
	results := []doctor.Result{
		makeResult(1, "install", doctor.PASS, "all good"),
		makeResult(3, "signals", doctor.PASS, "fresh"),
	}
	var out strings.Builder
	p := &fakePrompter{}
	rp := nopRepairer()
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.Applied+summary.Skipped+summary.NonFixable != 0 {
		t.Errorf("expected all-zero summary for PASS results, got %+v", summary)
	}
}

// -- SKIP results are also skipped --

func TestRepair_SkipsSkipResults(t *testing.T) {
	results := []doctor.Result{
		makeResult(5, "manifest", doctor.SKIP, "not in atomic-claude repo"),
	}
	var out strings.Builder
	p := &fakePrompter{}
	rp := nopRepairer()
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.NonFixable != 0 {
		t.Errorf("NonFixable = %d, want 0 (SKIP results not counted)", summary.NonFixable)
	}
}

// -- install repair: struct injection --

func TestRepair_Install_Yes(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error {
		called = true
		return nil
	}

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "2 files differ"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if !called {
		t.Error("install repair fn not called on Yes")
	}
	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
	// The command line is printed before it runs.
	if !strings.Contains(sb.String(), "run `atomic claude install` to re-sync") {
		t.Errorf("print-before-run missing in output:\n%s", sb.String())
	}
}

func TestRepair_Install_No(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error {
		called = true
		return nil
	}

	results := []doctor.Result{
		makeResult(1, "install", doctor.FAIL, "missing files"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionNo}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if called {
		t.Error("install repair fn called on No")
	}
	if summary.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", summary.Skipped)
	}
}

func TestRepair_Install_All_RunsRemainingWithoutPrompt(t *testing.T) {
	installCalled := false
	hooksCalled := false
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error { installCalled = true; return nil }
	rp.HooksFn = func(out io.Writer) error { hooksCalled = true; return nil }

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(2, "hooks", doctor.WARN, "missing"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionAll}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if !installCalled || !hooksCalled {
		t.Errorf("install=%v hooks=%v — both should have run on 'all'", installCalled, hooksCalled)
	}
	if summary.Applied != 2 {
		t.Errorf("Applied = %d, want 2", summary.Applied)
	}
	if p.nextIdx != 1 {
		t.Errorf("Confirm called %d times, want 1", p.nextIdx)
	}
}

func TestRepair_Quit_StopsRemaining(t *testing.T) {
	installCalled := false
	hooksCalled := false
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error { installCalled = true; return nil }
	rp.HooksFn = func(out io.Writer) error { hooksCalled = true; return nil }

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(2, "hooks", doctor.WARN, "missing"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionQuit}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if installCalled || hooksCalled {
		t.Error("no repair should run after Quit")
	}
	if summary.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", summary.Skipped)
	}
}

// -- hooks repair --

func TestRepair_Hooks_Yes(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.HooksFn = func(out io.Writer) error { called = true; return nil }

	results := []doctor.Result{
		makeResult(2, "hooks", doctor.WARN, "session-start hook missing"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if !called {
		t.Error("hooks repair fn not called on Yes")
	}
	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
	if !strings.Contains(sb.String(), "atomic hooks install") {
		t.Errorf("print-before-run missing:\n%s", sb.String())
	}
}

// -- manifest repair --

func TestRepair_Manifest_Yes_InRepoDev(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.ManifestFn = func(out io.Writer) error { called = true; return nil }
	rp.IsRepoDevFn = func() (bool, error) { return true, nil }

	results := []doctor.Result{
		makeResult(5, "manifest", doctor.FAIL, "manifest stale"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if !called {
		t.Error("manifest repair fn not called on Yes (repo-dev)")
	}
	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
	if !strings.Contains(sb.String(), "make -C atomic bundle") {
		t.Errorf("print-before-run missing:\n%s", sb.String())
	}
}

func TestRepair_Manifest_RefusesOutsideRepoDev(t *testing.T) {
	called := false
	rp := nopRepairer()
	rp.ManifestFn = func(out io.Writer) error { called = true; return nil }
	rp.IsRepoDevFn = func() (bool, error) { return false, nil }

	results := []doctor.Result{
		makeResult(5, "manifest", doctor.FAIL, "manifest stale"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if called {
		t.Error("manifest repair fn called outside repo-dev — should refuse")
	}
	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1 (refused outside repo-dev)", summary.NonFixable)
	}
}

// -- refs repair --

func writeSteeringMode(t *testing.T, root, mode string) {
	t.Helper()
	path := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[steering]\nclaude = \""+mode+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repairNoRefs(t *testing.T, dir string) {
	t.Helper()
	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return t.TempDir(), nil }
	rp.RepoRootFn = func() string { return dir }
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	rp.Repair([]doctor.Result{makeResult(4, "refs", doctor.FAIL, "refs not present")}, doctor.Opts{Fix: true}, p, &sb)
}

// agents-md writes no loader: a CLAUDE.md beside AGENTS.md makes Claude skip it.
func TestRepair_Refs_NoExistingCandidates_AgentsMdModeWritesAgentsMdOnly(t *testing.T) {
	dir := t.TempDir()
	writeSteeringMode(t, dir, "agents-md")
	repairNoRefs(t, dir)

	data, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(data), "@docs/wiki/index.md") {
		t.Errorf("ref missing from AGENTS.md:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("agents-md mode wrote CLAUDE.md: %v", err)
	}
	if r := doctor.RunCheckRefsWith(dir); r.Severity != doctor.PASS {
		t.Errorf("refs = %q (%s), want PASS for a ref wired in AGENTS.md", r.Severity, r.Detail)
	}
}

func TestRepair_Refs_NoExistingCandidates_ClaudeMdModeWritesClaudeMdOnly(t *testing.T) {
	dir := t.TempDir()
	writeSteeringMode(t, dir, "claude-md")
	repairNoRefs(t, dir)

	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(data), "@docs/wiki/index.md") {
		t.Errorf("ref missing from CLAUDE.md:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("claude-md mode wrote AGENTS.md: %v", err)
	}
}

func TestRepair_Refs_NoExistingCandidates_LoaderModeWiresAgentsMdBehindLoader(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return t.TempDir(), nil }
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// The prompter offers no Indexed answer: with no candidate there is
	// nothing to pick from.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if !strings.Contains(string(data), "@docs/wiki/index.md") {
		t.Errorf("signals ref missing from AGENTS.md")
	}
	// The bracketed document, not a bare import: a later mode conversion
	// recognises only that shape as Atomic's own loader.
	loader, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !bytes.Equal(loader, bundlespec.ScopeSteering.LoaderDocument()) {
		t.Errorf("loader = %q, want %q", loader, bundlespec.ScopeSteering.LoaderDocument())
	}
	if r := doctor.RunCheckRefsWith(dir); r.Severity != doctor.PASS {
		t.Errorf("after repair refs = %q (%s), want PASS", r.Severity, r.Detail)
	}
}

// A ref in the owned loader keeps CLAUDE.md beside AGENTS.md after a switch to
// agents-md, so Claude stops reading AGENTS.md; the repair offers only AGENTS.md.
func TestRepair_Refs_LoaderMode_ExcludesOwnedLoaderFromCandidates(t *testing.T) {
	dir := t.TempDir()
	loaderDoc := bundlespec.ScopeSteering.LoaderDocument()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), loaderDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	repairNoRefs(t, dir)

	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	loader, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(agents), "@docs/wiki/index.md") {
		t.Errorf("ref missing from AGENTS.md:\n%s", agents)
	}
	if !bytes.Equal(loader, loaderDoc) {
		t.Errorf("owned loader was modified:\n%s", loader)
	}
}

// A user-authored CLAUDE.md that does not import AGENTS.md hides a ref left in
// AGENTS.md, so the repair adds the import (Step 8 of the wiki pipeline).
func TestRepair_Refs_LoaderMode_AddsImportToNonImportingClaudeMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return t.TempDir(), nil }
	rp.RepoRootFn = func() string { return dir }
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}, indexInputs: []int{1}}
	rp.Repair([]doctor.Result{makeResult(4, "refs", doctor.FAIL, "refs not present")}, doctor.Opts{Fix: true}, p, &sb)

	if r := doctor.RunCheckRefsWith(dir); r.Severity != doctor.PASS {
		t.Errorf("after repair refs = %q (%s), want PASS", r.Severity, r.Detail)
	}
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.HasPrefix(string(claude), "# mine\n") || !strings.Contains(string(claude), "@AGENTS.md") {
		t.Errorf("CLAUDE.md = %q, want user text kept and the import appended", claude)
	}
}

func TestRepair_Refs_OneCandidateExisting_SingleYesNo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# existing content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// One candidate needs only the outer confirm, no Indexed answer.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(data), "@docs/wiki/index.md") {
		t.Errorf("signals ref not appended to existing CLAUDE.md")
	}
}

func TestRepair_Refs_MultipleCandidates_IndexedSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude.local.md"), []byte("# local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# global\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	p := &fakePrompter{
		decisions:   []doctor.Decision{doctor.DecisionYes},
		indexInputs: []int{2},
	}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}

	// Candidates keep candidateFiles order, so of the two on disk index 1 is
	// claude.local.md and index 2 is CLAUDE.md.
	localData, _ := os.ReadFile(filepath.Join(dir, "claude.local.md"))
	globalData, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))

	localHasRef := strings.Contains(string(localData), "@docs/wiki/index.md")
	if localHasRef {
		t.Errorf("claude.local.md should not have been patched (index 2 maps to CLAUDE.md); content:\n%s", string(localData))
	}

	refCount := strings.Count(string(globalData), "@docs/wiki/index.md")
	if refCount != 1 {
		t.Errorf("CLAUDE.md: signals ref count=%d (want 1)\ncontent:\n%s", refCount, string(globalData))
	}
}

// The wiki pipeline wires the shared AGENTS.md ahead of its CLAUDE.md loader,
// so the repair offers it first.
func TestRepair_Refs_AgentsMdAndClaudeMd_OffersAgentsMdFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("@AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }
	var sb strings.Builder
	p := &fakePrompter{
		decisions:   []doctor.Decision{doctor.DecisionYes},
		indexInputs: []int{1},
	}
	summary := rp.Repair([]doctor.Result{makeResult(4, "refs", doctor.FAIL, "refs not present")}, doctor.Opts{Fix: true}, p, &sb)
	if summary.Applied != 1 {
		t.Fatalf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}

	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	loader, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(agents), "@docs/wiki/index.md") {
		t.Errorf("index 1 should patch AGENTS.md; content:\n%s", agents)
	}
	if string(loader) != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md loader changed:\n%s", loader)
	}
	if r := doctor.RunCheckRefsWith(dir); r.Severity != doctor.PASS {
		t.Errorf("after repair refs = %q (%s), want PASS", r.Severity, r.Detail)
	}
}

func TestRepair_Refs_Idempotent(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return t.TempDir(), nil }
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}

	for i := 0; i < 2; i++ {
		var sb strings.Builder
		p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
		rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)
	}

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not found: %v", err)
	}
	content := string(data)
	count := strings.Count(content, "@docs/wiki/index.md")
	if count != 1 {
		t.Errorf("signals ref appears %d times (want 1) — idempotency broken", count)
	}
}

func TestRepair_Refs_ExistingContent_AppendsRef(t *testing.T) {
	dir := t.TempDir()
	initial := "# My project\n\nSome existing content.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "ref not present"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	content := string(data)

	refCount := strings.Count(content, "@docs/wiki/index.md")
	if refCount != 1 {
		t.Errorf("signals ref appears %d times (want 1)", refCount)
	}

	if !strings.Contains(content, "Some existing content.") {
		t.Errorf("existing content was lost")
	}
}

// Pins the user-visible section title so a rename cannot regress it silently.
func TestRepair_Refs_HeadingIsProjectWiki(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return t.TempDir(), nil }
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Project wiki (auto-loaded)") {
		t.Errorf("refsBlock heading should be '## Project wiki (auto-loaded)'; content:\n%s", content)
	}
	if strings.Contains(content, "## Project signals") {
		t.Errorf("refsBlock must not use old 'Project signals' heading; content:\n%s", content)
	}
}

// -- summary line --

func TestRepair_SummaryLine(t *testing.T) {
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error { return nil }

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(3, "signals", doctor.WARN, "stale"),
		makeResult(8, "binary", doctor.WARN, "outdated"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 || summary.NonFixable != 2 {
		t.Errorf("summary = %+v, want Applied=1 NonFixable=2", summary)
	}
	output := sb.String()
	if !strings.Contains(output, "1 repair") {
		t.Errorf("summary line missing '1 repair':\n%s", output)
	}
}

// -- DecisionAbort --

// Treating Ctrl+C as "No" would skip only the current item when the user is
// trying to escape the whole loop.
//
// Coverage gap: stdinPrompter's own ErrAborted → DecisionAbort translation
// lives in the huh branch of Confirm and is reachable only with a real TTY.
func TestRepair_DecisionAbort_stopsLoop(t *testing.T) {
	installCalled := false
	hooksCalled := false
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error { installCalled = true; return nil }
	rp.HooksFn = func(out io.Writer) error { hooksCalled = true; return nil }

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(2, "hooks", doctor.WARN, "missing"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionAbort}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if installCalled || hooksCalled {
		t.Error("no repair should run after DecisionAbort")
	}
	if summary.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (Abort stops all remaining)", summary.Skipped)
	}
	if p.nextIdx != 1 {
		t.Errorf("Confirm called %d times, want 1", p.nextIdx)
	}
	output := sb.String()
	if !strings.Contains(output, "Aborted") {
		t.Errorf("expected 'Aborted' in output, got:\n%s", output)
	}
}

// FormatHuman runs before Repair, so this loop is the only channel that can
// tell the user a repair ran.
func TestRepair_PrintsFixedOnSuccess(t *testing.T) {
	rp := nopRepairer()
	rp.InstallFn = func(out io.Writer) error { return nil }

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "2 files differ"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Fatalf("Applied = %d, want 1", summary.Applied)
	}
	output := sb.String()
	if !strings.Contains(output, "✓ fixed:") {
		t.Errorf("expected '✓ fixed:' in output after successful repair, got:\n%s", output)
	}
}

// -- manifest repair streams make output --

// ManifestFn must receive the repair's writer, or the user sees nothing of
// what make regenerated.
func TestRepair_Manifest_WriterReceivesMakeOutput(t *testing.T) {
	const fakeOutput = "FAKE MAKE OUTPUT SENTINEL"
	rp := nopRepairer()
	rp.IsRepoDevFn = func() (bool, error) { return true, nil }
	rp.ManifestFn = func(out io.Writer) error {
		_, err := io.WriteString(out, fakeOutput)
		return err
	}

	results := []doctor.Result{
		makeResult(5, "manifest", doctor.FAIL, "manifest stale"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Fatalf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}
	if !strings.Contains(sb.String(), fakeOutput) {
		t.Errorf("writer did not receive make output; got:\n%s", sb.String())
	}
}

// The project-local override stays the first offer, ahead of the mode's file:
// it is the file the wiki pipeline prefers when it exists.
func TestRepair_Refs_OverrideFileStaysFirstAheadOfModeFile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"claude.local.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return t.TempDir(), nil }
	rp.RepoRootFn = func() string { return dir }
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}, indexInputs: []int{1}}
	rp.Repair([]doctor.Result{makeResult(4, "refs", doctor.FAIL, "refs not present")}, doctor.Opts{Fix: true}, p, &sb)

	local, _ := os.ReadFile(filepath.Join(dir, "claude.local.md"))
	if !strings.Contains(string(local), "@docs/wiki/index.md") {
		t.Errorf("index 1 should patch claude.local.md; content:\n%s", local)
	}
}
