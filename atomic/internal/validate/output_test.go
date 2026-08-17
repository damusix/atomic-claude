package validate

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintHumanTwoFindings(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Rule: "C3", Path: "commands/foo.md", Line: 42, Message: `subagent_type "bar" — no agents/bar.md`},
		{Severity: "WARN", Rule: "C9", Path: "agents/AtomicBuilder.md", Line: 0, Message: "agents/AtomicBuilder.md missing atomic- prefix; will not bundle"},
	}
	s := summarize(findings)

	var buf bytes.Buffer
	printHuman(&buf, findings, s, false)
	got := buf.String()

	wantLines := []string{
		`[1] FAIL  C3   commands/foo.md:42  subagent_type "bar" — no agents/bar.md`,
		`[2] WARN  C9   agents/AtomicBuilder.md  agents/AtomicBuilder.md missing atomic- prefix; will not bundle`,
		`0 PASS, 1 WARN, 1 FAIL. exit 1.`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("printHuman output missing expected line.\nwant: %q\ngot:\n%s", want, got)
		}
	}
}

func TestPrintJSONTwoFindings(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Rule: "C3", Path: "commands/foo.md", Line: 42, Message: `subagent_type "bar" — no agents/bar.md`},
		{Severity: "WARN", Rule: "C9", Path: "agents/AtomicBuilder.md", Line: 0, Message: "agents/AtomicBuilder.md missing atomic- prefix; will not bundle"},
	}
	s := summarize(findings)

	var buf bytes.Buffer
	printJSON(&buf, findings, s)
	got := buf.String()

	wantContains := []string{
		`"schema_version": 1`,
		`"findings":`,
		`"severity": "FAIL"`,
		`"rule": "C3"`,
		`"path": "commands/foo.md"`,
		`"line": 42`,
		`"severity": "WARN"`,
		`"rule": "C9"`,
		`"summary":`,
		`"pass": 0`,
		`"warn": 1`,
		`"fail": 1`,
		`"exit": 1`,
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Errorf("printJSON output missing %q\ngot:\n%s", want, got)
		}
	}

	if strings.Contains(got, "atomic validate") {
		t.Errorf("printJSON must not emit header line, got:\n%s", got)
	}
}

func TestPrintHumanEmpty(t *testing.T) {
	var findings []Finding
	s := summarize(findings)

	var buf bytes.Buffer
	printHuman(&buf, findings, s, false)
	got := buf.String()

	if !strings.Contains(got, "0 PASS, 0 WARN, 0 FAIL. exit 0.") {
		t.Errorf("empty findings must produce '0 PASS, 0 WARN, 0 FAIL. exit 0.', got:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "[") {
			t.Errorf("no finding lines expected for empty result, got line: %q", line)
		}
	}
}

func TestPrintJSONEmpty(t *testing.T) {
	var findings []Finding
	s := summarize(findings)

	var buf bytes.Buffer
	printJSON(&buf, findings, s)
	got := buf.String()

	if !strings.Contains(got, `"findings": []`) {
		t.Errorf("JSON empty findings must be [] not null, got:\n%s", got)
	}
	if !strings.Contains(got, `"exit": 0`) {
		t.Errorf("JSON summary exit must be 0 for empty findings, got:\n%s", got)
	}
}

func TestPrintHumanSuggestS5(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Rule: "S5", Path: "docs/spec/foo.md", Line: 0, Message: "missing `## Checkpoints` section"},
	}
	s := summarize(findings)

	var buf bytes.Buffer
	printHuman(&buf, findings, s, true)
	got := buf.String()

	if !strings.Contains(got, "## Checkpoints") {
		t.Errorf("--suggest must print Checkpoints template for S5, got:\n%s", got)
	}
	findingIdx := strings.Index(got, "[1]")
	tmplIdx := strings.Index(got, "## Checkpoints")
	if findingIdx < 0 || tmplIdx < 0 || tmplIdx < findingIdx {
		t.Errorf("template must appear after finding line, got:\n%s", got)
	}
	if strings.Contains(got, "atomic-") {
		t.Errorf("--suggest must not include artifact names, got:\n%s", got)
	}
}

func TestPrintHumanSuggestS6(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Rule: "S6", Path: "docs/spec/bar.md", Line: 0, Message: "missing `## Change log` section"},
	}
	s := summarize(findings)

	var buf bytes.Buffer
	printHuman(&buf, findings, s, true)
	got := buf.String()

	if !strings.Contains(got, "## Change log") {
		t.Errorf("--suggest must print Change log template for S6, got:\n%s", got)
	}
	// S6's template IS ## Change log — "insert before itself" makes no sense.
	if strings.Contains(got, "(insert before ## Change log)") {
		t.Errorf("S6 preamble must not say '(insert before ## Change log)', got:\n%s", got)
	}
}

func TestPrintHumanSuggestS5PreambleHint(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Rule: "S5", Path: "docs/spec/foo.md", Line: 0, Message: "missing `## Checkpoints` section"},
	}
	s := summarize(findings)

	var buf bytes.Buffer
	printHuman(&buf, findings, s, true)
	got := buf.String()

	if !strings.Contains(got, "(insert before ## Change log)") {
		t.Errorf("S5 preamble must contain '(insert before ## Change log)', got:\n%s", got)
	}
}

func TestEmptyOutputPath(t *testing.T) {
	var buf bytes.Buffer
	printHeader(&buf, "spec", "structural integrity")
	var findings []Finding
	s := summarize(findings)
	printHuman(&buf, findings, s, false)
	got := buf.String()

	wantParts := []string{
		"atomic validate spec — structural integrity",
		"0 PASS, 0 WARN, 0 FAIL. exit 0.",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Errorf("empty output path missing %q\ngot:\n%s", want, got)
		}
	}
	headerIdx := strings.Index(got, "atomic validate spec")
	summaryIdx := strings.Index(got, "0 PASS")
	if headerIdx < 0 || summaryIdx < 0 || summaryIdx < headerIdx {
		t.Errorf("header must appear before summary in empty path, got:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "[") {
			t.Errorf("no finding lines expected for empty result, got line: %q", line)
		}
	}
}

func TestPrintHeaderHumanOnly(t *testing.T) {
	var buf bytes.Buffer
	printHeader(&buf, "config", "referential integrity")
	got := buf.String()

	if !strings.Contains(got, "atomic validate config") {
		t.Errorf("printHeader must contain 'atomic validate config', got: %q", got)
	}
	if !strings.Contains(got, "referential integrity") {
		t.Errorf("printHeader must contain one-liner, got: %q", got)
	}
}
