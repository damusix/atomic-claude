package managedfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// renameDir performs a publication rename. It is a variable so a test can
// inject a failure between the current-to-backup displacement and the staged
// move, which is the only window the rollback path covers.
var renameDir = os.Rename

// WriteFileAtomic writes data to path through a same-directory temp file,
// fsyncing the bytes and the containing directory before the rename so a crash
// never publishes a torn file. An existing target's permission bits are
// preserved; a symlinked target is followed and rewritten rather than replaced
// by the rename.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	resolved, existingMode, writable, err := resolveFileTarget(path)
	if err != nil {
		return err
	}
	if !writable {
		return fmt.Errorf("managedfile: %s is not writable", resolved)
	}
	if existingMode != 0 {
		mode = existingMode
	}
	if mode == 0 {
		mode = 0o644
	}

	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("managedfile: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(resolved)+".*.tmp")
	if err != nil {
		return fmt.Errorf("managedfile: create temp for %s: %w", resolved, err)
	}
	tmpName := tmp.Name()

	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	for _, err := range []error{writeErr, syncErr, closeErr} {
		if err != nil {
			os.Remove(tmpName)
			return fmt.Errorf("managedfile: write %s: %w", resolved, err)
		}
	}

	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("managedfile: chmod temp for %s: %w", resolved, err)
	}
	if err := os.Rename(tmpName, resolved); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("managedfile: rename to %s: %w", resolved, err)
	}
	if err := FsyncDir(dir); err != nil {
		return fmt.Errorf("managedfile: sync %s: %w", dir, err)
	}
	return nil
}

// Publication records what a generated-directory publication did.
type Publication struct {
	// First reports that the destination did not exist, so the publication was
	// one rename of the staged tree.
	First bool `json:"first"`
	// Backup is the displaced tree's new location on a replacement.
	Backup string `json:"backup,omitempty"`
	// Digest is the published tree's digest.
	Digest string `json:"digest,omitempty"`
}

// PublishDir publishes stageDir as destDir. A first publication is one
// same-filesystem rename of the staged tree. A replacement moves the current
// tree into backupDir and then stages into place; if the second rename fails,
// the displaced tree is moved back before the error is returned, so the
// destination is never left absent by a failed replacement.
//
// The staging root can sit on another filesystem — a repository-local
// destination is published from the HOME-rooted transaction stage — where a
// rename out of staging cannot cross. That case copies the staged tree onto the
// destination's filesystem and publishes it with the same rename, and copies
// the displaced tree into the transaction backup, so the journaled backup
// record stays a durable pre-mutation copy.
func PublishDir(stageDir, destDir, backupDir string) (Publication, error) {
	if _, err := os.Stat(stageDir); err != nil {
		return Publication{}, fmt.Errorf("managedfile: staging %s: %w", stageDir, err)
	}

	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Publication{}, fmt.Errorf("managedfile: mkdir %s: %w", parent, err)
	}

	same, err := sameDevice(parent, stageDir)
	if err != nil {
		return Publication{}, err
	}

	digest, _, err := TreeDigest(stageDir)
	if err != nil {
		return Publication{}, err
	}

	if !same {
		return publishDirCopy(stageDir, destDir, backupDir, digest)
	}

	switch _, statErr := os.Stat(destDir); {
	case os.IsNotExist(statErr):
		if err := renameDir(stageDir, destDir); err != nil {
			return Publication{}, fmt.Errorf("managedfile: publish %s: %w", destDir, err)
		}
		if err := FsyncDir(parent); err != nil {
			return Publication{}, fmt.Errorf("managedfile: sync %s: %w", parent, err)
		}
		return Publication{First: true, Digest: digest}, nil
	case statErr != nil:
		return Publication{}, fmt.Errorf("managedfile: stat %s: %w", destDir, statErr)
	}

	if backupDir == "" {
		return Publication{}, fmt.Errorf("managedfile: replacing %s needs a transaction backup directory", destDir)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return Publication{}, fmt.Errorf("managedfile: mkdir %s: %w", backupDir, err)
	}
	backup := filepath.Join(backupDir, filepath.Base(destDir))
	if _, err := os.Stat(backup); err == nil {
		return Publication{}, fmt.Errorf("managedfile: transaction backup %s already exists", backup)
	} else if !os.IsNotExist(err) {
		return Publication{}, fmt.Errorf("managedfile: stat %s: %w", backup, err)
	}

	if err := renameDir(destDir, backup); err != nil {
		return Publication{}, fmt.Errorf("managedfile: move current %s to backup: %w", destDir, err)
	}
	if err := FsyncDir(parent); err != nil {
		return Publication{}, fmt.Errorf("managedfile: sync %s: %w", parent, err)
	}

	if err := renameDir(stageDir, destDir); err != nil {
		restoreErr := renameDir(backup, destDir)
		_ = FsyncDir(parent)
		if restoreErr != nil {
			return Publication{}, fmt.Errorf("managedfile: publish %s: %w; rollback to %s failed: %v", destDir, err, destDir, restoreErr)
		}
		return Publication{}, fmt.Errorf("managedfile: publish %s: %w; displaced tree restored", destDir, err)
	}
	if err := FsyncDir(parent); err != nil {
		return Publication{}, fmt.Errorf("managedfile: sync %s: %w", parent, err)
	}
	return Publication{Backup: backup, Digest: digest}, nil
}

