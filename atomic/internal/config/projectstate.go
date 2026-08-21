package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// projectHomeOverride bypasses os.UserHomeDir for tests, mirroring
// harnessDirOverride's seam.
var projectHomeOverride *string

// SetHomeDirForTest overrides the home directory used to derive
// ~/.atomic/<project-key>/, so tests never touch the real home. Returns a
// restore func (nil-safe) that the caller should defer immediately.
func SetHomeDirForTest(dir string) func() {
	prev := projectHomeOverride
	projectHomeOverride = &dir
	return func() { projectHomeOverride = prev }
}

func projectHome() string {
	if projectHomeOverride != nil {
		return *projectHomeOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// mainCheckoutRoot resolves a clone's main checkout root from repoRoot, with no
// git subprocess: it walks upward from repoRoot (repoRoot itself first, since
// repoRoot may be a scope="repo" marker directory that holds no .git of its
// own) looking for a `.git` entry. A directory means that location already is
// the main checkout. A file means a worktree — its `gitdir:` line is followed
// to the enclosing `.git` directory, whose parent is the main checkout. When
// the walk reaches the filesystem root without finding either, repoRoot itself
// is returned unchanged.
func mainCheckoutRoot(repoRoot string) string {
	dir, err := filepath.Abs(repoRoot)
	if err != nil {
		dir = repoRoot
	}
	dir = filepath.Clean(dir)

	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			if info.IsDir() {
				return resolveSymlinks(dir)
			}
			if root, ok := resolveWorktreeGitFile(gitPath, dir); ok {
				return resolveSymlinks(root)
			}
			return resolveSymlinks(repoRoot)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return resolveSymlinks(repoRoot)
		}
		dir = parent
	}
}

// resolveSymlinks gives path's absolute, symlink-free form, falling back to
// its absolute (but unresolved) form when EvalSymlinks errors — the shape it
// takes on a path that doesn't exist, since resolution requires lstat-ing
// every component. Applied on every mainCheckoutRoot return path so a main
// checkout and a worktree of the same clone agree on the key even when the
// clone sits under a symlinked ancestor (macOS /tmp and /var both are): git
// itself writes an already-resolved absolute `gitdir:` target when creating
// a worktree, so leaving the main-checkout branch unresolved is exactly the
// asymmetry that made the two branches disagree.
func resolveSymlinks(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return real
}

