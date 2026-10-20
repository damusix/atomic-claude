package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlemirror"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

// universalFlags is the set of flags always accepted by every command.
var universalFlags = map[string]bool{
	"--help":            true,
	"-h":                true,
	"--version":         true,
	"-v":                true,
	"--repo":            true,
	"--no-update-check": true,
}

// ScanArtifactText is the pure scanning seam: no filesystem access, callers
// supply the content. Exported so tests can skip writing fixture files.
func ScanArtifactText(path, src string) []Finding {
	return scanArtifactBytes(path, []byte(src))
}

func scanArtifactBytes(path string, src []byte) []Finding {
	topVerbs := cliusage.TopLevelVerbs()

	var findings []Finding
	for _, c := range extractCitations(src) {
		findings = append(findings, checkSpan(path, c.Text, c.Line, topVerbs)...)
	}
	return findings
}

// extractCitations returns inline code spans plus one entry per code-block
// line, so a flag on one line is never attributed to a verb on another.
func extractCitations(src []byte) []mdparse.CodeLine {
	var out []mdparse.CodeLine
	refs, _ := mdparse.InlineRefs(src)
	for _, r := range refs {
		if r.Kind == "code" {
			out = append(out, mdparse.CodeLine{Text: r.Text, Line: r.Line})
		}
	}
	for _, c := range mdparse.CodeBlockLines(src) {
		if c.Lang != "mermaid" {
			out = append(out, c)
		}
	}
	return out
}

var shellSeparators = map[string]bool{"|": true, "||": true, "&&": true, ";": true, "&": true, `\`: true}

const trailingPunct = ",;:.)]\"'"

var boundaryPrefixes = []string{">", "2>", "1>", "&>", "$(", "`", "#"}

// isShellBoundary judges t, the first of tokens. A `<` opens an input redirect
// unless a token from t on closes it as a `<multi word>` placeholder.
func isShellBoundary(tokens []string) bool {
	t := tokens[0]
	if shellSeparators[t] {
		return true
	}
	if strings.HasPrefix(t, "<") {
		return !slices.ContainsFunc(tokens, func(s string) bool {
			return strings.HasSuffix(strings.TrimRight(s, trailingPunct), ">")
		})
	}
	for _, p := range boundaryPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// citationTokens returns the original-case tokens after the first "atomic", up
// to the first shell boundary; later tokens belong to another command.
func citationTokens(text string) []string {
	tokens := strings.Fields(text)
	for i, t := range tokens {
		if !strings.EqualFold(t, "atomic") {
			continue
		}
		rest := tokens[i+1:]
		for j := range rest {
			if isShellBoundary(rest[j:]) {
				return rest[:j]
			}
		}
		return rest
	}
	return nil
}

// checkSpan reports A1 violations in one citation: an inline code span or a
// code-block line.
func checkSpan(path, text string, line int, topVerbs map[string]bool) []Finding {
	raw := citationTokens(text)
	rest := lowerAll(raw)
	if len(rest) == 0 || !topVerbs[rest[0]] {
		return nil
	}

	cmd := longestMatch(rest)
	if cmd == nil {
		return checkUnresolved(path, line, pathWords(raw))
	}

	known := make(map[string]bool, len(cmd.Flags))
	for _, f := range cmd.Flags {
		known[f] = true
	}

	var findings []Finding
	for _, tok := range rest[len(cmd.Path):] {
		if tok == "--" {
			break
		}
		if !looksLikeFlag(tok) {
			continue
		}
		normalized := normalizeFlag(tok)
		if universalFlags[normalized] || known[normalized] {
			continue
		}
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "A1",
			Path:     path,
			Line:     line,
			Message: fmt.Sprintf(
				"unknown flag %s for `atomic %s`; known flags: %s",
				normalized,
				strings.Join(cmd.Path, " "),
				formatFlagList(cmd.Flags),
			),
		})
	}
	return findings
}

// checkUnresolved judges a citation that names no full command, per the rules
// in docs/spec/validate-artifact-cli-flags.md.
func checkUnresolved(path string, line int, words []string) []Finding {
	commands := cliusage.Commands()
	var findings []Finding
	namespaces := [][]string{nil}
	for _, w := range words {
		var next [][]string
		for _, ns := range namespaces {
			for _, alt := range strings.FieldsFunc(w, func(r rune) bool { return r == '|' || r == '/' }) {
				p := append(slices.Clone(ns), alt)
				if cliusage.LookupByPath(p) != nil {
					continue
				}
				if len(subcommands(commands, p)) > 0 {
					next = append(next, p)
					continue
				}
				msg := fmt.Sprintf(
					"unknown command `atomic %s`; known subcommands of `atomic %s`: %s",
					strings.Join(p, " "),
					strings.Join(ns, " "),
					strings.Join(subcommands(commands, ns), ", "),
				)
				findings = append(findings, Finding{Severity: "FAIL", Rule: "A1", Path: path, Line: line, Message: msg})
			}
		}
		namespaces = next
	}
	return findings
}

// pathWords returns the lowercased verb-path words of original-case tokens;
// capitals mark a placeholder, so case is judged before lowering.
func pathWords(tokens []string) []string {
	var words []string
	for _, t := range tokens {
		if t == "--" || looksLikeFlag(t) || isPlaceholder(t) {
			break
		}
		w := strings.TrimRight(t, trailingPunct)
		if isAllCaps(w) {
			break
		}
		if w != "" {
			words = append(words, strings.ToLower(w))
		}
		if w != t {
			break
		}
	}
	return words
}

