package validate

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Finding represents a single linting finding from any validator rule.
// Severity is "FAIL" or "WARN". Rule is the rule ID (S0, S1, C1, etc.).
// Line is 0 if not applicable.
type Finding struct {
	Severity string
	Rule     string
	Path     string
	Line     int
	Message  string
}

// sortFindings sorts findings by (Path, Line, Rule) for deterministic output.
func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
}

// summary counts findings by severity.
type summary struct {
	Pass int
	Warn int
	Fail int
}

func summarize(findings []Finding) summary {
	var s summary
	for _, f := range findings {
		switch f.Severity {
		case "WARN":
			s.Warn++
		case "FAIL":
			s.Fail++
		default:
			s.Pass++
		}
	}
	return s
}

// exitCode returns the appropriate exit code given the summary.
// 0 = all PASS or only WARN, 1 = any FAIL.
func exitCode(s summary) int {
	if s.Fail > 0 {
		return 1
	}
	return 0
}

// printHuman writes findings in the human-readable format and a summary line.
// If suggest is true and a finding has a suggestion, it prints it.
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
				if tmpl := suggestionTemplate(f); tmpl != "" {
					fmt.Fprintf(w, "\nSuggestion for %s in %s (insert before ## Change log):\n\n%s\n", f.Rule, f.Path, tmpl)
				}
			}
		}
	}
	code := exitCode(s)
	fmt.Fprintf(w, "\n%d PASS, %d WARN, %d FAIL. exit %d.\n", s.Pass, s.Warn, s.Fail, code)
}

// suggestionTemplate returns a structural template for the given finding.
// Only S5 has a template in CP-5. Others return "".
func suggestionTemplate(f Finding) string {
	// TODO(CP-7): add templates for S0/S1/S6
	switch f.Rule {
	case "S5":
		return "## Checkpoints\n\n| # | Checkpoint | Files/areas | Verifies |\n|---|------------|-------------|----------|\n| 1 |            |             |          |"
	}
	return ""
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

// printJSON writes findings as JSON.
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
