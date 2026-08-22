// The Plans aggregator: one row per slug, spanning every git worktree of a
// repo. Three collapses build a row — git supplies the checkouts, the
// filename stem supplies the slug, and content bytes supply a committed
// doc's version. Bundles never collapse: each worktree's scratchpad bundle
// is its own entry, since nothing merges an uncommitted directory.
//
// Cross-worktree reads never touch safeResolve's allowed-root set (see
// render.go) — a worktree can sit anywhere on disk, so this package issues
// its own opaque, server-issued worktree id and resolves through that
// instead. See docs/design/serve-plans-page.md "What aggregation actually
// does" for the full model.
package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
)

// planCheckout is one git worktree's view of a single document version or
// bundle: its opaque id, branch, path relative to the served root (absolute
// and flagged when it lies outside), the file's own mtime in that checkout,
// and the checkout's creation time when one is available. No field is
// synthesized when its source is absent.
type planCheckout struct {
	ID          string     `json:"id"`
	Branch      string     `json:"branch"`
	Path        string     `json:"path"`
	OutsideRoot bool       `json:"outsideRoot"`
	IsMain      bool       `json:"isMain"`
	FileMtime   time.Time  `json:"fileMtime"`
	Created     *time.Time `json:"created,omitempty"`
}

// planDocVersion is one distinct content SHA holding a set of checkouts. It
// is labelled by the merged (main) checkout when one is in the set, else by
// the checkout with the newest file mtime — but matchable by every checkout
// name in the set.
type planDocVersion struct {
	SHA       string         `json:"sha"`
	Label     string         `json:"label"`
	IsMain    bool           `json:"isMain"`
	Mtime     time.Time      `json:"mtime"`
	Checkouts []planCheckout `json:"checkouts"`
}

// planDoc is one committed document (docs/design/<slug>.md or
// docs/spec/<slug>.md) and every version of it found across all checkouts.
type planDoc struct {
	Path     string           `json:"path"`
	Versions []planDocVersion `json:"versions"`
}

// bundleFile is one file inside a scratchpad bundle other than meta.toml,
// classified by how the page should render it.
type bundleFile struct {
	RelPath string    `json:"relpath"`
	Kind    string    `json:"kind"` // "markdown" | "html" | "file"
	Mtime   time.Time `json:"mtime"`
}

// planBundle is one worktree's scratchpad bundle for a slug. Bundles never
// dedup across checkouts — a slug worked on in two worktrees at once is two
// bundles in the row, each attributed to the checkout that holds it.
type planBundle struct {
	WorktreeID string       `json:"worktreeId"`
	Branch     string       `json:"branch"`
	Purposes   []string     `json:"purposes"`
	Status     string       `json:"status"`
	Files      []bundleFile `json:"files"`
}

