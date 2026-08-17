package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Nothing to migrate, and no directories created.
func TestMigrateUserState_FreshNoOp(t *testing.T) {
	home := t.TempDir()

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("MigrateUserState: %v", err)
	}

	if _, err := os.Stat(Dir(home)); !os.IsNotExist(err) {
		t.Errorf("expected %s to not exist, stat err = %v", Dir(home), err)
	}
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if _, err := os.Lstat(legacyDir); !os.IsNotExist(err) {
		t.Errorf("expected %s to not exist, lstat err = %v", legacyDir, err)
	}
}

// A real legacy directory is renamed to ~/.atomic with a compat symlink left
// behind at the old path.
func TestMigrateUserState_LegacyRenameAndSymlink(t *testing.T) {
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.toml"), []byte("output.signals.max_depth = 5\n"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("MigrateUserState: %v", err)
	}

	newDir := Dir(home)
	info, err := os.Stat(newDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected %s to be a real directory, stat = %v, err = %v", newDir, info, err)
	}
	data, err := os.ReadFile(filepath.Join(newDir, "config.toml"))
	if err != nil {
		t.Fatalf("read migrated config.toml: %v", err)
	}
	if string(data) != "output.signals.max_depth = 5\n" {
		t.Errorf("migrated content = %q, want preserved content", data)
	}

	linkInfo, err := os.Lstat(legacyDir)
	if err != nil {
		t.Fatalf("lstat legacy dir: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink, mode = %v", legacyDir, linkInfo.Mode())
	}
	target, err := os.Readlink(legacyDir)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != newDir {
		t.Errorf("symlink target = %q, want %q", target, newDir)
	}

	// The old @-ref path must still resolve through the symlink.
	data, err = os.ReadFile(filepath.Join(legacyDir, "config.toml"))
	if err != nil {
		t.Fatalf("read through compat symlink: %v", err)
	}
	if string(data) != "output.signals.max_depth = 5\n" {
		t.Errorf("read-through content = %q, want preserved content", data)
	}
}

// Once ~/.atomic is a real directory, a second run is a no-op.
func TestMigrateUserState_IdempotentSecondRun(t *testing.T) {
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("first MigrateUserState: %v", err)
	}
	newDir := Dir(home)
	linkTargetBefore, err := os.Readlink(legacyDir)
	if err != nil {
		t.Fatalf("readlink after first run: %v", err)
	}

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("second MigrateUserState: %v", err)
	}

	info, err := os.Stat(newDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected %s to remain a real directory after second run", newDir)
	}
	linkTargetAfter, err := os.Readlink(legacyDir)
	if err != nil {
		t.Fatalf("readlink after second run: %v", err)
	}
	if linkTargetAfter != linkTargetBefore {
		t.Errorf("symlink target changed across idempotent runs: %q -> %q", linkTargetBefore, linkTargetAfter)
	}
}

// With ~/.atomic already real AND a real legacy directory present, prefer the
// new location and never merge — the legacy dir is left for doctor to flag.
func TestMigrateUserState_BothDirsRealConflict(t *testing.T) {
	home := t.TempDir()
	newDir := Dir(home)
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("seed new dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "config.toml"), []byte("new-content\n"), 0o644); err != nil {
		t.Fatalf("seed new content: %v", err)
	}

	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.toml"), []byte("legacy-content\n"), 0o644); err != nil {
		t.Fatalf("seed legacy content: %v", err)
	}

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("MigrateUserState: %v", err)
	}

	newData, err := os.ReadFile(filepath.Join(newDir, "config.toml"))
	if err != nil {
		t.Fatalf("read new content: %v", err)
	}
	if string(newData) != "new-content\n" {
		t.Errorf("new dir content changed: %q, want untouched %q", newData, "new-content\n")
	}

	legacyInfo, err := os.Lstat(legacyDir)
	if err != nil {
		t.Fatalf("lstat legacy dir: %v", err)
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected legacy dir to remain a real directory, not a symlink")
	}
	legacyData, err := os.ReadFile(filepath.Join(legacyDir, "config.toml"))
	if err != nil {
		t.Fatalf("read legacy content: %v", err)
	}
	if string(legacyData) != "legacy-content\n" {
		t.Errorf("legacy dir content changed: %q, want untouched %q", legacyData, "legacy-content\n")
	}
}

