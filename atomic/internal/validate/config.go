package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

// builtinSubagents need no agents/<name>.md file — Claude Code ships them.
var builtinSubagents = map[string]bool{
	"general-purpose": true,
	"Explore":         true,
	"Plan":            true,
}

var reSubagentType = regexp.MustCompile(`subagent_type:\s*["']([a-zA-Z][a-zA-Z0-9_-]+)["']`)

// reAtRef is deliberately loose to the right of the @; runC5's email guard is
// what keeps `bob@host.com` from reading as a file include.
var reAtRef = regexp.MustCompile(`@([./a-zA-Z0-9_-]+\.[a-zA-Z]{2,4})`)

// isEmailLocalChar reports whether b can appear left of an email's @. A real
// @-ref sits at a word boundary, never after a local-part character. RE2 has no
// lookbehind, so runC5 applies this at match time instead of in the regex.
func isEmailLocalChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '_', b == '%', b == '+', b == '-':
		return true
	default:
		return false
	}
}

// RunConfigRules runs the referential-integrity rules over repoRoot, returning
// findings sorted by (Path, Line, Rule). Exported so tests can drive a fixture.
func RunConfigRules(repoRoot string) ([]Finding, error) {
	var findings []Finding

	c7, err := runC7(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c7...)

	c9, err := runC9(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c9...)

	c3, err := runC3(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c3...)

	c5, err := runC5(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c5...)

	sortFindings(findings)
	return findings, nil
}

// runC3 checks that every subagent_type literal in commands/*.md prose resolves
// to an agents/<name>.md or a built-in.
func runC3(repoRoot string) ([]Finding, error) {
	commandsDir := filepath.Join(bundlespec.SourceRoot(repoRoot), "commands")
	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("C3: read commands dir: %w", err)
	}

	// Commands are checked expanded, as bundlemirror expands them: a dispatch can
	// live inside a shared partial, invisible in the unexpanded source.
	partials, err := templaterender.LoadPartials(
		filepath.Join(bundlespec.SourceRoot(repoRoot), templaterender.PartialsDir))
	if err != nil {
		return nil, fmt.Errorf("C3: load partials: %w", err)
	}

	var findings []Finding
	for _, e := range entries {
		if e.IsDir() {
			continue // top-level commands/*.md only
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		cmdPath := filepath.Join(commandsDir, e.Name())
		src, err := os.ReadFile(cmdPath)
		if err != nil {
			return nil, fmt.Errorf("C3: read %s: %w", cmdPath, err)
		}
		src, err = templaterender.Expand(partials, e.Name(), src)
		if err != nil {
			return nil, fmt.Errorf("C3: expand %s: %w", cmdPath, err)
		}

		// Fenced and indented code blocks are skipped, but inline backtick spans
		// are not — a literal inside one still matches.
		segments := mdparse.TextSegments(src)
		for _, seg := range segments {
			matches := reSubagentType.FindAllStringSubmatchIndex(seg.Text, -1)
			for _, loc := range matches {
				name := seg.Text[loc[2]:loc[3]]
				if builtinSubagents[name] {
					continue
				}
				agentPath := filepath.Join(bundlespec.SourceRoot(repoRoot), "agents", name+".md")
				if _, err := os.Stat(agentPath); os.IsNotExist(err) {
					line := seg.Line + strings.Count(seg.Text[:loc[0]], "\n")
					findings = append(findings, Finding{
						Severity: "FAIL",
						Rule:     "C3",
						Path:     relPath(repoRoot, cmdPath),
						Line:     line,
						Message:  fmt.Sprintf("subagent_type %q — no agents/%s.md", name, name),
					})
				}
			}
		}
	}
	return findings, nil
}

// runC5 checks that every @-ref in CLAUDE.md resolves. Project-local overlays
// are skipped: they are user-owned and may carry @-ref-shaped text such as npm
// scoped package names.
func runC5(repoRoot string) ([]Finding, error) {
	candidates := []string{
		filepath.Join(bundlespec.SourceRoot(repoRoot), "CLAUDE.md"),
	}

	var findings []Finding
	for _, p := range candidates {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		segments := mdparse.TextSegments(src)
		for _, seg := range segments {
			matches := reAtRef.FindAllStringSubmatchIndex(seg.Text, -1)
			for _, loc := range matches {
				if loc[0] > 0 && isEmailLocalChar(seg.Text[loc[0]-1]) {
					continue
				}
				refPath := seg.Text[loc[2]:loc[3]]
				// Repo-root relative, not relative to the containing file.
				target := filepath.Join(repoRoot, filepath.FromSlash(refPath))
				if _, err := os.Stat(target); os.IsNotExist(err) {
					line := seg.Line + strings.Count(seg.Text[:loc[0]], "\n")
					findings = append(findings, Finding{
						Severity: "FAIL",
						Rule:     "C5",
						Path:     relPath(repoRoot, p),
						Line:     line,
						Message:  fmt.Sprintf("@-ref %s does not resolve", refPath),
					})
				}
			}
		}
	}
	return findings, nil
}

