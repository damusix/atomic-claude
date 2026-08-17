package doctor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// indentPrefix is prepended to secondary lines (remediation, findings, fix summary).
const indentPrefix = "   "

// nameWidth is the check-name column width; the longest name is 9 chars.
const nameWidth = 25

// FormatHuman renders the result table plus a summary line, adding a repair
// hint when any WARN or FAIL is present. Remediation prints on every non-PASS
// result; Findings print only under opts.Verbose.
func FormatHuman(results []Result, opts Opts, project string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "atomic doctor — integrity check  (project: %s)\n", project)
	b.WriteString("\n")

	for _, r := range results {
		b.WriteString(FormatResultLine(r))
		if r.Severity != PASS && r.Severity != SKIP && r.Remediation != "" {
			fmt.Fprintf(&b, "%s↳ fix: %s\n", indentPrefix, r.Remediation)
		}
		if opts.Verbose {
			for _, f := range r.Findings {
				fmt.Fprintf(&b, "%s• %s\n", indentPrefix, f)
			}
		}
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

// FormatResultLine returns one result line, newline included. Shared with the
// post-update doctor adapter so the column layout lives in one place.
func FormatResultLine(r Result) string {
	return fmt.Sprintf("[%d] %-4s  %-*s  %s\n",
		r.Index,
		string(r.Severity),
		nameWidth,
		r.Name,
		r.Detail,
	)
}

// FormatJSON returns the machine-readable output for a completed doctor run.
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

// FormatJSONMissingHome returns the short-circuit payload for a machine with
// no ~/.claude/ — not installed, exit 0.
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

// countSeverities tallies each severity into its own bucket.
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
