package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlemirror"
	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

// universalFlags is the set of flags always accepted by every command.
// Normalised forms only (values already stripped by callers).
var universalFlags = map[string]bool{
	"--help":            true,
	"-h":                true,
	"--version":         true,
	"-v":                true,
	"--repo":            true,
	"--no-update-check": true,
}

// ScanArtifactText is the pure scanning seam. It accepts the artifact path
// (for Finding.Path) and the raw markdown bytes as a string, and returns all
// A1 findings. No filesystem access — callers supply the content.
//
// Exported so the test package can call it without writing fixture files.
func ScanArtifactText(path, src string) []Finding {
	return scanArtifactBytes(path, []byte(src))
}

// scanArtifactBytes is the internal implementation shared by ScanArtifactText
// and the file-based scanners.
func scanArtifactBytes(path string, src []byte) []Finding {
	topVerbs := cliusage.TopLevelVerbs()
	spans := extractCodeSpans(src)

	var findings []Finding
	for _, span := range spans {
		ff := checkSpan(path, span.text, span.line, topVerbs)
		findings = append(findings, ff...)
	}
	return findings
}

// codeSpanEntry is a text+line pair extracted from inline code spans and fenced
// code blocks.
type codeSpanEntry struct {
	text string
	line int
}

// extractCodeSpans returns all inline code span texts (via mdparse.InlineRefs)
// and fenced code block contents (via a line-prescan). Each entry carries the
// 1-indexed line of the span or the first line of the block.
func extractCodeSpans(src []byte) []codeSpanEntry {
	var out []codeSpanEntry

	// Inline code spans — goldmark AST walk via InlineRefs (Kind=="code").
	// InlineRefs skips fenced/indented code block subtrees.
	refs, _ := mdparse.InlineRefs(src)
	for _, r := range refs {
		if r.Kind == "code" {
			out = append(out, codeSpanEntry{text: r.Text, line: r.Line})
		}
	}

	// Fenced code blocks — line-prescan to extract their content.
	out = append(out, extractFencedBlocks(src)...)
	return out
}

// extractFencedBlocks returns one codeSpanEntry per line inside fenced code
// blocks. Each line is emitted separately so that flag tokens on one line
// (e.g. from a find command) cannot be attributed to an atomic citation on a
// different line.
func extractFencedBlocks(src []byte) []codeSpanEntry {
	lines := strings.Split(string(src), "\n")
	var out []codeSpanEntry

	inFence := false
	var fenceMarker byte
	var fenceLen int

	for i, raw := range lines {
		lineNum := i + 1
		rawBytes := []byte(raw)

		if ch, flen := fenceOpenByte(rawBytes); !inFence && flen > 0 {
			inFence = true
			fenceMarker = ch
			fenceLen = flen
			continue
		}
		if inFence {
			if isFenceCloseByte(rawBytes, fenceMarker, fenceLen) {
				inFence = false
				fenceMarker = 0
				fenceLen = 0
				continue
			}
			// Emit each non-empty content line as its own span.
			if strings.TrimSpace(raw) != "" {
				out = append(out, codeSpanEntry{
					text: raw,
					line: lineNum,
				})
			}
		}
	}
	// Unclosed fence at EOF: content lines already emitted; inFence state dropped (no matching close).
	return out
}

// fenceOpenByte and isFenceCloseByte are byte-slice versions of the mdparse
// internal helpers (not exported from mdparse, so we replicate them minimally here).

