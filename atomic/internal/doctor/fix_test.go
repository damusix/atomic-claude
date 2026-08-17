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

func TestRepair_Refs_NoExistingCandidates_DefaultsToClaudeMD(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
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
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "@docs/wiki/index.md") {
		t.Errorf("signals ref missing from CLAUDE.md")
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

func TestRepair_Refs_Idempotent(t *testing.T) {
	dir := t.TempDir()

	rp := nopRepairer()
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}

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
	rp.RepoRootFn = func() string { return dir }

	results := []doctor.Result{
		makeResult(4, "refs", doctor.FAIL, "refs not present"),
	}
	var sb strings.Builder
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes}}
	rp.Repair(results, doctor.Opts{Fix: true}, p, &sb)

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
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