// publishDirCopy is PublishDir's cross-filesystem path: it copies the staged
// tree beside the destination and publishes the copy with a same-filesystem
// rename, so the destination never observes a half-copied tree. A replacement
// first copies the displaced tree into the transaction backup and removes the
// original; if the staged copy cannot be published, the backup is copied back,
// so a failed replacement never leaves the destination absent.
func publishDirCopy(stageDir, destDir, backupDir, digest string) (Publication, error) {
	parent := filepath.Dir(destDir)

	backup := ""
	displaced := false
	switch _, statErr := os.Stat(destDir); {
	case statErr == nil:
		if backupDir == "" {
			return Publication{}, fmt.Errorf("managedfile: replacing %s needs a transaction backup directory", destDir)
		}
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return Publication{}, fmt.Errorf("managedfile: mkdir %s: %w", backupDir, err)
		}
		backup = filepath.Join(backupDir, filepath.Base(destDir))
		if _, err := os.Stat(backup); err == nil {
			return Publication{}, fmt.Errorf("managedfile: transaction backup %s already exists", backup)
		} else if !os.IsNotExist(err) {
			return Publication{}, fmt.Errorf("managedfile: stat %s: %w", backup, err)
		}
		if err := copyTree(destDir, backup); err != nil {
			return Publication{}, fmt.Errorf("managedfile: back up current %s: %w", destDir, err)
		}
		if err := FsyncDir(backupDir); err != nil {
			return Publication{}, fmt.Errorf("managedfile: sync %s: %w", backupDir, err)
		}
		if err := os.RemoveAll(destDir); err != nil {
			return Publication{}, fmt.Errorf("managedfile: remove displaced %s: %w", destDir, err)
		}
		displaced = true
	case os.IsNotExist(statErr):
	default:
		return Publication{}, fmt.Errorf("managedfile: stat %s: %w", destDir, statErr)
	}

	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(destDir)+".staging-")
	if err != nil {
		return Publication{}, restoreDisplaced(displaced, backup, destDir, fmt.Errorf("managedfile: stage beside %s: %w", destDir, err))
	}
	if err := copyTree(stageDir, tmp); err != nil {
		os.RemoveAll(tmp)
		return Publication{}, restoreDisplaced(displaced, backup, destDir, fmt.Errorf("managedfile: publish %s: %w", destDir, err))
	}
	if err := renameDir(tmp, destDir); err != nil {
		os.RemoveAll(tmp)
		return Publication{}, restoreDisplaced(displaced, backup, destDir, fmt.Errorf("managedfile: publish %s: %w", destDir, err))
	}
	if err := FsyncDir(parent); err != nil {
		return Publication{}, fmt.Errorf("managedfile: sync %s: %w", parent, err)
	}
	if displaced {
		return Publication{Backup: backup, Digest: digest}, nil
	}
	return Publication{First: true, Digest: digest}, nil
}

