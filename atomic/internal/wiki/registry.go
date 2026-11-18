package wiki

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

const wikisMarkerOpen = "<wikis>"

const wikisMarkerClose = "</wikis>"

const atomicClose = "</atomic>"

// wikisFileHeader is written once, when the authority file is created. Later
// saves preserve every byte outside the <wikis> block.
const wikisFileHeader = "# Wiki registry\n\nAuthoritative realm registry for Atomic. The <wikis> block installed in a\nharness's global CLAUDE.md is a derived projection of this file.\n\n"

// WikiRegistry is the authoritative registry at ~/.atomic/wikis.md. Mutations
// land here first; harness projections receive the entries it holds.
type WikiRegistry struct {
	path string
}

// NewWikiRegistry returns the registry under home's ~/.atomic.
func NewWikiRegistry(home string) *WikiRegistry {
	return &WikiRegistry{path: config.WikisPath(home)}
}

// Path returns the authority file path.
func (r *WikiRegistry) Path() string { return r.path }

// Load returns the registered index paths. A missing file is an empty registry,
// never an error; the entries are normalized absolute paths.
func (r *WikiRegistry) Load() ([]string, error) {
	return ReadWikiIndexPaths(r.path)
}

// Save rewrites only the <wikis> block of the authority file and returns the
// block read back, so the caller verifies the authority before projecting it.
func (r *WikiRegistry) Save(paths []string) ([]string, error) {
	existing, err := os.ReadFile(r.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("wiki registry: read %s: %w", r.path, err)
	}
	content := string(existing)
	if os.IsNotExist(err) || len(existing) == 0 {
		content = wikisFileHeader + buildWikisBlock(paths)
	} else {
		content = setWikisBlock(content, paths)
	}
	if err := writeFileAtomic(r.path, []byte(content)); err != nil {
		return nil, err
	}
	return ReadWikiIndexPaths(r.path)
}

// SeedFromProjection adopts claudeMDPath's <wikis> block as the authority while
// ~/.atomic/wikis.md does not exist yet, so realms registered before it survive.
func (r *WikiRegistry) SeedFromProjection(claudeMDPath string) error {
	if _, err := os.Stat(r.path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("wiki registry: stat %s: %w", r.path, err)
	}
	legacy, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		return err
	}
	var seeded []string
	seen := map[string]bool{}
	for _, p := range legacy {
		normalized, ok := entryPath(p)
		if !ok || seen[normalized] {
			continue
		}
		seen[normalized] = true
		seeded = append(seeded, normalized)
	}
	if len(seeded) == 0 {
		return nil
	}
	_, err = r.Save(seeded)
	return err
}