// A failed rename falls back to a recursive copy. The legacy directory stays in
// place, so the symlink is skipped and doctor flags the leftover.
func TestMigrateUserState_RenameErrorCopyFallback(t *testing.T) {
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(filepath.Join(legacyDir, "backups"), 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.toml"), []byte("copied-content\n"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "backups", "nested.txt"), []byte("nested\n"), 0o644); err != nil {
		t.Fatalf("seed nested legacy file: %v", err)
	}

	restore := setRenameStateForTest(func(string, string) error {
		return &os.LinkError{Op: "rename", Err: os.ErrInvalid}
	})
	defer restore()

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("MigrateUserState: %v", err)
	}

	newDir := Dir(home)
	data, err := os.ReadFile(filepath.Join(newDir, "config.toml"))
	if err != nil {
		t.Fatalf("read copied config.toml: %v", err)
	}
	if string(data) != "copied-content\n" {
		t.Errorf("copied content = %q, want %q", data, "copied-content\n")
	}
	nested, err := os.ReadFile(filepath.Join(newDir, "backups", "nested.txt"))
	if err != nil {
		t.Fatalf("read copied nested file: %v", err)
	}
	if string(nested) != "nested\n" {
		t.Errorf("copied nested content = %q, want %q", nested, "nested\n")
	}

	// Copy fallback never symlinks: the legacy dir stays a real directory so
	// doctor can flag the leftover.
	legacyInfo, err := os.Lstat(legacyDir)
	if err != nil {
		t.Fatalf("lstat legacy dir: %v", err)
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected legacy dir to remain a real directory after copy fallback, not a symlink")
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "config.toml")); err != nil {
		t.Errorf("expected legacy config.toml to remain after copy fallback: %v", err)
	}
}

// When the rename succeeds but the symlink fails, the idempotency
// short-circuit on a later run must not swallow the retry.
func TestMigrateUserState_SymlinkRetryOnSecondRun(t *testing.T) {
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.toml"), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}

	restoreSymlink := setSymlinkStateForTest(func(string, string) error {
		return &os.LinkError{Op: "symlink", Err: os.ErrPermission}
	})
	if err := MigrateUserState(home); err == nil {
		restoreSymlink()
		t.Fatal("expected MigrateUserState to surface the symlink error on first run")
	}
	restoreSymlink()

	newDir := Dir(home)
	info, err := os.Stat(newDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected rename to have succeeded despite symlink failure: stat = %v, err = %v", info, err)
	}
	if _, err := os.Lstat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("expected legacy path to remain absent after failed symlink, lstat err = %v", err)
	}

	// Second run: rename is a no-op, so the symlink retry must fire.
	if err := MigrateUserState(home); err != nil {
		t.Fatalf("second MigrateUserState: %v", err)
	}

	linkInfo, err := os.Lstat(legacyDir)
	if err != nil {
		t.Fatalf("lstat legacy dir after retry: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected legacy path to be a symlink after retry, mode = %v", linkInfo.Mode())
	}
	target, err := os.Readlink(legacyDir)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != newDir {
		t.Errorf("symlink target = %q, want %q", target, newDir)
	}
}

// A copy that fails partway must never become visible at the final ~/.atomic
// path, or a later idempotency check would trust a half-copied directory.
func TestMigrateUserState_StagedCopyAtomicity(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 000 does not restrict access")
	}

	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "a-good.txt"), []byte("good\n"), 0o644); err != nil {
		t.Fatalf("seed good file: %v", err)
	}
	badPath := filepath.Join(legacyDir, "z-bad.txt")
	if err := os.WriteFile(badPath, []byte("bad\n"), 0o644); err != nil {
		t.Fatalf("seed bad file: %v", err)
	}
	if err := os.Chmod(badPath, 0o000); err != nil {
		t.Fatalf("chmod bad file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o644) })

	restore := setRenameStateForTest(func(string, string) error {
		return &os.LinkError{Op: "rename", Err: os.ErrInvalid}
	})
	defer restore()

	if err := MigrateUserState(home); err == nil {
		t.Fatal("expected MigrateUserState to surface the copy failure")
	}

	newDir := Dir(home)
	if info, err := os.Stat(newDir); err == nil {
		t.Errorf("expected %s to not exist after a failed copy, but found: %v", newDir, info)
	} else if !os.IsNotExist(err) {
		t.Errorf("expected stat %s to report not-exist, got: %v", newDir, err)
	}
}

// A staging dir left by a crashed run is wiped before a new copy attempt, not
// merged with — otherwise its stale content leaks into the migrated ~/.atomic.
func TestMigrateUserState_StaleStagingDirCleanedBeforeCopy(t *testing.T) {
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.toml"), []byte("fresh-content\n"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}

	stagingDir := filepath.Join(home, stagingDirName)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("seed stale staging dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "stale.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("seed stale staging file: %v", err)
	}

	restore := setRenameStateForTest(func(string, string) error {
		return &os.LinkError{Op: "rename", Err: os.ErrInvalid}
	})
	defer restore()

	if err := MigrateUserState(home); err != nil {
		t.Fatalf("MigrateUserState: %v", err)
	}

	newDir := Dir(home)
	data, err := os.ReadFile(filepath.Join(newDir, "config.toml"))
	if err != nil {
		t.Fatalf("read migrated config.toml: %v", err)
	}
	if string(data) != "fresh-content\n" {
		t.Errorf("migrated content = %q, want %q", data, "fresh-content\n")
	}
	if _, err := os.Stat(filepath.Join(newDir, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("expected stale staging content not to leak into newDir, stat err = %v", err)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("expected staging dir to be consumed (renamed into newDir), stat err = %v", err)
	}
}
