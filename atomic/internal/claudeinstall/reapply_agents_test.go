package claudeinstall_test

// ReapplyAgents re-patches installed agent files after a config-only change,
// without a full `atomic claude install` run.

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// writeInstalledAgent simulates a prior plain install with no overrides in effect.
func writeInstalledAgent(t *testing.T, target string) {
	t.Helper()
	src, err := fs.ReadFile(embedded.FS, "bundle/agents/atomic-implementer.md")
	if err != nil {
		t.Fatalf("read embedded atomic-implementer: %v", err)
	}
	agentPath := filepath.Join(target, "agents", "atomic-implementer.md")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatalf("mkdir agents dir: %v", err)
	}
	if err := os.WriteFile(agentPath, src, 0o644); err != nil {
		t.Fatalf("write installed agent: %v", err)
	}
}

func TestReapplyAgents_PatchesInstalledAgent(t *testing.T) {
	target := t.TempDir()
	home := t.TempDir()

	writeInstalledAgent(t, target)

	// Config: effort override for the installed agent only.
	cfg := config.Default()
	cfg.Claude.Agents = map[string]config.AgentOverride{"atomic-implementer": {Effort: "high"}}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	changed, installed, err := claudeinstall.ReapplyAgents(target, home)
	if err != nil {
		t.Fatalf("ReapplyAgents: %v", err)
	}
	if installed < 1 {
		t.Errorf("installed = %d, want >= 1", installed)
	}
	if len(changed) != 1 || changed[0] != "atomic-implementer" {
		t.Errorf("changed = %v, want [atomic-implementer]", changed)
	}

	agentPath := filepath.Join(target, "agents", "atomic-implementer.md")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read patched agent: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}
	if meta["effort"] != "high" {
		t.Errorf("effort = %q, want %q", meta["effort"], "high")
	}

	// An absent agent (atomic-reviewer never installed) must not be created.
	reviewerPath := filepath.Join(target, "agents", "atomic-reviewer.md")
	if _, statErr := os.Stat(reviewerPath); !os.IsNotExist(statErr) {
		t.Error("atomic-reviewer was created — ReapplyAgents must never perform a first-time install")
	}
}

func TestReapplyAgents_SkipsAbsentAgent(t *testing.T) {
	target := t.TempDir()
	home := t.TempDir()

	// No agent files installed at all.
	cfg := config.Default()
	cfg.Claude.Agents = map[string]config.AgentOverride{"atomic-implementer": {Model: "opus"}}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	changed, installed, err := claudeinstall.ReapplyAgents(target, home)
	if err != nil {
		t.Fatalf("ReapplyAgents: %v", err)
	}
	if installed != 0 {
		t.Errorf("installed = %d, want 0 (nothing on disk)", installed)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want empty", changed)
	}
	if _, statErr := os.Stat(filepath.Join(target, "agents", "atomic-implementer.md")); !os.IsNotExist(statErr) {
		t.Error("atomic-implementer was created — ReapplyAgents must never perform a first-time install")
	}
}

func TestReapplyAgents_IdempotentOnSecondCall(t *testing.T) {
	target := t.TempDir()
	home := t.TempDir()

	writeInstalledAgent(t, target)

	cfg := config.Default()
	cfg.Claude.Agents = map[string]config.AgentOverride{"atomic-implementer": {Effort: "max"}}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	if _, _, err := claudeinstall.ReapplyAgents(target, home); err != nil {
		t.Fatalf("first ReapplyAgents: %v", err)
	}

	changed, installed, err := claudeinstall.ReapplyAgents(target, home)
	if err != nil {
		t.Fatalf("second ReapplyAgents: %v", err)
	}
	if installed < 1 {
		t.Errorf("installed = %d, want >= 1", installed)
	}
	if len(changed) != 0 {
		t.Errorf("second call changed = %v, want empty (already in sync)", changed)
	}
}
