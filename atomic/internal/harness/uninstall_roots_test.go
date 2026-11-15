package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Two Claude roots hold rows with one root-relative id for different files, so
// uninstalling one root must remove its own files and leave the other's alone.
func TestRemovingOneOfTwoClaudeRootsRemovesItsFiles(t *testing.T) {
	home := t.TempDir()
	const id = "agents/atomic-example.md"
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var targets []Target
	for _, name := range []string{".claude", ".claude-work"} {
		root := filepath.Join(home, name)
		target := Target{Kind: KindClaude, Instance: root, NativeRoot: root}
		targets = append(targets, target)
		path := filepath.Join(root, filepath.FromSlash(id))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("owned\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ledger.UpsertTarget(installstate.TargetRecord{Harness: string(KindClaude), Instance: root, NativeRoot: root})
		ledger.Upsert(installstate.Row{Target: target.Key(), Resource: id, Consumer: target.Key(),
			Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: managedfile.Digest([]byte("owned\n"))}})
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	removal, err := RemoveTargetResources(home, targets[0])
	if err != nil {
		t.Fatalf("RemoveTargetResources: %v", err)
	}
	if len(removal.Removed) != 1 || len(removal.Retained) != 0 {
		t.Errorf("removal = %+v, want the root's own file removed and nothing retained", removal)
	}
	if _, err := os.Stat(filepath.Join(targets[0].NativeRoot, filepath.FromSlash(id))); !os.IsNotExist(err) {
		t.Errorf("the uninstalled root's file survived (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(targets[1].NativeRoot, filepath.FromSlash(id))); err != nil {
		t.Errorf("the other root's file was touched: %v", err)
	}
	after, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Find(targets[1].Key(), id); !ok {
		t.Error("the other root lost its row")
	}
}

// A CLAUDE_CONFIG_DIR symlinked to ~/.claude is a second target over the same
// files, so uninstalling one target must retain what the other still owns.
func TestRemovingOneOfTwoTargetsOverOneRootRetainsItsFiles(t *testing.T) {
	home := t.TempDir()
	const id = "agents/atomic-example.md"
	real := filepath.Join(home, ".claude")
	alias := filepath.Join(home, "claude-config")
	path := filepath.Join(real, filepath.FromSlash(id))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var targets []Target
	for _, root := range []string{real, alias} {
		target := Target{Kind: KindClaude, Instance: root, NativeRoot: root}
		targets = append(targets, target)
		ledger.UpsertTarget(installstate.TargetRecord{Harness: string(KindClaude), Instance: root, NativeRoot: root})
		ledger.Upsert(installstate.Row{Target: target.Key(), Resource: id, Consumer: target.Key(),
			Applied: installstate.AppliedValue{Path: filepath.Join(root, filepath.FromSlash(id)), Kind: managedfile.KindFile, Digest: managedfile.Digest([]byte("owned\n"))}})
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	removal, err := RemoveTargetResources(home, targets[1])
	if err != nil {
		t.Fatalf("RemoveTargetResources: %v", err)
	}
	if len(removal.Retained) != 1 || len(removal.Removed) != 0 {
		t.Errorf("removal = %+v, want the shared file retained", removal)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file the other target owns was deleted: %v", err)
	}
}
