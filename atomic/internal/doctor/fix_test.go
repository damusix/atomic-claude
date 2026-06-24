package doctor_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// fakePrompter records prompts received and returns canned decisions.
type fakePrompter struct {
	decisions   []doctor.Decision
	nextIdx     int
	indexInputs []int // returned from Indexed calls in order
	nextIdxIdx  int
	// output accumulates anything written to the prompter's output writer.
}

func (f *fakePrompter) Confirm(prompt string) doctor.Decision {
	if f.nextIdx >= len(f.decisions) {
		return doctor.DecisionNo // default safe
	}
	d := f.decisions[f.nextIdx]
	f.nextIdx++
	return d
}

func (f *fakePrompter) Indexed(items []string) int {
	if f.nextIdxIdx >= len(f.indexInputs) {
		return 0 // cancel
	}
	i := f.indexInputs[f.nextIdxIdx]
	f.nextIdxIdx++
	return i
}

// makeResult builds a Result for a single named category with the given severity.
func makeResult(idx int, name string, sev doctor.Severity, detail string) doctor.Result {
	return doctor.Result{Index: idx, Name: name, Severity: sev, Detail: detail}
}

// nopRepairer returns a Repairer with all injectable functions stubbed to no-ops.
// Tests override only the fields they care about.
func nopRepairer() doctor.Repairer {
	return doctor.Repairer{
		InstallFn:         func(io.Writer) error { return nil },
		HooksFn:           func(io.Writer) error { return nil },
		ManifestFn:        func(io.Writer) error { return nil },
		FollowupsRenderFn: func(io.Writer) error { return nil },
		ConfigFn:          func(string) error { return nil },
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
	// DecisionYes: "y" and "Y"
	// DecisionNo: "" (default) and "n"
	// DecisionAll: "a"
	// DecisionQuit: "q"
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
	if !strings.Contains(output, "/refresh-signals") {
		t.Errorf("expected /refresh-signals instruction in output, got:\n%s", output)
	}
	// No prompt should be shown (non-fixable).
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
	// Print-before-run: synthetic command line present.
	if !strings.Contains(sb.String(), "atomic claude install --merge") {
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
	// "all" on first prompt → no second prompt needed.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionAll}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if !installCalled || !hooksCalled {
		t.Errorf("install=%v hooks=%v — both should have run on 'all'", installCalled, hooksCalled)
	}
	if summary.Applied != 2 {
		t.Errorf("Applied = %d, want 2", summary.Applied)
	}
	// Only one prompt should have been fired.
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

func TestRepair_Refs_NoExistingCandidates_DefaultsToClaudeMD(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// No Indexed call expected — defaults to CLAUDE.md.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "@.claude/project/signals.md") {
		t.Errorf("signals ref missing from CLAUDE.md")
	}
}

func TestRepair_Refs_OneCandidateExisting_SingleYesNo(t *testing.T) {
	dir := t.TempDir()
	// Write one existing candidate (CLAUDE.md with unrelated content).
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# existing content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// One existing candidate → user confirms at outer prompt; no inner Indexed needed.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(data), "@.claude/project/signals.md") {
		t.Errorf("signals ref not appended to existing CLAUDE.md")
	}
}

func TestRepair_Refs_MultipleCandidates_IndexedSelection(t *testing.T) {
	dir := t.TempDir()
	// Write two existing candidates.
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
	// Outer "apply?" prompt → Yes; then Indexed returns 2 → choose the second candidate.
	p := &fakePrompter{
		decisions:   []doctor.Decision{doctor.DecisionYes},
		indexInputs: []int{2},
	}
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}

	// Candidate iteration order is the fixed search order from checks_refs.go:
	// claude.local.md, CLAUDE.local.md, CLAUDE.md, claude.md. Of the two on disk
	// (claude.local.md and CLAUDE.md), existing[1]=claude.local.md, existing[2]=CLAUDE.md.
	// Index 2 -> CLAUDE.md. Assert exactly that file was patched.
	localData, _ := os.ReadFile(filepath.Join(dir, "claude.local.md"))
	globalData, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))

	localHasRef := strings.Contains(string(localData), "@.claude/project/signals.md")
	if localHasRef {
		t.Errorf("claude.local.md should not have been patched (index 2 maps to CLAUDE.md); content:\n%s", string(localData))
	}

	refCount := strings.Count(string(globalData), "@.claude/project/signals.md")
	if refCount != 1 {
		t.Errorf("CLAUDE.md: signals ref count=%d (want 1)\ncontent:\n%s", refCount, string(globalData))
	}
}

func TestRepair_Refs_Idempotent(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}

	// Run repair twice.
	for i := 0; i < 2; i++ {
		var sb strings.Builder
		p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
		rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	content := string(data)
	count := strings.Count(content, "@.claude/project/signals.md")
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

	refCount := strings.Count(content, "@.claude/project/signals.md")
	if refCount != 1 {
		t.Errorf("signals ref appears %d times (want 1)", refCount)
	}

	if !strings.Contains(content, "Some existing content.") {
		t.Errorf("existing content was lost")
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

	// 1 applied (install), 2 non-fixable (signals + binary)
	if summary.Applied != 1 || summary.NonFixable != 2 {
		t.Errorf("summary = %+v, want Applied=1 NonFixable=2", summary)
	}
	output := sb.String()
	if !strings.Contains(output, "1 repair") {
		t.Errorf("summary line missing '1 repair':\n%s", output)
	}
}

// -- DecisionAbort: Repair loop stops on Abort (Ctrl+C / huh ErrUserAborted) --

// TestRepair_DecisionAbort_stopsLoop verifies the Repair loop short-circuits
// on DecisionAbort, prints "Aborted", and counts remaining items as Skipped.
// WHY: silently treating Ctrl+C as "No" means a user trying to escape the
// entire repair loop gets only the current item skipped, not the whole loop.
//
// Part (a) — prompt.Confirm surfaces ErrAborted — is covered in the prompt
// package via stubbed runConfirm. Part (b) — stdinPrompter translates
// ErrAborted → DecisionAbort — has no direct unit test because the
// translation lives in the huh-path branch of stdin_prompter.Confirm and is
// reachable only with a real TTY. Tracked as a structural coverage gap.
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
	// First prompt returns Abort; second should never be reached.
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

// TestRepair_PrintsFixedOnSuccess verifies that after a successful repair
// the Repair loop prints "✓ fixed: <summary>" to the writer.
// WHY: the user needs inline feedback that a repair ran and what it did;
// FormatHuman is called before Repair in main.go so the Repair loop is the
// only output channel that reaches the user at repair time.
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
	// The loop prints "✓ fixed: <summary>" immediately after a successful repair.
	if !strings.Contains(output, "✓ fixed:") {
		t.Errorf("expected '✓ fixed:' in output after successful repair, got:\n%s", output)
	}
}

// -- manifest repair streams make output to writer (f-4) --

// TestRepair_Manifest_WriterReceivesMakeOutput verifies that the ManifestFn is called
// with the repair's io.Writer, so make's combined output flows to the caller.
// WHY: the old applyManifestRepair discarded output on success (CombinedOutput with no
// streaming) — the user had no visibility into what regenerated. The injected fn now
// receives the writer and can write known bytes to it.
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
