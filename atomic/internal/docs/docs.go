// Package docs writes a doc-surfaces cache under config.ProjectDir: every
// discovered .md file with its H1 and first three H2 headings, so the signals
// workflow can index the documentation without loading it.
package docs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

const cacheFileName = "doc-surfaces.md"

// docDirs is searched in order; root README.md is handled separately.
var docDirs = []string{
	"docs",
	"doc",
	"documentation",
	"wiki",
	"ADR",
	"adr",
	"decisions",
}

// Options configures a ScanWithOptions run. All fields are optional; an unset
// ExcludeGlobs is filled from the repo's .signalsignore.
type Options struct {
	Clock        func() time.Time
	ExcludeGlobs []string
}

func (o *Options) clock() time.Time {
	if o != nil && o.Clock != nil {
		return o.Clock()
	}
	return time.Now().UTC()
}

// ErrStale is returned by Stale when the cache is out of date.
var ErrStale = fmt.Errorf("docs stale: doc files are newer than doc-surfaces cache")

// Scan writes the doc-surfaces cache under config.ProjectDir(root).
func Scan(root string) error {
	return ScanWithOptions(root, nil)
}

// ScanWithOptions is Scan with injectable clock and exclude globs.
func ScanWithOptions(root string, opts *Options) error {
	if opts == nil {
		opts = &Options{}
	}
	if len(opts.ExcludeGlobs) == 0 {
		excl, err := readSignalsIgnore(root)
		if err != nil {
			return fmt.Errorf("docs scan: %w", err)
		}
		opts.ExcludeGlobs = excl
	}

	surfaces, err := collectSurfaces(root, opts)
	if err != nil {
		return fmt.Errorf("docs scan: %w", err)
	}

	return writeCacheFile(root, surfaces, opts.clock())
}

type surface struct {
	rel   string
	title string
	h2s   []string
}

func collectSurfaces(root string, opts *Options) ([]surface, error) {
	paths, err := docPaths(root, opts.ExcludeGlobs)
	if err != nil {
		return nil, err
	}

	var surfaces []surface
	for _, rel := range paths {
		s, err := parseSurface(root, rel)
		if err != nil {
			continue // an unreadable file must not abort the scan
		}
		surfaces = append(surfaces, s)
	}
	return surfaces, nil
}

// docPaths is the single source of truth for "which doc files exist", shared
// by collectSurfaces and Stale so scan and delete-detection cannot disagree.
func docPaths(root string, excludeGlobs []string) ([]string, error) {
	var paths []string

	rootReadme := filepath.Join(root, "README.md")
	if _, err := os.Stat(rootReadme); err == nil {
		paths = append(paths, "README.md")
	}

	for _, dir := range docDirs {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) != ".md" {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			paths = append(paths, rel)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}

	var kept []string
	for _, rel := range paths {
		if matchesGlobs(rel, excludeGlobs) {
			continue
		}
		kept = append(kept, rel)
	}
	return kept, nil
}

func parseSurface(root, rel string) (surface, error) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return surface{}, err
	}
	sections, err := mdparse.Sections(data)
	if err != nil {
		return surface{}, err
	}

	s := surface{rel: rel}
	h2Count := 0
	for _, sec := range sections {
		switch sec.Level {
		case 1:
			if s.title == "" {
				s.title = sec.Heading
			}
		case 2:
			if h2Count < 3 {
				s.h2s = append(s.h2s, sec.Heading)
				h2Count++
			}
		}
	}
	return s, nil
}

func writeCacheFile(root string, surfaces []surface, now time.Time) error {
	var sb strings.Builder
	sb.WriteString("# Doc surfaces\n\n")
	sb.WriteString("last-scanned: ")
	sb.WriteString(now.Format(time.RFC3339))
	sb.WriteString("\n\n")

	for _, s := range surfaces {
		sb.WriteString("- ")
		sb.WriteString(s.rel)
		if s.title != "" {
			sb.WriteString(" — ")
			sb.WriteString(s.title)
		}
		if len(s.h2s) > 0 {
			sb.WriteString(" [")
			sb.WriteString(strings.Join(s.h2s, ", "))
			sb.WriteString("]")
		}
		sb.WriteString("\n")
	}

	outPath := filepath.Join(config.ProjectDir(root), cacheFileName)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	return os.WriteFile(outPath, []byte(sb.String()), 0o644)
}

