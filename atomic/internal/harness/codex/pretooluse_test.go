package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

const userPreToolUseJSON = `{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "python3 guard.py" }] }
    ]
  }
}
`

func TestEnrollOwnsThePreToolUseMemberBesideTheUsersGroup(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, hooks.CodexHooksPath(root), userPreToolUseJSON)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, hooks.CodexHooksPath(root))
	if !strings.Contains(got, `"command": "atomic hooks pre-tool-use"`) || !strings.Contains(got, "guard.py") {
		t.Errorf("hooks.json lacks the owned PreToolUse member or dropped the user's:\n%s", got)
	}
	row, ok := sessionHookRow(t, home, root)
	if !ok || row.Applied.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("row = %+v, want the one hooks.json row digesting both owned members", row.Applied)
	}
	if _, err := a.Lifecycle(home).Remove(codexTarget(root)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if after := readFile(t, hooks.CodexHooksPath(root)); after != userPreToolUseJSON {
		t.Errorf("target uninstall did not restore the user's bytes:\n%s", after)
	}
}

// An install recorded before the PreToolUse member existed still matches its
// row, so only a plan that notices the missing member ever delivers cards.
func TestEnrollCompletesAnInstallRecordedWithOnlyTheSessionStartMember(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	legacy := `{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`
	writeFile(t, hooks.CodexHooksPath(root), legacy)
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := sessionHookRow(t, home, root)
	row.Applied.Digest = hooks.CodexLegacyDigest()
	ledger.Upsert(row)
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Converged {
		t.Error("a file missing the PreToolUse member reads converged")
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); !strings.Contains(got, hooks.CodexPreToolUseCommand) {
		t.Errorf("enroll left the file without the PreToolUse member:\n%s", got)
	}
	if row, _ := sessionHookRow(t, home, root); row.Applied.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("row digest = %s, want the canonical %s", row.Applied.Digest, hooks.CodexCanonicalDigest())
	}
}

func preToolUseIdentityHash() string {
	identity := `{"event_name":"pre_tool_use","hooks":[{"async":false,"command":"atomic hooks pre-tool-use","timeout":600,"type":"command"}],"matcher":"apply_patch"}`
	sum := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Codex keys a hook's trust by event and position, so the user's group ahead of
// Atomic's moves the key from 0:0 to 1:0.
func TestPreToolUseTrustIsReadFromTheKeyOfTheMembersPosition(t *testing.T) {
	home, root, a := enrolledHome(t)
	if trust, _ := ObservePreToolUseHookTrust(home, root); trust != HookTrustUnknown {
		t.Errorf("trust with no owned member = %s, want unknown", trust)
	}
	writeFile(t, hooks.CodexHooksPath(root), userPreToolUseJSON)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	path := hooks.CodexHooksPath(root)

	writeFile(t, ConfigPath(root), fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", path+":pre_tool_use:0:0", preToolUseIdentityHash()))
	if trust, ev := ObservePreToolUseHookTrust(home, root); trust != HookUntrusted {
		t.Errorf("a record under the user's group key = %s (%s), want untrusted", trust, ev)
	}
	writeFile(t, ConfigPath(root), fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", path+":pre_tool_use:1:0", preToolUseIdentityHash()))
	if trust, ev := ObservePreToolUseHookTrust(home, root); trust != HookTrusted {
		t.Errorf("a matching record at the member's position = %s (%s), want trusted", trust, ev)
	}
	writeFile(t, ConfigPath(root), fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", path+":pre_tool_use:1:0", "sha256:0000"))
	if trust, ev := ObservePreToolUseHookTrust(home, root); trust != HookTrustModified {
		t.Errorf("a stale hash = %s (%s), want modified", trust, ev)
	}
}

// A row recorded before the PreToolUse member existed holds the legacy digest;
// --replace must still repair an edited member under it.
func TestEnrollReplaceRepairsAMemberEditedUnderALegacyRow(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	writeFile(t, hooks.CodexHooksPath(root), `{"hooks":{"SessionStart":[{"matcher":"other","hooks":[{"type":"command","command":"`+hooks.CodexSessionStartCommand+`"}]}]}}`+"\n")
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := sessionHookRow(t, home, root)
	row.Applied.Digest = hooks.CodexLegacyDigest()
	ledger.Upsert(row)
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	if obs, err := hooks.ObserveCodexHooksOwned(root); err != nil || obs.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("member digest = %q (err %v), want the canonical form", obs.Digest, err)
	}
}
