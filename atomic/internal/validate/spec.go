package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

// checkpointsRequiredColumns must appear in the header row as an ordered
// subsequence; extra columns may sit between or after them.
var checkpointsRequiredColumns = []string{"#", "Checkpoint", "Files/areas", "Verifies"}

// RunSpecRules runs the spec-structure rules over one file's content. path
// feeds Finding.Path only — nothing is read from disk.
func RunSpecRules(path string, src []byte) ([]Finding, error) {
	var findings []Finding

	if !mdparse.IsATXOnly(src) {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "S0",
			Path:     path,
			Line:     0,
			Message:  "file contains Setext-style headings; use ATX headings only (# ## ###)",
		})
		// Section parsing is unreliable on Setext docs, so the later rules
		// would report noise.
		return findings, nil
	}

	sections, err := mdparse.Sections(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	s1ok := false
	if len(sections) > 0 && sections[0].Level == 1 && sections[0].Start == 1 {
		s1ok = true
	}
	if !s1ok {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "S1",
			Path:     path,
			Line:     1,
			Message:  "file must start with # <title> (H1) at line 1",
		})
	}

	var checkpointsSection *mdparse.Section
	for i := range sections {
		if sections[i].Level == 2 && sections[i].Heading == "Checkpoints" {
			checkpointsSection = &sections[i]
			break
		}
	}
	if checkpointsSection == nil {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "S5",
			Path:     path,
			Line:     0,
			Message:  "missing `## Checkpoints` section",
		})
	} else {
		found, line, err := findTableInSection(src, checkpointsSection, checkpointsRequiredColumns)
		if err != nil {
			return nil, fmt.Errorf("parse %s S5: %w", path, err)
		}
		if !found {
			findings = append(findings, Finding{
				Severity: "FAIL",
				Rule:     "S5",
				Path:     path,
				Line:     checkpointsSection.Start,
				Message:  `## Checkpoints table must include columns "# | Checkpoint | Files/areas | … | Verifies" in order (extra columns allowed)`,
			})
		} else {
			_ = line // line info available if needed for future polish
		}
	}

	var hasChangeLog bool
	for _, s := range sections {
		if s.Level == 2 && s.Heading == "Change log" {
			hasChangeLog = true
			break
		}
	}
	if !hasChangeLog {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "S6",
			Path:     path,
			Line:     0,
			Message:  "missing `## Change log` section",
		})
	}

	sortFindings(findings)
	return findings, nil
}

// findTableInSection looks within sec's line range for a table whose header
// contains requiredCols as an ordered subsequence.
func findTableInSection(src []byte, sec *mdparse.Section, requiredCols []string) (bool, int, error) {
	sectionSrc := extractLines(src, sec.Start, sec.End)
	found, lineInSection, err := mdparse.FindTableByRequiredColumns(sectionSrc, requiredCols)
	if err != nil {
		return false, 0, err
	}
	var globalLine int
	if found {
		globalLine = sec.Start + lineInSection - 1
	}
	return found, globalLine, nil
}

// extractLines returns lines [start, end] of src, 1-indexed and inclusive. An
// end of 0 runs to EOF.
func extractLines(src []byte, start, end int) []byte {
	lines := splitLines(src)
	if start < 1 {
		start = 1
	}
	if end == 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return nil
	}
	var out []byte
	for i := start - 1; i < end && i < len(lines); i++ {
		out = append(out, lines[i]...)
		out = append(out, '\n')
	}
	return out
}

func splitLines(src []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range src {
		if b == '\n' {
			lines = append(lines, src[start:i])
			start = i + 1
		}
	}
	if start < len(src) {
		lines = append(lines, src[start:])
	}
	return lines
}

func runSpec(subArgs []string, jsonOut, suggest bool, w io.Writer) int {
	// Re-parsed so flags placed after the subcommand are honored; top-level
	// flags already won, so these only fill gaps left false.
	subFS := flag.NewFlagSet("validate spec", flag.ContinueOnError)
	cliutil.SetUsage(subFS, "atomic validate spec [--json] [--suggest]")
	subFS.SetOutput(w)
	var subJSON, subSuggest bool
	subFS.BoolVar(&subJSON, "json", false, "emit JSON output ({schema_version:1, findings:[...]})")
	subFS.BoolVar(&subSuggest, "suggest", false, "print structural templates for content-FAIL rules")
	_ = subFS.Parse(subArgs)
	paths := subFS.Args()

	if subJSON {
		jsonOut = true
	}
	if subSuggest {
		suggest = true
	}

	if len(paths) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(w, "atomic validate spec: cannot get working directory: %v\n", err)
			return 2
		}
		root := findRepoRoot(cwd)
		if root == "" {
			fmt.Fprintf(w, "atomic validate spec: no .git found from %s\n", cwd)
			return 2
		}
		globbed, err := filepath.Glob(filepath.Join(root, "docs", "spec", "*.md"))
		if err != nil {
			fmt.Fprintf(w, "atomic validate spec: glob error: %v\n", err)
			return 2
		}
		paths = globbed
		if len(paths) == 0 {
			fmt.Fprintf(w, "atomic validate spec: no spec files found in docs/spec/\n")
			return 2
		}
	}

	var all []Finding
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(w, "atomic validate spec: cannot read %s: %v\n", p, err)
			return 2
		}
		findings, err := RunSpecRules(p, src)
		if err != nil {
			fmt.Fprintf(w, "atomic validate spec: %v\n", err)
			return 2
		}
		all = append(all, findings...)
	}

	sortFindings(all)
	s := summarize(all)

	if jsonOut {
		printJSON(w, all, s)
	} else {
		printHeader(w, "spec", "structural integrity")
		printHuman(w, all, s, suggest)
	}

	return exitCode(s)
}
