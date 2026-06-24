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
)

// builtinSubagents is the set of subagent names built into Claude Code that do
// not require a corresponding agents/<name>.md file. Hardcoded per spec C3.
var builtinSubagents = map[string]bool{
	"general-purpose": true,
	"Explore":         true,
	"Plan":            true,
}

// reSubagentType matches subagent_type: "name" or subagent_type: 'name' in
// command prose (outside fenced code blocks). The name must start with a letter
// and contain only alphanumerics, underscores, and hyphens.
//
// C3: subagent_type grammar.
var reSubagentType = regexp.MustCompile(`subagent_type:\s*["']([a-zA-Z][a-zA-Z0-9_-]+)["']`)

// reAtRef matches @-refs in CLAUDE.md / claude.local.md prose. An @-ref is a
// bare @-prefixed path that resolves to a file: @path/to/file.ext where the
// path contains only letters, digits, underscores, hyphens, dots, and slashes,
// and ends with a 2-4 character extension.
//
// Grammar: @([./a-zA-Z0-9_-]+\.[a-zA-Z]{2,4})
//
// C5: @-ref grammar. The pattern is intentionally simple — it matches the
// actual @-ref syntax used by Claude Code (e.g. @.claude/project/signals.md)
// without false-positives on email addresses (which contain @ but not
// extension-terminated paths) or markdown links.
var reAtRef = regexp.MustCompile(`@([./a-zA-Z0-9_-]+\.[a-zA-Z]{2,4})`)

// RunConfigRules runs C3, C5, C7, C9 on the repo rooted at repoRoot.
// Returns findings sorted by (Path, Line, Rule) and any filesystem error.
//
// Exported so tests can call it directly with a synthetic repo fixture.
func RunConfigRules(repoRoot string) ([]Finding, error) {
	var findings []Finding

	// C7: duplicate name: across agents/*.md — run first, independent of CLAUDE.md.
	c7, err := runC7(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c7...)

	// C9: prefix check on agents/, skills/, output-styles/ files.
	c9, err := runC9(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c9...)

	// C3: subagent_type in commands/*.md.
	c3, err := runC3(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c3...)

	// C5: @-refs in CLAUDE.md, claude.local.md, CLAUDE.local.md.
	c5, err := runC5(repoRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, c5...)

	sortFindings(findings)
	return findings, nil
}

// runC3 checks that every subagent_type: "name" literal in commands/*.md
// prose (outside fenced code blocks) resolves to agents/<name>.md or is a
// known built-in.
func runC3(repoRoot string) ([]Finding, error) {
	commandsDir := filepath.Join(repoRoot, "commands")
	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("C3: read commands dir: %w", err)
	}

	var findings []Finding
	for _, e := range entries {
		if e.IsDir() {
			continue // skip _templates/ and other subdirs
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		cmdPath := filepath.Join(commandsDir, e.Name())
		src, err := os.ReadFile(cmdPath)
		if err != nil {
			return nil, fmt.Errorf("C3: read %s: %w", cmdPath, err)
		}

		// Extract prose-only text segments (skips fenced/indented code blocks).
		// Note: inline backtick code spans are NOT excluded; a subagent_type
		// literal inside `backtick span` will still match reSubagentType.
		segments := mdparse.TextSegments(src)
		for _, seg := range segments {
			matches := reSubagentType.FindAllStringSubmatchIndex(seg.Text, -1)
			for _, loc := range matches {
				name := seg.Text[loc[2]:loc[3]]
				if builtinSubagents[name] {
					continue
				}
				agentPath := filepath.Join(repoRoot, "agents", name+".md")
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

// runC5 checks that every @-ref in CLAUDE.md resolves to an existing file
// (case-sensitive). Project-local overlays (claude.local.md, CLAUDE.local.md)
// are intentionally NOT scanned: they are user-owned and may contain backtick
// spans resembling @-refs (e.g. npm package paths like @fortawesome/...).
func runC5(repoRoot string) ([]Finding, error) {
	candidates := []string{
		filepath.Join(repoRoot, "CLAUDE.md"),
	}

	var findings []Finding
	for _, p := range candidates {
		src, err := os.ReadFile(p)
		if err != nil {
			continue // file absent: skip
		}

		// Extract prose-only text segments (skips fenced/indented code blocks).
		// Note: inline backtick code spans are NOT excluded; an @-ref inside
		// `backtick span` will still match reAtRef.
		segments := mdparse.TextSegments(src)
		for _, seg := range segments {
			matches := reAtRef.FindAllStringSubmatchIndex(seg.Text, -1)
			for _, loc := range matches {
				refPath := seg.Text[loc[2]:loc[3]]
				// Resolve relative to the repo root (not the file's directory).
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
	agentsDir := filepath.Join(repoRoot, "agents")
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
			continue // no frontmatter or unparsable: skip
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

// runC9 checks that files in agents/, skills/, and output-styles/ satisfy the
// atomic- prefix requirement. skills/ entries are directory names; the others
// are files. Commands are explicitly excluded — they have no prefix requirement.
func runC9(repoRoot string) ([]Finding, error) {
	var findings []Finding

	// agents/*.md — must match bundlespec.MatchesAgent (atomic- prefix + .md)
	agentsDir := filepath.Join(repoRoot, "agents")
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

	// skills/ — directories must have atomic- prefix (bundlespec.MatchesSkillDir)
	skillsDir := filepath.Join(repoRoot, "skills")
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

	// output-styles/*.md — must match bundlespec.MatchesOutputStyle (atomic prefix + .md)
	stylesDir := filepath.Join(repoRoot, "output-styles")
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

// runConfig is the config validator entry point, implementing CP-6 rules.
// Replaces the CP-1 stub in validate.go.
func runConfig(subArgs []string, jsonOut, suggest bool, w io.Writer) int {
	// Honor flags placed after the subcommand (F-1 fix, same pattern as runSpec).
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

	// Path args: ignored in CP-6 (whole-repo only). CP-8 enhancement noted in spec.
	// _ = subFS.Args()

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
		// No header in JSON mode — JSON envelope is the only UI chrome.
		printJSON(w, findings, s)
	} else {
		printHeader(w, "config", "referential integrity")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}
