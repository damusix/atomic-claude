package doctor_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestFormatHumanRemediationShownWithoutVerbose verifies that a FAIL result
// with Remediation set prints the remediation line even when Verbose=false.
func TestFormatHumanRemediationShownWithoutVerbose(t *testing.T) {
	results := []doctor.Result{
		{
			Index:       4,
			Name:        "refs",
			Severity:    doctor.FAIL,
			Detail:      "@-refs not present",
			Remediation: "add @.claude/project/signals.md to CLAUDE.md",
		},
	}
	opts := doctor.Opts{Verbose: false}
	out := doctor.FormatHuman(results, opts, "myproject")

	if !strings.Contains(out, "add @.claude/project/signals.md to CLAUDE.md") {
		t.Errorf("remediation must appear on FAIL even when Verbose=false:\n%s", out)
	}
	if !strings.Contains(out, "↳ fix:") {
		t.Errorf("remediation prefix '↳ fix:' missing:\n%s", out)
	}
}

// TestFormatHumanFindingsShownOnlyWhenVerbose verifies that Findings lines are
// gated behind Verbose=true.
func TestFormatHumanFindingsShownOnlyWhenVerbose(t *testing.T) {
	results := []doctor.Result{
		{
			Index:    2,
			Name:     "hooks",
			Severity: doctor.WARN,
			Detail:   "session-start hook missing",
			Findings: []string{"missing: session-start", "drift: legacy script"},
		},
	}

	// With Verbose=true — findings must appear.
	outVerbose := doctor.FormatHuman(results, doctor.Opts{Verbose: true}, "myproject")
	if !strings.Contains(outVerbose, "missing: session-start") {
		t.Errorf("finding line missing when Verbose=true:\n%s", outVerbose)
	}
	if !strings.Contains(outVerbose, "drift: legacy script") {
		t.Errorf("finding line missing when Verbose=true:\n%s", outVerbose)
	}
	if !strings.Contains(outVerbose, "•") {
		t.Errorf("finding bullet '•' missing when Verbose=true:\n%s", outVerbose)
	}

	// With Verbose=false — findings must be absent.
	outQuiet := doctor.FormatHuman(results, doctor.Opts{Verbose: false}, "myproject")
	if strings.Contains(outQuiet, "missing: session-start") {
		t.Errorf("finding line must be absent when Verbose=false:\n%s", outQuiet)
	}
	if strings.Contains(outQuiet, "drift: legacy script") {
		t.Errorf("finding line must be absent when Verbose=false:\n%s", outQuiet)
	}
}

// TestFormatHumanPassResultClean verifies that a PASS result produces a single
// line — no remediation, no findings.
func TestFormatHumanPassResultClean(t *testing.T) {
	results := []doctor.Result{
		{Index: 1, Name: "install", Severity: doctor.PASS, Detail: "36/36 files match bundle"},
	}
	opts := doctor.Opts{Verbose: true} // even with verbose, PASS is clean
	out := doctor.FormatHuman(results, opts, "myproject")

	if strings.Contains(out, "↳ fix:") {
		t.Errorf("PASS result must not print remediation:\n%s", out)
	}
	if strings.Contains(out, "•") {
		t.Errorf("PASS result must not print findings even with Verbose=true:\n%s", out)
	}
}
