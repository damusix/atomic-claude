package doctor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// nameWidth is the fixed column width for the check name field.
// Spec example longest name is "followups" (9 chars); 25 gives comfortable padding.
const nameWidth = 25

// FormatHuman returns the human-readable output string per the spec:
//
//	atomic doctor — integrity check  (project: <name>)
//
//	[1] PASS  install                  <detail>
//	...
//
//	N PASS, N WARN, N FAIL, N SKIP. exit N.
//
//	To repair: atomic doctor --fix   (only when WARN or FAIL present)
func FormatHuman(results []Result, project string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "atomic doctor — integrity check  (project: %s)\n", project)
	b.WriteString("\n")

	for _, r := range results {
		b.WriteString(FormatResultLine(r))
	}

	b.WriteString("\n")

	pass, warn, fail, skip := countSeverities(results)
	exitCode := ExitCode(results)
	fmt.Fprintf(&b, "%d PASS, %d WARN, %d FAIL, %d SKIP. exit %d.\n", pass, warn, fail, skip, exitCode)

	if warn > 0 || fail > 0 {
		b.WriteString("\n")
		b.WriteString("To repair: atomic doctor --fix\n")
	}

	return b.String()
}

// FormatResultLine returns one formatted result line (with trailing newline)
// using the canonical column layout: [index] severity  name  detail.
// Used by both FormatHuman and the post-update doctor adapter (updatedoctor)
// so the format stays in one place.
func FormatResultLine(r Result) string {
	return fmt.Sprintf("[%d] %-4s  %-*s  %s\n",
		r.Index,
		string(r.Severity),
		nameWidth,
		r.Name,
		r.Detail,
	)
}

// FormatJSON returns the machine-readable JSON output bytes per the spec schema:
//
//	{
//	  "schema_version": 1,
//	  "project": "...",
//	  "results": [...],
//	  "summary": {"pass": N, "warn": N, "fail": N, "skip": N, "exit": N}
//	}
func FormatJSON(results []Result, project string, exitCode int) ([]byte, error) {
	type resultJSON struct {
		Index    int    `json:"index"`
		Name     string `json:"name"`
		Severity string `json:"severity"`
		Detail   string `json:"detail"`
	}
	type summaryJSON struct {
		Pass int `json:"pass"`
		Warn int `json:"warn"`
		Fail int `json:"fail"`
		Skip int `json:"skip"`
		Exit int `json:"exit"`
	}
	type outputJSON struct {
		SchemaVersion int          `json:"schema_version"`
		Project       string       `json:"project"`
		Results       []resultJSON `json:"results"`
		Summary       summaryJSON  `json:"summary"`
	}

	pass, warn, fail, skip := countSeverities(results)

	rs := make([]resultJSON, len(results))
	for i, r := range results {
		rs[i] = resultJSON{
			Index:    r.Index,
			Name:     r.Name,
			Severity: string(r.Severity),
			Detail:   r.Detail,
		}
	}

	out := outputJSON{
		SchemaVersion: 1,
		Project:       project,
		Results:       rs,
		Summary: summaryJSON{
			Pass: pass,
			Warn: warn,
			Fail: fail,
			Skip: skip,
			Exit: exitCode,
		},
	}

	return json.MarshalIndent(out, "", "  ")
}

// FormatJSONMissingHome returns the short-circuit JSON payload for the case
// where ~/.claude/ does not exist.
//
//	{"schema_version": 1, "installed": false, "message": "...", "summary": {"exit": 0}}
func FormatJSONMissingHome(message string) ([]byte, error) {
	type summaryJSON struct {
		Exit int `json:"exit"`
	}
	type outputJSON struct {
		SchemaVersion int         `json:"schema_version"`
		Installed     bool        `json:"installed"`
		Message       string      `json:"message"`
		Summary       summaryJSON `json:"summary"`
	}
	out := outputJSON{
		SchemaVersion: 1,
		Installed:     false,
		Message:       message,
		Summary:       summaryJSON{Exit: 0},
	}
	return json.MarshalIndent(out, "", "  ")
}

// countSeverities tallies PASS/WARN/FAIL/SKIP counts.
// SKIP is counted in its own bucket; excluded from PASS/WARN/FAIL totals.
func countSeverities(results []Result) (pass, warn, fail, skip int) {
	for _, r := range results {
		switch r.Severity {
		case PASS:
			pass++
		case WARN:
			warn++
		case FAIL:
			fail++
		case SKIP:
			skip++
		}
	}
	return
}
