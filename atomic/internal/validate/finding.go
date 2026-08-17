// This file holds the shared Finding type and the ordering and counting
// helpers every rule runner depends on. Formatters live in output.go.
package validate

import "sort"

// Finding is one linting result. Severity is "FAIL" or "WARN"; Line is 0 when
// the rule has no meaningful location.
type Finding struct {
	Severity string
	Rule     string
	Path     string
	Line     int
	Message  string
}

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
func exitCode(s summary) int {
	if s.Fail > 0 {
		return 1
	}
	return 0
}
