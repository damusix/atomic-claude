package hooks_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const userPreToolUseHooks = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "python3 ~/.codex/hooks/guard.py" }]
      }
    ]
  }
}
`

func TestInstallCodexAddsThePreToolUseMemberForApplyPatch(t *testing.T) {
	root := writeCodexHooks(t, "")
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	got := readHooksFile(t, root)
	for _, want := range []string{`"PreToolUse"`, `"matcher": "apply_patch"`, `"command": "atomic hooks pre-tool-use"`} {
		if !strings.Contains(got, want) {
			t.Errorf("hooks.json missing %s:\n%s", want, got)
		}
	}
}

// Enrollment owns only its own PreToolUse member: a user's group on the same
// event keeps its bytes and its position, and uninstall gives the file back.
func TestPreToolUseMemberRoundTripPreservesTheUsersGroup(t *testing.T) {
	root := writeCodexHooks(t, userPreToolUseHooks)
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	got := readHooksFile(t, root)
	if strings.Count(got, hooks.CodexPreToolUseCommand) != 1 {
		t.Fatalf("want exactly one owned PreToolUse member:\n%s", got)
	}
	if strings.Index(got, "guard.py") > strings.Index(got, hooks.CodexPreToolUseCommand) {
		t.Errorf("the user's PreToolUse group lost its position:\n%s", got)
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if again := readHooksFile(t, root); again != got {
		t.Errorf("a second install rewrote the file:\n%s", again)
	}
	if _, err := hooks.UninstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if after := readHooksFile(t, root); after != userPreToolUseHooks {
		t.Errorf("uninstall did not restore the user's bytes:\n%s", after)
	}
}

func TestCodexPreToolUsePositionFollowsTheUsersGroups(t *testing.T) {
	root := writeCodexHooks(t, userPreToolUseHooks)
	if _, _, found, err := hooks.CodexPreToolUsePosition(root); err != nil || found {
		t.Fatalf("found = %v, err = %v before install", found, err)
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	group, handler, found, err := hooks.CodexPreToolUsePosition(root)
	if err != nil || !found || group != 1 || handler != 0 {
		t.Errorf("position = (%d, %d, %v, %v), want (1, 0, true, nil)", group, handler, found, err)
	}
}

// A file Atomic wrote before the PreToolUse member existed holds only the
// SessionStart member; install must complete it.
func TestInstallCodexOverAFileWithOnlyTheSessionStartMemberAddsThePreToolUseOne(t *testing.T) {
	legacy := `{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`
	root := writeCodexHooks(t, legacy)
	before, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest != hooks.CodexLegacyDigest() || before.Digest == hooks.CodexCanonicalDigest() {
		t.Fatalf("a SessionStart-only file digests %s, want the legacy %s and not the canonical", before.Digest, hooks.CodexLegacyDigest())
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	after, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if after.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("install digests %s, want the canonical %s:\n%s", after.Digest, hooks.CodexCanonicalDigest(), readHooksFile(t, root))
	}
}

func TestMalformedPreToolUseShapeRefusesAndKeepsItsBytes(t *testing.T) {
	body := `{"hooks":{"PreToolUse":{}}}`
	root := writeCodexHooks(t, body)
	obs, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Conflict != managedfile.ConflictMalformedSettings {
		t.Errorf("observed as %+v, want a malformed conflict", obs)
	}
	if _, err := hooks.InstallCodex(root); err == nil {
		t.Error("install succeeded over a malformed PreToolUse shape")
	}
	if got := readHooksFile(t, root); got != body {
		t.Errorf("bytes changed to %s", got)
	}
}
