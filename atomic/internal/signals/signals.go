package signals

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

const (
	signalsFile = "docs/wiki/scan.md"
	prevFile    = "tmp/.scan.prev.md"
)

// SignalsPath returns the absolute path to the signals file for the given repo root.
func SignalsPath(root string) string {
	return filepath.Join(root, signalsFile)
}

// PrevPath returns the absolute path to the prev signals file for the given repo root.
func PrevPath(root string) string {
	return filepath.Join(root, prevFile)
}

// Options configures a Scan run. All fields are optional.
type Options struct {
	// MaxDepth limits the tree depth: files at depth ≤ MaxDepth get per-file
	// metadata, directories at MaxDepth+1 collapse to a summary, deeper ones are
	// elided. 0 falls back to output.signals.max_depth, then to 3.
	MaxDepth int
	// ConfigPath overrides ~/.atomic/config.toml. Empty resolves from the home dir.
	ConfigPath string
	// ExcludeGlobs omits matching files from the tree entirely.
	ExcludeGlobs []string
	// GeneratedGlobs keeps matching files in the tree with a [generated] marker
	// so the inferrer skips them for domain content.
	GeneratedGlobs []string
	// OutDir redirects the substrate away from <root>, leaving the scanned repo
	// unwritten.
	OutDir string
}

// readSignalsIgnore resolves the scan's exclude and generated globs from
// [scan] in the repo config, falling back to a legacy .signalsignore. See
// config.ScanGlobs for the precedence rule.
func readSignalsIgnore(root string) (excludeGlobs, generatedGlobs []string, err error) {
	excludeGlobs, generatedGlobs, _, err = config.ScanGlobs(root)
	return excludeGlobs, generatedGlobs, err
}

// Scan walks the repo at root, assembles the signals document, and writes it.
// The file is rewritten only when the body changes, so mtime stays stable on
// repeated scans of an unchanged repo.
func Scan(root string) error {
	return ScanWithOptions(root, nil)
}

// resolveConfigPath falls back to "", which config.Load treats as missing.
func resolveConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return config.TOMLPath(home)
}

// resolveScanOptions resolves the globs and MaxDepth a scan uses, so Scan and
// Stale assemble identical bodies from identical source. Works on a copy; the
// caller's *Options is never mutated.
func resolveScanOptions(root string, opts *Options) (*Options, error) {
	var resolved Options
	if opts != nil {
		resolved = *opts
	}

	if resolved.MaxDepth == 0 {
		cfgPath := resolved.ConfigPath
		if cfgPath == "" {
			cfgPath = resolveConfigPath()
		}
		cfg, _, _ := config.Load(cfgPath) // lenient: ignore warnings and errors
		if cfg != nil {
			resolved.MaxDepth = cfg.Output.Signals.MaxDepth
		}
		// Zero still falls through to ScanTreeWithOptions's defaultMaxDepth.
	}

	if len(resolved.ExcludeGlobs) == 0 && len(resolved.GeneratedGlobs) == 0 {
		excl, gen, err := readSignalsIgnore(root)
		if err != nil {
			return nil, err
		}
		resolved.ExcludeGlobs = excl
		resolved.GeneratedGlobs = gen
	}
	return &resolved, nil
}

// ScanWithOptions is like Scan but accepts Options for dependency injection.
func ScanWithOptions(root string, opts *Options) error {
	opts, err := resolveScanOptions(root, opts)
	if err != nil {
		return fmt.Errorf("signals scan: %w", err)
	}

	body, err := assembleBody(root, opts)
	if err != nil {
		return fmt.Errorf("signals scan: %w", err)
	}

	outputRoot := root
	if opts.OutDir != "" {
		outputRoot = opts.OutDir
	}
	outPath := filepath.Join(outputRoot, signalsFile)
	prevPath := filepath.Join(outputRoot, prevFile)

	existingRaw, readErr := os.ReadFile(outPath)

	rewrite := true
	if readErr == nil && string(existingRaw) == body {
		rewrite = false // unchanged body: skip the write so mtime stays stable
	}

	if rewrite {
		if readErr == nil {
			if err := os.MkdirAll(filepath.Dir(prevPath), 0o755); err != nil {
				return fmt.Errorf("signals scan: create prev dir: %w", err)
			}
			if err := os.WriteFile(prevPath, existingRaw, 0o644); err != nil {
				return fmt.Errorf("signals scan: write prev file: %w", err)
			}
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("signals scan: create output dir: %w", err)
		}
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			return fmt.Errorf("signals scan: write output: %w", err)
		}
	}

	return nil
}

// assembleBody builds the signals body (no frontmatter) in one shared read pass:
// the tree scanner fills a metaCache the language counter reuses, so only files
// beyond the depth cap are read twice.
func assembleBody(root string, opts *Options) (string, error) {
	tree, metaCache, err := scanTreeWithMetaCache(root, opts)
	if err != nil {
		return "", fmt.Errorf("tree scanner: %w", err)
	}

	manifests, err := ScanManifests(root)
	if err != nil {
		return "", fmt.Errorf("manifests scanner: %w", err)
	}

	langs, err := scanLanguagesFromCache(root, metaCache)
	if err != nil {
		return "", fmt.Errorf("languages scanner: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# Deterministic signals\n")
	sb.WriteString("\n## Tree\n\n")
	sb.WriteString(tree)
	sb.WriteString("\n\n## Manifests\n\n")
	sb.WriteString(manifests)
	sb.WriteString("\n\n## Languages\n\n")
	sb.WriteString(langs)
	sb.WriteString("\n")

	return sb.String(), nil
}

// Show prints the signals file content to stdout.
func Show(root string) error {
	path := SignalsPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("signals show: file not found at %s — run 'atomic signals scan' first", path)
		}
		return fmt.Errorf("signals show: %w", err)
	}
	_, err = os.Stdout.Write(data)
	return err
}

