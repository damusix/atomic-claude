package migrate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func init() {
	Registry = append(Registry, Migration{
		TargetVersion: "1.2.0",
		Scope:         "repo",
		Up:            scratchpadRelocate,
		Summary:       "Relocate session-reports/ and reminders/ to ~/.atomic/<project-key>/, and rename dated <YYYY-MM-DD>-<slug> scratchpad bundles to <slug> where both docs/design/<slug>.md and docs/spec/<slug>.md exist",
		Instructions:  "Run `atomic migrate --repo <path>` once per checkout; a second run is a no-op.",
		Date:          "2026-08-20",
	})
}

// scratchpadRelocate moves a repo's legacy session-reports/ and reminders/
// under its scratchpad root to the project-keyed state home, then renames any
// dated scratchpad bundle it can confirm via the checkout's own docs. Both
// halves are idempotent by absence: a second run finds nothing to move or
// rename.
func scratchpadRelocate(ctx *Context) error {
	if err := relocateReportsAndReminders(ctx.Root); err != nil {
		return err
	}
	return redateScratchpadBundles(ctx.Root)
}

func relocateReportsAndReminders(root string) error {
	legacyReports := filepath.Join(config.ScratchpadDir(root), "session-reports")
	if err := relocateTree(legacyReports, config.ReportsRoot(root), "session report"); err != nil {
		return err
	}
	legacyReminders := config.RemindersDirLegacy(root)
	if err := relocateTree(legacyReminders, config.ProjectRemindersDir(root), "reminder"); err != nil {
		return err
	}
	return nil
}

// relocateTree moves every file under src to the identical relative path
// under dst, mechanically (no content rewrite). A destination collision — a
// file of the same relative path already present, from an earlier migration
// of a different checkout sharing the same project key — is left in place
// under src and reported rather than overwritten. src is pruned of any
// directory relocateTree emptied; a directory still holding a skipped file
// is left behind.
func relocateTree(src, dst, kind string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("migrate scratchpad: stat %s: %w", src, err)
	}

	var skipped []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		destPath := filepath.Join(dst, rel)
		if _, err := os.Stat(destPath); err == nil {
			skipped = append(skipped, rel)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		return moveFileCrossDevice(path, destPath)
	})
	if err != nil {
		return fmt.Errorf("migrate scratchpad: relocate %s: %w", kind, err)
	}

	for _, rel := range skipped {
		fmt.Printf("migrate: skipped %s %s (destination already exists at %s)\n", kind, rel, filepath.Join(dst, rel))
	}

	return pruneEmptyDirs(src)
}

// moveFileCrossDevice renames src to dst, falling back to copy+remove when
// src and dst live on different filesystems (~/.atomic vs. a repo checkout
// commonly do), which os.Rename cannot cross.
func moveFileCrossDevice(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// pruneEmptyDirs removes root and any subdirectory left empty by
// relocateTree, deepest first, so a directory that still holds a skipped
// file (or one of its ancestors does) is left in place.
func pruneEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			os.Remove(dir) // best-effort; a non-empty ancestor simply stays
		}
	}
	return nil
}

