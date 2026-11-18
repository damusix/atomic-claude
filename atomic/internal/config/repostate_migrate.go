package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// ErrStateMigrationConflict is returned when the legacy root and the
// destination both hold state, so moving either side would merge or clobber.
var ErrStateMigrationConflict = errors.New("config: legacy and destination state roots both hold state")

// ErrHomeStateRoot refuses a repository root that is the home directory,
// whose `.claude` is Claude's own configuration, not repository state.
var ErrHomeStateRoot = errors.New("config: the home directory is not a repository state root")

const legacyIndexEntry = ".atomic-index"

// migratedEntries are the legacy-root entries a migration moves. Everything
// else in `.claude` (worktrees, settings, harness-native files) stays.
var migratedEntries = []string{
	"atomic.toml",
	"workflow-preferences.md",
	"project",
	filepath.Join("rules", "wiki"),
	".scratchpad",
}

// legacyReferenceFiles are where users hand-wire legacy state paths: steering
// imports and hook scripts. Directories list every file directly inside them.
var legacyReferenceFiles = []string{
	"CLAUDE.md",
	"CLAUDE.local.md",
	"AGENTS.md",
	filepath.Join(stateDirLegacy, "CLAUDE.md"),
	filepath.Join(stateDirLegacy, "settings.json"),
	filepath.Join(stateDirLegacy, "settings.local.json"),
}

var legacyReferenceDirs = []string{
	".githooks",
	filepath.Join(stateDirLegacy, "hooks"),
}

// IndexInUse reports whether an MCP daemon is live at a code-index directory; a
// not-in-use answer returns the release of its spawn-lock hold. Unwired, every index is in use.
var IndexInUse func(indexDir string) (inUse bool, release func(), err error)

// AfterRepoStateMigration runs once a migration has moved state, so harness
// card projections can follow the new root before the verb continues.
var AfterRepoStateMigration func(repoRoot, stateRoot string) error

// StagedRename is one tracked file whose rename the migration staged.
type StagedRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// RepoStateMigration reports what one migration did. Paths are absolute
// except StagedRename, which uses the repository's own relative paths.
type RepoStateMigration struct {
	Legacy       string         `json:"legacy"`
	Destination  string         `json:"destination"`
	Moved        []string       `json:"moved,omitempty"`
	Staged       []StagedRename `json:"staged,omitempty"`
	IndexRemoved string         `json:"index_removed,omitempty"`
	IndexInUse   string         `json:"index_in_use,omitempty"`
	References   []string       `json:"references,omitempty"`
}

// RepoStateMigrationPending reports whether the legacy root holds Atomic
// state other than a code index while the destination holds none.
func RepoStateMigrationPending(repoRoot string) (bool, error) {
	legacy, dest, err := migrationRoots(repoRoot)
	if err != nil || legacy == dest {
		return false, err
	}
	return len(legacyState(legacy)) > 0 && !dirHasStateSignature(dest), nil
}

// legacyState is the legacy root's signatures other than the rebuildable
// code index.
func legacyState(legacy string) []string {
	var out []string
	for _, e := range signatureEntriesIn(legacy) {
		if e != legacyIndexEntry {
			out = append(out, e)
		}
	}
	return out
}

// migrationRoots returns the legacy root and destination (record, state.dir,
// .atomic; never ATOMIC_STATE_DIR). A stateless home returns legacy twice.
func migrationRoots(repoRoot string) (string, string, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("config: resolve %s: %w", repoRoot, err)
	}
	legacy := filepath.Join(abs, stateDirLegacy)
	if home := projectHome(); home != "" && sameDir(abs, home) {
		if len(legacyState(legacy)) == 0 {
			return legacy, legacy, nil
		}
		return "", "", fmt.Errorf("%w: %s", ErrHomeStateRoot, abs)
	}
	sel, ok, err := ReadStateSelection(abs)
	if err != nil {
		return "", "", err
	}
	if ok {
		dest, err := resolveStateDirValue(abs, sel.StateDir)
		return legacy, dest, err
	}
	seg, _ := configuredStateDir()
	return legacy, filepath.Join(abs, seg), nil
}

