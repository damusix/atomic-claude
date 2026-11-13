package install

import (
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// A Codex target owns no rule file, so its tier cannot come from a ledger row;
// it comes from the card-delivery row the PreToolUse hook is proven by.
func TestRulesStatusReportsTheCodexWikiCardTier(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	mkdirs(t, root)
	ledger := &installstate.Ledger{}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: string(harness.KindCodex), Instance: root, NativeRoot: root, Status: string(harness.StatusEnrolled)})
	if err := ledger.Save(ledgerPath(home)); err != nil {
		t.Fatal(err)
	}

	statuses, err := detectSteps(t, home, "", root).RulesStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Tier != "hook-required" {
		t.Errorf("statuses = %+v, want one Codex target at hook-required", statuses)
	}
}
