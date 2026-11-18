package install

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// The operator approves an uninstall knowing the card link stays behind.
func TestUninstallApprovalCountsTheLinkLeft(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(t.TempDir(), ".claude", "rules", "atomic")
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: "claude", Instance: "default", NativeRoot: filepath.Join(home, ".claude")})
	ledger.Upsert(installstate.Row{Target: "claude:default", Resource: link,
		Applied: installstate.AppliedValue{Path: link, Kind: managedfile.KindSymlink, Digest: "d"}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	var summary string
	steps := DefaultSteps(home)
	steps.Confirm = func(_, desc string, _ bool) (bool, error) {
		summary = desc
		return false, nil
	}
	if _, err := steps.UninstallTarget("claude:default"); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("UninstallTarget error = %v, want the declined approval", err)
	}
	if !strings.Contains(summary, "1 left") {
		t.Errorf("approval summary = %q, want the left link counted", summary)
	}
}
