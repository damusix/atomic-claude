package doctor_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// sampleResults is a consistent fixture used across format tests.
var sampleResults = []doctor.Result{
	{Index: 1, Name: "install", Severity: doctor.PASS, Detail: "36/36 files match bundle"},
	{Index: 2, Name: "hooks", Severity: doctor.WARN, Detail: "session-start hook missing"},
	{Index: 3, Name: "signals", Severity: doctor.PASS, Detail: "last scan 3d ago (threshold 7d)"},
	{Index: 4, Name: "refs", Severity: doctor.FAIL, Detail: "@-refs not present"},
	{Index: 5, Name: "manifest", Severity: doctor.SKIP, Detail: "not in atomic-claude repo"},
}

func TestFormatHumanHeader(t *testing.T) {
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	if !strings.Contains(out, "atomic doctor") {
		t.Errorf("output missing 'atomic doctor': %q", out)
	}
	if !strings.Contains(out, "myproject") {
		t.Errorf("output missing project name 'myproject': %q", out)
	}
}

func TestFormatHumanIndexedRows(t *testing.T) {
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	for _, r := range sampleResults {
		marker := "[" + string(rune('0'+r.Index)) + "]"
		if !strings.Contains(out, marker) {
			t.Errorf("output missing row marker %q: %q", marker, out)
		}
	}
}

func TestFormatHumanSeverityColumn(t *testing.T) {
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	for _, sev := range []string{"PASS", "WARN", "FAIL", "SKIP"} {
		if !strings.Contains(out, sev) {
			t.Errorf("output missing severity %q", sev)
		}
	}
}

func TestFormatHumanCountersLine(t *testing.T) {
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	if !strings.Contains(out, "2 PASS") {
		t.Errorf("counters line missing '2 PASS': %q", out)
	}
	if !strings.Contains(out, "1 WARN") {
		t.Errorf("counters line missing '1 WARN': %q", out)
	}
	if !strings.Contains(out, "1 FAIL") {
		t.Errorf("counters line missing '1 FAIL': %q", out)
	}
	if !strings.Contains(out, "1 SKIP") {
		t.Errorf("counters line missing '1 SKIP': %q", out)
	}
}

func TestFormatHumanExitCodeInCounters(t *testing.T) {
	exitCode := doctor.ExitCode(sampleResults)
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	if !strings.Contains(out, "exit 1") && exitCode == 1 {
		t.Errorf("counters line missing 'exit 1': %q", out)
	}
}

func TestFormatHumanRepairHintShownOnWarnOrFail(t *testing.T) {
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	if !strings.Contains(out, "To repair:") {
		t.Errorf("output missing repair hint when WARN/FAIL present: %q", out)
	}
}

func TestFormatHumanRepairHintAbsentOnAllPass(t *testing.T) {
	allPass := []doctor.Result{
		{Index: 1, Name: "install", Severity: doctor.PASS, Detail: "ok"},
		{Index: 2, Name: "hooks", Severity: doctor.PASS, Detail: "ok"},
	}
	out := doctor.FormatHuman(allPass, doctor.Opts{}, "myproject")
	if strings.Contains(out, "To repair:") {
		t.Errorf("repair hint must be absent when all PASS: %q", out)
	}
}

func TestFormatHumanRepairHintAbsentOnAllSkip(t *testing.T) {
	allSkip := []doctor.Result{
		{Index: 5, Name: "manifest", Severity: doctor.SKIP, Detail: "not in repo"},
	}
	out := doctor.FormatHuman(allSkip, doctor.Opts{}, "myproject")
	if strings.Contains(out, "To repair:") {
		t.Errorf("repair hint must be absent when all SKIP: %q", out)
	}
}

func TestFormatHumanDetailPresent(t *testing.T) {
	out := doctor.FormatHuman(sampleResults, doctor.Opts{}, "myproject")
	if !strings.Contains(out, "36/36 files match bundle") {
		t.Errorf("output missing detail for install check: %q", out)
	}
}