// Stale returns ErrStale if the cache is out of date, or an error if it does
// not exist. Two triggers: any doc newer than the cache, or set drift between
// disk and cache — a delete bumps no surviving file's mtime.
func Stale(root string) error {
	cachePath := filepath.Join(config.ProjectDir(root), cacheFileName)
	fi, err := os.Stat(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("docs stale: cache not found at %s — run scan first", cachePath)
		}
		return fmt.Errorf("docs stale: %w", err)
	}
	cacheMtime := fi.ModTime()

	newest, err := newestDocMtime(root)
	if err != nil {
		return fmt.Errorf("docs stale: %w", err)
	}

	if newest.After(cacheMtime) {
		return ErrStale
	}

	excl, err := readSignalsIgnore(root)
	if err != nil {
		return fmt.Errorf("docs stale: %w", err)
	}
	current, err := docPaths(root, excl)
	if err != nil {
		return fmt.Errorf("docs stale: %w", err)
	}
	cached, err := cachedDocPaths(cachePath)
	if err != nil {
		return fmt.Errorf("docs stale: %w", err)
	}
	if !sameStringSet(current, cached) {
		return ErrStale
	}
	return nil
}

// cachedDocPaths reverses writeCacheFile's "- <rel>[ — <title>][ [<h2s>]]" line
// format back to bare paths.
func cachedDocPaths(cachePath string) ([]string, error) {
	f, err := os.Open(cachePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var paths []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		rest := strings.TrimPrefix(line, "- ")
		cut := len(rest)
		if i := strings.Index(rest, " — "); i >= 0 && i < cut {
			cut = i
		}
		if i := strings.Index(rest, " ["); i >= 0 && i < cut {
			cut = i
		}
		rel := strings.TrimSpace(rest[:cut])
		if rel != "" {
			paths = append(paths, rel)
		}
	}
	return paths, scanner.Err()
}

func sameStringSet(a, b []string) bool {
	setA := make(map[string]bool, len(a))
	for _, s := range a {
		setA[s] = true
	}
	setB := make(map[string]bool, len(b))
	for _, s := range b {
		setB[s] = true
	}
	if len(setA) != len(setB) {
		return false
	}
	for s := range setA {
		if !setB[s] {
			return false
		}
	}
	return true
}

func newestDocMtime(root string) (time.Time, error) {
	var newest time.Time

	checkFile := func(path string) error {
		fi, err := os.Stat(path)
		if err != nil {
			return nil
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	}

	_ = checkFile(filepath.Join(root, "README.md"))

	for _, dir := range docDirs {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) != ".md" {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			if fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
			return nil
		})
		if err != nil {
			return newest, fmt.Errorf("walk %s: %w", dir, err)
		}
	}

	return newest, nil
}

// readSignalsIgnore returns .signalsignore's exclude globs. '+'-prefixed lines
// are generated-markers and are dropped: this scanner has no such concept.
// An absent file is not an error.
func readSignalsIgnore(root string) ([]string, error) {
	path := filepath.Join(root, ".signalsignore")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read .signalsignore: %w", err)
	}
	defer f.Close()

	var globs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "+") {
			continue
		}
		globs = append(globs, line)
	}
	return globs, scanner.Err()
}

// matchesGlobs tests each pattern against both the relative path and the base
// filename, so "excluded.md" matches "docs/excluded.md" too.
func matchesGlobs(rel string, globs []string) bool {
	base := filepath.Base(rel)
	for _, glob := range globs {
		if ok, _ := filepath.Match(glob, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(glob, base); ok {
			return true
		}
	}
	return false
}