func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// MigrateRepositoryState moves Atomic's entries out of `.claude` into the
// destination; see docs/spec/omp-plugin-compatibility.md for the contract.
func MigrateRepositoryState(repoRoot string) (RepoStateMigration, error) {
	legacy, dest, err := migrationRoots(repoRoot)
	if err != nil {
		return RepoStateMigration{}, err
	}
	abs := filepath.Dir(legacy)
	report := RepoStateMigration{Legacy: legacy, Destination: dest}
	state := legacyState(legacy)
	if legacy == dest {
		return report, nil
	}
	if dirHasStateSignature(dest) {
		if len(state) > 0 {
			leftover := make([]string, len(state))
			for i, e := range state {
				leftover[i] = filepath.Join(legacy, e)
			}
			return report, fmt.Errorf("%w: %s holds %s; merge or remove them",
				ErrStateMigrationConflict, dest, strings.Join(leftover, ", "))
		}
		return report, removeLegacyIndex(legacy, &report)
	}
	if len(state) == 0 {
		return report, nil
	}

	moves := pendingMoves(abs, legacy, dest)
	g := openGitIndex(abs, legacy, dest)
	for i := range moves {
		if _, err := os.Lstat(moves[i].to); err == nil {
			return report, fmt.Errorf("config: %s already exists; merge or remove it before migrating", moves[i].to)
		}
		if moves[i].tracked, err = g.tracked(moves[i].from); err != nil {
			return report, err
		}
	}

	var done []pendingMove
	var created []string
	ignore := filepath.Join(dest, ".gitignore")
	priorIgnore, ignoreErr := os.ReadFile(ignore)
	undo := func(cause error) error {
		if ignoreErr == nil {
			_ = os.WriteFile(ignore, priorIgnore, 0o644)
		} else {
			_ = os.Remove(ignore)
		}
		var failed []string
		for i := len(done) - 1; i >= 0; i-- {
			if err := g.unmove(done[i]); err != nil {
				failed = append(failed, err.Error())
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
		if len(failed) > 0 {
			return fmt.Errorf("%w (rollback incomplete: %s)", cause, strings.Join(failed, "; "))
		}
		return cause
	}

	for _, m := range moves {
		if err := mkdirAllTracked(filepath.Dir(m.to), &created); err != nil {
			return report, undo(err)
		}
		if err := g.move(m); err != nil {
			return report, undo(err)
		}
		done = append(done, m)
	}

	if _, inside := RepoRelative(abs, dest); inside {
		for _, rule := range []string{"/.scratchpad/", "/.atomic-index/", "/worktrees/", DocSurfacesIgnoreRule} {
			if err := AppendIgnoreRule(ignore, rule); err != nil {
				return report, undo(err)
			}
		}
		if err := g.add(ignore); err != nil {
			return report, undo(err)
		}
	}

	for _, m := range done {
		report.Moved = append(report.Moved, m.from)
		for _, e := range m.tracked {
			report.Staged = append(report.Staged, StagedRename{From: e.path, To: g.renamed(m, e.path)})
		}
	}
	report.References = legacyReferences(abs)
	invalidateStateCache()
	if err := removeLegacyIndex(legacy, &report); err != nil {
		return report, err
	}
	if AfterRepoStateMigration != nil {
		if err := AfterRepoStateMigration(abs, dest); err != nil {
			return report, fmt.Errorf("config: state moved, but projecting the wiki cards failed: %w; fix it and run `atomic repo init`", err)
		}
	}
	return report, nil
}

// mkdirAllTracked is os.MkdirAll that records each directory it creates, so
// a rollback can remove them.
func mkdirAllTracked(dir string, created *[]string) error {
	if _, err := os.Lstat(dir); err == nil {
		return nil
	}
	if err := mkdirAllTracked(filepath.Dir(dir), created); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	*created = append(*created, dir)
	return nil
}

type pendingMove struct {
	from, to string
	tracked  []indexEntry
}

// pendingMoves lists the legacy entries present on disk, plus the realm
// federation files under `.atomic` when the destination is somewhere else.
func pendingMoves(repoRoot, legacy, dest string) []pendingMove {
	var moves []pendingMove
	for _, e := range migratedEntries {
		from := filepath.Join(legacy, e)
		if _, err := os.Lstat(from); err == nil {
			moves = append(moves, pendingMove{from: from, to: filepath.Join(dest, e)})
		}
	}
	federation := filepath.Join(repoRoot, stateDirDefault)
	if federation == dest {
		return moves
	}
	var names []string
	for _, pattern := range []string{"code.toml", "*.db", "*.db-wal", "*.db-shm"} {
		matches, _ := filepath.Glob(filepath.Join(federation, pattern))
		names = append(names, matches...)
	}
	sort.Strings(names)
	for _, from := range names {
		moves = append(moves, pendingMove{from: from, to: filepath.Join(dest, filepath.Base(from))})
	}
	return moves
}

type indexEntry struct {
	meta string // "<mode> <object> <stage>"
	path string // relative to the top of the work tree
}

// gitIndex stages renames; a nil prefix means plain renames, as for a root
// that is neither a repository's top nor repo-scoped (a nested realm).
type gitIndex struct {
	root   string
	prefix *string
}

func openGitIndex(repoRoot, legacy, dest string) gitIndex {
	g := gitIndex{root: repoRoot}
	if _, inside := RepoRelative(repoRoot, dest); !inside {
		return g
	}
	top, err := g.git(nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return g
	}
	if !sameDir(strings.TrimSpace(string(top)), repoRoot) {
		cfg, _, err := LoadRepoConfig(filepath.Join(legacy, "atomic.toml"))
		if err != nil || cfg.Scope != "repo" {
			return g
		}
	}
	out, err := g.git(nil, "rev-parse", "--show-prefix")
	if err != nil {
		return g
	}
	prefix := strings.TrimSpace(string(out))
	g.prefix = &prefix
	return g
}

func (g gitIndex) git(stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.root
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("config: git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// rel is path relative to the root, the form pathspecs take when git runs
// there.
func (g gitIndex) rel(path string) string {
	rel, _ := filepath.Rel(g.root, path)
	return filepath.ToSlash(rel)
}

// fullName is path relative to the top of the work tree, the form index
// entries carry.
func (g gitIndex) fullName(path string) string {
	return *g.prefix + g.rel(path)
}

func (g gitIndex) renamed(m pendingMove, path string) string {
	return g.fullName(m.to) + strings.TrimPrefix(path, g.fullName(m.from))
}

// tracked lists the index entries under path, refusing a gitlink or an
// unmerged entry, which `git mv` refuses or mishandles.
func (g gitIndex) tracked(path string) ([]indexEntry, error) {
	if g.prefix == nil {
		return nil, nil
	}
	out, err := g.git(nil, "ls-files", "-s", "-z", "--full-name", "--", g.rel(path))
	if err != nil {
		return nil, err
	}
	var entries []indexEntry
	for _, raw := range bytes.Split(out, []byte{0}) {
		meta, p, ok := bytes.Cut(raw, []byte{'\t'})
		if !ok {
			continue
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 {
			continue
		}
		if fields[0] == "160000" || fields[2] != "0" {
			return nil, fmt.Errorf("config: %s is a submodule or unmerged; resolve it before migrating", p)
		}
		entries = append(entries, indexEntry{meta: string(meta), path: string(p)})
	}
	return entries, nil
}

// move stages the rename `git mv` would, by index rewrite, so it also works
// after the cross-device copy and leaves unstaged edits unstaged.
func (g gitIndex) move(m pendingMove) error {
	if err := moveOnDisk(m.from, m.to); err != nil {
		return err
	}
	if err := g.rewrite(m.tracked, func(p string) string { return g.renamed(m, p) }); err != nil {
		_ = moveOnDisk(m.to, m.from)
		return err
	}
	return nil
}

// unmove reverses one completed move, index first.
func (g gitIndex) unmove(m pendingMove) error {
	moved := make([]indexEntry, len(m.tracked))
	for i, e := range m.tracked {
		moved[i] = indexEntry{meta: e.meta, path: g.renamed(m, e.path)}
	}
	original := map[string]string{}
	for i, e := range moved {
		original[e.path] = m.tracked[i].path
	}
	if err := g.rewrite(moved, func(p string) string { return original[p] }); err != nil {
		return err
	}
	return moveOnDisk(m.to, m.from)
}

// rewrite replaces each entry's path with to(path), keeping its object.
func (g gitIndex) rewrite(entries []indexEntry, to func(string) string) error {
	if len(entries) == 0 {
		return nil
	}
	var info bytes.Buffer
	for _, e := range entries {
		object := strings.Fields(e.meta)[1]
		fmt.Fprintf(&info, "0 %s\t%s\x00", strings.Repeat("0", len(object)), e.path)
		fmt.Fprintf(&info, "%s\t%s\x00", e.meta, to(e.path))
	}
	_, err := g.git(info.Bytes(), "update-index", "-z", "--index-info")
	return err
}

func (g gitIndex) add(path string) error {
	if g.prefix == nil {
		return nil
	}
	_, err := g.git(nil, "add", "--", g.rel(path))
	return err
}

func moveOnDisk(from, to string) error {
	err := renameState(from, to)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("config: move %s to %s: %w", from, to, err)
	}
	if err := copyEntry(from, to); err != nil {
		_ = os.RemoveAll(to)
		return fmt.Errorf("config: copy %s to %s: %w", from, to, err)
	}
	return os.RemoveAll(from)
}

// copyEntry copies a file, symlink, or tree, keeping symlinks as links.
func copyEntry(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(target, to)
	case info.IsDir():
		return copyDirRecursive(from, to)
	default:
		return copyFile(from, to, info.Mode().Perm())
	}
}

// removeLegacyIndex deletes the legacy code index, or reports it while an MCP
// daemon is live there; `atomic code index` builds a fresh one.
func removeLegacyIndex(legacy string, report *RepoStateMigration) error {
	dir := filepath.Join(legacy, legacyIndexEntry)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	if IndexInUse == nil {
		report.IndexInUse = dir
		return nil
	}
	inUse, release, err := IndexInUse(dir)
	if release != nil {
		defer release()
	}
	if err != nil {
		return fmt.Errorf("config: check legacy index %s: %w", dir, err)
	}
	if inUse {
		report.IndexInUse = dir
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("config: remove legacy index %s: %w", dir, err)
	}
	report.IndexRemoved = dir
	return nil
}

// legacyReferences finds hand-wired mentions of moved legacy paths, as
// `path:line`. They are reported for the user to update, never rewritten.
func legacyReferences(repoRoot string) []string {
	files := append([]string(nil), legacyReferenceFiles...)
	for _, dir := range legacyReferenceDirs {
		entries, _ := os.ReadDir(filepath.Join(repoRoot, dir))
		for _, e := range entries {
			if e.Type().IsRegular() {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	var needles []string
	for _, e := range migratedEntries {
		needles = append(needles, stateDirLegacy+"/"+filepath.ToSlash(e))
	}
	var found []string
	for _, rel := range files {
		f, err := os.Open(filepath.Join(repoRoot, rel))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for n := 1; scanner.Scan(); n++ {
			for _, needle := range needles {
				if strings.Contains(scanner.Text(), needle) {
					found = append(found, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), n))
					break
				}
			}
		}
		if err := scanner.Err(); err != nil {
			found = append(found, fmt.Sprintf("%s: not fully scanned: %v", filepath.ToSlash(rel), err))
		}
		f.Close()
	}
	return found
}

// invalidateStateCache drops cached resolutions so path helpers see a root a
// migration just populated.
func invalidateStateCache() {
	stateCacheMu.Lock()
	stateDirCached = map[string]cachedState{}
	stateCacheMu.Unlock()
}