// planRow is one slug, aggregated across every checkout: its committed docs
// (each with their own version set) and every worktree's bundle for it.
// Title and Description come from extractMeta on the doc that carries
// "## Goal" (docs/spec/<slug>.md) — never from a bundle's meta.toml.
type planRow struct {
	Slug        string       `json:"slug"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Docs        []planDoc    `json:"docs"`
	Bundles     []planBundle `json:"bundles"`
	DotCount    int          `json:"dotCount"`
	DotMerged   bool         `json:"dotMerged"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// checkoutInfo is one entry from `git worktree list --porcelain`, enriched
// with the merged-branch discrimination the whole aggregation depends on.
// isMain here means "this checkout's branch is the repository's default
// branch" (see resolveDefaultBranch) — a claim about branch content, never
// about whether path's .git is a directory (that distinction decides only
// checkoutCreated, applied separately).
type checkoutInfo struct {
	id     string
	path   string
	branch string
	isMain bool
}

// plansAggregator builds the per-slug row set for one repo root, caching by
// a stat-only fingerprint so a request against an unchanged tree costs one
// git subprocess plus a stat walk, never a content re-read.
type plansAggregator struct {
	root        string
	quietWindow time.Duration

	mu         sync.Mutex
	fp         string
	cachedRows []planRow
	resolver   map[string]string
	warnings   []string

	// onBuild, when set, is called with the freshly built resolver after
	// every real rebuild (never on a cache hit, since the map is unchanged
	// there). plansRegistry uses it to keep its id->root index current
	// without querying every aggregator on every lookup.
	onBuild func(resolver map[string]string)
}

// newPlansAggregator returns an aggregator at production defaults.
func newPlansAggregator(root string) *plansAggregator {
	return newPlansAggregatorWithQuietWindow(root, defaultQuietWindow)
}

// newPlansAggregatorWithQuietWindow lets a test disable the quiet window, so
// a fixture file written moments ago is not excluded from the same build.
func newPlansAggregatorWithQuietWindow(root string, quietWindow time.Duration) *plansAggregator {
	return &plansAggregator{root: root, quietWindow: quietWindow}
}

// rows returns the current row set, the worktree-id resolver map consumed by
// /api/plans/page (CP11), and any scratchpad.List warnings — rebuilding when
// the worktree set, committed docs, or any worktree's scratchpad root has
// drifted since the last build.
func (a *plansAggregator) rows() ([]planRow, map[string]string, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	checkouts := a.worktrees()
	fp := a.fingerprint(checkouts)
	if a.cachedRows != nil && fp == a.fp {
		return a.cachedRows, a.resolver, a.warnings
	}

	rows, resolver, warnings := a.build(checkouts)
	a.fp, a.cachedRows, a.resolver, a.warnings = fp, rows, resolver, warnings
	if a.onBuild != nil {
		a.onBuild(resolver)
	}
	return rows, resolver, warnings
}

// runGitWorktreeList runs `git worktree list --porcelain` in dir. A package
// variable rather than an inline exec.Command call so a test can count
// invocations without parsing process-tree output.
var runGitWorktreeList = func(dir string) ([]byte, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = dir
	return cmd.Output()
}

// runGitStatus reports docs/design and docs/spec's untracked and modified
// state in dir via `git status --porcelain -z`. A package variable, like
// runGitWorktreeList, so a test can count invocations or stub a failure.
var runGitStatus = func(dir string) ([]byte, error) {
	cmd := exec.Command("git", "status", "--porcelain", "-z", "--untracked-files=all", "--", "docs/design", "docs/spec")
	cmd.Dir = dir
	return cmd.Output()
}

// dirtyDocs returns the docs/design and docs/spec relpaths that are
// untracked or modified in checkout dir — anything `git status` reports at
// all, since a clean tracked file never appears in that output. ok is false
// only when git itself failed; the caller must then treat every relpath in
// that checkout as unmerged rather than risk labelling stale content "main".
func dirtyDocs(dir string) (dirty map[string]bool, ok bool) {
	out, err := runGitStatus(dir)
	if err != nil {
		return nil, false
	}
	dirty = map[string]bool{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 3 {
			continue
		}
		status := entry[:2]
		dirty[filepath.ToSlash(entry[3:])] = true
		// A rename/copy entry's original path is the following NUL-delimited
		// field, not a second docs relpath — skip it.
		if status[0] == 'R' || status[0] == 'C' {
			i++
		}
	}
	return dirty, true
}

// worktrees enumerates a.root's checkouts via `git worktree list
// --porcelain` — the only source that reports the branch and the path for
// every checkout, including ones outside the served root. Re-run on every
// call (this method, never cached) so a worktree added after the last build
// is noticed on the next fingerprint check. A prunable entry is dropped; a
// bare entry (no working tree) is dropped, since it has no docs or bundle to
// read.
func (a *plansAggregator) worktrees() []checkoutInfo {
	out, err := runGitWorktreeList(a.root)
	if err != nil {
		return nil
	}
	checkouts, bareDir := parseWorktreePorcelain(string(out))
	defaultBranch := resolveDefaultBranch(checkouts, bareDir)
	for i := range checkouts {
		checkouts[i].isMain = checkouts[i].branch == defaultBranch
	}
	return checkouts
}

// parseWorktreePorcelain returns every non-bare, non-prunable checkout plus
// the bare hub's own path when the repository is bare — the bare entry
// carries no working tree to read docs or a bundle from, so it never
// becomes a checkoutInfo, but its path is still where refs/config live when
// resolveDefaultBranch needs the repository's shared git directory.
func parseWorktreePorcelain(out string) (checkouts []checkoutInfo, bareDir string) {
	records := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n\n")
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		var path, branch, headSHA string
		detached, prunable, bare := false, false, false
		for _, line := range strings.Split(rec, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "HEAD "):
				headSHA = strings.TrimPrefix(line, "HEAD ")
			case strings.HasPrefix(line, "branch "):
				branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			case line == "detached":
				detached = true
			case line == "prunable" || strings.HasPrefix(line, "prunable "):
				prunable = true
			case line == "bare":
				bare = true
			}
		}
		if path == "" || prunable {
			continue
		}
		if bare {
			if bareDir == "" {
				bareDir = path
			}
			continue
		}
		if detached {
			branch = shortSHA(headSHA)
		}
		checkouts = append(checkouts, checkoutInfo{
			id:     checkoutID(path),
			path:   path,
			branch: branch,
		})
	}
	return checkouts, bareDir
}

