// Package validate — Finding type and sort/summary helpers.
//
// Formatter helpers (printHuman, printJSON, printHeader, suggestionTemplate)
// live in output.go. This file is intentionally minimal: only the shared data
// type and the deterministic ordering/counting utilities that every rule runner
// depends on.
package validate

import "sort"

// Finding represents a single linting finding from any validator rule.
// Severity is "FAIL" or "WARN". Rule is the rule ID (S0, S1, C3, etc.).
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

// summarize counts findings by severity.
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
