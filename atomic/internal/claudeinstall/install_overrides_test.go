package claudeinstall_test

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

// writeOverrideConfig writes a config.toml with one [claude.agents] entry.
func writeOverrideConfig(t *testing.T, targetDir, agentName, tier string) {
	t.Helper()
	cfg := config.Default()
	cfg.Claude.Agents = map[string]config.AgentOverride{agentName: {Model: tier}}
	if err := config.WritePersist(config.TOMLPath(targetDir), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}
}

// suppressPrune stubs PruneConfirm: a re-install would otherwise surface a prune
// prompt in a test environment with no TTY.
func suppressPrune(t *testing.T) {
	t.Helper()
	claudeinstall.PruneConfirm = func(_ []string) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })
}

// suppressProfileRefresh stubs the refresh seam to avoid real env detection.
func suppressProfileRefresh(t *testing.T) {
	t.Helper()
	claudeinstall.ProfileRefresh = func(_, _ string, _ int) (bool, error) { return false, nil }
	prevProfileRefresh := claudeinstall.ProfileRefresh
	t.Cleanup(func() { claudeinstall.ProfileRefresh = prevProfileRefresh })
}

func TestAgentModelOverride_FreshInstall(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	agentPath := filepath.Join(target, "agents", "atomic-implementer.md")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read installed agent: %v", err)
	}

	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}

	if meta["model"] != "haiku" {
		t.Errorf("model = %q, want %q", meta["model"], "haiku")
	}
	if meta["name"] != "atomic-implementer" {
		t.Errorf("name = %q, want %q", meta["name"], "atomic-implementer")
	}
}

func TestAgentModelOverride_NoOverride(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	// No config written — loadAgentOverrides must return nil.

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	for _, a := range embedded.Manifest() {
		if a.Kind != "agent" {
			continue
		}
		onDisk := filepath.Join(target, filepath.FromSlash(a.Target))
		diskData, err := os.ReadFile(onDisk)
		if err != nil {
			t.Errorf("read %s: %v", a.Target, err)
			continue
		}
		embData, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			t.Errorf("read embedded %s: %v", a.Source, err)
			continue
		}
		if sha256hex(diskData) != sha256hex(embData) {
			t.Errorf("%s: on-disk SHA differs from embedded — override must not apply without config", a.Target)
		}
	}
}

func TestAgentModelOverride_Idempotent(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	suppressPrune(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	for _, fa := range plan {
		if fa.Artifact.Target == "agents/atomic-implementer.md" {
			if fa.Kind != claudeinstall.ActionUnchanged {
				t.Errorf("second install action for atomic-implementer = %s, want unchanged", fa.Kind)
			}
			return
		}
	}
	t.Error("atomic-implementer not found in second install plan")
}

func TestAgentModelOverride_ConfigChange(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	suppressPrune(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install (haiku): %v", err)
	}

	writeOverrideConfig(t, target, "atomic-implementer", "sonnet")

	if _, err := claudeinstall.Update(target, target, false, fixedClock); err != nil {
		t.Fatalf("Update (sonnet): %v", err)
	}

	agentPath := filepath.Join(target, "agents", "atomic-implementer.md")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read installed agent: %v", err)
	}

	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}
	if meta["model"] != "sonnet" {
		t.Errorf("after config change: model = %q, want %q", meta["model"], "sonnet")
	}
}

func TestAgentModelOverride_KeyAdded(t *testing.T) {
	content := []byte("---\nname: test-agent\ndescription: simple test\n---\nBody here.\n")
	overrides := map[string]config.AgentOverride{"test-agent": {Model: "opus"}}

	result := claudeinstall.PatchAgentContent("agents/test-agent.md", content, overrides)

	meta, body, err := frontmatter.Parse(string(result))
	if err != nil {
		t.Fatalf("parse patched frontmatter: %v", err)
	}
	if meta["model"] != "opus" {
		t.Errorf("model = %q, want %q", meta["model"], "opus")
	}
	if meta["name"] != "test-agent" {
		t.Errorf("name = %q, want %q", meta["name"], "test-agent")
	}
	if body != "Body here.\n" {
		t.Errorf("body = %q, want %q", body, "Body here.\n")
	}
}

func TestAgentModelOverride_DryRun(t *testing.T) {
	target := t.TempDir()
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	plan, err := claudeinstall.Install(target, target, true /* dryRun */, fixedClock)
	if err != nil {
		t.Fatalf("dry-run Install: %v", err)
	}

	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed == 0 {
		t.Error("dry-run plan has zero installs — unexpected")
	}

	agentsDir := filepath.Join(target, "agents")
	if _, err := os.Stat(agentsDir); !os.IsNotExist(err) {
		t.Error("dry-run wrote agents/ dir — should not have written anything")
	}
}

