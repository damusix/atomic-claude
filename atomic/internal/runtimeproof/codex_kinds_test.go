package runtimeproof

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
)

// codexPromptInput renders the model input `codex debug prompt-input` builds,
// which needs no model call or credential.
func codexPromptInput(t *testing.T, workDir, prompt string) string {
	t.Helper()
	cmd := exec.Command("codex", "debug", "prompt-input", prompt)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex debug prompt-input: %v\n%s", err, out)
	}
	return string(out)
}

// TestCodexNativeKindDelivery probes each kind against the layout install
// writes; record changes in docs/research/harness-capability-matrix.md.
func TestCodexNativeKindDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is unavailable: %v", err)
	}
	if version, err := exec.Command("codex", "--version").Output(); err == nil {
		t.Logf("observed %s", strings.TrimSpace(string(version)))
	}
	root := isolatedHome(t)
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	codexRoot := codexHomeRoot(t, root)
	writeTree(t, home, map[string]string{".atomic/profile.md": "PROFILE_IMPORT_MARKER\n"})
	userSteering := "GLOBAL_USER_MARKER\n\n@~/.atomic/profile.md\n"
	writeTree(t, codexRoot, map[string]string{"AGENTS.md": userSteering})
	work := filepath.Join(root, "work")
	writeTree(t, work, map[string]string{
		"AGENTS.md":          "REPO_MARKER\n\n@docs/wiki/index.md\n",
		"docs/wiki/index.md": "WIKI_ROUTER_MARKER\n",
		"src/a.ts":           "export const a = 1;\n",
	})

	a := codex.New()
	cat := probeCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return cat, nil }
	target := harness.Target{Kind: harness.KindCodex, Instance: codexRoot, NativeRoot: codexRoot}
	plan, err := a.Lifecycle(home).Project(target, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Lifecycle(home).Converge(target, plan)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(c.Unproven) > 0 {
		t.Fatalf("convergence left %v", c.Unproven)
	}

	input := codexPromptInput(t, work, "/atomic-probe alpha beta")
	checks := []struct{ kind, want string }{
		{"user line in the global AGENTS.md", "GLOBAL_USER_MARKER"},
		{"Atomic block in the global AGENTS.md", "Walk the ladder before writing anything."},
		{"output style inside the block", "You respond in atomic style."},
		{"skill name, namespaced by the plugin", "atomic:atomic-probe"},
		{"skill description", "PROBE_SKILL_LISTED"},
		{"repository AGENTS.md", "REPO_MARKER"},
		{"command left as literal user text", "/atomic-probe alpha beta"},
		{"global @ import left literal", "@~/.atomic/profile.md"},
		{"repository @ import left literal", "@docs/wiki/index.md"},
	}
	for _, c := range checks {
		if !strings.Contains(input, c.want) {
			t.Errorf("%s: %q did not reach the prompt input", c.kind, c.want)
		}
	}
	for kind, absent := range map[string]string{
		"skill body":            "PROBE_SKILL_BODY",
		"command expansion":     "PROBE_COMMAND_EXPANDED",
		"profile import target": "PROFILE_IMPORT_MARKER",
		"wiki router target":    "WIKI_ROUTER_MARKER",
		"rule body":             "# TypeScript",
	} {
		if strings.Contains(input, absent) {
			t.Errorf("%s reached the prompt input; revisit its capability row", kind)
		}
	}
	if global, _ := os.ReadFile(codex.SteeringPath(codexRoot)); strings.Count(string(global), "@~/.atomic/profile.md") != 1 {
		t.Errorf("the Atomic block carries an import line Codex leaves literal:\n%s", global)
	}
	if strings.Index(input, "GLOBAL_USER_MARKER") > strings.Index(input, "REPO_MARKER") {
		t.Error("the global AGENTS.md no longer precedes the repository AGENTS.md")
	}

	removal, err := a.Lifecycle(home).Remove(target)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removal, err = a.Lifecycle(home).Removed(target, removal); err != nil || len(removal.Skipped) > 0 {
		t.Fatalf("unregister: %v, skipped %v", err, removal.SkipReasons)
	}
	if got, err := os.ReadFile(codex.SteeringPath(codexRoot)); err != nil || string(got) != userSteering {
		t.Errorf("global AGENTS.md after uninstall = %q (%v), want only the user's lines", got, err)
	}
	if config, _ := os.ReadFile(codex.ConfigPath(codexRoot)); strings.Contains(string(config), codex.MarketplaceName) {
		t.Errorf("Codex config after uninstall still registers Atomic:\n%s\nremoved %v", config, removal.Removed)
	}
	if after := codexPromptInput(t, work, "hi"); strings.Contains(after, "PROBE_SKILL_LISTED") {
		t.Error("the plugin's skills are still listed after uninstall")
	}
}
