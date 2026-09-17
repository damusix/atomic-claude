package wiki

import (
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

// WikiRegistry is the authoritative registry at ~/.atomic/wikis.md. One writer:
// registration and removal mutate this file first, and harness projections are
// rendered from the entries it holds.
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

// Save rewrites only the <wikis> block of the authority file, preserving every
// other byte, then reads the block back so the caller can verify the authority
// before projecting it.
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

// SeedFromProjection adopts claudeMDPath's existing <wikis> block as the initial
// authority, once, while ~/.atomic/wikis.md does not exist yet. The installed
// block is the pre-authority registry: seeding it before the first mutation keeps
// a realm that only ever lived there in the authority, and therefore in the
// derived projection and in every reader that prefers the authority. Once the
// authority file exists it is the single source — a changed projection is a
// conflict, never an implicit import.
//
// The block is read with the package's whole-line tag matching, so prose that
// merely mentions `<wikis>` — the shipped <atomic> block does — seeds nothing.
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
		normalized, err := normalizePath(p)
		if err != nil || seen[normalized] {
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

// ProjectWikis writes paths into claudeMDPath's <wikis> block, preserving every
// other byte. It is the derived projection: call it only after the authority
// bytes have been verified.
func ProjectWikis(claudeMDPath string, paths []string) error {
	existing, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki registry: read %s: %w", claudeMDPath, err)
	}
	var content string
	if os.IsNotExist(err) || len(existing) == 0 {
		content = buildWikisBlock(paths)
	} else {
		content = setWikisBlock(string(existing), paths)
	}
	return writeFileAtomic(claudeMDPath, []byte(content))
}

// ProjectionConflict reports whether claudeMDPath's <wikis> block differs from
// the authority's derived projection. A missing authority or a missing
// projection file is not a conflict; a changed derived copy is.
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
	existing, err := os.ReadFile(claudeMDPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("wiki registry: read %s: %w", claudeMDPath, err)
	}
	_, _, body := findBareLineBlock(string(existing), wikisMarkerOpen, wikisMarkerClose)
	return normalizeBlockBody(body) != normalizeBlockBody(blockBody(paths)), nil
}

// RegisteredIndexPaths returns the registry entries, preferring the
// authoritative ~/.atomic/wikis.md. The <wikis> block in claudeMDPath is read
// only as a fallback — before adoption, or for a non-conventional install root —
// so a changed projection can never override the authority.
func RegisteredIndexPaths(claudeMDPath string) ([]string, error) {
	if home, ok := conventionalHome(claudeMDPath); ok {
		authority := NewWikiRegistry(home)
		if _, err := os.Stat(authority.Path()); err == nil {
			return authority.Load()
		}
	}
	return ReadWikiIndexPaths(claudeMDPath)
}

// conventionalHome derives the user home from a Claude home's CLAUDE.md path,
// reporting false when the path does not have the conventional
// <home>/.claude/CLAUDE.md shape — a custom install root has no derivable
// authority.
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

// normalizeBlockBody drops blank lines so formatting differences alone never
// read as a conflict.
func normalizeBlockBody(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	return strings.Join(lines, "\n")
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

// RegisterWiki records indexPath in the authoritative ~/.atomic/wikis.md and
// then projects the verified registry into claudeMDPath's <wikis> block. While
// the authority file does not exist yet, the installed block is adopted first
// (SeedFromProjection), so realms registered before the authority survive the
// switch. A claudeMDPath without the conventional <home>/.claude/CLAUDE.md shape
// has no derivable authority, so only its projection is updated.
//
// Tags are matched only as whole lines, so a sentence or backtick span
// mentioning "<wikis>" cannot be mistaken for the block.
func RegisterWiki(claudeMDPath, indexPath string) error {
	normalized, err := normalizePath(indexPath)
	if err != nil {
		return fmt.Errorf("wiki registry: normalize path: %w", err)
	}

	home, ok := conventionalHome(claudeMDPath)
	if !ok {
		existing, err := os.ReadFile(claudeMDPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("wiki registry: read %s: %w", claudeMDPath, err)
		}
		if os.IsNotExist(err) || len(existing) == 0 {
			return writeFileAtomic(claudeMDPath, []byte(buildWikisBlock([]string{normalized})))
		}
		return writeFileAtomic(claudeMDPath, []byte(appendWikisEntry(string(existing), normalized)))
	}

	registry := NewWikiRegistry(home)
	if err := registry.SeedFromProjection(claudeMDPath); err != nil {
		return err
	}
	paths, _, err := registry.Add(normalized)
	if err != nil {
		return err
	}
	return ProjectWikis(claudeMDPath, paths)
}

// appendWikisEntry adds one normalized entry to an existing block, or inserts a
// fresh block. Used only for a non-conventional install root.
func appendWikisEntry(content, normalized string) string {
	blockStart, _, body := findBareLineBlock(content, wikisMarkerOpen, wikisMarkerClose)
	if blockStart == -1 {
		return insertWikisBlock(content, []string{normalized})
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		if existingNorm, err := normalizePath(strings.TrimPrefix(trimmed, "- ")); err == nil && existingNorm == normalized {
			return content
		}
	}
	openTagEnd := blockStart + len(wikisMarkerOpen)
	if openTagEnd < len(content) && content[openTagEnd] == '\n' {
		openTagEnd++
	}
	return content[:openTagEnd] + "- " + normalized + "\n" + content[openTagEnd:]
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

// writeFileAtomic writes data to path via a temp file + rename.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
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
