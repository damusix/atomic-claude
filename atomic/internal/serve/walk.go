// The directory-skip predicate every walker in this package shares.
package serve

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

type documentScope struct {
	root       string
	logical    string
	rootInfo   os.FileInfo
	bound      bool
	private    documentState
	stateReady bool
	roots      map[string]realmshare.DocumentRoot
	bucketView wiki.BucketView
}

func loadDocumentScope(root string) documentScope {
	logical, _ := filepath.Abs(root)
	s := documentScope{root: resolveDir(root), logical: logical}
	s.rootInfo, _ = os.Lstat(s.root)
	view, roots, err := realmshare.DocumentView(root)
	s.bound = view.Bound || err != nil
	s.bucketView = view
	if len(roots) != 0 {
		s.roots = make(map[string]realmshare.DocumentRoot, len(roots))
		for _, mount := range roots {
			s.roots[mount.Local] = mount
		}
	}
	if s.bound {
		s.private = make(documentState)
		s.private.add(s.logical)
		logical, canonical := strings.ToLower(s.logical), strings.ToLower(s.root)
		for _, path := range config.AbsoluteStateSelections() {
			for _, state := range [2]string{strings.ToLower(path), strings.ToLower(resolveDir(path))} {
				_, underLogical := config.RepoRelative(logical, state)
				_, underCanonical := config.RepoRelative(canonical, state)
				if underLogical || underCanonical {
					s.private[state] = true
					continue
				}
				for _, mount := range roots {
					if _, ok := config.RepoRelative(strings.ToLower(mount.Target), state); ok {
						s.private[state] = true
						break
					}
				}
			}
		}
		if realm, ok := s.openRealm(); ok {
			managed := ""
			if home := config.ProjectHome(); home != "" {
				managed = strings.ToLower(filepath.Join(config.Dir(resolveDir(home)), "realms"))
				if _, inside := config.RepoRelative(managed, canonical); inside {
					realm.Close()
					return s
				}
				if _, inside := config.RepoRelative(canonical, managed); !inside {
					managed = ""
				}
			}
			s.stateReady = s.discoverState(realm, managed)
			realm.Close()
		}
	}
	return s
}

func (s documentScope) buckets() []wiki.BucketEntry {
	var entries []wiki.BucketEntry
	for _, group := range [][]wiki.BucketEntry{s.bucketView.Shared, s.bucketView.Private} {
		for _, bucket := range group {
			rel, err := filepath.Rel(s.logical, bucket.Path)
			if err != nil || !filepath.IsLocal(rel) {
				rel, err = filepath.Rel(s.root, bucket.Path)
			}
			if err != nil || !filepath.IsLocal(rel) {
				continue
			}
			if path, ok := s.documentPath(rel); ok {
				if path.info.IsDir() {
					bucket.Path = filepath.Join(s.root, rel)
					entries = append(entries, bucket)
				}
				path.parent.Close()
			}
		}
	}
	return entries
}

type documentState map[string]bool

func (d documentState) add(repo string) {
	for _, state := range config.QuietStateRoots(repo) {
		d[strings.ToLower(state)] = true
		d[strings.ToLower(resolveDir(state))] = true
	}
}

func (s documentScope) discoverState(dir *os.Root, managed string) bool {
	file, err := dir.Open(".")
	if err != nil {
		return false
	}
	entries, err := file.ReadDir(-1)
	file.Close()
	if err != nil {
		return false
	}
	checkGit := dir.Name() != s.root
	// Document ignore rules cannot hide a repo that selects state elsewhere.
	for _, entry := range entries {
		name := entry.Name()
		if strings.EqualFold(name, ".git") {
			if checkGit {
				checkGit = false
				git, err := dir.Lstat(".git")
				if err != nil && !os.IsNotExist(err) {
					return false
				}
				if err == nil && (git.IsDir() || git.Mode().IsRegular()) {
					s.private.add(dir.Name())
				}
			}
			continue
		}
		if !entry.IsDir() || (managed != "" && strings.EqualFold(name, "realms") &&
			strings.EqualFold(filepath.Join(dir.Name(), name), managed)) {
			continue
		}
		info, err := dir.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false
		}
		child, ok := (documentPath{dir, name, info}).openDir()
		if !ok {
			return false
		}
		ok = s.discoverState(child, managed)
		child.Close()
		if !ok {
			return false
		}
	}
	return true
}

func (s documentScope) privateStateRoots() []string {
	if s.private == nil {
		return nil
	}
	roots := make([]string, 0, len(s.private))
	for path := range s.private {
		roots = append(roots, path)
	}
	slices.Sort(roots)
	return roots
}