// datedBundlePattern matches the <YYYY-MM-DD>-<slug> shape /subagent-implementation
// and /quick-fix produce.
var datedBundlePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(.+)$`)

// redateScratchpadBundles converts every <YYYY-MM-DD>-<slug> directory under
// root's scratchpad root to <slug>, but only when both docs/design/<slug>.md
// and docs/spec/<slug>.md exist — that pair is the confirmation a stripped
// name is a real slug, checked with a stat and nothing else. A directory
// whose stripped name no document matches was never a candidate, and is
// left untouched with no output.
func redateScratchpadBundles(root string) error {
	scratchpadRoot := config.ScratchpadDir(root)
	entries, err := os.ReadDir(scratchpadRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("migrate scratchpad: read %s: %w", scratchpadRoot, err)
	}

	for _, c := range collisions(candidates(root, entries)) {
		if err := redateOne(scratchpadRoot, c.dirName, c.date, c.slug); err != nil {
			return err
		}
	}
	return nil
}

type redateCandidate struct {
	dirName string
	date    string
	slug    string
}

// candidates scans a scratchpad root's entries for the <YYYY-MM-DD>-<slug>
// shape and keeps only the ones both docs/design/<slug>.md and
// docs/spec/<slug>.md confirm — the check that excludes the spec-loop,
// diagnose, and challenge-swarm shapes without a name list. A name only one
// doc confirms is reported and dropped; a name neither confirms is silently
// never a candidate.
func candidates(root string, entries []os.DirEntry) map[string][]redateCandidate {
	bySlug := map[string][]redateCandidate{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m := datedBundlePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		date, slug := m[1], m[2]

		designExists := fileExists(filepath.Join(root, "docs", "design", slug+".md"))
		specExists := fileExists(filepath.Join(root, "docs", "spec", slug+".md"))

		switch {
		case designExists && specExists:
			bySlug[slug] = append(bySlug[slug], redateCandidate{dirName: e.Name(), date: date, slug: slug})
		case designExists || specExists:
			fmt.Printf("migrate: skipped %s: only one of docs/design/%s.md or docs/spec/%s.md exists\n", e.Name(), slug, slug)
		default:
			// Neither doc names this slug: never a candidate.
		}
	}
	return bySlug
}

// collisions resolves bySlug down to one candidate per slug, in sorted slug
// order. A slug two dated directories both strip to disqualifies both,
// reported rather than silently picking one.
func collisions(bySlug map[string][]redateCandidate) []redateCandidate {
	slugs := make([]string, 0, len(bySlug))
	for s := range bySlug {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	resolved := make([]redateCandidate, 0, len(slugs))
	for _, slug := range slugs {
		cands := bySlug[slug]
		if len(cands) > 1 {
			names := make([]string, len(cands))
			for i, c := range cands {
				names[i] = c.dirName
			}
			sort.Strings(names)
			fmt.Printf("migrate: skipped %s: multiple dated bundles strip to slug %q\n", strings.Join(names, ", "), slug)
			continue
		}
		resolved = append(resolved, cands[0])
	}
	return resolved
}

// redateOne renames one confirmed dated bundle to <slug> and seeds meta.toml
// entirely from disk facts: the date prefix as Created, the directory's own
// mtime as Updated, and the fixed purposes/status/description this migration
// always writes.
func redateOne(scratchpadRoot, dirName, date, slug string) error {
	if err := config.ValidateSegment("slug", slug); err != nil {
		fmt.Printf("migrate: skipped %s: %v\n", dirName, err)
		return nil
	}

	srcDir := filepath.Join(scratchpadRoot, dirName)
	dstDir := filepath.Join(scratchpadRoot, slug)

	if fileExists(dstDir) {
		fmt.Printf("migrate: skipped %s: destination %s already exists\n", dirName, slug)
		return nil
	}
	if fileExists(filepath.Join(srcDir, "meta.toml")) {
		fmt.Printf("migrate: skipped %s: source already has meta.toml\n", dirName)
		return nil
	}

	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("migrate scratchpad: stat %s: %w", dirName, err)
	}
	updated := info.ModTime().UTC().Format("2006-01-02")

	if err := os.Rename(srcDir, dstDir); err != nil {
		return fmt.Errorf("migrate scratchpad: rename %s: %w", dirName, err)
	}

	data, err := toml.Marshal(seedMeta(slug, date, updated))
	if err != nil {
		return fmt.Errorf("migrate scratchpad: encode meta.toml for %s: %w", slug, err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "meta.toml"), data, 0o644); err != nil {
		return fmt.Errorf("migrate scratchpad: write meta.toml for %s: %w", slug, err)
	}

	fmt.Printf("migrate: renamed %s -> %s\n", dirName, slug)
	return nil
}

// seedMeta builds meta.toml content entirely from disk facts: date is the
// dated directory's stripped prefix, updated is the directory's own mtime.
func seedMeta(slug, date, updated string) map[string]any {
	return map[string]any{
		"slug":        slug,
		"purposes":    []string{"plan"},
		"created":     date,
		"updated":     updated,
		"status":      "active",
		"description": "migrated",
	}
}
