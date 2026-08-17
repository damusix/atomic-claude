// This file owns the human and JSON output contracts for every validate
// subcommand. Rule files call printHeader + printHuman/printJSON and never
// format findings themselves.
package validate

import (
	"encoding/json"
	"fmt"
	"io"
)

// printHeader writes the subcommand header, once per run. Never in JSON mode.
func printHeader(w io.Writer, sub, oneLiner string) {
	fmt.Fprintf(w, "atomic validate %s — %s\n\n", sub, oneLiner)
}

// printHuman writes findings as "[N] SEVERITY RULE path:line message", then a
// summary line, and a template block per FAIL when suggest is set.
func printHuman(w io.Writer, findings []Finding, s summary, suggest bool) {
	for i, f := range findings {
		loc := f.Path
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
		}
		fmt.Fprintf(w, "[%d] %-4s  %-3s  %s  %s\n", i+1, f.Severity, f.Rule, loc, f.Message)
	}
	if suggest {
		for _, f := range findings {
			if f.Severity == "FAIL" {
				if tmpl, hint := suggestionTemplate(f); tmpl != "" {
					preamble := ""
					if hint != "" {
						preamble = " (" + hint + ")"
					}
					fmt.Fprintf(w, "\nSuggestion for %s in %s%s:\n\n%s\n", f.Rule, f.Path, preamble, tmpl)
				}
			}
		}
	}
	code := exitCode(s)
	fmt.Fprintf(w, "\n%d PASS, %d WARN, %d FAIL. exit %d.\n", s.Pass, s.Warn, s.Fail, code)
}

// suggestionTemplate returns a structural skeleton and an optional parenthetical
// hint for a finding. Structural only: the author writes content, the tool only
// shapes the container, so no rule ever suggests a name or fuzzy-matches.
func suggestionTemplate(f Finding) (template, hint string) {
	switch f.Rule {
	case "S5":
		return "## Checkpoints\n\n| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |\n|---|------------|-------------|-------|------------|----------|\n| 1 |            |             |       |            |          |",
			"insert before ## Change log"
	case "S6":
		// Heading only; log entries are human-authored. No hint — the template
		// is the ## Change log heading, so "insert before it" is meaningless.
		return "## Change log\n\n<!-- Empty on creation. Append dated entries on amendments. -->", ""
	}
	return "", ""
}

type jsonFinding struct {
	Index    int    `json:"index"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// SchemaVersion is the forward-compat hedge: other formats ship as siblings,
// so bump it only on a breaking change to this shape.
type jsonOutput struct {
	SchemaVersion int           `json:"schema_version"`
	Findings      []jsonFinding `json:"findings"`
	Summary       jsonSummary   `json:"summary"`
}

type jsonSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Exit int `json:"exit"`
}

// printJSON writes the findings envelope. findings is always an array, never
// null, even when empty.
func printJSON(w io.Writer, findings []Finding, s summary) {
	out := jsonOutput{
		SchemaVersion: 1,
		Findings:      make([]jsonFinding, len(findings)),
		Summary: jsonSummary{
			Pass: s.Pass,
			Warn: s.Warn,
			Fail: s.Fail,
			Exit: exitCode(s),
		},
	}
	for i, f := range findings {
		out.Findings[i] = jsonFinding{
			Index:    i + 1,
			Severity: f.Severity,
			Rule:     f.Rule,
			Path:     f.Path,
			Line:     f.Line,
			Message:  f.Message,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
