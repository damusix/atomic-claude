package scratchpad

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// ArchiveRoot returns root's project-keyed archive home — the directory
// List walks for `scratchpad list --archived`.
func ArchiveRoot(root string) string {
	return filepath.Join(config.ProjectStateDir(root), "archive")
}

// HasArchive reports whether slug has an existing archived bundle under
// root's archive home — an exact slug match, checked by stat-ing the
// <slug>/ directory regardless of how many dated archives it holds — and
// returns that directory's path.
func HasArchive(root, slug string) (string, bool) {
	dir := filepath.Join(ArchiveRoot(root), slug)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

// Archive sets slug's meta.toml status to "archived", then moves its bundle
// to the project-keyed archive directory keyed by the bundle's own creation
// date. Re-archiving a slug created and archived again within the same day
// collides on that date; the destination then takes the next free "-2",
// "-3" suffix rather than overwriting the earlier archive — an archive is
// the audit trail, so losing one to a same-day repeat is not an option.
func Archive(root, slug string) (dest string, err error) {
	bundleRoot := BundleRoot(root, slug)
	meta, err := Load(bundleRoot)
	if err != nil {
		return "", fmt.Errorf("scratchpad: no bundle for %q: %w", slug, err)
	}

	// meta.toml's Created field is hand-editable local state — a truncated,
	// empty, or path-shaped value (e.g. "../../../evil") must never reach
	// nextArchiveDest, or the destination can land outside the archive root.
	created, err := config.ValidateDateSegment("bundle created date", meta.Created)
	if err != nil {
		return "", fmt.Errorf("scratchpad: bundle %q: %w", slug, err)
	}

	meta.Status = "archived"
	if err := Save(bundleRoot, meta); err != nil {
		return "", err
	}

	dest = nextArchiveDest(root, slug, created)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("scratchpad: create archive dir: %w", err)
	}
	if err := os.Rename(bundleRoot, dest); err != nil {
		return "", fmt.Errorf("scratchpad: move bundle to archive: %w", err)
	}
	return dest, nil
}

// nextArchiveDest returns config.ArchiveDir(root, slug, created), or the
// next free "-2", "-3", ... suffix on created when that directory is already
// taken by an earlier archive of the same slug on the same date.
func nextArchiveDest(root, slug, created string) string {
	dest := config.ArchiveDir(root, slug, created)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest
	}
	for i := 2; ; i++ {
		candidate := config.ArchiveDir(root, slug, fmt.Sprintf("%s-%d", created, i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
