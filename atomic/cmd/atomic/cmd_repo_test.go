package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// Only an enrolled Claude target makes repo init prepare .claude/; an OMP or
// Codex enrollment, or an absent or unreadable ledger, must not.
func TestClaudeEnrolled(t *testing.T) {
	enroll := func(t *testing.T, home string, records ...installstate.TargetRecord) {
		t.Helper()
		ledger := &installstate.Ledger{Header: installstate.NewHeader(), Targets: records}
		if err := ledger.Save(config.LedgerPath(home)); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no ledger", func(t *testing.T) {
		var warn bytes.Buffer
		if claudeEnrolled(t.TempDir(), &warn) || warn.Len() != 0 {
			t.Errorf("a missing ledger must read as not enrolled, silently; warned %q", warn.String())
		}
	})
	t.Run("unreadable ledger", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Dir(config.LedgerPath(home)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.LedgerPath(home), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		var warn bytes.Buffer
		if claudeEnrolled(home, &warn) {
			t.Error("an unreadable ledger must read as not enrolled")
		}
		if !strings.Contains(warn.String(), "ledger") {
			t.Errorf("want a warning naming the ledger, got %q", warn.String())
		}
	})
	t.Run("omp only", func(t *testing.T) {
		home := t.TempDir()
		enroll(t, home, installstate.TargetRecord{Harness: "omp", Instance: "default"})
		if claudeEnrolled(home, io.Discard) {
			t.Error("an OMP enrollment must not read as Claude")
		}
	})
	t.Run("claude", func(t *testing.T) {
		home := t.TempDir()
		enroll(t, home, installstate.TargetRecord{Harness: "claude", Instance: "/x/.claude"})
		if !claudeEnrolled(home, io.Discard) {
			t.Error("a Claude target record must read as enrolled")
		}
	})
}

// A repo init that migrates scaffolds the new root in the same process, never
// a fresh layout beside the legacy one.
func TestRepoInitMigratesThenWritesIntoTheNewRoot(t *testing.T) {
	repo := legacyStateRepo(t)
	var out, errOut strings.Builder
	if err := repoInit(repo, t.TempDir(), &out, &errOut); err != nil {
		t.Fatalf("repo init: %v", err)
	}
	if !strings.Contains(errOut.String(), "moving repository state") {
		t.Errorf("no migration notice on stderr:\n%s", errOut.String())
	}
	for _, p := range []string{".atomic/atomic.toml", ".atomic/.scratchpad", ".atomic/project/followups/a.md"} {
		if !pathExists(filepath.Join(repo, p)) {
			t.Errorf("%s missing after repo init", p)
		}
	}
	for _, p := range []string{".claude/.scratchpad", ".claude/project", ".claude/atomic.toml"} {
		if pathExists(filepath.Join(repo, p)) {
			t.Errorf("%s written to the legacy root", p)
		}
	}
}
