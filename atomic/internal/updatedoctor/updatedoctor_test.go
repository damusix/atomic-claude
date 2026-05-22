package updatedoctor

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestCategoryIndices asserts that the doctor registry's Index==3 entry is
// "signals" and Index==8 is "binary". If the registry reorders, this test
// breaks before Skip indices drift silently.
func TestCategoryIndices(t *testing.T) {
	cats := doctor.Categories()
	found := map[int]string{}
	for _, c := range cats {
		found[c.Index] = c.Name
	}
	if found[3] != "signals" {
		t.Errorf("doctor.Categories(): index 3 = %q, want \"signals\"", found[3])
	}
	if found[8] != "binary" {
		t.Errorf("doctor.Categories(): index 8 = %q, want \"binary\"", found[8])
	}
}

// stubResults returns a RunDoctorFn that yields fixed results and no error.
func stubResults(results []doctor.Result) RunDoctorFn {
	return func(opts doctor.Opts) ([]doctor.Result, error) {
		return results, nil
	}
}

// stubError returns a RunDoctorFn that yields an error.
func stubError(err error) RunDoctorFn {
	return func(opts doctor.Opts) ([]doctor.Result, error) {
		return nil, err
	}
}

// stubPanic returns a RunDoctorFn that panics.
func stubPanic(msg string) RunDoctorFn {
	return func(opts doctor.Opts) ([]doctor.Result, error) {
		panic(msg)
	}
}

// TestRunAllPass: all-PASS results → no output.
func TestRunAllPass(t *testing.T) {
	results := []doctor.Result{
		{Index: 1, Name: "install", Severity: doctor.PASS, Detail: "ok"},
		{Index: 2, Name: "hooks", Severity: doctor.PASS, Detail: "ok"},
	}
	var buf bytes.Buffer
	Run(stubResults(results), &buf)
	if buf.Len() != 0 {
		t.Errorf("all-PASS should produce no output, got: %q", buf.String())
	}
}

// TestRunWarnOnly: WARN-only results → no output (WARN suppressed at post-update surface).
func TestRunWarnOnly(t *testing.T) {
	results := []doctor.Result{
		{Index: 1, Name: "install", Severity: doctor.WARN, Detail: "stale"},
	}
	var buf bytes.Buffer
	Run(stubResults(results), &buf)
	if buf.Len() != 0 {
		t.Errorf("WARN-only should produce no output, got: %q", buf.String())
	}
}

// TestRunSkipOnly: SKIP-only results → no output.
func TestRunSkipOnly(t *testing.T) {
	results := []doctor.Result{
		{Index: 3, Name: "signals", Severity: doctor.SKIP, Detail: "skipped"},
	}
	var buf bytes.Buffer
	Run(stubResults(results), &buf)
	if buf.Len() != 0 {
		t.Errorf("SKIP-only should produce no output, got: %q", buf.String())
	}
}

// TestRunFailPrinted: exactly 2 FAIL lines printed; WARN in the same batch is suppressed.
// A bug that emits duplicates, stray SKIP lines, or the WARN detail would fail this test.
func TestRunFailPrinted(t *testing.T) {
	results := []doctor.Result{
		{Index: 1, Name: "install", Severity: doctor.FAIL, Detail: "missing files"},
		{Index: 2, Name: "hooks", Severity: doctor.WARN, Detail: "hook stale"},
		{Index: 4, Name: "refs", Severity: doctor.FAIL, Detail: "dangling ref"},
	}
	var buf bytes.Buffer
	Run(stubResults(results), &buf)
	out := buf.String()

	// Split into non-empty lines; must be exactly 2 (one per FAIL result).
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 output lines, got %d: %q", len(lines), out)
	}
	// Each line must contain "FAIL".
	for _, l := range lines {
		if !strings.Contains(l, "FAIL") {
			t.Errorf("line does not contain 'FAIL': %q", l)
		}
	}
	// WARN detail must not appear anywhere.
	if strings.Contains(out, "hook stale") {
		t.Errorf("WARN detail should be suppressed, got: %q", out)
	}
}

// TestRunDoctorError: doctor returns non-nil error → print one-liner, no panic.
func TestRunDoctorError(t *testing.T) {
	var buf bytes.Buffer
	Run(stubError(errors.New("some internal failure")), &buf)
	out := buf.String()
	if !strings.Contains(out, "doctor self-check failed") {
		t.Errorf("expected 'doctor self-check failed' in output, got: %q", out)
	}
	if !strings.Contains(out, "some internal failure") {
		t.Errorf("expected error detail in output, got: %q", out)
	}
}

// TestRunDoctorPanic: doctor panics → recovered, one-liner printed, no re-panic.
func TestRunDoctorPanic(t *testing.T) {
	var buf bytes.Buffer
	// Must not panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Run should have recovered the panic, but it propagated: %v", r)
			}
		}()
		Run(stubPanic("boom"), &buf)
	}()
	out := buf.String()
	if !strings.Contains(out, "doctor self-check failed") {
		t.Errorf("expected 'doctor self-check failed' in output, got: %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("expected panic message in output, got: %q", out)
	}
}

// TestSkipOpts: Run calls doctor with Skip: []int{3, 8}.
func TestSkipOpts(t *testing.T) {
	var gotOpts doctor.Opts
	stub := func(opts doctor.Opts) ([]doctor.Result, error) {
		gotOpts = opts
		return nil, nil
	}
	var buf bytes.Buffer
	Run(stub, &buf)
	if len(gotOpts.Skip) != 2 {
		t.Fatalf("expected 2 skip indices, got %v", gotOpts.Skip)
	}
	skipSet := map[int]bool{}
	for _, i := range gotOpts.Skip {
		skipSet[i] = true
	}
	if !skipSet[3] || !skipSet[8] {
		t.Errorf("expected Skip=[3,8], got %v", gotOpts.Skip)
	}
}
