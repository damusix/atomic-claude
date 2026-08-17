package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScopeSource enumerates how a root was decided — the marker in the repo config,
// a fallback mechanism, or the process cwd.
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

// FindScopeRoot walks upward from startDir for the nearest repo config whose
// top-level scope key equals scope. It takes the first marker of the requested
// kind and continues past a missing file, a parse error, an invalid value, or a
// marker naming the other kind — discovery degrades, never fails, which is why
// there is no error return. The walk does not stop at a .git boundary, because
// a realm root sits above its member repos.
//
// startDir is absolutized first: filepath.Dir on a relative path short-circuits
// at ".", so a relative walk would never reach real ancestors. A failing
// filepath.Abs degrades to the cleaned relative path rather than erroring.
func FindScopeRoot(startDir, scope string) (root string, found bool) {
	dir := filepath.Clean(startDir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
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
	// ScopeMarkerConflict: the file declares a different scope; nothing was
	// written. A conflicting marker is never rewritten — the caller decides how
	// to surface it.
	ScopeMarkerConflict ScopeMarkerOutcome = "conflict"
)

// EnsureScopeMarker guarantees root's repo config declares scope = "<scope>" as
// a top-level key, writing the minimum change needed. Errors only on I/O failure
// or an existing file that fails to parse — a malformed file is never blindly
// written into.
//
// On an existing file the key is inserted above the first "[table]" header:
// appending at EOF would land it inside that table and parse as "code.scope".
// Every other byte is preserved — no reordering, reformatting, or comment loss.
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
// inserted line does not splice a lone LF into an otherwise-CRLF file. A file
// with no line ending anywhere defaults to "\n".
func dominantLineEnding(raw []byte) string {
	crlf := bytes.Count(raw, []byte("\r\n"))
	lf := bytes.Count(raw, []byte("\n")) - crlf
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// bracketDelta returns the net TOML bracket-depth change contributed by line,
// counting only brackets outside quoted strings. Basic and literal strings are
// recognized, with backslash escaping honored only inside basic ones per TOML's
// rules. Multi-line triple-quoted strings are not modeled — this is a position
// finder, not a parser.
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

// insertTopLevelScope inserts the scope line immediately before the first table
// header — a line whose trimmed form starts with "[" AND whose preceding
// accumulated bracket depth is zero, so an interior line of a multi-line array is
// never mistaken for one — or appends at EOF when there is no table header. The
// inserted line uses raw's dominant line ending; every existing byte survives.
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