// checkoutID derives a stable, opaque id from the checkout's resolved
// filesystem path — never from its position in the enumeration — so
// removing one checkout can only make its own id vanish, never reassign it
// to a neighbour.
func checkoutID(path string) string {
	sum := sha256.Sum256([]byte(resolveDir(path)))
	return hex.EncodeToString(sum[:])[:12]
}

// resolveDefaultBranch determines the repository's default branch — the one
// whose content the picker calls "merged" — without spawning git:
// refs/remotes/origin/HEAD's symbolic-ref line, else init.defaultBranch from
// the repository's own config, else "main" when some checkout holds it,
// else "master". This is a claim about branch content, never about
// worktree structure — the primary checkout may sit on a feature branch
// while a linked worktree holds the default branch, and a bare-repository
// hub has no primary checkout at all.
func resolveDefaultBranch(checkouts []checkoutInfo, bareDir string) string {
	if gitDir := commonGitDir(checkouts, bareDir); gitDir != "" {
		if raw, err := os.ReadFile(filepath.Join(gitDir, "refs", "remotes", "origin", "HEAD")); err == nil {
			if branch, ok := parseSymbolicRefBranch(string(raw)); ok {
				return branch
			}
		}
		if branch, ok := defaultBranchFromConfig(filepath.Join(gitDir, "config")); ok {
			return branch
		}
	}
	for _, c := range checkouts {
		if c.branch == "main" {
			return "main"
		}
	}
	return "master"
}

// commonGitDir locates the repository's shared git directory — where
// refs/remotes/origin/HEAD and config live — with no git subprocess. It is
// the main checkout's real `.git` directory when one is enumerated, else
// the bare hub's own path, else resolved by following a linked worktree's
// `.git` file up to the enclosing `.git` directory every checkout shares.
func commonGitDir(checkouts []checkoutInfo, bareDir string) string {
	for _, c := range checkouts {
		if info, err := os.Stat(filepath.Join(c.path, ".git")); err == nil && info.IsDir() {
			return filepath.Join(c.path, ".git")
		}
	}
	if bareDir != "" {
		return bareDir
	}
	for _, c := range checkouts {
		if dir, ok := resolveWorktreeCommonGitDir(c.path); ok {
			return dir
		}
	}
	return ""
}

// resolveWorktreeCommonGitDir follows a linked worktree's `.git` file (a
// `gitdir: <path>` line) up to the enclosing `.git` directory it shares
// with every other checkout of the clone — a worktree's own gitdir is a
// private per-checkout subdirectory, not where refs/remotes or config live.
func resolveWorktreeCommonGitDir(path string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return "", false
	}
	target := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "gitdir:"); ok {
			target = strings.TrimSpace(rest)
			break
		}
	}
	if target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(path, target)
	}
	target = filepath.Clean(target)
	for dir := target; ; {
		if filepath.Base(dir) == ".git" {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// checkoutGitIndexPath locates <checkout>/.git/index without spawning git.
// A main checkout keeps `.git` as a directory holding the index directly; a
// linked worktree keeps `.git` as a file naming its own private gitdir
// (unlike resolveWorktreeCommonGitDir, this does NOT climb to the shared
// common dir — a worktree's index lives under its own gitdir, not the one
// every checkout shares).
func checkoutGitIndexPath(path string) (string, bool) {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "index"), true
	}
	raw, err := os.ReadFile(gitPath)
	if err != nil {
		return "", false
	}
	target := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "gitdir:"); ok {
			target = strings.TrimSpace(rest)
			break
		}
	}
	if target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(path, target)
	}
	return filepath.Join(filepath.Clean(target), "index"), true
}

