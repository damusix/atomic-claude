package mcp_test

import (
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
)

// A held spawn lock keeps a second holder, and so a starter, out until release.
func TestTryHoldSpawnLockExcludesOtherHolders(t *testing.T) {
	db := filepath.Join(t.TempDir(), "atomic.db")

	release, ok, err := mcp.TryHoldSpawnLock(db)
	if err != nil || !ok {
		t.Fatalf("an idle lock could not be held (ok %v, err %v)", ok, err)
	}
	if _, second, err := mcp.TryHoldSpawnLock(db); second || err != nil {
		t.Errorf("a second holder = ok %v, err %v; want contention without an error", second, err)
	}
	if !mcp.SpawnLockHeld(db) {
		t.Error("SpawnLockHeld reads a held lock as free")
	}

	release()
	again, ok, err := mcp.TryHoldSpawnLock(db)
	if err != nil || !ok {
		t.Fatalf("the lock stayed held after release (ok %v, err %v)", ok, err)
	}
	again()
}

// A lock file that cannot be opened is a failure to report, not contention.
func TestTryHoldSpawnLockReportsOpenFailure(t *testing.T) {
	db := filepath.Join(t.TempDir(), "missing-dir", "atomic.db")
	if _, ok, err := mcp.TryHoldSpawnLock(db); ok || err == nil {
		t.Errorf("TryHoldSpawnLock = ok %v, err %v; want an open error", ok, err)
	}
}
