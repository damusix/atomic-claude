package install

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// An adoption whose marker write failed leaves the root enrolled and the legacy
// record unmarked; the next update must write the marker, not re-adopt.
func TestAdoptLegacyClaudeSelfHealsMissingMarker(t *testing.T) {
	home := t.TempDir()
	root := claude.DefaultConfigDir(home)

	ledger := &installstate.Ledger{Header: installstate.NewHeader()}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: string(harness.KindClaude), Instance: root, NativeRoot: root, Status: string(harness.StatusConverged)})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Install.Version = "1.0.0"
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}

	announced := false
	_, blockers, err := testSteps(t, home).AdoptLegacyClaude(func(string) { announced = true })
	if err != nil || len(blockers) > 0 {
		t.Fatalf("AdoptLegacyClaude = %v, %v; want the marker written without error", blockers, err)
	}
	if announced {
		t.Error("an enrolled root was announced as being adopted")
	}
	rec, err := claudeinstall.ReadInstallRecord(home)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Adopted {
		t.Error("the legacy record stayed unmarked, so update would adopt it again after an uninstall")
	}
}