// parseSymbolicRefBranch extracts the branch name from a symbolic-ref file's
// content — the shape `refs/remotes/origin/HEAD` carries.
func parseSymbolicRefBranch(content string) (string, bool) {
	line := strings.TrimSpace(content)
	if rest, ok := strings.CutPrefix(line, "ref: refs/remotes/origin/"); ok && rest != "" {
		return rest, true
	}
	return "", false
}

// defaultBranchFromConfig reads init.defaultBranch from a git config file's
// [init] section, with no dependency on git itself.
func defaultBranchFromConfig(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	inInit := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			inInit = strings.EqualFold(strings.TrimSpace(line), "[init]")
			continue
		}
		if !inInit {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "defaultBranch"); ok {
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "="))
			if rest != "" {
				return rest, true
			}
		}
	}
	return "", false
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// checkoutCreated returns the mtime of path's .git FILE — written once at
// `git worktree add`, never rewritten. A main checkout's .git is a
// directory, which git rewrites on ordinary ref updates, so it reports no
// creation time rather than that unrelated timestamp.
func checkoutCreated(path string) *time.Time {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || info.IsDir() {
		return nil
	}
	t := info.ModTime()
	return &t
}

// checkoutDisplayPath renders path relative to root when it lies within
// root, or returns it absolute and flagged when it lies outside — reusing
// walk.go's symlink-resolved comparison so a root that resolves differently
// (macOS /var<->/private/var) does not spuriously flag an in-root worktree.
func checkoutDisplayPath(root, path string) (display string, outside bool) {
	rroot := resolveDir(root)
	rpath := resolveDir(path)
	rel, err := filepath.Rel(rroot, rpath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path, true
	}
	return filepath.ToSlash(rel), false
}

// docKey identifies one committed document within one slug: which half
// (docs/design or docs/spec) and its filename.
type docKey struct {
	slug    string
	relpath string
}