func (s documentScope) protectedPath(path string) bool {
	if len(s.private) == 0 {
		return false
	}
	for dir := strings.ToLower(filepath.Clean(path)); ; dir = filepath.Dir(dir) {
		if s.private[dir] {
			return true
		}
		if dir == filepath.Dir(dir) {
			return false
		}
	}
}

func (s documentScope) protected(rel string) bool {
	if !s.bound {
		return false
	}
	if !s.stateReady || s.protectedPath(filepath.Join(s.root, filepath.FromSlash(rel))) {
		return true
	}
	rel = strings.ToLower(rel)
	for part := range strings.SplitSeq(rel, "/") {
		if strings.HasPrefix(part, ".env.") {
			return true
		}
		switch part {
		case ".git", ".atomic", ".atomic-index", ".scratchpad", "realm.toml",
			"realm-share.toml", "realm-share-owners.toml", ".mcp.json", ".env", ".netrc",
			".git-credentials", "credentials.json", "auth.json",
			"agents.local.md", "claude.local.md", "gemini.local.md":
			return true
		}
	}
	return false
}

func (s documentScope) mount(rel string) (realmshare.DocumentRoot, string, bool) {
	if len(s.roots) == 0 {
		return realmshare.DocumentRoot{}, "", false
	}
	for name := rel; name != "."; name = filepath.ToSlash(filepath.Dir(name)) {
		if mount, ok := s.roots[name]; ok {
			return mount, strings.TrimPrefix(strings.TrimPrefix(rel, name), "/"), true
		}
	}
	return realmshare.DocumentRoot{}, "", false
}

type documentPath struct {
	parent *os.Root
	name   string
	info   os.FileInfo
}

func (p documentPath) physical() string {
	return filepath.Join(p.parent.Name(), p.name)
}

func (p documentPath) matches(info os.FileInfo) bool {
	return info != nil && info.Mode().Type() == p.info.Mode().Type() && os.SameFile(info, p.info)
}

func (p documentPath) current() bool {
	info, err := p.parent.Lstat(p.name)
	return err == nil && p.matches(info)
}

func (p documentPath) open() (*os.File, os.FileInfo, bool) {
	file, err := p.parent.OpenFile(p.name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, false
	}
	info, err := file.Stat()
	if err != nil || !p.matches(info) || !p.current() {
		file.Close()
		return nil, nil, false
	}
	return file, info, true
}

func (p documentPath) openDir() (*os.Root, bool) {
	if !p.info.IsDir() {
		return nil, false
	}
	dir, err := p.parent.OpenRoot(p.name)
	if err != nil {
		return nil, false
	}
	info, err := dir.Stat(".")
	if err != nil || !p.matches(info) || !p.current() {
		dir.Close()
		return nil, false
	}
	return dir, true
}

