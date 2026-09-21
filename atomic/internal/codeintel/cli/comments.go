package cli

// `atomic code comments`: a line scan over `git diff`, per
// docs/design/comment-counter.md. Never opens the code-intel index.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

type commentSyntax struct {
	linePrefix string
	blockOpen  string
	blockClose string
}

var extSyntax = map[string]commentSyntax{
	"go":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"js":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"ts":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"tsx":    {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"jsx":    {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"java":   {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"kt":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"c":      {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"cc":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"cpp":    {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"h":      {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"hpp":    {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"cs":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"swift":  {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"rs":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"scala":  {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"dart":   {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"php":    {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"m":      {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"mm":     {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"proto":  {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"groovy": {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},

	"py":   {linePrefix: "#"},
	"rb":   {linePrefix: "#"},
	"sh":   {linePrefix: "#"},
	"bash": {linePrefix: "#"},
	"zsh":  {linePrefix: "#"},
	"yaml": {linePrefix: "#"},
	"yml":  {linePrefix: "#"},
	"toml": {linePrefix: "#"},
	"pl":   {linePrefix: "#"},
	"r":    {linePrefix: "#"},
	"ex":   {linePrefix: "#"},
	"exs":  {linePrefix: "#"},
	"ps1":  {linePrefix: "#"},

	"sql": {linePrefix: "--"},
	"lua": {linePrefix: "--"},
	"hs":  {linePrefix: "--"},
	"elm": {linePrefix: "--"},

	"erl": {linePrefix: "%"},
	"hrl": {linePrefix: "%"},

	"html":   {blockOpen: "<!--", blockClose: "-->"},
	"vue":    {blockOpen: "<!--", blockClose: "-->"},
	"svelte": {blockOpen: "<!--", blockClose: "-->"},
	"xml":    {blockOpen: "<!--", blockClose: "-->"},

	"css":  {blockOpen: "/*", blockClose: "*/"},
	"scss": {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
	"less": {linePrefix: "//", blockOpen: "/*", blockClose: "*/"},
}

func syntaxFor(path string) (commentSyntax, bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	s, ok := extSyntax[ext]
	return s, ok
}

// isDirective reports the two line-prefix matches excluded from comment
// classification even though they share a comment's prefix.
func isDirective(trimmed, linePrefix string) bool {
	switch linePrefix {
	case "//":
		return strings.HasPrefix(trimmed, "//go:")
	case "#":
		return strings.HasPrefix(trimmed, "#!")
	default:
		return false
	}
}

// commentEntry is one reported comment: a run of consecutive added comment
// lines in one file.
type commentEntry struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Lines int    `json:"lines"`
	Text  string `json:"text"`
	Over  bool   `json:"over"`
}

type openComment struct {
	path      string
	startLine int
	endLine   int
	texts     []string
}

// commentScanner accumulates comment entries for one file. Block state
// resets per file: -U0 gives no context to tell a resumed block from code.
type commentScanner struct {
	maxLines int
	out      *[]commentEntry

	file      string
	syntax    commentSyntax
	hasSyntax bool
	inBlock   bool
	open      *openComment
}

func newCommentScanner(maxLines int, out *[]commentEntry) *commentScanner {
	return &commentScanner{maxLines: maxLines, out: out}
}

func (s *commentScanner) setFile(path string) {
	s.closeOpen()
	s.file = path
	s.syntax, s.hasSyntax = syntaxFor(path)
	s.inBlock = false
}

func (s *commentScanner) closeOpen() {
	if s.open == nil {
		return
	}
	lines := s.open.endLine - s.open.startLine + 1
	*s.out = append(*s.out, commentEntry{
		Path:  s.open.path,
		Line:  s.open.startLine,
		Lines: lines,
		Text:  firstWords(strings.Join(s.open.texts, " ")),
		Over:  lines > s.maxLines,
	})
	s.open = nil
}

// feed classifies one added line. A line-number gap since the open comment
// closes it: hunk boundaries are the only source of gaps.
func (s *commentScanner) feed(lineNo int, text string) {
	if !s.hasSyntax {
		s.closeOpen()
		return
	}
	if s.open != nil && lineNo != s.open.endLine+1 {
		s.closeOpen()
	}

	trimmed := strings.TrimSpace(text)
	var isComment bool
	var stripped string

	switch {
	case s.inBlock:
		isComment = true
		stripped = trimmed
		if s.syntax.blockClose != "" {
			if idx := strings.Index(trimmed, s.syntax.blockClose); idx >= 0 {
				stripped = trimmed[:idx]
				s.inBlock = false
			}
		}
	case s.syntax.blockOpen != "" && strings.HasPrefix(trimmed, s.syntax.blockOpen):
		isComment = true
		rest := trimmed[len(s.syntax.blockOpen):]
		if idx := strings.Index(rest, s.syntax.blockClose); idx >= 0 {
			stripped = rest[:idx]
		} else {
			stripped = rest
			s.inBlock = true
		}
	case s.syntax.linePrefix != "" && strings.HasPrefix(trimmed, s.syntax.linePrefix) && !isDirective(trimmed, s.syntax.linePrefix):
		isComment = true
		stripped = strings.TrimPrefix(trimmed, s.syntax.linePrefix)
	}

	if !isComment {
		s.closeOpen()
		return
	}
	stripped = strings.TrimSpace(stripped)

	if s.open == nil {
		texts := []string{}
		if stripped != "" {
			texts = append(texts, stripped)
		}
		s.open = &openComment{path: s.file, startLine: lineNo, endLine: lineNo, texts: texts}
		return
	}
	s.open.endLine = lineNo
	if stripped != "" {
		s.open.texts = append(s.open.texts, stripped)
	}
}

func firstWords(s string) string {
	joined := strings.Join(strings.Fields(s), " ")
	runes := []rune(joined)
	if len(runes) > 60 {
		joined = strings.TrimSpace(string(runes[:60]))
	}
	return joined
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func parseAddedComments(diffText string, maxLines int) []commentEntry {
	var out []commentEntry
	scanner := newCommentScanner(maxLines, &out)

	lineNo := 0
	skipFile := false

	for _, line := range strings.Split(diffText, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			scanner.closeOpen()
			scanner.file = ""
			scanner.hasSyntax = false
			scanner.inBlock = false
			skipFile = false

		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				scanner.closeOpen()
				scanner.hasSyntax = false
				skipFile = true
				continue
			}
			skipFile = false
			scanner.setFile(strings.TrimPrefix(path, "b/"))

		case strings.HasPrefix(line, "@@ "):
			if n, ok := parseHunkNewStart(line); ok {
				lineNo = n
			}

		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if !skipFile {
				scanner.feed(lineNo, strings.TrimPrefix(line, "+"))
			}
			lineNo++
		}
	}
	scanner.closeOpen()
	return out
}

func parseHunkNewStart(line string) (int, bool) {
	m := hunkHeaderRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// scanFile classifies an untracked file's full contents, every line counted
// as added from line 1.
func scanFile(absPath, relPath string, maxLines int) ([]commentEntry, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	var out []commentEntry
	scanner := newCommentScanner(maxLines, &out)
	scanner.setFile(relPath)

	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, text := range lines {
		scanner.feed(i+1, text)
	}
	scanner.closeOpen()
	return out, nil
}

func runGitCommand(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var stdout, stderrBuf bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func listUntrackedFiles(root string) ([]string, error) {
	out, err := runGitCommand(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func runComments(args []string, root string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code comments", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code comments [--diff <range>] [--json]")
	var diffRange string
	var asJSON bool
	fs.StringVar(&diffRange, "diff", "HEAD", "git diff range to scan for added comments")
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, warns, err := config.LoadRepoConfig(config.RepoConfigPath(root))
	if err != nil {
		// A degraded config never fails the run: max_lines falls back to the
		// default, same lenient contract `atomic code index` uses.
		fmt.Fprintf(stderr, "atomic code comments: config: %v (using default max_lines)\n", err)
		cfg = &config.RepoConfig{}
	}
	for _, w := range warns {
		fmt.Fprintf(stderr, "atomic code comments: %s\n", w.Message)
	}
	maxLines, warn := config.ResolveMaxLines(cfg.Comments)
	if warn != nil {
		fmt.Fprintf(stderr, "atomic code comments: %s\n", warn.Message)
	}

	diffOut, err := runGitCommand(root, "diff", "-U0", "--no-color", "--no-ext-diff", diffRange, "--")
	if err != nil {
		fmt.Fprintf(stderr, "atomic code comments: %v\n", err)
		return 2
	}

	comments := parseAddedComments(diffOut, maxLines)

	if !strings.Contains(diffRange, "..") {
		untracked, err := listUntrackedFiles(root)
		if err != nil {
			fmt.Fprintf(stderr, "atomic code comments: %v\n", err)
			return 2
		}
		for _, rel := range untracked {
			entries, err := scanFile(filepath.Join(root, rel), rel, maxLines)
			if err != nil {
				fmt.Fprintf(stderr, "atomic code comments: %s: %v\n", rel, err)
				continue
			}
			comments = append(comments, entries...)
		}
	}

	sort.Slice(comments, func(i, j int) bool {
		if comments[i].Path != comments[j].Path {
			return comments[i].Path < comments[j].Path
		}
		return comments[i].Line < comments[j].Line
	})

	over := 0
	for _, c := range comments {
		if c.Over {
			over++
		}
	}

	if asJSON {
		payload := struct {
			Diff     string         `json:"diff"`
			MaxLines int            `json:"max_lines"`
			Added    int            `json:"added"`
			Over     int            `json:"over"`
			Comments []commentEntry `json:"comments"`
		}{diffRange, maxLines, len(comments), over, comments}
		enc, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code comments: marshal: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		fmt.Fprintf(stdout, "comments added: %d (max_lines %d)\n", len(comments), maxLines)
		for _, c := range comments {
			suffix := ""
			if c.Over {
				suffix = "   OVER"
			}
			plural := "s"
			if c.Lines == 1 {
				plural = ""
			}
			span := fmt.Sprintf("%d line%s", c.Lines, plural)
			fmt.Fprintf(stdout, "%s:%d   %-10s%s%s\n", c.Path, c.Line, span, c.Text, suffix)
		}
	}

	if over > 0 {
		return 1
	}
	return 0
}