// Add records indexPath idempotently, returning the authority's entries and
// whether this call changed them.
func (r *WikiRegistry) Add(indexPath string) ([]string, bool, error) {
	normalized, err := normalizePath(indexPath)
	if err != nil {
		return nil, false, fmt.Errorf("wiki registry: normalize path: %w", err)
	}
	paths, err := r.Load()
	if err != nil {
		return nil, false, err
	}
	for _, p := range paths {
		if p == normalized {
			return paths, false, nil
		}
	}
	updated, err := r.Save(append(paths, normalized))
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

// Remove drops indexPath from the authority, returning whether it was present.
func (r *WikiRegistry) Remove(indexPath string) (bool, error) {
	normalized, err := normalizePath(indexPath)
	if err != nil {
		return false, fmt.Errorf("wiki registry: normalize path: %w", err)
	}
	paths, err := r.Load()
	if err != nil {
		return false, err
	}
	kept := paths[:0]
	removed := false
	for _, p := range paths {
		if p == normalized {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	if !removed {
		return false, nil
	}
	if _, err := r.Save(kept); err != nil {
		return false, err
	}
	return true, nil
}

// ProjectWikis adds the authority paths claudeMDPath's <wikis> block lacks; a
// user's line in the block is a conflict to report, never data to drop.
func ProjectWikis(claudeMDPath string, paths []string) error {
	existing, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki registry: read %s: %w", claudeMDPath, err)
	}
	var content string
	if os.IsNotExist(err) || len(existing) == 0 {
		content = buildWikisBlock(paths)
	} else {
		content = addToWikisBlock(string(existing), paths)
		if content == string(existing) {
			return nil
		}
	}
	return writeFileAtomic(claudeMDPath, []byte(content))
}

// addToWikisBlock inserts the paths the block lacks before </wikis>, so a realm
// resolver that takes the first match keeps its earlier winner.
func addToWikisBlock(content string, paths []string) string {
	blockStart, blockEnd, body := findBareLineBlock(content, wikisMarkerOpen, wikisMarkerClose)
	if blockStart == -1 {
		return insertWikisBlock(content, paths)
	}
	present := normalizedSet(blockEntries(body))
	var missing []string
	for _, p := range paths {
		if !present[p] {
			present[p] = true
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return content
	}
	openEnd := blockStart + strings.IndexByte(content[blockStart:], '\n')
	terminator := "\n"
	if strings.HasSuffix(content[:openEnd], "\r") {
		terminator = "\r\n"
	}
	closeStart := strings.LastIndexByte(content[:blockEnd-1], '\n') + 1
	var lines strings.Builder
	for _, p := range missing {
		lines.WriteString("- " + p + terminator)
	}
	return content[:closeStart] + lines.String() + content[closeStart:]
}

// entryPath returns a block entry's cleaned path. A relative entry has no
// meaning independent of the process cwd, so it reports false.
func entryPath(entry string) (string, bool) {
	if !filepath.IsAbs(entry) {
		return "", false
	}
	return filepath.Clean(entry), true
}

// normalizedSet maps each absolute entry's cleaned path.
func normalizedSet(entries []string) map[string]bool {
	set := map[string]bool{}
	for _, e := range entries {
		if n, ok := entryPath(e); ok {
			set[n] = true
		}
	}
	return set
}

// ProjectionOnlyEntries returns the entries of claudeMDPath's <wikis> block, as
// written, that are relative or that authority does not hold.
func ProjectionOnlyEntries(claudeMDPath string, authority []string) ([]string, error) {
	entries, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		return nil, err
	}
	registered := normalizedSet(authority)
	var extra []string
	for _, e := range entries {
		if n, ok := entryPath(e); !ok || !registered[n] {
			extra = append(extra, e)
		}
	}
	return extra, nil
}

// ProjectionConflict reports whether claudeMDPath's <wikis> entries differ from
// the authority's; a missing authority or projection file is no conflict.
func ProjectionConflict(claudeMDPath string) (bool, error) {
	home, ok := conventionalHome(claudeMDPath)
	if !ok {
		return false, nil
	}
	authority := NewWikiRegistry(home)
	paths, err := authority.Load()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(authority.Path()); err != nil {
		return false, nil
	}
	if _, err := os.Stat(claudeMDPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("wiki registry: stat %s: %w", claudeMDPath, err)
	}
	extra, err := ProjectionOnlyEntries(claudeMDPath, paths)
	if err != nil || len(extra) > 0 {
		return len(extra) > 0, err
	}
	entries, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		return false, err
	}
	projected := normalizedSet(entries)
	for _, p := range paths {
		if !projected[p] {
			return true, nil
		}
	}
	return false, nil
}

// RegisteredIndexPaths returns the registry entries from ~/.atomic/wikis.md,
// falling back to claudeMDPath's <wikis> block only when no authority exists.
func RegisteredIndexPaths(claudeMDPath string) ([]string, error) {
	if home, ok := conventionalHome(claudeMDPath); ok {
		authority := NewWikiRegistry(home)
		if _, err := os.Stat(authority.Path()); err == nil {
			return authority.Load()
		}
	}
	return ReadWikiIndexPaths(claudeMDPath)
}

// A custom install root has no derivable home, so it has no authority file.
func conventionalHome(claudeMDPath string) (string, bool) {
	claudeHome := filepath.Dir(claudeMDPath)
	if filepath.Base(claudeHome) != ".claude" {
		return "", false
	}
	home := filepath.Dir(claudeHome)
	if home == "" || home == claudeHome || home == string(filepath.Separator) {
		return "", false
	}
	return home, true
}

// blockBody renders entries as the block body, one "- path" line each.
func blockBody(paths []string) string {
	var sb strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&sb, "- %s\n", p)
	}
	return sb.String()
}

// setWikisBlock replaces the whole <wikis> block with paths, preserving every
// other byte. An absent block is inserted after </atomic> or at EOF.
func setWikisBlock(content string, paths []string) string {
	blockStart, blockEnd, _ := findBareLineBlock(content, wikisMarkerOpen, wikisMarkerClose)
	if blockStart == -1 {
		return insertWikisBlock(content, paths)
	}
	return content[:blockStart] + buildWikisBlock(paths) + content[blockEnd:]
}