// restoreDisplaced copies a replacement's backup back over the destination
// after a failed cross-filesystem publication. A first publication displaced
// nothing, so the cause is returned unchanged.
func restoreDisplaced(displaced bool, backup, destDir string, cause error) error {
	if !displaced {
		return cause
	}
	if err := copyTree(backup, destDir); err != nil {
		return fmt.Errorf("%w; rollback to %s failed: %v", cause, destDir, err)
	}
	if err := FsyncDir(filepath.Dir(destDir)); err != nil {
		return fmt.Errorf("%w; rollback to %s failed: %v", cause, destDir, err)
	}
	return fmt.Errorf("%w; displaced tree restored", cause)
}

// copyTree copies the tree at src into dest, which may exist as a directory,
// preserving every entry's type and mode. Files go through WriteFileAtomic and
// directories are fsynced bottom-up, so the copy is as durable as the rename it
// stands in for.
func copyTree(src, dest string) error {
	var dirs []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
			dirs = append(dirs, target)
			return nil
		default:
			info, err := d.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return WriteFileAtomic(target, data, info.Mode().Perm())
		}
	})
	if err != nil {
		return fmt.Errorf("managedfile: copy tree %s: %w", dest, err)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := FsyncDir(dirs[i]); err != nil {
			return fmt.Errorf("managedfile: sync %s: %w", dirs[i], err)
		}
	}
	return nil
}

// MoveTree moves the tree at src to dest, which must not exist. A same-device
// move is one rename; a cross-device move copies the tree and removes the
// source, so a transaction backup captured on another filesystem still
// restores.
func MoveTree(src, dest string) error {
	same, err := sameDevice(filepath.Dir(dest), src)
	if err != nil {
		return err
	}
	if same {
		if err := os.Rename(src, dest); err != nil {
			return fmt.Errorf("managedfile: move tree %s: %w", dest, err)
		}
		return FsyncDir(filepath.Dir(dest))
	}
	if err := copyTree(src, dest); err != nil {
		return fmt.Errorf("managedfile: move tree %s: %w", dest, err)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("managedfile: move tree %s: remove source %s: %w", dest, src, err)
	}
	return FsyncDir(filepath.Dir(dest))
}

// FsyncDir flushes a directory entry so a rename or create is durable.
func FsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// resolveFileTarget follows path through any symlink, reports an existing
// target's permission bits, and probes writability so a read-only target is
// reported rather than clobbered by a rename that only needs directory
// permission. A missing file resolves to itself with writable true.
func resolveFileTarget(path string) (resolved string, mode os.FileMode, writable bool, err error) {
	resolved = path
	if r, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
		resolved = r
	} else if !os.IsNotExist(evalErr) {
		return "", 0, false, fmt.Errorf("managedfile: resolve %s: %w", path, evalErr)
	}

	if fi, statErr := os.Stat(resolved); statErr == nil {
		mode = fi.Mode().Perm()
	}

	f, openErr := os.OpenFile(resolved, os.O_WRONLY, 0)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return resolved, mode, true, nil
		}
		if os.IsPermission(openErr) {
			return resolved, mode, false, nil
		}
		return "", 0, false, fmt.Errorf("managedfile: probe %s: %w", resolved, openErr)
	}
	f.Close()
	return resolved, mode, true, nil
}

// sameDevice reports whether two paths resolve to one filesystem device. It is
// a variable so a test can prove the cross-filesystem publication path on one
// physical volume; production never reassigns it.
var sameDevice = func(a, b string) (bool, error) {
	devA, err := deviceOf(a)
	if err != nil {
		return false, err
	}
	devB, err := deviceOf(b)
	if err != nil {
		return false, err
	}
	return devA == devB, nil
}

func deviceOf(path string) (uint64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("managedfile: stat %s: %w", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("managedfile: %s has no device identity", path)
	}
	return uint64(st.Dev), nil
}