func fenceOpenByte(line []byte) (marker byte, length int) {
	if len(line) == 0 {
		return 0, 0
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 0
	for n < len(line) && line[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return ch, n
}

func isFenceCloseByte(line []byte, marker byte, fenceLen int) bool {
	n := 0
	for n < len(line) && line[n] == marker {
		n++
	}
	if n < fenceLen {
		return false
	}
	rest := strings.TrimRight(string(line[n:]), " \t\r")
	return len(rest) == 0
}

// checkSpan checks a single code span text for A1 violations and returns any
// findings. The span text has already been extracted from backticks or a fenced
// block by the caller.
func checkSpan(path, text string, line int, topVerbs map[string]bool) []Finding {
	// Tokenize the span: split on whitespace, keep only non-empty tokens.
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}

	// Locate "atomic" in the token stream.
	atomicIdx := -1
	for i, t := range tokens {
		if t == "atomic" {
			atomicIdx = i
			break
		}
	}
	if atomicIdx < 0 {
		return nil
	}

	rest := tokens[atomicIdx+1:]
	if len(rest) == 0 {
		return nil
	}

	// Gate: first token after "atomic" must be a known top-level verb.
	if !topVerbs[rest[0]] {
		return nil
	}

	// Greedily resolve the longest known verb-path prefix.
	cmd := longestMatch(rest)
	if cmd == nil {
		// Unresolved citation: no known path matches. Emit nothing (accepted false-negative).
		return nil
	}

	// Build flag set for this command — O(1) lookup.
	known := make(map[string]bool, len(cmd.Flags))
	for _, f := range cmd.Flags {
		known[f] = true
	}

	// Remaining tokens after the matched path are positional args + flags.
	matched := cmd.Path
	flagTokens := rest[len(matched):]

	var findings []Finding
	for _, tok := range flagTokens {
		if !looksLikeFlag(tok) {
			continue
		}
		normalized := normalizeFlag(tok)
		if universalFlags[normalized] {
			continue
		}
		if known[normalized] {
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

// longestMatch returns the Command whose Path is the longest prefix of tokens
// that matches any table entry. Returns nil when no entry matches.
func longestMatch(tokens []string) *cliusage.Command {
	for length := len(tokens); length >= 1; length-- {
		candidate := tokens[:length]
		// Strip flag tokens: paths are bare word tokens (no leading -).
		// Trim the candidate to the first flag token if any.
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
			// Iterating longest-first: first match is always the longest path.
			return cmd
		}
	}
	return nil
}

// looksLikeFlag reports whether token starts with one or two dashes followed
// by a letter, e.g. "--json" or "-h".
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

// normalizeFlag strips a trailing =value from a flag token, e.g.
// "--limit=10" → "--limit", "--json" → "--json".
func normalizeFlag(token string) string {
	if idx := strings.IndexByte(token, '='); idx >= 0 {
		return token[:idx]
	}
	return token
}

// tokenize splits text on whitespace and returns non-empty lowercase tokens.
// Lowercasing is intentional: flag tokens like "--JSON" are treated as "--json".
func tokenize(text string) []string {
	raw := strings.Fields(text)
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if t != "" {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}

// formatFlagList formats a flag list as a comma-separated string, or "(none)"
// when empty.
func formatFlagList(flags []string) string {
	if len(flags) == 0 {
		return "(none)"
	}
	return strings.Join(flags, ", ")
}

// RunArtifactRules scans the artifact corpus rooted at repoRoot for A1
// violations and returns all findings. When paths is non-empty, only those
// files are scanned; otherwise the full corpus is enumerated via
// bundlemirror.Enumerate.
func RunArtifactRules(repoRoot string, paths []string) ([]Finding, error) {
	if len(paths) > 0 {
		return runArtifactPaths(repoRoot, paths)
	}
	return runArtifactCorpus(repoRoot)
}

// runArtifactCorpus scans all artifact files enumerated by bundlemirror.
func runArtifactCorpus(repoRoot string) ([]Finding, error) {
	artifacts, err := bundlemirror.Enumerate(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate artifacts: %w", err)
	}

	var all []Finding
	for _, a := range artifacts {
		// a.Target is the relative path inside the install layout (e.g.
		// "agents/atomic-builder.md"). The source file is at repoRoot/a.Target.
		srcPath := filepath.Join(repoRoot, a.Target)
		src, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", a.Target, err)
		}
		ff := scanArtifactBytes(a.Target, src)
		all = append(all, ff...)
	}
	sortFindings(all)
	return all, nil
}

// runArtifactPaths scans the explicitly provided paths.
func runArtifactPaths(repoRoot string, paths []string) ([]Finding, error) {
	var all []Finding
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(repoRoot, p)
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		rel := p
		if filepath.IsAbs(p) {
			if r, err := filepath.Rel(repoRoot, p); err == nil {
				rel = r
			}
		}
		ff := scanArtifactBytes(rel, src)
		all = append(all, ff...)
	}
	sortFindings(all)
	return all, nil
}

// runArtifacts is the entry point for `atomic validate artifacts [paths...]`.
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

	findings, err := RunArtifactRules(root, paths)
	if err != nil {
		fmt.Fprintf(w, "atomic validate artifacts: %v\n", err)
		return 2
	}

	s := summarize(findings)
	if jsonOut {
		printJSON(w, findings, s)
	} else {
		printHeader(w, "artifacts", "CLI-flag citation integrity")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}

// runArtifactsCollect runs the artifacts check and returns findings + summary
// without printing. Used by runWholeRepo to aggregate before printing.
func runArtifactsCollect(repoRoot string) ([]Finding, summary, int) {
	findings, err := RunArtifactRules(repoRoot, nil)
	if err != nil {
		return nil, summary{}, 2
	}
	return findings, summarize(findings), 0
}

// runFlagSet is the flag.FlagSet parse helper shared by runArtifacts callers.
// It mirrors the pattern used by runSpec / runConfig.
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
