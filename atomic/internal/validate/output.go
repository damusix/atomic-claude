// Package validate — output formatters.
//
// This file owns the human and JSON output contracts for all validate
// subcommands. Keep all fmt.Fprintf calls for findings here; rule files
// (spec.go, config.go, bundle.go) call printHeader + printHuman / printJSON
// and never format findings directly.
//
// JSON contract (schema_version: 1):
//
//	{
//	  "schema_version": 1,
//	  "findings": [
//	    {"index": 1, "severity": "FAIL", "rule": "C3", "path": "commands/foo.md", "line": 42, "message": "..."}
//	  ],
//	  "summary": {"pass": 0, "warn": 1, "fail": 2, "exit": 1}
//	}
//
// path is repo-root-relative when callers supply a relative path (the norm for
// all rule runners in this package). For findings without a meaningful line
// number, line is 0 (field always present for stable JSON consumers).
//
// Headers: human mode emits a one-liner header before findings. JSON mode
// suppresses headers entirely — the JSON envelope is the only UI chrome.
package validate

import (
	"encoding/json"
	"fmt"
	"io"
)

// printHeader writes the subcommand header line to w. Called once per
// subcommand run, before any findings. Not called in JSON mode.
//
// Format: "atomic validate <sub> — <oneLiner>\n\n"
//
// One-liners per subcommand:
//
//	spec   → "structural integrity"
//	config → "referential integrity"
//	bundle → "manifest parity"
func printHeader(w io.Writer, sub, oneLiner string) {
	fmt.Fprintf(w, "atomic validate %s — %s\n\n", sub, oneLiner)
}

// printHuman writes findings in the human-readable format followed by a
// summary line. If suggest is true and a FAIL finding has a template, it
// prints the template block after the findings.
//
// Format per finding:
//
//	[N] SEVERITY  RULE  path:line  message
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

// suggestionTemplate returns a structural template and an optional hint for
// the given finding. The hint, when non-empty, is rendered as a parenthetical
// in the suggestion preamble: "Suggestion for RULE in path (hint):".
//
// Scope: structural-only — no name suggestions, no fuzzy matching. The author
// writes content; the tool only shapes the container. Per spec non-goal:
// "never suggests names, never fuzzy-matches against existing artifacts."
//
// Rule coverage:
//   - S5: Checkpoints table skeleton. Hint: "insert before ## Change log"
//     because the Checkpoints section must precede the Change log section.
//   - S6: empty ## Change log heading — just the heading, no content.
//     No hint: the template IS ## Change log, so "insert before itself" would
//     be nonsensical. Intentionally minimal: log entries are human-authored.
//   - C5: NO template — cannot generate the correct @-ref path without knowing
//     what the author intended to reference. Explicitly skipped.
//   - All others: no template currently.
func suggestionTemplate(f Finding) (template, hint string) {
	switch f.Rule {
	case "S5":
		return "## Checkpoints\n\n| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |\n|---|------------|-------------|-------|------------|----------|\n| 1 |            |             |       |            |          |",
			"insert before ## Change log"
	case "S6":
		// Empty heading only — content is human-authored per the spec amendment rule.
		// No hint: the template itself IS ## Change log; a hint would say "insert
		// before itself," which is nonsensical.
		return "## Change log\n\n<!-- Empty on creation. Append dated entries on amendments. -->", ""
	}
	return "", ""
}

// jsonFinding is the JSON representation of a Finding (for --json output).
type jsonFinding struct {
	Index    int    `json:"index"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// jsonOutput is the top-level JSON output structure.
// schema_version: 1 is the forward-compat hedge — additional --format options
// (SARIF, etc.) ship as siblings, never replacements. Bump only on breaking
// changes to this shape.
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

// printJSON writes findings as a JSON envelope. Headers are suppressed —
// JSON consumers parse the envelope directly. findings is always an array
// (never null), even when empty.
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