func openDocumentRoot(path string, expected os.FileInfo) (*os.Root, bool) {
	if expected == nil || !expected.IsDir() {
		return nil, false
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, false
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(info, expected) {
		root.Close()
		return nil, false
	}
	return root, true
}

func (s documentScope) openRealm() (*os.Root, bool) {
	info := s.rootInfo
	if info == nil {
		info, _ = os.Lstat(s.root)
	}
	return openDocumentRoot(s.root, info)
}

// pathBelow owns root and transfers ownership of the leaf's parent on success.
func (s documentScope) pathBelow(root *os.Root, rel string, protect bool) (documentPath, bool) {
	if protect && s.protectedPath(filepath.Join(root.Name(), filepath.FromSlash(rel))) {
		root.Close()
		return documentPath{}, false
	}
	for {
		name, rest, more := strings.Cut(rel, "/")
		info, err := root.Lstat(name)
		if err != nil {
			break
		}
		path := documentPath{root, name, info}
		if !more {
			return path, true
		}
		next, ok := path.openDir()
		if !ok {
			break
		}
		root.Close()
		root, rel = next, rest
	}
	root.Close()
	return documentPath{}, false
}

func (s documentScope) mountPath(m realmshare.DocumentRoot) (documentPath, bool) {
	if s.protected(m.Local) {
		return documentPath{}, false
	}
	realm, ok := s.openRealm()
	if !ok {
		return documentPath{}, false
	}
	link, ok := s.pathBelow(realm, m.Local, s.bound)
	if !ok {
		return documentPath{}, false
	}
	defer link.parent.Close()
	target, err := link.parent.Readlink(link.name)
	if err != nil || target != m.Target || !link.matches(m.Witness) || !link.current() {
		return documentPath{}, false
	}
	source, err := filepath.Rel(m.Clone, m.Target)
	if err != nil || !filepath.IsLocal(source) || source == "." {
		return documentPath{}, false
	}
	clone, ok := openDocumentRoot(m.Clone, m.CloneInfo)
	if !ok {
		return documentPath{}, false
	}
	path, ok := s.pathBelow(clone, filepath.ToSlash(source), false)
	if !ok {
		return documentPath{}, false
	}
	if !path.matches(m.SourceInfo) || s.protectedPath(path.physical()) {
		path.parent.Close()
		return documentPath{}, false
	}
	return path, true
}

func (s documentScope) mountAvailable(m realmshare.DocumentRoot) bool {
	path, ok := s.mountPath(m)
	if ok {
		path.parent.Close()
	}
	return ok
}

func (s documentScope) documentPath(rel string) (documentPath, bool) {
	path, ok := s.locate(rel)
	if !ok {
		return documentPath{}, false
	}
	if path.info.IsDir() || path.info.Mode().IsRegular() {
		return path, true
	}
	path.parent.Close()
	return documentPath{}, false
}

func (s documentScope) locate(rel string) (documentPath, bool) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(rel) {
		return documentPath{}, false
	}
	rel = filepath.ToSlash(rel)
	if s.protected(rel) {
		return documentPath{}, false
	}
	if mount, suffix, ok := s.mount(rel); ok {
		path, ok := s.mountPath(mount)
		if !ok || suffix == "" {
			return path, ok
		}
		dir, ok := path.openDir()
		path.parent.Close()
		if !ok {
			return documentPath{}, false
		}
		return s.pathBelow(dir, suffix, s.bound)
	}
	realm, ok := s.openRealm()
	if !ok {
		return documentPath{}, false
	}
	if path, ok := s.pathBelow(realm, rel, s.bound); ok {
		if path.info.Mode()&fs.ModeSymlink == 0 {
			return path, true
		}
		path.parent.Close()
	}
	if s.protected(rel) {
		return documentPath{}, false
	}
	physical, ok := resolveContained(s.root, filepath.FromSlash(rel))
	if !ok {
		return documentPath{}, false
	}
	canonical, err := filepath.Rel(s.root, physical)
	if err != nil || !filepath.IsLocal(canonical) || filepath.ToSlash(canonical) == rel || s.protected(filepath.ToSlash(canonical)) {
		return documentPath{}, false
	}
	realm, ok = s.openRealm()
	if !ok {
		return documentPath{}, false
	}
	return s.pathBelow(realm, filepath.ToSlash(canonical), s.bound)
}

// resolve provides a physical path for metadata only, not for reopening content.
func (s documentScope) resolve(rel string) (string, bool) {
	path, ok := s.documentPath(rel)
	if !ok {
		return "", false
	}
	defer path.parent.Close()
	return path.physical(), true
}

func (s documentScope) openDocument(rel string) (*os.File, os.FileInfo, bool) {
	path, ok := s.documentPath(rel)
	if !ok {
		return nil, nil, false
	}
	defer path.parent.Close()
	return path.open()
}

func (s documentScope) open(rel string) (*os.File, bool) {
	file, _, ok := s.openDocument(rel)
	return file, ok
}

func (s documentScope) readFile(rel string) ([]byte, error) {
	file, info, ok := s.openDocument(rel)
	if !ok {
		return nil, fs.ErrNotExist
	}
	defer file.Close()
	if !info.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	return io.ReadAll(io.LimitReader(file, info.Size()))
}

type documentEntry struct {
	fs.DirEntry
	name string
}

func (d documentEntry) Name() string { return d.name }

