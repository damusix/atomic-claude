package runtimeproof

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
)

// Startup-level proof for the Codex wiki-card row; no model turn edits a file.
func TestCodexPreToolUseHookIsAcceptedAndDeliversTheMatchingCard(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "codex/pre-tool-use-cards",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex app-server)",
		Criterion: "Codex lists the owned PreToolUse member and its hash equals Atomic's; the verb emits additionalContext with the matching card for an apply_patch payload, once per session",
		Command:   "atomic install --harness codex --yes; codex app-server hooks/list; atomic hooks pre-tool-use < apply_patch payload",
	}
	const card = "---\npaths:\n  - \"src/**\"\n---\nDomain: src. Read docs/wiki/src.md before editing here.\n"
	c := newCodexRedesign(t, ev, map[string]string{
		"README.md":                 "# probe\n",
		"src/a.go":                  "package src\n",
		".atomic/rules/wiki/src.md": card,
	}, nil)
	ev.Paths = []string{c.home, c.codexHome, c.repo}

	hooks, raw := c.hooksList(t)
	var member codexHook
	for _, h := range hooks {
		if h.EventName == "preToolUse" && h.Command == "atomic hooks pre-tool-use" {
			member = h
		}
	}
	if member.Key == "" {
		t.Fatalf("hooks/list does not report Atomic's PreToolUse member:\n%s", raw)
	}
	if !strings.HasSuffix(member.Key, ":pre_tool_use:0:0") {
		t.Errorf("member key = %q, want the pre_tool_use:0:0 suffix Atomic reads", member.Key)
	}
	if trust, _ := codex.ObservePreToolUseHookTrust(c.home, c.codexHome); trust != codex.HookUntrusted {
		t.Errorf("trust before review = %s, want untrusted", trust)
	}
	c.setHookState(t, "[hooks.state."+quote(member.Key)+"]\ntrusted_hash = "+quote(member.CurrentHash)+"\n")
	if trust, detail := codex.ObservePreToolUseHookTrust(c.home, c.codexHome); trust != codex.HookTrusted {
		t.Errorf("trust with Codex's own currentHash = %s (%s), want trusted: Atomic's identity hash differs from Codex's", trust, detail)
	}

	payload := `{"session_id":"runtimeproof-1","cwd":` + quote(c.repo) + `,"hook_event_name":"PreToolUse","tool_name":"apply_patch",` +
		`"tool_input":{"command":"*** Begin Patch\n*** Update File: src/a.go\n@@\n-package src\n+package src // edited\n*** End Patch\n"}}`
	first := c.verb(t, payload)
	if !strings.Contains(hookAdditionalContext(t, first), "Domain: src. Read docs/wiki/src.md before editing here.") {
		t.Errorf("the verb did not deliver the src card:\n%s", first)
	}
	if second := c.verb(t, payload); second != "" {
		t.Errorf("a second edit in the session resent the card:\n%s", second)
	}
	ev.Outcome = c.version + ": hooks/list reports the PreToolUse member at " + member.Key + "; Codex's currentHash equals Atomic's identity hash; the verb emits hookSpecificOutput.additionalContext for an apply_patch payload and not again in the session"
	ev.Detail = "level startup (app-server hooks/list, no model turn); hooks/list sha256 " + retain(t, "pre-tool-use-cards.hooks-list.json", raw) +
		"; verb output sha256 " + retain(t, "pre-tool-use-cards.verb.json", first)
	recordScenario(t, ev)
}

func (c *codexRedesign) verb(t *testing.T, payload string) string {
	t.Helper()
	cmd := exec.Command(c.atomic, "hooks", "pre-tool-use")
	cmd.Dir = c.repo
	cmd.Env = c.env()
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("atomic hooks pre-tool-use: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