func isPlaceholder(t string) bool {
	return strings.ContainsAny(t[:1], "<[{($\"'") || strings.Contains(t, "…")
}

func isAllCaps(w string) bool {
	if len(w) < 2 || strings.IndexFunc(w, func(r rune) bool { return r >= 'A' && r <= 'Z' }) < 0 {
		return false
	}
	return strings.IndexFunc(w, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) < 0
}

// subcommands is sorted, and empty when prefix is not a namespace.
func subcommands(commands []cliusage.Command, prefix []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range commands {
		if len(c.Path) <= len(prefix) || !slices.Equal(c.Path[:len(prefix)], prefix) {
			continue
		}
		if next := c.Path[len(prefix)]; !seen[next] {
			seen[next] = true
			out = append(out, next)
		}
	}
	sort.Strings(out)
	return out
}

// longestMatch returns the Command whose Path is the longest matching prefix
// of tokens, or nil.
func longestMatch(tokens []string) *cliusage.Command {
	for length := len(tokens); length >= 1; length-- {
		candidate := tokens[:length]
		pathEnd := length
		for i, t := range candidate {
			if looksLikeFlag(t) {
				pathEnd = i
				break
			}
		}
		if pathEnd == 0 {
			continue
		}
		if cmd := cliusage.LookupByPath(tokens[:pathEnd]); cmd != nil {
			return cmd
		}
	}
	return nil
}

func looksLikeFlag(token string) bool {
	if len(token) < 2 || token[0] != '-' {
		return false
	}
	if token[1] == '-' {
		return len(token) > 2 && isAlpha(token[2])
	}
	return isAlpha(token[1])
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func normalizeFlag(token string) string {
	if idx := strings.IndexByte(token, '='); idx >= 0 {
		return token[:idx]
	}
	return token
}

func lowerAll(tokens []string) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		out[i] = strings.ToLower(t)
	}
	return out
}

func formatFlagList(flags []string) string {
	if len(flags) == 0 {
		return "(none)"
	}
	return strings.Join(flags, ", ")
}

// RunArtifactRules scans for A1 violations. A non-empty paths limits the scan;
// otherwise the whole artifact corpus is enumerated.
func RunArtifactRules(repoRoot string, paths []string) ([]Finding, error) {
	_, findings, err := artifactRules(repoRoot, paths)
	return findings, err
}

// artifactRules also returns the scanned files, keyed as Finding.Path is: the
// context-relative Target for the corpus, repo-relative for explicit paths.
func artifactRules(repoRoot string, paths []string) ([]string, []Finding, error) {
	if len(paths) > 0 {
		return runArtifactPaths(repoRoot, paths)
	}
	return runArtifactCorpus(repoRoot)
}

func runArtifactCorpus(repoRoot string) ([]string, []Finding, error) {
	artifacts, err := bundlemirror.Enumerate(repoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate artifacts: %w", err)
	}

	var scanned []string
	var all []Finding
	for _, a := range artifacts {
		srcPath := filepath.Join(bundlespec.SourceRoot(repoRoot), a.Target)
		src, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read artifact %s: %w", a.Target, err)
		}
		scanned = append(scanned, a.Target)
		ff := scanArtifactBytes(a.Target, src)
		all = append(all, ff...)
	}
	sortFindings(all)
	return scanned, all, nil
}

func runArtifactPaths(repoRoot string, paths []string) ([]string, []Finding, error) {
	var scanned []string
	var all []Finding
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(repoRoot, p)
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", p, err)
		}
		rel := p
		if filepath.IsAbs(p) {
			if r, err := filepath.Rel(repoRoot, p); err == nil {
				rel = r
			}
		}
		scanned = append(scanned, rel)
		ff := scanArtifactBytes(rel, src)
		all = append(all, ff...)
	}
	sortFindings(all)
	return scanned, all, nil
}

func runArtifacts(paths []string, jsonOut, suggest bool, w io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate artifacts: cannot get working directory: %v\n", err)
		return 2
	}
	root := findRepoRoot(cwd)
	if root == "" {
		fmt.Fprintf(w, "atomic validate artifacts: no .git found from %s\n", cwd)
		return 2
	}

	scanned, findings, err := artifactRules(root, paths)
	if err != nil {
		fmt.Fprintf(w, "atomic validate artifacts: %v\n", err)
		return 2
	}

	s := summarize(scanned, findings)
	if jsonOut {
		printJSON(w, findings, s)
	} else {
		printHeader(w, "artifacts", "CLI verb/flag citation integrity")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}

func runArtifactsCollect(repoRoot string) ([]Finding, summary, int) {
	scanned, findings, err := artifactRules(repoRoot, nil)
	if err != nil {
		return nil, summary{}, 2
	}
	return findings, summarize(scanned, findings), 0
}

func parseArtifactsFlags(args []string, w io.Writer) (paths []string, jsonOut, suggest bool, ok bool) {
	fs := flag.NewFlagSet("validate artifacts", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	fs.BoolVar(&suggest, "suggest", false, "print structural templates for content-FAIL rules")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			ok = true
		}
		return nil, false, false, false
	}
	return fs.Args(), jsonOut, suggest, true
}
