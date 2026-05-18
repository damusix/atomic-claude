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

// makeResultsFor builds a []Result for a single named category with the given severity.
func makeResult(idx int, name string, sev doctor.Severity, detail string) doctor.Result {
	return doctor.Result{Index: idx, Name: name, Severity: sev, Detail: detail}
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
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &out)

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
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &out)

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
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &out)

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
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &out)

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
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &out)

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
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.NonFixable != 0 {
		t.Errorf("NonFixable = %d, want 0 (SKIP results not counted)", summary.NonFixable)
	}
}

// -- install repair: stubbed mutation --

func TestRepair_Install_Yes(t *testing.T) {
	called := false
	doctor.SetInstallRepairFn(func(out io.Writer) error {
		called = true
		return nil
	})
	t.Cleanup(func() { doctor.SetInstallRepairFn(nil) })

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "2 files differ"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

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
	doctor.SetInstallRepairFn(func(out io.Writer) error {
		called = true
		return nil
	})
	t.Cleanup(func() { doctor.SetInstallRepairFn(nil) })

	results := []doctor.Result{
		makeResult(1, "install", doctor.FAIL, "missing files"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionNo}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

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
	doctor.SetInstallRepairFn(func(out io.Writer) error { installCalled = true; return nil })
	doctor.SetHooksRepairFn(func(out io.Writer) error { hooksCalled = true; return nil })
	t.Cleanup(func() {
		doctor.SetInstallRepairFn(nil)
		doctor.SetHooksRepairFn(nil)
	})

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(2, "hooks", doctor.WARN, "missing"),
	}
	var sb strings.Builder
	// "all" on first prompt → no second prompt needed.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionAll}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

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
	doctor.SetInstallRepairFn(func(out io.Writer) error { installCalled = true; return nil })
	doctor.SetHooksRepairFn(func(out io.Writer) error { hooksCalled = true; return nil })
	t.Cleanup(func() {
		doctor.SetInstallRepairFn(nil)
		doctor.SetHooksRepairFn(nil)
	})

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(2, "hooks", doctor.WARN, "missing"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionQuit}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

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
	doctor.SetHooksRepairFn(func(out io.Writer) error { called = true; return nil })
	t.Cleanup(func() { doctor.SetHooksRepairFn(nil) })

	results := []doctor.Result{
		makeResult(2, "hooks", doctor.WARN, "session-start hook missing"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

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
	doctor.SetManifestRepairFn(func(out io.Writer) error { called = true; return nil })
	t.Cleanup(func() { doctor.SetManifestRepairFn(nil) })

	results := []doctor.Result{
		makeResult(5, "manifest", doctor.FAIL, "manifest stale"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	opts := doctor.Opts{Fix: true}
	// Force isRepoDev = true via the injectable.
	doctor.SetIsRepoDevFn(func() (bool, error) { return true, nil })
	t.Cleanup(func() { doctor.SetIsRepoDevFn(nil) })

	summary := doctor.Repair(results, opts, p, &sb)

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
	doctor.SetManifestRepairFn(func(out io.Writer) error { called = true; return nil })
	t.Cleanup(func() { doctor.SetManifestRepairFn(nil) })

	results := []doctor.Result{
		makeResult(5, "manifest", doctor.FAIL, "manifest stale"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	doctor.SetIsRepoDevFn(func() (bool, error) { return false, nil })
	t.Cleanup(func() { doctor.SetIsRepoDevFn(nil) })

	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

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

	doctor.SetRepoRootFn(func() string { return dir })
	t.Cleanup(func() { doctor.SetRepoRootFn(nil) })

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// No Indexed call expected — defaults to CLAUDE.md.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1", summary.Applied)
	}
	// CLAUDE.md should now contain both refs.
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "@.claude/project/deterministic-signals.md") {
		t.Errorf("deterministic-signals ref missing from CLAUDE.md")
	}
	if !strings.Contains(content, "@.claude/project/inferred-signals.md") {
		t.Errorf("inferred-signals ref missing from CLAUDE.md")
	}
}

func TestRepair_Refs_OneCandidateExisting_SingleYesNo(t *testing.T) {
	dir := t.TempDir()
	// Write one existing candidate (CLAUDE.md with unrelated content).
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# existing content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doctor.SetRepoRootFn(func() string { return dir })
	t.Cleanup(func() { doctor.SetRepoRootFn(nil) })

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// One existing candidate → user confirms at outer prompt; no inner Indexed needed.
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(data), "@.claude/project/deterministic-signals.md") {
		t.Errorf("ref not appended to existing CLAUDE.md")
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

	doctor.SetRepoRootFn(func() string { return dir })
	t.Cleanup(func() { doctor.SetRepoRootFn(nil) })

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	// Outer "apply?" prompt → Yes; then Indexed returns 2 → choose the second candidate.
	p := &fakePrompter{
		decisions:   []doctor.Decision{doctor.DecisionYes},
		indexInputs: []int{2},
	}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}

	// Candidate iteration order is the fixed search order from checks_refs.go:
	// claude.local.md, CLAUDE.local.md, CLAUDE.md, claude.md. Of the two on disk
	// (claude.local.md and CLAUDE.md), existing[1]=claude.local.md, existing[2]=CLAUDE.md.
	// Index 2 -> CLAUDE.md. Assert exactly that file was patched.
	localData, _ := os.ReadFile(filepath.Join(dir, "claude.local.md"))
	globalData, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))

	localHasRefs := strings.Contains(string(localData), "@.claude/project/deterministic-signals.md") ||
		strings.Contains(string(localData), "@.claude/project/inferred-signals.md")
	if localHasRefs {
		t.Errorf("claude.local.md should not have been patched (index 2 maps to CLAUDE.md); content:\n%s", string(localData))
	}

	detCount := strings.Count(string(globalData), "@.claude/project/deterministic-signals.md")
	infCount := strings.Count(string(globalData), "@.claude/project/inferred-signals.md")
	if detCount != 1 || infCount != 1 {
		t.Errorf("CLAUDE.md: det=%d inf=%d (both want 1)\ncontent:\n%s", detCount, infCount, string(globalData))
	}
}

func TestRepair_Refs_Idempotent(t *testing.T) {
	dir := t.TempDir()

	doctor.SetRepoRootFn(func() string { return dir })
	t.Cleanup(func() { doctor.SetRepoRootFn(nil) })

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}

	// Run repair twice.
	for i := 0; i < 2; i++ {
		var sb strings.Builder
		p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
		doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	content := string(data)
	count := strings.Count(content, "@.claude/project/deterministic-signals.md")
	if count != 1 {
		t.Errorf("deterministic-signals ref appears %d times (want 1) — idempotency broken", count)
	}
}

func TestRepair_Refs_PartialFile_AppendsOnlyMissing(t *testing.T) {
	dir := t.TempDir()
	// CLAUDE.md already has the deterministic ref but is missing the inferred ref.
	initial := "# My project\n\n@.claude/project/deterministic-signals.md\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	doctor.SetRepoRootFn(func() string { return dir })
	t.Cleanup(func() { doctor.SetRepoRootFn(nil) })

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	if summary.Applied != 1 {
		t.Errorf("Applied = %d, want 1\noutput:\n%s", summary.Applied, sb.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	content := string(data)

	// The missing inferred ref must be present exactly once.
	infCount := strings.Count(content, "@.claude/project/inferred-signals.md")
	if infCount != 1 {
		t.Errorf("inferred-signals ref appears %d times (want 1)", infCount)
	}

	// The deterministic ref that was already there must still appear exactly once (no duplicate).
	detCount := strings.Count(content, "@.claude/project/deterministic-signals.md")
	if detCount != 1 {
		t.Errorf("deterministic-signals ref appears %d times (want 1) — partial-append introduced duplicate", detCount)
	}

	// No duplicate ## Project signals header should have been appended.
	headerCount := strings.Count(content, "## Project signals")
	if headerCount > 1 {
		t.Errorf("## Project signals header appears %d times (want ≤1) — partial-append duplicated header", headerCount)
	}
}

// -- summary line --

func TestRepair_SummaryLine(t *testing.T) {
	doctor.SetInstallRepairFn(func(out io.Writer) error { return nil })
	t.Cleanup(func() { doctor.SetInstallRepairFn(nil) })

	results := []doctor.Result{
		makeResult(1, "install", doctor.WARN, "drift"),
		makeResult(3, "signals", doctor.WARN, "stale"),
		makeResult(8, "binary", doctor.WARN, "outdated"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	summary := doctor.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	// 1 applied (install), 2 non-fixable (signals + binary)
	if summary.Applied != 1 || summary.NonFixable != 2 {
		t.Errorf("summary = %+v, want Applied=1 NonFixable=2", summary)
	}
	output := sb.String()
	if !strings.Contains(output, "1 repair") {
		t.Errorf("summary line missing '1 repair':\n%s", output)
	}
}