// RegisterAndProject records indexPath in ~/.atomic/wikis.md, seeded once from
// ~/.claude/CLAUDE.md, then projects the registry into every steering file.
func RegisterAndProject(home, indexPath string, steering []string) error {
	registry := NewWikiRegistry(home)
	if err := registry.SeedFromProjection(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		return fmt.Errorf("register wiki: %w", err)
	}
	paths, _, err := registry.Add(indexPath)
	if err != nil {
		return fmt.Errorf("register wiki: %w", err)
	}
	var errs []error
	for _, path := range steering {
		errs = append(errs, ProjectWikis(path, paths))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("project <wikis>: %w", err)
	}
	return nil
}

// normalizePath is Abs then Clean, deliberately without symlink resolution.
func normalizePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// findBareLineBlock locates a block whose tags each occupy a whole line,
// returning the open line's start offset, the close line's end offset, and the
// body between them, or (-1, -1, "") when absent.
func findBareLineBlock(content, openTag, closeTag string) (blockStart, blockEnd int, body string) {
	lines := strings.Split(content, "\n")
	openLine := -1
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		if i < len(lines)-1 {
			lineLen++ // the \n Split consumed
		}
		if openLine == -1 {
			if strings.TrimSpace(line) == openTag {
				openLine = i
				blockStart = pos
			}
		} else {
			if strings.TrimSpace(line) == closeTag {
				blockEnd = pos + lineLen
				// Rebuilt from the line slice rather than sliced by offset, to
				// keep the index arithmetic out of the body entirely.
				bodyLines := lines[openLine+1 : i]
				body = strings.Join(bodyLines, "\n")
				if len(bodyLines) > 0 {
					body += "\n"
				}
				return blockStart, blockEnd, body
			}
		}
		pos += lineLen
	}
	return -1, -1, ""
}

// findBareAtomicClose returns the offset just past the whole-line </atomic>
// tag, trailing newline included, or -1.
func findBareAtomicClose(content string) int {
	lines := strings.Split(content, "\n")
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		if i < len(lines)-1 {
			lineLen++
		}
		if strings.TrimSpace(line) == atomicClose {
			return pos + lineLen
		}
		pos += lineLen
	}
	return -1
}

// insertWikisBlock places a fresh block just after </atomic>, so the registry
// sits outside the managed block, or at EOF when there is none.
func insertWikisBlock(content string, paths []string) string {
	block := "\n" + buildWikisBlock(paths)

	insertAt := findBareAtomicClose(content)
	if insertAt == -1 {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + block
	}

	before := content[:insertAt]
	after := content[insertAt:]
	return before + block + after
}

func buildWikisBlock(paths []string) string {
	var sb strings.Builder
	sb.WriteString(wikisMarkerOpen)
	sb.WriteString("\n")
	for _, p := range paths {
		fmt.Fprintf(&sb, "- %s\n", p)
	}
	sb.WriteString(wikisMarkerClose)
	sb.WriteString("\n")
	return sb.String()
}

// writeFileAtomic writes data to path via a temp file + rename, keeping an
// existing file's permission bits and giving a new file 0644.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}

// PrintHandoff writes the deterministic summary `atomic wiki scan` ends with:
// counts, one line per member, then next steps for anything still pending.
func PrintHandoff(w io.Writer, members []Member) {
	total := len(members)
	indexed := 0
	pending := 0
	for _, m := range members {
		switch m.Status {
		case "indexed":
			indexed++
		case "pending":
			pending++
		}
	}

	fmt.Fprintf(w, "%d repos · %d indexed · %d pending\n", total, indexed, pending)
	fmt.Fprintln(w)
	for _, m := range members {
		if m.Status == "indexed" && m.SignalsPath != "" {
			fmt.Fprintf(w, "%s %s → %s\n", m.Status, m.Path, m.SignalsPath)
		} else {
			fmt.Fprintf(w, "%s %s\n", m.Status, m.Path)
		}
	}

	if pending > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "NEXT STEPS")
		for _, m := range members {
			if m.Status == "pending" {
				fmt.Fprintf(w, "  run /refresh-wiki for: %s\n", m.Path)
			}
		}
	}
}