// runC7 checks for duplicate name: values across agents/*.md frontmatter.
func runC7(repoRoot string) ([]Finding, error) {
	agentsDir := filepath.Join(bundlespec.SourceRoot(repoRoot), "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("C7: read agents dir: %w", err)
	}

	// name → first file that declared it
	seen := make(map[string]string)
	var findings []Finding

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		agentPath := filepath.Join(agentsDir, e.Name())
		src, err := os.ReadFile(agentPath)
		if err != nil {
			return nil, fmt.Errorf("C7: read %s: %w", agentPath, err)
		}

		meta, _, err := frontmatter.Parse(string(src))
		if err != nil || meta == nil {
			continue
		}

		nameVal, ok := meta["name"]
		if !ok {
			continue
		}
		name, ok := nameVal.(string)
		if !ok || name == "" {
			continue
		}

		rel := relPath(repoRoot, agentPath)
		if first, dup := seen[name]; dup {
			findings = append(findings, Finding{
				Severity: "FAIL",
				Rule:     "C7",
				Path:     rel,
				Line:     0,
				Message:  fmt.Sprintf("duplicate name: %q — also declared in %s", name, first),
			})
		} else {
			seen[name] = rel
		}
	}
	return findings, nil
}

// runC9 checks the atomic- prefix on agents/, skills/, and output-styles/
// entries. Commands are excluded — they carry no prefix requirement.
func runC9(repoRoot string) ([]Finding, error) {
	var findings []Finding

	agentsDir := filepath.Join(bundlespec.SourceRoot(repoRoot), "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if !bundlespec.MatchesAgent(e.Name()) {
				findings = append(findings, Finding{
					Severity: "WARN",
					Rule:     "C9",
					Path:     relPath(repoRoot, filepath.Join(agentsDir, e.Name())),
					Line:     0,
					Message:  fmt.Sprintf("agents/%s missing atomic- prefix; will not bundle", e.Name()),
				})
			}
		}
	}

	// skills/ entries are directories, unlike the other two.
	skillsDir := filepath.Join(bundlespec.SourceRoot(repoRoot), "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if !bundlespec.MatchesSkillDir(e.Name()) {
				findings = append(findings, Finding{
					Severity: "WARN",
					Rule:     "C9",
					Path:     relPath(repoRoot, filepath.Join(skillsDir, e.Name())),
					Line:     0,
					Message:  fmt.Sprintf("skills/%s missing atomic- prefix; will not bundle", e.Name()),
				})
			}
		}
	}

	stylesDir := filepath.Join(bundlespec.SourceRoot(repoRoot), "output-styles")
	if entries, err := os.ReadDir(stylesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if !bundlespec.MatchesOutputStyle(e.Name()) {
				findings = append(findings, Finding{
					Severity: "WARN",
					Rule:     "C9",
					Path:     relPath(repoRoot, filepath.Join(stylesDir, e.Name())),
					Line:     0,
					Message:  fmt.Sprintf("output-styles/%s missing atomic prefix; will not bundle", e.Name()),
				})
			}
		}
	}

	return findings, nil
}

// relPath returns path relative to root, or path itself on error.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func runConfig(subArgs []string, jsonOut, suggest bool, w io.Writer) int {
	// Re-parsed here so flags placed after the subcommand are honored.
	subFS := flag.NewFlagSet("validate config", flag.ContinueOnError)
	cliutil.SetUsage(subFS, "atomic validate config [--json] [--suggest]")
	subFS.SetOutput(w)
	var subJSON, subSuggest bool
	subFS.BoolVar(&subJSON, "json", false, "emit JSON output ({schema_version:1, findings:[...]})")
	subFS.BoolVar(&subSuggest, "suggest", false, "print structural templates for content-FAIL rules")
	_ = subFS.Parse(subArgs)

	if subJSON {
		jsonOut = true
	}
	if subSuggest {
		suggest = true
	}

	// Path args are ignored: config validation is whole-repo only.

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate config: cannot get working directory: %v\n", err)
		return 2
	}
	root := findRepoRoot(cwd)
	if root == "" {
		fmt.Fprintf(w, "atomic validate config: no .git found from %s\n", cwd)
		return 2
	}

	findings, err := RunConfigRules(root)
	if err != nil {
		fmt.Fprintf(w, "atomic validate config: %v\n", err)
		return 2
	}

	s := summarize(findings)
	if jsonOut {
		// No header: the JSON envelope is the only chrome.
		printJSON(w, findings, s)
	} else {
		printHeader(w, "config", "referential integrity")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}