// build re-enumerates checkouts' docs and bundles and assembles the row set.
// Grouping key for a committed doc is (slug, relpath) -> content SHA -> the
// set of checkouts carrying those bytes; a bundle is per-checkout and never
// grouped.
func (a *plansAggregator) build(checkouts []checkoutInfo) ([]planRow, map[string]string, []string) {
	resolver := a.resolverFor(checkouts)

	var warnings []string
	shaCheckouts := map[docKey]map[string][]planCheckout{}
	shaContent := map[docKey]map[string][]byte{}
	var docOrder []docKey
	seenDocKey := map[docKey]bool{}

	for _, c := range checkouts {
		dispPath, outside := checkoutDisplayPath(a.root, c.path)
		created := checkoutCreated(c.path)

		// Only the default-branch checkout can ever label a version "main",
		// so only it needs a git status call — a non-default checkout's
		// planCheckout.IsMain is already false via c.isMain.
		var dirty map[string]bool
		dirtyOK := true
		if c.isMain {
			dirty, dirtyOK = dirtyDocs(c.path)
			if !dirtyOK {
				warnings = append(warnings, fmt.Sprintf("plans: git status failed in %s; treating its docs as unmerged", dispPath))
			}
		}

		for _, kind := range [2]string{"design", "spec"} {
			dir := filepath.Join(c.path, "docs", kind)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, de := range entries {
				if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
					continue
				}
				// A symlinked entry is never followed — this walk spans
				// every checkout including review-only worktrees, and a
				// symlink there would hash and title-extract an arbitrary
				// file from outside the repository into a row.
				if de.Type()&fs.ModeSymlink != 0 {
					continue
				}
				info, err := de.Info()
				if err != nil || time.Since(info.ModTime()) < a.quietWindow {
					continue
				}
				filePath := filepath.Join(dir, de.Name())
				data, err := os.ReadFile(filePath) //nolint:gosec // repo-controlled doc path
				if err != nil {
					continue
				}
				slug := strings.TrimSuffix(de.Name(), ".md")
				if err := config.ValidateSegment("doc filename stem", slug); err != nil {
					warnings = append(warnings, fmt.Sprintf("plans: skipping %s: %v", filePath, err))
					continue
				}
				relpath := "docs/" + kind + "/" + de.Name()
				sum := sha256.Sum256(data)
				sha := hex.EncodeToString(sum[:])

				key := docKey{slug, relpath}
				if !seenDocKey[key] {
					seenDocKey[key] = true
					docOrder = append(docOrder, key)
					shaCheckouts[key] = map[string][]planCheckout{}
					shaContent[key] = map[string][]byte{}
				}
				shaCheckouts[key][sha] = append(shaCheckouts[key][sha], planCheckout{
					ID: c.id, Branch: c.branch, Path: dispPath, OutsideRoot: outside,
					IsMain: c.isMain && dirtyOK && !dirty[relpath], FileMtime: info.ModTime(), Created: created,
				})
				if _, ok := shaContent[key][sha]; !ok {
					shaContent[key][sha] = data
				}
			}
		}
	}

	docsBySlug := map[string][]planDoc{}
	specContentBySlug := map[string][]byte{}
	designContentBySlug := map[string][]byte{}
	for _, key := range docOrder {
		versions := versionsFor(shaCheckouts[key])
		docsBySlug[key.slug] = append(docsBySlug[key.slug], planDoc{Path: key.relpath, Versions: versions})

		rep := representativeVersion(versions)
		if rep != nil {
			content := shaContent[key][rep.SHA]
			if strings.HasPrefix(key.relpath, "docs/spec/") {
				specContentBySlug[key.slug] = content
			} else {
				designContentBySlug[key.slug] = content
			}
		}
	}

	bundlesBySlug := map[string][]planBundle{}
	var bundleSlugOrder []string
	seenBundleSlug := map[string]bool{}
	for _, c := range checkouts {
		entries, warns, err := scratchpad.List(config.ScratchpadDir(c.path))
		warnings = append(warnings, warns...)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !seenBundleSlug[e.Slug] {
				seenBundleSlug[e.Slug] = true
				bundleSlugOrder = append(bundleSlugOrder, e.Slug)
			}
			bundlesBySlug[e.Slug] = append(bundlesBySlug[e.Slug], planBundle{
				WorktreeID: c.id,
				Branch:     c.branch,
				Purposes:   e.Meta.Purposes,
				Status:     e.Meta.Status,
				Files:      bundleFilesFor(c.path, e.Path),
			})
		}
	}

	slugSet := map[string]bool{}
	var slugs []string
	for _, key := range docOrder {
		if !slugSet[key.slug] {
			slugSet[key.slug] = true
			slugs = append(slugs, key.slug)
		}
	}
	for _, slug := range bundleSlugOrder {
		if !slugSet[slug] {
			slugSet[slug] = true
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)

	rows := make([]planRow, 0, len(slugs))
	for _, slug := range slugs {
		content := specContentBySlug[slug]
		if content == nil {
			content = designContentBySlug[slug]
		}
		title, description := extractMeta(content)

		docs := append([]planDoc(nil), docsBySlug[slug]...)
		sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })

		bundles := append([]planBundle(nil), bundlesBySlug[slug]...)
		sort.Slice(bundles, func(i, j int) bool { return bundles[i].WorktreeID < bundles[j].WorktreeID })

		dotCount, dotMerged := rowDots(docsBySlug[slug])

		rows = append(rows, planRow{
			Slug: slug, Title: title, Description: description,
			Docs: docs, Bundles: bundles,
			DotCount: dotCount, DotMerged: dotMerged,
			UpdatedAt: rowUpdatedAt(docs, bundles),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		}
		return rows[i].Slug < rows[j].Slug
	})
	return rows, resolver, warnings
}

// resolverFor builds the worktree-id -> root map api_plans_page.go (CP11)
// resolves requests through. Rebuilt fresh on every aggregator build, never
// derived from anything a client sends.
func (a *plansAggregator) resolverFor(checkouts []checkoutInfo) map[string]string {
	m := make(map[string]string, len(checkouts))
	for _, c := range checkouts {
		m[c.id] = c.path
	}
	return m
}