// FormatHuman and updatedoctor share this layout, so the padding is a
// cross-package contract, not a local formatting choice.
func TestFormatResultLine(t *testing.T) {
	r := doctor.Result{Index: 4, Name: "refs", Severity: doctor.FAIL, Detail: "@-refs not present"}
	line := doctor.FormatResultLine(r)
	if !strings.HasPrefix(line, "[4] FAIL") {
		t.Errorf("line does not start with '[4] FAIL': %q", line)
	}
	// "refs" padded to 25 chars, then the two-space separator.
	want := "[4] FAIL  refs                       @-refs not present\n"
	if line != want {
		t.Errorf("FormatResultLine =\n  %q\nwant\n  %q", line, want)
	}
}

func TestFormatJSONSchemaVersion(t *testing.T) {
	data, err := doctor.FormatJSON(sampleResults, "myproject", 1)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var got struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", got.SchemaVersion)
	}
}

func TestFormatJSONProject(t *testing.T) {
	data, err := doctor.FormatJSON(sampleResults, "myproject", 1)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var got struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.Project != "myproject" {
		t.Errorf("project = %q, want %q", got.Project, "myproject")
	}
}

func TestFormatJSONResults(t *testing.T) {
	data, err := doctor.FormatJSON(sampleResults, "myproject", 1)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var got struct {
		Results []struct {
			Index    int    `json:"index"`
			Name     string `json:"name"`
			Severity string `json:"severity"`
			Detail   string `json:"detail"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(got.Results) != len(sampleResults) {
		t.Fatalf("results len = %d, want %d", len(got.Results), len(sampleResults))
	}
	for i, r := range got.Results {
		want := sampleResults[i]
		if r.Index != want.Index {
			t.Errorf("results[%d].index = %d, want %d", i, r.Index, want.Index)
		}
		if r.Name != want.Name {
			t.Errorf("results[%d].name = %q, want %q", i, r.Name, want.Name)
		}
		if r.Severity != string(want.Severity) {
			t.Errorf("results[%d].severity = %q, want %q", i, r.Severity, string(want.Severity))
		}
	}
}

func TestFormatJSONSummary(t *testing.T) {
	data, err := doctor.FormatJSON(sampleResults, "myproject", 1)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var got struct {
		Summary struct {
			Pass int `json:"pass"`
			Warn int `json:"warn"`
			Fail int `json:"fail"`
			Skip int `json:"skip"`
			Exit int `json:"exit"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.Summary.Pass != 2 {
		t.Errorf("summary.pass = %d, want 2", got.Summary.Pass)
	}
	if got.Summary.Warn != 1 {
		t.Errorf("summary.warn = %d, want 1", got.Summary.Warn)
	}
	if got.Summary.Fail != 1 {
		t.Errorf("summary.fail = %d, want 1", got.Summary.Fail)
	}
	if got.Summary.Skip != 1 {
		t.Errorf("summary.skip = %d, want 1", got.Summary.Skip)
	}
	if got.Summary.Exit != 1 {
		t.Errorf("summary.exit = %d, want 1", got.Summary.Exit)
	}
}

func TestFormatJSONMissingHome(t *testing.T) {
	msg := "atomic-claude not installed; run `atomic claude install`."
	data, err := doctor.FormatJSONMissingHome(msg)
	if err != nil {
		t.Fatalf("FormatJSONMissingHome: %v", err)
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		Installed     bool   `json:"installed"`
		Message       string `json:"message"`
		Summary       struct {
			Exit int `json:"exit"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", got.SchemaVersion)
	}
	if got.Installed {
		t.Error("installed = true, want false")
	}
	if got.Message != msg {
		t.Errorf("message = %q, want %q", got.Message, msg)
	}
	if got.Summary.Exit != 0 {
		t.Errorf("summary.exit = %d, want 0", got.Summary.Exit)
	}
}
