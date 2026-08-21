package scratchpad

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Entry is one bundle List found: its slug, parsed Meta, and bundle path.
type Entry struct {
	Slug string
	Meta *Meta
	Path string
}

// List walks root for directories that contain meta.toml, skipping every
// entry that doesn't. It descends into a directory with no meta.toml of its
// own, but stops and emits at the first directory that does — a bundle's own
// contents are never walked further. Dot-prefixed entries are never walked.
//
// The same rule serves both the live scratchpad root (one level:
// <slug>/meta.toml) and the archive root (two levels:
// <slug>/<created>/meta.toml) — depth is never special-cased, so a
// pre-migration reminders/ dir, a legacy session-reports/ dir, and a legacy
// dated bundle are all skipped by the same content-based check.
//
// A corrupt meta.toml (unreadable, or a directory instead of a file) costs
// only its own entry, never the rest of the listing; it is reported through
// the returned warnings rather than the stdlib global logger, so a caller —
// including a long-running atomic serve process — decides where it goes.
func List(root string) (entries []Entry, warnings []string, err error) {
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	for _, de := range dirEntries {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		path := filepath.Join(root, de.Name())

		meta, loadErr := Load(path)
		switch {
		case loadErr == nil:
			entries = append(entries, Entry{Slug: meta.Slug, Meta: meta, Path: path})
			continue
		case os.IsNotExist(loadErr):
			// no meta.toml here — descend
		default:
			warnings = append(warnings, fmt.Sprintf("scratchpad: skipping %s: %v", path, loadErr))
			continue
		}

		nestedEntries, nestedWarnings, nestedErr := List(path)
		if nestedErr != nil {
			return nil, nil, nestedErr
		}
		entries = append(entries, nestedEntries...)
		warnings = append(warnings, nestedWarnings...)
	}
	return entries, warnings, nil
}