func TestAgentModelOverride_RoundTrip(t *testing.T) {
	original := "---\nname: my-agent\ntools: [Read]\nmodel: sonnet\n---\nMy body.\n"
	overrides := map[string]config.AgentOverride{"my-agent": {Model: "haiku"}}

	result := claudeinstall.PatchAgentContent("agents/my-agent.md", []byte(original), overrides)

	kvs, body, err := frontmatter.ParseOrdered(string(result))
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	wantOrder := []string{"name", "tools", "model"}
	for i, kv := range kvs {
		if i >= len(wantOrder) {
			break
		}
		if kv.Key != wantOrder[i] {
			t.Errorf("key[%d] = %q, want %q", i, kv.Key, wantOrder[i])
		}
	}
	if len(kvs) != len(wantOrder) {
		t.Errorf("key count = %d, want %d", len(kvs), len(wantOrder))
	}

	for _, kv := range kvs {
		if kv.Key == "model" && kv.Value != "haiku" {
			t.Errorf("model = %v, want %q", kv.Value, "haiku")
		}
	}

	if body != "My body.\n" {
		t.Errorf("body = %q, want %q", body, "My body.\n")
	}
}

func TestAgentModelOverride_NonAgentUnchanged(t *testing.T) {
	content := []byte("---\nname: test\n---\nBody.\n")
	overrides := map[string]config.AgentOverride{"test": {Model: "haiku"}}

	result := claudeinstall.PatchAgentContent("commands/test.md", content, overrides)
	if sha256hex(result) != sha256hex(content) {
		t.Error("patchAgentContent modified a non-agent artifact — must be no-op")
	}
}

