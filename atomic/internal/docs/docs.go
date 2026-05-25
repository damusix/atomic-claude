// Package docs scans a repository's documentation directories and writes a
// lightweight "doc surfaces" cache file at .claude/project/doc-surfaces.md.
//
// The cache lists each discovered .md file with its H1 title and up to the
// first three H2 section headings. The file is used by the signals workflow to
// give Claude an index of what documentation exists without loading every doc.
//
// Directory search order: docs/, doc/, documentation/, wiki/, ADR/, adr/,
// decisions/, and any README.md found anywhere in the repo root (non-recursive
// at root). .signalsignore exclude globs are respected.
package docs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

const cacheFile = ".claude/project/doc-surfaces.md"

// docDirs is the ordered list of directories searched for .md files.
var docDirs = []string{
	"docs",
	"doc",
	"documentation",
	"wiki",
	"ADR",
	"adr",
	"decisions",
}

// Options configures a ScanWithOptions run. All fields are optional.
type Options struct {
	// Clock returns the current time. Inject a fixed clock in tests to get
	// deterministic last-scanned timestamps.
	Clock func() time.Time
	// ExcludeGlobs holds glob patterns (plain, no prefix) from .signalsignore.
	// Files matching any glob are omitted. Populated automatically by
	// ScanWithOptions from the repo's .signalsignore when not set by the caller.
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

// Scan walks the repo at root and writes .claude/project/doc-surfaces.md.
func Scan(root string) error {
	return ScanWithOptions(root, nil)
}

// ScanWithOptions is like Scan but accepts Options for dependency injection.
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

// surface represents one discovered doc file.
type surface struct {
	// rel is the repo-relative path (e.g. "docs/guide.md")
	rel string
	// title is the H1 heading text (empty if none found)
	title string
	// h2s holds up to 3 H2 heading texts
	h2s []string
}

// collectSurfaces discovers all .md files in the configured directories and
// README.md files at repo root, parses headings, and returns one surface per
// file (excluding signalsignore matches).
func collectSurfaces(root string, opts *Options) ([]surface, error) {
	var paths []string

	// README.md at repo root.
	rootReadme := filepath.Join(root, "README.md")
	if _, err := os.Stat(rootReadme); err == nil {
		paths = append(paths, "README.md")
	}

	// Configured doc directories (recursive *.md).
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

	var surfaces []surface
	for _, rel := range paths {
		if matchesGlobs(rel, opts.ExcludeGlobs) {
			continue
		}
		s, err := parseSurface(root, rel)
		if err != nil {
			// Skip unreadable files; don't abort the whole scan.
			continue
		}
		surfaces = append(surfaces, s)
	}
	return surfaces, nil
}

// parseSurface reads one .md file and extracts H1 + up to 3 H2 headings.
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

// writeCacheFile writes the doc-surfaces.md cache at .claude/project/doc-surfaces.md.
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

	outPath := filepath.Join(root, cacheFile)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	return os.WriteFile(outPath, []byte(sb.String()), 0o644)
}

// Stale returns ErrStale if any .md file in the scanned directories is newer
// than the cache file, or an error if the cache does not exist.
func Stale(root string) error {
	cachePath := filepath.Join(root, cacheFile)
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
	return nil
}

// newestDocMtime returns the mtime of the newest .md file across all doc dirs
// and root README.md. Returns zero time if no doc files exist.
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

	// Root README.md
	_ = checkFile(filepath.Join(root, "README.md"))

	// Configured doc directories
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

// readSignalsIgnore reads .signalsignore from the repo root and returns the
// exclude globs (plain lines without '+' prefix). Comment lines and blank lines
// are ignored. '+'-prefixed lines (generated markers) are not returned — the
// docs scanner has no "generated" concept. Absent file is not an error.
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

// matchesGlobs reports whether rel matches any of the provided glob patterns.
// Each pattern is tested against the full repo-relative path and the base
// filename, so patterns like "excluded.md" match both "excluded.md" and
// "docs/excluded.md".
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
