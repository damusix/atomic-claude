package harness

import (
	"strings"
	"testing"
)

// The Codex row is a claim only the retained startup-level probe backs, and it
// is partial because Codex delivers a card for edits and nothing else.
func TestCodexWikiCardRowIsPartialEditsOnlyAndNamesItsEvidence(t *testing.T) {
	row := CodexCapabilities().Capability(RoleWikiCardDelivery)
	if row.Status != StatusPartial {
		t.Fatalf("status = %s, want partial", row.Status)
	}
	if !strings.Contains(row.Native, "PreToolUse") || !strings.Contains(row.Native, "apply_patch") {
		t.Errorf("native = %q, want the PreToolUse apply_patch hook", row.Native)
	}
	if !strings.Contains(row.Limitation, "edits only") {
		t.Errorf("limitation = %q, want it to say edits only", row.Limitation)
	}
	if !strings.Contains(row.Evidence, "TestCodexPreToolUseHookIsAcceptedAndDeliversTheMatchingCard") {
		t.Errorf("evidence = %q, want the runtimeproof test that recorded it", row.Evidence)
	}
}

func TestWikiCardTierIsHookRequiredOnlyWhereARowIsRecorded(t *testing.T) {
	for _, c := range []struct {
		m    CapabilityMatrix
		want string
	}{
		{CodexCapabilities(), "hook-required"},
		{ClaudeCapabilities(), "unsupported"},
		{OMPCapabilities(), "unsupported"},
		{CapabilityMatrix{}, "unsupported"},
	} {
		if got := WikiCardTier(c.m); got != c.want {
			t.Errorf("%q: tier = %s, want %s", c.m.Harness, got, c.want)
		}
	}
}
