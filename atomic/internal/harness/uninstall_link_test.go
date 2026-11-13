package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// The card link is a tracked file in the user's repository, so target
// uninstall reports it and releases its claim without deleting it.
func TestRemoveTargetResourcesLeavesTheCardLink(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	link := filepath.Join(repo, ".claude", "rules", "atomic")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.atomic/rules", link); err != nil {
		t.Fatal(err)
	}
	target := Target{Kind: KindClaude, Instance: "default", NativeRoot: filepath.Join(home, ".claude")}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: string(KindClaude), Instance: "default", NativeRoot: target.NativeRoot})
	ledger.Upsert(installstate.Row{Target: target.Key(), Resource: link, Consumer: "repo",
		Applied: installstate.AppliedValue{Path: link, Kind: managedfile.KindSymlink, Digest: managedfile.Digest([]byte("../../.atomic/rules"))}})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	removal, err := RemoveTargetResources(home, target)
	if err != nil {
		t.Fatalf("RemoveTargetResources: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("uninstall deleted the card link: %v", err)
	}
	if len(removal.Left) != 1 || removal.Left[0] != link || len(removal.Removed) != 0 {
		t.Errorf("removal = %+v, want the link reported as left", removal)
	}
	after, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Find(target.Key(), link); ok {
		t.Error("the link's ledger row survived the uninstall")
	}
}

// A symlinked CLAUDE.md or AGENTS.md is the user's link to a file they keep
// elsewhere; stripping the block must not take the link or leave the block.
func TestRemoveBlockWritesWhitespaceRemainderThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("\n<atomic>\nbody\n</atomic>\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := removeBlock(link); err != nil {
		t.Fatalf("removeBlock: %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink did not survive: %v", err)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if managedfile.HasBlock(data) {
		t.Errorf("link target still holds the Atomic block: %q", data)
	}
}

func TestPlanTargetRemovalSortsTheLinksLeft(t *testing.T) {
	ledger := &installstate.Ledger{}
	target := Target{Kind: KindClaude, Instance: "default"}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: string(KindClaude), Instance: "default"})
	for _, p := range []string{"/b/.claude/rules/atomic", "/a/.claude/rules/atomic"} {
		ledger.Upsert(installstate.Row{Target: target.Key(), Resource: p,
			Applied: installstate.AppliedValue{Path: p, Kind: managedfile.KindSymlink, Digest: "d"}})
	}
	removal, _, err := PlanTargetRemoval(t.TempDir(), target, ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(removal.Left) != 2 || removal.Left[0] != "/a/.claude/rules/atomic" {
		t.Errorf("left = %v, want sorted", removal.Left)
	}
}
