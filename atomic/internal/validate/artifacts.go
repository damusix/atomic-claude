package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	spans := extractCodeSpans(src)

	var findings []Finding
	for _, span := range spans {
		ff := checkSpan(path, span.text, span.line, topVerbs)
		findings = append(findings, ff...)
	}
	return findings
}

type codeSpanEntry struct {
	text string
	line int
}

// extractCodeSpans returns inline code spans plus fenced block contents, each
// carrying its 1-indexed line.
func extractCodeSpans(src []byte) []codeSpanEntry {
	var out []codeSpanEntry

	// InlineRefs skips fenced and indented code block subtrees.
	refs, _ := mdparse.InlineRefs(src)
	for _, r := range refs {
		if r.Kind == "code" {
			out = append(out, codeSpanEntry{text: r.Text, line: r.Line})
		}
	}

	out = append(out, extractFencedBlocks(src)...)
	return out
}

// extractFencedBlocks emits one entry per content line, so a flag token on one
// line is never attributed to a citation on another.
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
			if strings.TrimSpace(raw) != "" {
				out = append(out, codeSpanEntry{
					text: raw,
					line: lineNum,
				})
			}
		}
	}
	return out
}

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

// checkSpan reports A1 violations in one already-extracted code span.
func checkSpan(path, text string, line int, topVerbs map[string]bool) []Finding {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}

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

	if !topVerbs[rest[0]] {
		return nil
	}

	cmd := longestMatch(rest)
	if cmd == nil {
		// No known path matches — accepted false negative.
		return nil
	}

	known := make(map[string]bool, len(cmd.Flags))
	for _, f := range cmd.Flags {
		known[f] = true
	}

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

func formatFlagList(flags []string) string {
	if len(flags) == 0 {
		return "(none)"
	}
	return strings.Join(flags, ", ")
}

// RunArtifactRules scans for A1 violations. A non-empty paths limits the scan;
// otherwise the whole artifact corpus is enumerated.
func RunArtifactRules(repoRoot string, paths []string) ([]Finding, error) {
	if len(paths) > 0 {
		return runArtifactPaths(repoRoot, paths)
	}
	return runArtifactCorpus(repoRoot)
}

func runArtifactCorpus(repoRoot string) ([]Finding, error) {
	artifacts, err := bundlemirror.Enumerate(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate artifacts: %w", err)
	}

	var all []Finding
	for _, a := range artifacts {
		srcPath := filepath.Join(bundlespec.SourceRoot(repoRoot), a.Target)
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

func runArtifactsCollect(repoRoot string) ([]Finding, summary, int) {
	findings, err := RunArtifactRules(repoRoot, nil)
	if err != nil {
		return nil, summary{}, 2
	}
	return findings, summarize(findings), 0
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
