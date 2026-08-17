package config

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// renameState is the seam over os.Rename so tests can force the copy-fallback
// path without a real multi-filesystem (EXDEV) setup.
var renameState = os.Rename

// setRenameStateForTest overrides renameState and returns a restore func.
func setRenameStateForTest(fn func(oldpath, newpath string) error) func() {
	prev := renameState
	renameState = fn
	return func() { renameState = prev }
}

// symlinkState is the seam over os.Symlink so tests can force a symlink
// failure (e.g. a transient permission error) independently of the rename.
var symlinkState = os.Symlink

// setSymlinkStateForTest overrides symlinkState and returns a restore func.
func setSymlinkStateForTest(fn func(oldname, newname string) error) func() {
	prev := symlinkState
	symlinkState = fn
	return func() { symlinkState = prev }
}

// stagingDirName is where the copy fallback lands first, so a partial copy is
// never visible at the final ~/.atomic path; a same-filesystem rename then moves
// it into place. A leftover from a crashed run is wiped before each attempt.
const stagingDirName = ".atomic.migrating"

// MigrateUserState moves legacy per-user state from <home>/.claude/.atomic to
// <home>/.atomic. Idempotent: once <home>/.atomic exists as a real directory
// this is a no-op, and a legacy dir found at that point is left for `atomic
// doctor` to flag rather than merged. A compat symlink stays at the legacy path
// so old @-refs in installed CLAUDE.md files keep resolving.
//
// home is injected, never os.UserHomeDir, so tests can use a temp dir.
func MigrateUserState(home string) error {
	newDir := Dir(home)
	legacyDir := filepath.Join(home, ".claude", ".atomic")

	if info, err := os.Stat(newDir); err == nil && info.IsDir() {
		// Already migrated. Still ensure the compat symlink: a prior run's rename
		// may have succeeded while the symlink step failed, and this idempotency
		// check must not short-circuit that retry forever.
		return ensureCompatSymlink(home, newDir)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("config: stat %s: %w", newDir, err)
	}

	legacyInfo, err := os.Lstat(legacyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh machine: nothing to migrate
		}
		return fmt.Errorf("config: stat legacy state dir %s: %w", legacyDir, err)
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 || !legacyInfo.IsDir() {
		// Not a real legacy directory (already a symlink, or some other file
		// occupying the path) — nothing to migrate.
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return fmt.Errorf("config: create state parent dir: %w", err)
	}

	if err := renameState(legacyDir, newDir); err != nil {
		// Cross-device or other rename failure: fall back to a recursive copy,
		// staged into a sibling temp dir so a partial copy can never occupy newDir
		// — the idempotency check above would otherwise trust a half-copied
		// directory forever. legacyDir stays put, so no symlink can be created
		// there and doctor flags the leftover.
		stagingDir := filepath.Join(home, stagingDirName)
		if serr := os.RemoveAll(stagingDir); serr != nil {
			return fmt.Errorf("config: clean stale staging dir %s: %w", stagingDir, serr)
		}
		if cerr := copyDirRecursive(legacyDir, stagingDir); cerr != nil {
			_ = os.RemoveAll(stagingDir)
			return fmt.Errorf("config: rename legacy state dir failed (%v); copy fallback failed: %w", err, cerr)
		}
		if rerr := os.Rename(stagingDir, newDir); rerr != nil {
			return fmt.Errorf("config: rename legacy state dir failed (%v); staged copy succeeded but final rename failed: %w", err, rerr)
		}
		return nil
	}

	if err := symlinkState(newDir, legacyDir); err != nil {
		return fmt.Errorf("config: create compat symlink %s -> %s: %w", legacyDir, newDir, err)
	}
	return nil
}

// ensureCompatSymlink links <home>/.claude/.atomic -> newDir when it is safe:
// <home>/.claude must already exist (this never creates ~/.claude) and the legacy
// path must be absent. A real directory there is the both-real conflict, left for
// doctor; an existing symlink is left alone wherever it points.
func ensureCompatSymlink(home, newDir string) error {
	claudeDir := filepath.Join(home, ".claude")
	if info, err := os.Stat(claudeDir); err != nil || !info.IsDir() {
		return nil
	}

	legacyDir := filepath.Join(claudeDir, ".atomic")
	if _, err := os.Lstat(legacyDir); err == nil {
		// Already occupied — a real dir (both-real conflict) or an existing
		// symlink, either way left alone here.
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("config: stat legacy compat path %s: %w", legacyDir, err)
	}

	if err := symlinkState(newDir, legacyDir); err != nil {
		return fmt.Errorf("config: create compat symlink %s -> %s: %w", legacyDir, newDir, err)
	}
	return nil
}

// copyDirRecursive copies src into dst, creating dst and intermediates. The EXDEV
// fallback when a same-filesystem rename is not possible.
func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