// StaleInfo carries the magnitude of drift, which the CLI renders as imperative
// output: the gate is consumed by an LLM orchestrator that can rationalize a
// silent exit code away. Zero when fresh.
type StaleInfo struct {
	// ChangedLines is added + removed body lines a re-scan would produce.
	ChangedLines int
}

// Stale reports whether the signals file is out of date, by content rather than
// mtime — mtime cannot tell an idempotent regeneration from a real edit, which
// makes commit-time regen a false-positive treadmill.
//
// Returns (zero, nil) fresh, (info, ErrStale) drifted, (zero, error) on a hard
// failure such as a missing file — CLI exit codes 0 / 1 / 2.
func Stale(root string) (StaleInfo, error) {
	path := SignalsPath(root)
	existingRaw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StaleInfo{}, fmt.Errorf("signals stale: file not found at %s — run 'atomic signals scan' first", path)
		}
		return StaleInfo{}, fmt.Errorf("signals stale: %w", err)
	}

	opts, err := resolveScanOptions(root, nil)
	if err != nil {
		return StaleInfo{}, fmt.Errorf("signals stale: %w", err)
	}
	newBody, err := assembleBody(root, opts)
	if err != nil {
		return StaleInfo{}, fmt.Errorf("signals stale: %w", err)
	}

	oldBody := string(existingRaw)
	if newBody != oldBody {
		return StaleInfo{ChangedLines: lineDelta(oldBody, newBody)}, ErrStale
	}
	return StaleInfo{}, nil
}

// ErrStale is returned by Stale when a fresh scan would differ from the stored
// signals file.
var ErrStale = fmt.Errorf("signals stale: a fresh scan would differ from the stored signals file")

// lineDelta is a multiset symmetric difference — a cheap magnitude of drift,
// not a true edit distance.
func lineDelta(oldBody, newBody string) int {
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, line := range strings.Split(s, "\n") {
			m[line]++
		}
		return m
	}
	oldCounts, newCounts := count(oldBody), count(newBody)
	delta := 0
	for line, n := range newCounts {
		if extra := n - oldCounts[line]; extra > 0 {
			delta += extra // added
		}
	}
	for line, o := range oldCounts {
		if extra := o - newCounts[line]; extra > 0 {
			delta += extra // removed
		}
	}
	return delta
}

// Diff writes a unified diff of the previous and current signals files to out.
// Returns nil (exit 0), ErrDiffPresent (exit 1), or ErrNoPrior (exit 2).
func Diff(root string, out io.Writer) error {
	currentPath := SignalsPath(root)
	prevPath := PrevPath(root)

	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		return fmt.Errorf("signals diff: signals file not found at %s — run 'atomic signals scan' first", currentPath)
	}

	if isGitRepo(root) {
		return diffGit(root, currentPath, out)
	}
	return diffFallback(prevPath, currentPath, out)
}

// ErrDiffPresent signals that diff found changes (caller should exit 1).
var ErrDiffPresent = fmt.Errorf("signals diff: diff present")

// ErrNoPrior signals that no prior version is available (caller should exit 2).
var ErrNoPrior = fmt.Errorf("signals diff: no prior version available")

func isGitRepo(root string) bool {
	_, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil
}

func diffGit(root, currentPath string, out io.Writer) error {
	rel, err := filepath.Rel(root, currentPath)
	if err != nil {
		rel = currentPath
	}

	// --exit-code makes git diff exit 1 when differences are found.
	cmd := exec.Command("git", "-C", root, "diff", "--exit-code", "--", rel)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.ExitCode() == 1 {
				return ErrDiffPresent
			}
		}
		return fmt.Errorf("signals diff: git diff failed: %w", err)
	}
	return nil
}

func diffFallback(prevPath, currentPath string, out io.Writer) error {
	if _, err := os.Stat(prevPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "signals diff: no prior version available at %s\n", prevPath)
		return ErrNoPrior
	}

	cmd := exec.Command("diff", "-u", prevPath, currentPath)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.ExitCode() == 1 {
				return ErrDiffPresent
			}
		}
		return fmt.Errorf("signals diff: diff failed: %w", err)
	}
	return nil
}

// LinkifyFiles rewrites docs/wiki/*.md in place with resolved links, skipping
// files whose content is unchanged. Idempotent.
func LinkifyFiles(root string) error {
	return LinkifyFilesWithBase(root, root)
}

// LinkifyFilesWithBase is LinkifyFiles with an explicit base directory, so tests
// can point at a temp dir instead of a git repo.
func LinkifyFilesWithBase(root, base string) error {
	routerPath := filepath.Join(root, "docs", "wiki", "index.md")
	domainDir := filepath.Join(root, "docs", "wiki")

	// scan.md is the raw deterministic dump, CLAUDE.md is steering, and index.md
	// is already covered by routerPath.
	skipNames := map[string]bool{
		"scan.md":   true,
		"CLAUDE.md": true,
		"index.md":  true,
	}

	var targets []string

	if _, err := os.Stat(routerPath); err == nil {
		targets = append(targets, routerPath)
	}

	entries, err := os.ReadDir(domainDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if skipNames[name] || !strings.HasSuffix(name, ".md") {
				continue
			}
			targets = append(targets, filepath.Join(domainDir, name))
		}
	}

	for _, target := range targets {
		if err := linkifyFile(target, base); err != nil {
			return fmt.Errorf("linkify %s: %w", target, err)
		}
	}
	return nil
}

func linkifyFile(path, base string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	linkified := mdlink.LinkifyFile(string(raw), path, base)
	if linkified == string(raw) {
		return nil // no change
	}
	return os.WriteFile(path, []byte(linkified), 0o644)
}
