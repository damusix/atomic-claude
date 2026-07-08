// Package cli tests — graphignore CLI wiring (checkpoint 2).
//
// Covers the two CLI-visible success criteria that don't fit engine/indexer
// package tests: `atomic code index` prints exactly one repo-config warning
// line on a degraded .claude/atomic.toml, and `atomic code status` reports
// the active ignore-pattern count and config path.
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
)

// TestIndex_RepoConfigWarning_PrintsOneLine verifies that an invalid glob
// pattern in .claude/atomic.toml does not fail `atomic code index` — it
// degrades to unfiltered indexing plus exactly one warning line on stderr.
func TestIndex_RepoConfigWarning_PrintsOneLine(t *testing.T) {
	dir := writeFixture(t)
	must(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	// Same malformed-bracket pattern proven invalid in config/repo_test.go.
	toml := "[code]\nignore = [\"vendor[/**\"]\n"
	must(t, os.WriteFile(filepath.Join(dir, ".claude", "atomic.toml"), []byte(toml), 0o644))

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index exit %d; stderr: %s", code, stderr.String())
	}

	var warnLines []string
	for _, l := range strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n") {
		if strings.Contains(l, "repo config") {
			warnLines = append(warnLines, l)
		}
	}
	if len(warnLines) != 1 {
		t.Fatalf("expected exactly 1 repo-config warning line in stderr, got %d: %v\nfull stderr:\n%s",
			len(warnLines), warnLines, stderr.String())
	}
	if !strings.Contains(warnLines[0], "vendor[/**") {
		t.Errorf("warning line %q does not mention the invalid pattern", warnLines[0])
	}
}

// TestStatus_IgnorePatternCount verifies `atomic code status` reports the
// active ignore-pattern count and config path, in both --json and text form,
// once a well-formed .claude/atomic.toml is present.
func TestStatus_IgnorePatternCount(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	toml := "[code]\nignore = [\"vendor/**\", \"*.min.js\"]\n"
	must(t, os.WriteFile(filepath.Join(dir, ".claude", "atomic.toml"), []byte(toml), 0o644))

	// IgnorePatternInfo is a pool-free read; status only needs the index dir
	// to exist (IsInitialized), so Init (no IndexAll) is enough — and avoids
	// paying the indexer's pool-boot cost for a status-only test.
	ctx := testCtx(t)
	eng, err := engine.New(dir)
	must(t, err)
	must(t, eng.Init(ctx))
	eng.Close()

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"status", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status --json exit %d; stderr: %s", code, stderr.String())
	}

	var s codecli.StatusJSON
	must(t, json.Unmarshal(stdout.Bytes(), &s))
	if s.IgnorePatternCount != 2 {
		t.Errorf("IgnorePatternCount = %d, want 2", s.IgnorePatternCount)
	}
	wantPath := filepath.Join(dir, ".claude", "atomic.toml")
	if s.IgnoreConfigPath != wantPath {
		t.Errorf("IgnoreConfigPath = %q, want %q", s.IgnoreConfigPath, wantPath)
	}

	stdout.Reset()
	stderr.Reset()
	code = codecli.RunCode([]string{"status"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status exit %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ignore patterns: 2") {
		t.Errorf("status text output missing ignore-pattern line, got:\n%s", stdout.String())
	}
}

// TestStatus_NoIgnoreConfig_NoLine verifies the line is entirely absent (not
// printed as "0") when no .claude/atomic.toml is present.
func TestStatus_NoIgnoreConfig_NoLine(t *testing.T) {
	dir := t.TempDir()
	ctx := testCtx(t)
	eng, err := engine.New(dir)
	must(t, err)
	must(t, eng.Init(ctx))
	eng.Close()

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"status", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status --json exit %d; stderr: %s", code, stderr.String())
	}

	var s codecli.StatusJSON
	must(t, json.Unmarshal(stdout.Bytes(), &s))
	if s.IgnorePatternCount != 0 || s.IgnoreConfigPath != "" {
		t.Errorf("expected zero-value ignore fields with no config, got count=%d path=%q", s.IgnorePatternCount, s.IgnoreConfigPath)
	}

	stdout.Reset()
	code = codecli.RunCode([]string{"status"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status exit %d; stderr: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "ignore patterns:") {
		t.Errorf("status text output should omit the ignore-pattern line with no config, got:\n%s", stdout.String())
	}
}