// resolveWorktreeGitFile reads a worktree's `.git` file at gitFilePath (rooted
// at worktreeDir) and follows its `gitdir:` line to the enclosing `.git`
// directory, returning that directory's parent as the main checkout root. A
// relative gitdir value resolves against worktreeDir exactly as written — the
// shape a submodule's `.git` file carries too, which this treats identically,
// keying it to its superproject's `.git` root.
func resolveWorktreeGitFile(gitFilePath, worktreeDir string) (string, bool) {
	raw, err := os.ReadFile(gitFilePath)
	if err != nil {
		return "", false
	}

	target := parseGitdirLine(string(raw))
	if target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(worktreeDir, target)
	}
	target = filepath.Clean(target)

	dir := target
	for {
		if filepath.Base(dir) == ".git" {
			return filepath.Dir(dir), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func parseGitdirLine(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "gitdir:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// projectKey flattens mainCheckoutRoot's absolute path into a single path
// segment safe to use as a directory name under ~/.atomic/.
func projectKey(repoRoot string) string {
	root := mainCheckoutRoot(repoRoot)
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	abs = filepath.Clean(abs)

	key := strings.TrimPrefix(abs, string(filepath.Separator))
	key = strings.ReplaceAll(key, string(filepath.Separator), "-")
	return key
}

// ProjectStateDir returns ~/.atomic/<project-key>/, the root of the
// project-keyed state home shared by every worktree of repoRoot's clone.
func ProjectStateDir(repoRoot string) string {
	return filepath.Join(Dir(projectHome()), projectKey(repoRoot))
}

// ReportsRoot returns ProjectStateDir/reports/, with no branch applied — the
// directory a caller sweeping every branch (e.g. reaping gone branches) reads.
func ReportsRoot(repoRoot string) string {
	return filepath.Join(ProjectStateDir(repoRoot), "reports")
}

// ReportsDir returns the project-keyed reports/<branch>/ directory. It falls
// back to ReportsDirLegacy only when legacy already holds a report for the
// branch and the new directory does not, so a pre-migration report stays
// readable until migrate moves it. The default has to be the new home: this
// function decides where a report is written as well as read, and preferring
// legacy whenever the new directory was empty wrote every report to legacy
// and then found it there forever.
func ReportsDir(repoRoot, branch string) string {
	dir := filepath.Join(ReportsRoot(repoRoot), branchSegment(branch))
	if dirHasReport(dir) {
		return dir
	}
	if legacy := ReportsDirLegacy(repoRoot, branch); dirHasReport(legacy) {
		return legacy
	}
	return dir
}

// ReportsDirLegacy returns the pre-relocation session-reports path for a
// branch, flattened via branchSegment the way /session-report has always
// documented.
func ReportsDirLegacy(repoRoot, branch string) string {
	return filepath.Join(ScratchpadDir(repoRoot), "session-reports", branchSegment(branch))
}

// branchSegment flattens a branch name into the single path component every
// reports-path caller must agree on: a branch legitimately containing "/"
// (feature/plans-page) nests as one directory rather than several, and the
// project-keyed and legacy report paths flatten it identically.
func branchSegment(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// ProjectRemindersDir returns the project-keyed reminders directory.
// harness.go's RemindersDir delegates to this.
func ProjectRemindersDir(repoRoot string) string {
	return filepath.Join(ProjectStateDir(repoRoot), "reminders")
}

// RemindersDirLegacy returns the pre-relocation reminders path — harness.go's
// RemindersDir body before it delegated here.
func RemindersDirLegacy(repoRoot string) string {
	return filepath.Join(ScratchpadDir(repoRoot), "reminders")
}

// ArchiveDir returns the project-keyed archive destination for a slug's bundle
// created on the given date.
func ArchiveDir(repoRoot, slug, created string) string {
	return filepath.Join(ProjectStateDir(repoRoot), "archive", slug, created)
}

// BranchFromHEAD reads the current branch for repoRoot's own checkout by
// reading <gitdir>/HEAD directly — no git subprocess. Unlike mainCheckoutRoot,
// this never walks up to the enclosing .git: a worktree's own gitdir (the
// target its .git file's `gitdir:` line names) carries its own HEAD, which
// can differ from the main checkout's. Returns ("", false) when repoRoot has
// no .git entry or HEAD cannot be read.
func BranchFromHEAD(repoRoot string) (string, bool) {
	gitPath := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}

	gitDir := gitPath
	if !info.IsDir() {
		raw, err := os.ReadFile(gitPath)
		if err != nil {
			return "", false
		}
		target := parseGitdirLine(string(raw))
		if target == "" {
			return "", false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(repoRoot, target)
		}
		gitDir = filepath.Clean(target)
	}

	headRaw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	return parseHEAD(string(headRaw))
}

// refNamePattern is the allow-list a git ref name must satisfy: the same
// characters valid in a filesystem path segment, plus "/" for a
// hierarchical name like "feature/plans-page". ".." is rejected separately
// below rather than folded into the character class, since two literal dots
// are individually allowed characters that become dangerous only in that
// specific sequence.
var refNamePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// validateRefName rejects a value unless it has the shape a git branch ref
// name (the part after "refs/heads/") is allowed to have: on the allow-list
// of characters, no ".." sequence, and no leading, trailing, or doubled "/".
func validateRefName(name string) error {
	if name == "" || !refNamePattern.MatchString(name) {
		return fmt.Errorf("config: invalid ref name %q: not a safe shape", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("config: invalid ref name %q: contains \"..\"", name)
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return fmt.Errorf("config: invalid ref name %q: malformed path", name)
	}
	// Real git forbids a "/"-separated component from starting with ".": a
	// lone "." component (refs/heads/.) otherwise collapses via
	// filepath.Join+Clean to its parent directory rather than a real branch.
	for _, component := range strings.Split(name, "/") {
		if strings.HasPrefix(component, ".") {
			return fmt.Errorf("config: invalid ref name %q: component %q starts with \".\"", name, component)
		}
	}
	return nil
}

// hexSHAPattern matches a bare 40-character hex commit SHA — the shape HEAD
// holds when detached.
var hexSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// parseHEAD extracts a branch label from HEAD's content, recognizing exactly
// three legal shapes: `ref: refs/heads/<name>` yields <name> once validated
// as a safe ref name; a bare 40-character hex SHA yields its 7-character
// prefix; anything else — a ref outside refs/heads/, an empty file, a
// truncated or garbled line — is not a branch and reports false rather than
// a truncated prefix of untrusted bytes. HEAD is repo-controlled state a
// hostile or corrupt repository can shape arbitrarily, and its content
// eventually becomes a path segment (ReportsDir), so nothing outside these
// three shapes is trusted enough to return.
func parseHEAD(content string) (string, bool) {
	line := strings.TrimSpace(content)
	if rest, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
		if err := validateRefName(rest); err != nil {
			return "", false
		}
		return rest, true
	}
	if hexSHAPattern.MatchString(line) {
		return line[:7], true
	}
	return "", false
}

// dirHasReport reports whether dir contains at least one file a report
// actually is — a `.md` file per session-report.md's storage layout — so a
// stray dotfile (e.g. `.DS_Store`) does not suppress the legacy fallback.
func dirHasReport(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
	}
	return false
}
