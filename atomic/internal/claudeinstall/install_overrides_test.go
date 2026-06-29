package claudeinstall_test

// Tests for CP4: install-time agent model-tier frontmatter patching.
// Each test is independent (uses t.TempDir) and suppresses TTY-gated seams.

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

// writeOverrideConfig creates <targetDir>/.atomic/config.toml with a single
// [agents] override entry. Uses config.WritePersist for correctness.
func writeOverrideConfig(t *testing.T, targetDir, agentName, tier string) {
	t.Helper()
	cfg := config.Default()
	cfg.Agents = map[string]string{agentName: tier}
	if err := config.WritePersist(config.TOMLPath(targetDir), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}
}

// suppressPrune replaces PruneConfirm with a no-op for tests that re-install.
// Re-installs trigger writeInstallManifest which can surface a prune prompt when
// the test environment has no TTY. The seam is restored on t.Cleanup.
func suppressPrune(t *testing.T) {
	t.Helper()
	claudeinstall.PruneConfirm = func(_ []string) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })
}

// suppressProfileRefresh replaces the profile refresh seam with a no-op to
// avoid real env detection in tests that don't care about the profile.
func suppressProfileRefresh(t *testing.T) {
	t.Helper()
	claudeinstall.ProfileRefresh = func(_, _ string, _ int) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.ProfileRefresh = claudeinstall.DefaultProfileRefresh })
}

// TestAgentModelOverride_FreshInstall: install with [agents] override → installed
// file carries model: <tier> in frontmatter; other keys are preserved.
func TestAgentModelOverride_FreshInstall(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
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
	// Other keys must be preserved.
	if meta["name"] != "atomic-implementer" {
		t.Errorf("name = %q, want %q", meta["name"], "atomic-implementer")
	}
}

// TestAgentModelOverride_NoOverride: absent [agents] config → installed agent keeps
// the bundled-default model: value (bytes identical to embedded bundle).
func TestAgentModelOverride_NoOverride(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	// No config written — loadAgentOverrides must return nil.

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
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

// TestAgentModelOverride_Idempotent: re-install with the same config tier →
// Plan reports ActionUnchanged for the overridden agent (no unnecessary writes).
func TestAgentModelOverride_Idempotent(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	suppressPrune(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	plan, err := claudeinstall.Install(target, false, fixedClock)
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

// TestAgentModelOverride_ConfigChange: after first install with haiku, change the
// config to sonnet and re-install → agent file updated to sonnet.
func TestAgentModelOverride_ConfigChange(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	suppressPrune(t)
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("first Install (haiku): %v", err)
	}

	// Change the config to sonnet.
	writeOverrideConfig(t, target, "atomic-implementer", "sonnet")

	// Update (same as install) should re-apply with the new tier.
	if _, err := claudeinstall.Update(target, false, fixedClock); err != nil {
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

// TestAgentModelOverride_KeyAdded: when the bundled agent file has no model: key,
// the override must ADD the key. Tests patchAgentContent via the exported seam.
func TestAgentModelOverride_KeyAdded(t *testing.T) {
	// Synthetic content without a model: key.
	content := []byte("---\nname: test-agent\ndescription: simple test\n---\nBody here.\n")
	overrides := map[string]string{"test-agent": "opus"}

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

// TestAgentModelOverride_DryRun: dryRun=true with an override configured →
// no agent files written to disk.
func TestAgentModelOverride_DryRun(t *testing.T) {
	target := t.TempDir()
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	plan, err := claudeinstall.Install(target, true /* dryRun */, fixedClock)
	if err != nil {
		t.Fatalf("dry-run Install: %v", err)
	}

	// Must have planned installs.
	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed == 0 {
		t.Error("dry-run plan has zero installs — unexpected")
	}

	// No files on disk — agents/ dir must not exist.
	agentsDir := filepath.Join(target, "agents")
	if _, err := os.Stat(agentsDir); !os.IsNotExist(err) {
		t.Error("dry-run wrote agents/ dir — should not have written anything")
	}
}

// TestAgentModelOverride_RoundTrip: patchAgentContent on a simple synthetic file
// preserves key order and body when the model: key is changed.
func TestAgentModelOverride_RoundTrip(t *testing.T) {
	// Simple content with known key order: name, tools, model.
	original := "---\nname: my-agent\ntools: [Read]\nmodel: sonnet\n---\nMy body.\n"
	overrides := map[string]string{"my-agent": "haiku"}

	result := claudeinstall.PatchAgentContent("agents/my-agent.md", []byte(original), overrides)

	kvs, body, err := frontmatter.ParseOrdered(string(result))
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Key order must be preserved: name, tools, model.
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

	// model: must be patched to haiku.
	for _, kv := range kvs {
		if kv.Key == "model" && kv.Value != "haiku" {
			t.Errorf("model = %v, want %q", kv.Value, "haiku")
		}
	}

	// Body must be unchanged.
	if body != "My body.\n" {
		t.Errorf("body = %q, want %q", body, "My body.\n")
	}
}

// TestAgentModelOverride_NonAgentUnchanged: patchAgentContent is a no-op for
// non-agent targets even when an override is configured.
func TestAgentModelOverride_NonAgentUnchanged(t *testing.T) {
	content := []byte("---\nname: test\n---\nBody.\n")
	overrides := map[string]string{"test": "haiku"}

	result := claudeinstall.PatchAgentContent("commands/test.md", content, overrides)
	if sha256hex(result) != sha256hex(content) {
		t.Error("patchAgentContent modified a non-agent artifact — must be no-op")
	}
}

// TestAgentModelOverride_OtherAgentsUnaffected: installing with an override for
// one agent must leave other agents with their bundled-default model: values.
func TestAgentModelOverride_OtherAgentsUnaffected(t *testing.T) {
	target := t.TempDir()
	suppressProfileRefresh(t)
	// Only override atomic-implementer.
	writeOverrideConfig(t, target, "atomic-implementer", "haiku")

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// atomic-reviewer must keep its bundled model value.
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