// Diff must compare against patched embedded content, not raw bundle bytes, or a
// correct install with an override falsely shows as drifted.
func TestAgentModelOverride_DiffMatchesAfterInstall(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	rows, err := claudeinstall.Diff(target, target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, row := range rows {
		if row.Artifact.Target == "agents/atomic-implementer.md" {
			if row.Status != claudeinstall.DiffMatch {
				t.Errorf("Diff status for overridden agent = %s, want %s", row.Status, claudeinstall.DiffMatch)
			}
			return
		}
	}
	t.Error("agents/atomic-implementer.md not found in Diff rows")
}

func TestAgentOverride_EffortOnly(t *testing.T) {
	overrides := map[string]config.AgentOverride{"atomic-implementer": {Effort: "high"}}
	content, err := fs.ReadFile(embedded.FS, "bundle/agents/atomic-implementer.md")
	if err != nil {
		t.Fatalf("read embedded atomic-implementer: %v", err)
	}

	result := claudeinstall.PatchAgentContent("agents/atomic-implementer.md", content, overrides)

	meta, _, err := frontmatter.Parse(string(result))
	if err != nil {
		t.Fatalf("parse patched frontmatter: %v", err)
	}
	if meta["effort"] != "high" {
		t.Errorf("effort = %q, want %q", meta["effort"], "high")
	}
	origMeta, _, err := frontmatter.Parse(string(content))
	if err != nil {
		t.Fatalf("parse original frontmatter: %v", err)
	}
	if meta["model"] != origMeta["model"] {
		t.Errorf("model = %q, want unchanged %q", meta["model"], origMeta["model"])
	}
}

func TestAgentOverride_ModelOnly(t *testing.T) {
	content := []byte("---\nname: test-agent\nmodel: sonnet\n---\nBody.\n")
	overrides := map[string]config.AgentOverride{"test-agent": {Model: "haiku"}}

	result := claudeinstall.PatchAgentContent("agents/test-agent.md", content, overrides)

	meta, _, err := frontmatter.Parse(string(result))
	if err != nil {
		t.Fatalf("parse patched frontmatter: %v", err)
	}
	if meta["model"] != "haiku" {
		t.Errorf("model = %q, want %q", meta["model"], "haiku")
	}
	if _, ok := meta["effort"]; ok {
		t.Errorf("effort key present = %q, want absent", meta["effort"])
	}
}

func TestAgentOverride_BothSet(t *testing.T) {
	content := []byte("---\nname: test-agent\nmodel: sonnet\n---\nBody.\n")
	overrides := map[string]config.AgentOverride{"test-agent": {Model: "opus", Effort: "max"}}

	result := claudeinstall.PatchAgentContent("agents/test-agent.md", content, overrides)

	meta, _, err := frontmatter.Parse(string(result))
	if err != nil {
		t.Fatalf("parse patched frontmatter: %v", err)
	}
	if meta["model"] != "opus" {
		t.Errorf("model = %q, want %q", meta["model"], "opus")
	}
	if meta["effort"] != "max" {
		t.Errorf("effort = %q, want %q", meta["effort"], "max")
	}
}

func TestAgentOverride_BothEmpty(t *testing.T) {
	content := []byte("---\nname: test-agent\nmodel: sonnet\n---\nBody.\n")
	overrides := map[string]config.AgentOverride{"test-agent": {}}

	result := claudeinstall.PatchAgentContent("agents/test-agent.md", content, overrides)
	if sha256hex(result) != sha256hex(content) {
		t.Error("both-empty override modified content — must be a no-op")
	}
}

func TestAgentOverride_EffortAppended_KeyOrderPreserved(t *testing.T) {
	original := "---\nname: my-agent\ntools: [Read]\nmodel: sonnet\n---\nMy body.\n"
	overrides := map[string]config.AgentOverride{"my-agent": {Effort: "low"}}

	result := claudeinstall.PatchAgentContent("agents/my-agent.md", []byte(original), overrides)

	kvs, body, err := frontmatter.ParseOrdered(string(result))
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	wantOrder := []string{"name", "tools", "model", "effort"}
	if len(kvs) != len(wantOrder) {
		t.Fatalf("key count = %d, want %d", len(kvs), len(wantOrder))
	}
	for i, kv := range kvs {
		if kv.Key != wantOrder[i] {
			t.Errorf("key[%d] = %q, want %q", i, kv.Key, wantOrder[i])
		}
	}
	if kvs[3].Value != "low" {
		t.Errorf("effort = %v, want %q", kvs[3].Value, "low")
	}
	// model: must be untouched (effort-only override).
	if kvs[2].Value != "sonnet" {
		t.Errorf("model = %v, want unchanged %q", kvs[2].Value, "sonnet")
	}
	if body != "My body.\n" {
		t.Errorf("body = %q, want %q", body, "My body.\n")
	}
}

func TestAgentOverride_BothAppended(t *testing.T) {
	content := []byte("---\nname: test-agent\ndescription: simple test\n---\nBody here.\n")
	overrides := map[string]config.AgentOverride{"test-agent": {Model: "opus", Effort: "xhigh"}}

	result := claudeinstall.PatchAgentContent("agents/test-agent.md", content, overrides)

	kvs, body, err := frontmatter.ParseOrdered(string(result))
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	wantOrder := []string{"name", "description", "model", "effort"}
	if len(kvs) != len(wantOrder) {
		t.Fatalf("key count = %d, want %d", len(kvs), len(wantOrder))
	}
	for i, kv := range kvs {
		if kv.Key != wantOrder[i] {
			t.Errorf("key[%d] = %q, want %q", i, kv.Key, wantOrder[i])
		}
	}
	if kvs[2].Value != "opus" {
		t.Errorf("model = %v, want %q", kvs[2].Value, "opus")
	}
	if kvs[3].Value != "xhigh" {
		t.Errorf("effort = %v, want %q", kvs[3].Value, "xhigh")
	}
	if body != "Body here.\n" {
		t.Errorf("body = %q, want %q", body, "Body here.\n")
	}
}

// Plan's SHA computation routes through patchAgentContent, so Plan and Apply
// agree on what an effort-only override produces.
func TestAgentOverride_PlanReflectsEffort(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	cfg := config.Default()
	cfg.Claude.Agents = map[string]config.AgentOverride{"atomic-implementer": {Effort: "high"}}
	if err := config.WritePersist(config.TOMLPath(target), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	agentPath := filepath.Join(target, "agents", "atomic-implementer.md")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read installed agent: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}
	if meta["effort"] != "high" {
		t.Errorf("effort = %q, want %q", meta["effort"], "high")
	}

	// Diff must agree the installed content matches.
	rows, err := claudeinstall.Diff(target, target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, row := range rows {
		if row.Artifact.Target == "agents/atomic-implementer.md" {
			if row.Status != claudeinstall.DiffMatch {
				t.Errorf("Diff status for effort-overridden agent = %s, want %s", row.Status, claudeinstall.DiffMatch)
			}
			return
		}
	}
	t.Error("agents/atomic-implementer.md not found in Diff rows")
}

func TestAgentModelOverride_OtherAgentsUnaffected(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	reviewerPath := filepath.Join(target, "agents", "atomic-reviewer.md")
	diskData, err := os.ReadFile(reviewerPath)
	if err != nil {
		t.Fatalf("read atomic-reviewer: %v", err)
	}
	embData, err := fs.ReadFile(embedded.FS, "bundle/agents/atomic-reviewer.md")
	if err != nil {
		t.Fatalf("read embedded atomic-reviewer: %v", err)
	}
	if sha256hex(diskData) != sha256hex(embData) {
		t.Error("atomic-reviewer was modified despite having no override configured")
	}
}
