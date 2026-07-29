package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScopeSource enumerates how a root was decided — the marker declared in
// .claude/atomic.toml, a fallback mechanism, or the process cwd. String
// renders the lowercase token callers put in CLI output.
type ScopeSource string

const (
	ScopeSourceNone     ScopeSource = "none"
	ScopeSourceMarker   ScopeSource = "marker"
	ScopeSourceGit      ScopeSource = "git"
	ScopeSourceRegistry ScopeSource = "registry"
	ScopeSourceCwd      ScopeSource = "cwd"
)

// String renders the lowercase token used in output.
func (s ScopeSource) String() string {
	return string(s)
}

// ValidScope reports whether s is a recognized scope-marker value.
func ValidScope(s string) bool {
	return s == "repo" || s == "realm"
}

// FindScopeRoot walks from filepath.Clean(startDir) upward to the
// filesystem root looking for the nearest repo config (RepoConfigPath, so
// harness-dir aware) whose top-level scope key equals scope ("repo" or
// "realm"). It takes the first marker of the requested kind and continues
// past a missing file, a parse error, an invalid Scope value, or a Scope
// naming the other kind — discovery degrades, it never fails, so the
// return carries no error. The walk runs to the filesystem root; it does
// not stop at a .git boundary, because a realm root sits above its member
// repos.
func FindScopeRoot(startDir, scope string) (root string, found bool) {
	dir := filepath.Clean(startDir)
	for {
		cfg, _, err := LoadRepoConfig(RepoConfigPath(dir))
		if err == nil && cfg.Scope == scope {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// ScopeMarkerOutcome enumerates what EnsureScopeMarker did to the repo
// config file.
type ScopeMarkerOutcome string

const (
	// ScopeMarkerCreated: the repo config file did not exist; it was
	// created containing just the scope line.
	ScopeMarkerCreated ScopeMarkerOutcome = "created"
	// ScopeMarkerAdded: the file existed without a scope key; the key was
	// inserted.
	ScopeMarkerAdded ScopeMarkerOutcome = "added"
	// ScopeMarkerOK: the file already declared this exact scope; nothing
	// was written.
	ScopeMarkerOK ScopeMarkerOutcome = "ok"
	// ScopeMarkerConflict: the file declares a different scope; nothing
	// was written. The caller decides how to surface it — a conflicting
	// marker is never rewritten.
	ScopeMarkerConflict ScopeMarkerOutcome = "conflict"
)

// EnsureScopeMarker guarantees root's repo config declares
// scope = "<scope>" as a top-level key, writing the minimum change needed.
// Returns a non-nil error only on I/O failure or an existing file that
// fails to parse — a malformed file is never blindly written into.
//
// scope is a top-level key, so on an existing file it is inserted above the
// first "[table]" header: appending at EOF would land it inside the first
// table (e.g. [code]) and it would parse as "code.scope" instead of the
// top-level key. Every other byte of an existing file is preserved — no
// reordering, no reformatting, no comment loss.
func EnsureScopeMarker(root, scope string) (ScopeMarkerOutcome, error) {
	path := RepoConfigPath(root)

	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("config: create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(scopeLine(scope, "\n")), 0o644); err != nil {
			return "", fmt.Errorf("config: write %s: %w", path, err)
		}
		return ScopeMarkerCreated, nil
	}

	cfg, _, err := LoadRepoConfig(path)
	if err != nil {
		return "", fmt.Errorf("config: parse %s: %w", path, err)
	}

	switch {
	case cfg.Scope == scope:
		return ScopeMarkerOK, nil
	case cfg.Scope != "":
		return ScopeMarkerConflict, nil
	}

	if err := os.WriteFile(path, insertTopLevelScope(raw, scope), 0o644); err != nil {
		return "", fmt.Errorf("config: write %s: %w", path, err)
	}
	return ScopeMarkerAdded, nil
}

// scopeLine renders the top-level scope declaration line terminated with
// ending.
func scopeLine(scope, ending string) string {
	return fmt.Sprintf("scope = %q%s", scope, ending)
}

// dominantLineEnding reports the line ending most of raw's lines use, so an
// inserted line matches rather than splicing a lone LF into an
// otherwise-CRLF file. A file with no line ending anywhere defaults to "\n".
func dominantLineEnding(raw []byte) string {
	crlf := bytes.Count(raw, []byte("\r\n"))
	lf := bytes.Count(raw, []byte("\n")) - crlf
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// bracketDelta returns the net change in TOML bracket depth contributed by
// line, counting only '[' and ']' bytes outside quoted strings. TOML basic
// (double-quoted) and literal (single-quoted) strings are recognized;
// backslash escaping is honored only inside basic strings, per TOML's own
// escape rules. Multi-line triple-quoted strings aren't modeled — this is a
// position finder, not a TOML parser.
func bracketDelta(line string) int {
	delta := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if quote == '"' && c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			delta++
		case c == ']':
			delta--
		}
	}
	return delta
}

// insertTopLevelScope returns raw with a scope declaration line inserted as
// a top-level key: immediately before the first line that is a table header
// — its trimmed form starts with "[" **and** the accumulated bracket depth
// of every preceding line is zero, so an interior line of a multi-line array
// (e.g. "  [1, 2],") is never mistaken for one — or appended at EOF when raw
// has no table header. The inserted line is terminated with raw's dominant
// existing line ending. Every existing byte is preserved verbatim; only the
// new line is added.
func insertTopLevelScope(raw []byte, scope string) []byte {
	ending := dominantLineEnding(raw)
	line := scopeLine(scope, ending)

	lines := strings.SplitAfter(string(raw), "\n")
	depth := 0
	for i, l := range lines {
		if depth == 0 && strings.HasPrefix(strings.TrimSpace(l), "[") {
			var buf strings.Builder
			for _, prior := range lines[:i] {
				buf.WriteString(prior)
			}
			buf.WriteString(line)
			for _, rest := range lines[i:] {
				buf.WriteString(rest)
			}
			return []byte(buf.String())
		}
		depth += bracketDelta(l)
	}

	var buf strings.Builder
	buf.Write(raw)
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		buf.WriteString(ending)
	}
	buf.WriteString(line)
	return []byte(buf.String())
}