// rowDots reports the count and merged-state a row's dot picker uses: the
// spec document's version set when one exists, else the design document's —
// one document's set, never a union across the two.
func rowDots(docs []planDoc) (count int, merged bool) {
	var target *planDoc
	for i := range docs {
		if strings.HasPrefix(docs[i].Path, "docs/spec/") {
			target = &docs[i]
			break
		}
	}
	if target == nil {
		for i := range docs {
			if strings.HasPrefix(docs[i].Path, "docs/design/") {
				target = &docs[i]
				break
			}
		}
	}
	if target == nil {
		return 0, false
	}
	for _, v := range target.Versions {
		if v.IsMain {
			merged = true
			break
		}
	}
	return len(target.Versions), merged
}

// rowUpdatedAt is the newest real file mtime a row holds — every doc
// version's Mtime and every bundle file's Mtime, never meta.toml's `updated`
// (see docs/spec's rationale: Save only stamps that on a verb rewrite, not
// on every file an agent touches). A row with neither doc nor bundle mtime
// cannot happen in practice (a row exists because something produced it),
// but the zero time is returned rather than panicking.
func rowUpdatedAt(docs []planDoc, bundles []planBundle) time.Time {
	var newest time.Time
	for _, d := range docs {
		for _, v := range d.Versions {
			if v.Mtime.After(newest) {
				newest = v.Mtime
			}
		}
	}
	for _, b := range bundles {
		for _, f := range b.Files {
			if f.Mtime.After(newest) {
				newest = f.Mtime
			}
		}
	}
	return newest
}

// versionsFor groups one document's per-SHA checkout sets into
// planDocVersion entries, sorted newest-mtime first.
func versionsFor(bySHA map[string][]planCheckout) []planDocVersion {
	versions := make([]planDocVersion, 0, len(bySHA))
	for sha, cos := range bySHA {
		sorted := append([]planCheckout(nil), cos...)
		// Ids are opaque hashes now (see checkoutID), not enumeration
		// positions — sorting by id would order checkouts arbitrarily, so
		// sort by branch then path instead, both meaningful to a reader.
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Branch != sorted[j].Branch {
				return sorted[i].Branch < sorted[j].Branch
			}
			return sorted[i].Path < sorted[j].Path
		})
		label, isMain, newest := labelFor(sorted)
		versions = append(versions, planDocVersion{SHA: sha, Label: label, IsMain: isMain, Mtime: newest, Checkouts: sorted})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Mtime.After(versions[j].Mtime) })
	return versions
}

// labelFor picks a version's representative label: the merged (main)
// checkout's branch when the set holds one, else the branch of the checkout
// with the newest file mtime.
func labelFor(cos []planCheckout) (label string, isMain bool, newest time.Time) {
	var mainCO, freshest *planCheckout
	for i := range cos {
		if cos[i].IsMain {
			mainCO = &cos[i]
		}
		if freshest == nil || cos[i].FileMtime.After(freshest.FileMtime) {
			freshest = &cos[i]
		}
		if cos[i].FileMtime.After(newest) {
			newest = cos[i].FileMtime
		}
	}
	if mainCO != nil {
		return mainCO.Branch, true, newest
	}
	return freshest.Branch, false, newest
}

// representativeVersion picks the version extractMeta reads for a row: the
// merged (main) version when one exists, else the newest (versions is
// already sorted newest-first).
func representativeVersion(versions []planDocVersion) *planDocVersion {
	for i := range versions {
		if versions[i].IsMain {
			return &versions[i]
		}
	}
	if len(versions) > 0 {
		return &versions[0]
	}
	return nil
}