func (s documentScope) walk(startRel string, visit func(rel, physical string, entry fs.DirEntry) error) error {
	startRel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(startRel)))
	start, ok := s.documentPath(startRel)
	if !ok {
		return nil
	}
	defer start.parent.Close()
	startMount, _, shared := s.mount(startRel)
	physical := start.physical()
	if !shared && physical != filepath.Join(s.root, filepath.FromSlash(startRel)) {
		return nil
	}
	ignoreRoot := physical
	if !start.info.IsDir() {
		ignoreRoot = filepath.Dir(physical)
	}
	var sharedIgnores *gitIgnores
	if shared {
		ignoreRoot = startMount.Clone
	}
	ignores := loadGitIgnores(ignoreRoot, !s.bound)
	if shared {
		sharedIgnores = ignores
	}
	var walk func(string, documentPath, *gitIgnores, bool, bool) error
	walk = func(rel string, path documentPath, ignores *gitIgnores, shared, top bool) error {
		physical := path.physical()
		if s.protected(rel) || (shared && s.protectedPath(physical)) {
			return nil
		}
		info := path.info
		if info.Mode()&fs.ModeSymlink != 0 {
			mount, found := s.roots[rel]
			if shared || !found || shouldSkipDir(info.Name()) {
				return nil
			}
			mounted, ok := s.mountPath(mount)
			if !ok {
				return nil
			}
			defer mounted.parent.Close()
			if sharedIgnores == nil {
				sharedIgnores = loadGitIgnores(mount.Clone, false)
			}
			return walk(rel, mounted, sharedIgnores, true, true)
		}
		var dir *os.Root
		if info.IsDir() {
			if !top && ignores.skipWalkDir(physical) {
				return nil
			}
			var ok bool
			dir, ok = path.openDir()
			if !ok {
				return nil
			}
			defer dir.Close()
		} else if !info.Mode().IsRegular() || hiddenFile(filepath.Base(rel)) || ignores.skipFile(physical) {
			return nil
		}
		entry := fs.FileInfoToDirEntry(info)
		if base := filepath.Base(rel); rel != "." && entry.Name() != base {
			entry = documentEntry{DirEntry: entry, name: base}
		}
		if err := visit(rel, physical, entry); err != nil || dir == nil {
			if err == fs.SkipDir && dir != nil {
				return nil
			}
			return err
		}
		file, err := dir.Open(".")
		if err != nil {
			return nil
		}
		names, _ := file.Readdirnames(-1)
		file.Close()
		slices.Sort(names)
		for _, name := range names {
			child, err := dir.Lstat(name)
			if err != nil {
				continue
			}
			childRel := name
			if rel != "." {
				childRel = rel + "/" + name
			}
			if err := walk(childRel, documentPath{dir, name, child}, ignores, shared, false); err != nil {
				if err == fs.SkipDir {
					return nil
				}
				return err
			}
		}
		return nil
	}
	err := walk(startRel, start, ignores, shared, true)
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

// shouldSkipDir skips dotdirs and build dumps except .claude, whose markdown
// wiki links cite (docs/spec/atomic-serve.md). hiddenDir exempts state roots.
func shouldSkipDir(name string) bool {
	if name == ".claude" {
		return false
	}
	if len(name) > 0 && name[0] == '.' {
		return true
	}
	switch name {
	case "node_modules", "vendor", "tmp":
		return true
	}
	return false
}

// hiddenFile marks the backups, caches, and machinery no walker enumerates.
// Only files reach this; .claude is a directory, handled by shouldSkipDir.
func hiddenFile(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// gitIgnores excludes git-ignored paths from a walk spanning several repos.
//
// Ignored is not the test on its own: a realm gitignores its member repos, and
// those are exactly what it exists to serve. The test is whether a directory is
// its own repository. Enumeration only — an ignored file still renders by path.
type gitIgnores struct {
	base           string          // walk root exactly as the walker phrases its paths
	realBase       string          // same directory with symlinks and relativity resolved
	loaded         map[string]bool // repo roots already queried
	paths          map[string]bool // resolved paths git reports as ignored
	stateRoots     map[string]bool // resolved repository-state roots of the repos seen
	stateAncestors map[string]bool // dot-led directories a nested state root sits under
}

// newGitIgnores loads the ignore set of the repository enclosing root. Outside
// a repository the set is empty and the walk is unchanged.
func newGitIgnores(root string) *gitIgnores {
	return loadGitIgnores(root, true)
}

func loadGitIgnores(root string, exposeState bool) *gitIgnores {
	g := emptyGitIgnores(root, exposeState)
	if top, err := gitToplevel(root); err == nil {
		g.load(resolveDir(top))
	} else {
		g.exemptStateRoot(g.realBase)
	}
	return g
}

// newStateRoots is newGitIgnores without the ignore listing, for a caller that
// reads only hiddenDir.
func newStateRoots(root string) *gitIgnores {
	g := emptyGitIgnores(root, true)
	repo := g.realBase
	if !isGitRepoRoot(repo) {
		if top, err := gitToplevel(root); err == nil {
			repo = resolveDir(top)
		}
	}
	g.exemptStateRoot(repo)
	return g
}

func emptyGitIgnores(root string, exposeState bool) *gitIgnores {
	g := &gitIgnores{
		base:     root,
		realBase: resolveDir(root),
		loaded:   map[string]bool{},
		paths:    map[string]bool{},
	}
	if exposeState {
		g.stateRoots = make(map[string]bool)
		g.stateAncestors = make(map[string]bool)
	}
	return g
}

// exemptStateRoot records repoRoot's state root so the dot rule does not hide
// it. A root outside repoRoot, or one the ladder refuses, is never walked.
func (g *gitIgnores) exemptStateRoot(repoRoot string) {
	if g.stateRoots == nil {
		return
	}
	dir, ok := config.QuietStateDir(repoRoot)
	if !ok {
		return
	}
	rel, ok := config.RepoRelative(repoRoot, dir)
	if !ok || rel == "." {
		return
	}
	g.stateRoots[filepath.Join(repoRoot, rel)] = true
	parts := strings.Split(rel, string(filepath.Separator))
	fenced := false
	for i := 0; i < len(parts)-1; i++ {
		fenced = fenced || shouldSkipDir(parts[i])
		if fenced {
			g.stateAncestors[filepath.Join(append([]string{repoRoot}, parts[:i+1]...)...)] = true
		}
	}
}

// skipWalkDir is the test a walker applies below its root.
func (g *gitIgnores) skipWalkDir(dir string) bool {
	return g.hiddenDir(dir) || g.skipDir(dir)
}

// hiddenDir is shouldSkipDir with state roots exempt. Inside a skipped ancestor
// of a nested state root, only the path toward that root shows.
func (g *gitIgnores) hiddenDir(dir string) bool {
	real := g.resolve(dir)
	if g.stateRoots[real] || g.stateAncestors[real] {
		return false
	}
	return g.stateAncestors[filepath.Dir(real)] || shouldSkipDir(filepath.Base(dir))
}

// resolve rewrites a walker path into the form load stores. WalkDir echoes the
// root it was handed while git always answers with the real absolute path, so a
// relative or symlinked root makes every lookup silently miss.
func (g *gitIgnores) resolve(path string) string {
	if g.base == "" || g.base == g.realBase {
		return path
	}
	rel, err := filepath.Rel(g.base, path)
	if err != nil {
		return path
	}
	return filepath.Join(g.realBase, rel)
}

// resolveDir gives dir's absolute, symlink-free form.
func resolveDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return real
}

