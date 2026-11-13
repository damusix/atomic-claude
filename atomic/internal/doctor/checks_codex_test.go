package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

func TestReadCodexSurfacesReportsSessionHook(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	state := readCodexSurfaces(t.TempDir(), root)
	if state.trust != codex.HookUntrusted || state.trustEvidence == "" {
		t.Errorf("trust = %s (%q), want untrusted with evidence when Codex recorded none", state.trust, state.trustEvidence)
	}
	found := false
	for _, row := range state.surfaces {
		if row.Surface == "session-context delivery" {
			found = true
			if row.Status != harness.StatusUnsupported {
				t.Errorf("session-context delivery reads %s before a runtime probe", row.Status)
			}
		}
	}
	if !found {
		t.Error("no session-context delivery surface reported")
	}
}

// Every artifact kind Codex does not receive must reach doctor with a fix the
// user can act on, or with the reason no fix exists.
func TestReadCodexSurfacesReportsUndeliveredKindsWithFixes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	state := readCodexSurfaces(t.TempDir(), root)
	seen := map[string]bool{}
	for _, row := range state.surfaces {
		seen[row.Surface] = true
	}
	for _, want := range []string{"command delivery", "steering @ imports", "output-style selection", "repository wiki pointer cards"} {
		if !seen[want] {
			t.Errorf("no %q surface reported", want)
		}
	}
	if fix := codex.Remediation(t.TempDir(), root, "native marketplace and plugin registration"); !strings.Contains(fix, "atomic install --harness codex") {
		t.Errorf("registration fix = %q, want the install command", fix)
	}
	if fix := codex.Remediation(t.TempDir(), root, "command delivery"); !strings.Contains(fix, "harness-capability-matrix") {
		t.Errorf("unproven-surface fix = %q, want the matrix that tracks it", fix)
	}
}

func TestCheckCodexReportsSessionHookTrust(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	target := installstate.TargetRecord{Harness: string(harness.KindCodex), Instance: root, NativeRoot: root, Status: string(harness.StatusConverged)}
	saveLedger(t, home, []installstate.TargetRecord{target}, nil)
	withStatusSteps(t, func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{kind: harness.KindClaude})
	})
	restore := codexSurfacesFn
	t.Cleanup(func() { codexSurfacesFn = restore })
	codexSurfacesFn = func(_, got string) codexSurfaces {
		return codexSurfaces{root: got, trust: codex.HookTrustUnknown, trustEvidence: "no record"}
	}

	r := checkCodex(Opts{Home: home})
	joined := strings.Join(r.Findings, "\n")
	if !strings.Contains(joined, "session-start hook trust: unknown — no record") {
		t.Errorf("findings do not report the hook's trust:\n%s", joined)
	}
	if strings.Contains(joined, "session-start hook trust fix:") {
		t.Errorf("an unknown trust carries a re-trust fix:\n%s", joined)
	}
}

// Codex runs a modified hook only after the user trusts it again, so doctor
// names that step rather than only the state.
func TestCheckCodexTellsTheUserToRetrustAModifiedHook(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	target := installstate.TargetRecord{Harness: string(harness.KindCodex), Instance: root, NativeRoot: root, Status: string(harness.StatusConverged)}
	saveLedger(t, home, []installstate.TargetRecord{target}, nil)
	withStatusSteps(t, func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{kind: harness.KindClaude})
	})
	restore := codexSurfacesFn
	t.Cleanup(func() { codexSurfacesFn = restore })
	codexSurfacesFn = func(_, got string) codexSurfaces {
		return codexSurfaces{root: got, trust: codex.HookTrustModified, trustEvidence: "stale hash"}
	}

	joined := strings.Join(checkCodex(Opts{Home: home}).Findings, "\n")
	if !strings.Contains(joined, "session-start hook trust: modified — stale hash") {
		t.Errorf("findings do not report the modified trust:\n%s", joined)
	}
	if !strings.Contains(joined, root+" session-start hook trust fix: open Codex, run /hooks, and trust the Atomic SessionStart hook again") {
		t.Errorf("findings carry no re-trust fix:\n%s", joined)
	}
}

// Codex keys trust by event and position, so a user's PreToolUse group ahead of
// Atomic's moves the key; doctor must read the record at the member's position.
func TestReadCodexSurfacesReadsPreToolUseTrustFromTheMembersPosition(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	user := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"guard"}]}]}}`
	if err := os.WriteFile(hooks.CodexHooksPath(root), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	key := hooks.CodexHooksPath(root) + ":pre_tool_use:0:0"
	if err := os.WriteFile(codex.ConfigPath(root), []byte("[hooks.state.\""+key+"\"]\ntrusted_hash = \"sha256:x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := readCodexSurfaces(t.TempDir(), root)
	if state.cardTrust != codex.HookUntrusted || !strings.Contains(state.cardEvidence, "no trust for this member") {
		t.Errorf("card trust = %s (%q), want untrusted: the record belongs to the user's group", state.cardTrust, state.cardEvidence)
	}
}

func TestCheckCodexReportsPreToolUseTrustAndTellsTheUserToRetrustIt(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	target := installstate.TargetRecord{Harness: string(harness.KindCodex), Instance: root, NativeRoot: root, Status: string(harness.StatusConverged)}
	saveLedger(t, home, []installstate.TargetRecord{target}, nil)
	withStatusSteps(t, func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{kind: harness.KindClaude})
	})
	restore := codexSurfacesFn
	t.Cleanup(func() { codexSurfacesFn = restore })
	codexSurfacesFn = func(_, got string) codexSurfaces {
		return codexSurfaces{root: got, trust: codex.HookTrusted, cardTrust: codex.HookTrustModified, cardEvidence: "stale hash"}
	}

	joined := strings.Join(checkCodex(Opts{Home: home}).Findings, "\n")
	if !strings.Contains(joined, "pre-tool-use hook trust: modified — stale hash") {
		t.Errorf("findings do not report the PreToolUse trust:\n%s", joined)
	}
	if !strings.Contains(joined, root+" pre-tool-use hook trust fix: open Codex, run /hooks, and trust the Atomic PreToolUse hook again") {
		t.Errorf("findings carry no re-trust fix:\n%s", joined)
	}
}