// bundleFilesFor lists every file in a bundle other than meta.toml,
// classified by kind. RelPath is relative to the worktree root, not the
// bundle, because it is the value /api/plans/page resolves under the
// checkout's root — the client passes it through without knowing where
// the scratchpad lives.
func bundleFilesFor(worktreeRoot, bundleRoot string) []bundleFile {
	var files []bundleFile
	_ = filepath.WalkDir(bundleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != bundleRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "meta.toml" {
			return nil
		}
		rel, relErr := filepath.Rel(worktreeRoot, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		var mtime time.Time
		if infoErr == nil {
			mtime = info.ModTime()
		}
		files = append(files, bundleFile{RelPath: filepath.ToSlash(rel), Kind: classifyBundleFile(d.Name()), Mtime: mtime})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files
}

func classifyBundleFile(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "html"
	default:
		return "file"
	}
}

// fingerprint stat-fingerprints everything a rebuild would read: each
// checkout's identity, its git index (so a `git add && git commit` — which
// changes tracked-ness without touching a doc's working-file mtime — still
// invalidates the cache), its docs/design and docs/spec entries, and its
// scratchpad root — re-enumerating worktrees on every call (the caller,
// rows(), already does this) so a worktree added since the last build
// changes the fingerprint even before any file inside it is touched. A file
// modified inside the quiet window is excluded, matching build's own
// quiet-window read, so a mid-write file cannot tear a version's SHA.
func (a *plansAggregator) fingerprint(checkouts []checkoutInfo) string {
	now := time.Now()
	h := sha256.New()

	sorted := append([]checkoutInfo(nil), checkouts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })

	for _, c := range sorted {
		h.Write([]byte(c.path))
		h.Write([]byte{0})
		h.Write([]byte(c.branch))
		h.Write([]byte{'\n'})

		if idxPath, ok := checkoutGitIndexPath(c.path); ok {
			if info, err := os.Stat(idxPath); err == nil {
				writeFingerprintEntry(h, ".git/index", info)
			}
		}

		for _, kind := range [2]string{"design", "spec"} {
			dir := filepath.Join(c.path, "docs", kind)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			names := make([]string, 0, len(entries))
			infos := map[string]os.FileInfo{}
			for _, de := range entries {
				if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
					continue
				}
				if de.Type()&fs.ModeSymlink != 0 {
					continue
				}
				info, err := de.Info()
				if err != nil || now.Sub(info.ModTime()) < a.quietWindow {
					continue
				}
				names = append(names, de.Name())
				infos[de.Name()] = info
			}
			sort.Strings(names)
			for _, n := range names {
				writeFingerprintEntry(h, kind+"/"+n, infos[n])
			}
		}

		scratchpadRoot := config.ScratchpadDir(c.path)
		_ = filepath.WalkDir(scratchpadRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != scratchpadRoot && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil || now.Sub(info.ModTime()) < a.quietWindow {
				return nil
			}
			rel, relErr := filepath.Rel(scratchpadRoot, path)
			if relErr != nil {
				return nil
			}
			writeFingerprintEntry(h, rel, info)
			return nil
		})
	}

	return hex.EncodeToString(h.Sum(nil))
}

func writeFingerprintEntry(h io.Writer, name string, info os.FileInfo) {
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(info.Size(), 10)))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
	h.Write([]byte{'\n'})
}

// extractMeta reads a row's title from the document's first heading and its
// description from the "## Goal" paragraph, falling back to the document's
// first paragraph, then empty. Never meta.toml — a bundle's description
// field is provenance, not display text.
func extractMeta(data []byte) (title, description string) {
	if data == nil {
		return "", ""
	}
	title = firstHeading(data)
	description = goalParagraph(data)
	if description == "" {
		description = firstBodyParagraph(data)
	}
	return title, description
}

func goalParagraph(data []byte) string {
	_, body, err := frontmatter.Parse(string(data))
	if err != nil {
		body = string(data)
	}
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		if strings.TrimSpace(raw) == "## Goal" {
			return paragraphAfter(lines[i+1:])
		}
	}
	return ""
}

func firstBodyParagraph(data []byte) string {
	_, body, err := frontmatter.Parse(string(data))
	if err != nil {
		body = string(data)
	}
	return paragraphAfter(strings.Split(body, "\n"))
}

// paragraphAfter returns the first run of non-empty, non-heading lines,
// joined with spaces, skipping fenced code.
func paragraphAfter(lines []string) string {
	inFence := false
	var buf []string
	for _, raw := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if trimmed == "" {
			if len(buf) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if len(buf) > 0 {
				break
			}
			continue
		}
		buf = append(buf, trimmed)
	}
	return strings.Join(buf, " ")
}