// load records every path git ignores under repoRoot. --directory collapses a
// wholly-ignored directory to one entry, keeping the set small.
func (g *gitIgnores) load(repoRoot string) {
	if repoRoot == "" || g.loaded[repoRoot] {
		return
	}
	g.loaded[repoRoot] = true
	g.exemptStateRoot(repoRoot)

	// -z because git C-quotes non-ASCII paths, and a quoted string never matches.
	cmd := exec.Command("git", "ls-files", "-z", "--directory", "--others", "--ignored", "--exclude-standard")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return // not a repo, or git unavailable: nothing to exclude
	}
	for _, entry := range bytes.Split(out, []byte{0}) {
		rel := string(entry)
		if rel == "" {
			continue
		}
		g.paths[filepath.Join(repoRoot, rel)] = true
	}
}

// skipDir reports whether the walk should stop at dir.
func (g *gitIgnores) skipDir(dir string) bool {
	// Checked before ignored-ness: a realm that does not gitignore its members
	// would otherwise walk them under the parent's rules, which say nothing
	// about the member's own worktrees or build output.
	if isGitRepoRoot(dir) {
		real := g.resolve(dir)
		if g.worktreeOfLoaded(real) {
			return true
		}
		g.load(real)
		return false
	}
	return g.paths[g.resolve(dir)]
}

func (g *gitIgnores) skipFile(path string) bool {
	real := g.resolve(path)
	return g.paths[real] || g.stateAncestors[filepath.Dir(real)]
}

// gitToplevel resolves the repository root containing dir.
func gitToplevel(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isGitRepoRoot reports whether dir is a checkout: .git is a directory, or the
// file a linked worktree carries.
func isGitRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// worktreeOfLoaded: dir is a second copy when nested in a loaded checkout of
// the same repository; containment keeps sibling members walk-order independent.
func (g *gitIgnores) worktreeOfLoaded(dir string) bool {
	common, ok := gitCommonDir(dir)
	if !ok {
		return false
	}
	for repo := range g.loaded {
		if !strings.HasPrefix(dir, repo+string(filepath.Separator)) {
			continue
		}
		if c, ok := gitCommonDir(repo); ok && c == common {
			return true
		}
	}
	return false
}

// gitCommonDir resolves as git does: only a linked worktree's gitdir holds a
// commondir file, so a submodule's gitdir is its own common dir.
func gitCommonDir(dir string) (string, bool) {
	dotgit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotgit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return resolveDir(dotgit), true
	}
	data, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return "", false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
		return resolveDir(common), true
	}
	return resolveDir(gitdir), true
}
