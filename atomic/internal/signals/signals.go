package signals

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

const (
	signalsFile = ".claude/project/deterministic-signals.md"
	prevFile    = ".claude/project/.deterministic-signals.prev.md"
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
	// Clock returns the current time. If nil, time.Now().UTC() is used.
	// Inject a fixed clock in tests to get deterministic generated_at values.
	Clock func() time.Time
	// MaxDepth limits the tree depth. Files at depth ≤ MaxDepth are fully
	// enumerated with per-file metadata. Directories at MaxDepth+1 show a
	// summary (N files, M dirs). Directories beyond MaxDepth+1 are elided.
	// If 0, ScanWithOptions reads output.signals.max_depth from ConfigPath
	// (defaulting to 3 when the config file is absent or the key unset).
	MaxDepth int
	// ConfigPath is the path to the atomic config TOML file
	// (~/.claude/.atomic/config.toml). When empty, ScanWithOptions resolves it
	// from os.UserHomeDir. Used by tests to inject an alternate config.
	ConfigPath string
	// ExcludeGlobs holds plain (no-prefix) glob patterns from .signalsignore.
	// Files matching any glob are omitted from the tree entirely.
	// Populated automatically by ScanWithOptions from the repo's .signalsignore.
	// Callers may also set this directly for testing.
	ExcludeGlobs []string
	// GeneratedGlobs holds '+'-prefixed glob patterns from .signalsignore (prefix stripped).
	// Files matching any glob appear in the tree with a [generated] marker but
	// the inferrer skips them for domain content.
	// Populated automatically by ScanWithOptions from the repo's .signalsignore.
	// Callers may also set this directly for testing.
	GeneratedGlobs []string
	// OutDir, when non-empty, redirects the deterministic substrate to
	// <OutDir>/.claude/project/deterministic-signals.md instead of the
	// default <root>/.claude/project/deterministic-signals.md.
	// The scanned repo is never written to when OutDir is set.
	OutDir string
}

// readSignalsIgnore reads .signalsignore from the repo root and returns two
// slices: excludeGlobs (plain lines) and generatedGlobs ('+'-prefixed lines,
// with the '+' stripped). Comment lines (# ...) and blank lines are ignored.
// If the file is absent, both slices are nil and no error is returned.
func readSignalsIgnore(root string) (excludeGlobs, generatedGlobs []string, err error) {
	path := filepath.Join(root, ".signalsignore")
	f, ferr := os.Open(path)
	if ferr != nil {
		if os.IsNotExist(ferr) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read .signalsignore: %w", ferr)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			generatedGlobs = append(generatedGlobs, line[1:])
		} else {
			excludeGlobs = append(excludeGlobs, line)
		}
	}
	return excludeGlobs, generatedGlobs, scanner.Err()
}

func (o *Options) clock() time.Time {
	if o != nil && o.Clock != nil {
		return o.Clock()
	}
	return time.Now().UTC()
}

// Scan walks the repo at root, assembles the signals document, and writes it.
// Idempotency: if the body is unchanged, the existing generated_at is kept
// and the file is NOT rewritten (so mtime stays stable).
func Scan(root string) error {
	return ScanWithOptions(root, nil)
}

// resolveConfigPath returns the atomic config TOML path for the current user.
// Falls back to empty string (which config.Load treats as missing) on error.
func resolveConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return config.TOMLPath(filepath.Join(home, ".claude"))
}

// resolveScanOptions fills opts with the .signalsignore globs and config-driven
// MaxDepth that a scan uses, so callers that assemble a body (Scan and Stale)
// produce identical output for identical source. Reading .signalsignore also
// lets the tree scanner flag matching paths as [generated].
func resolveScanOptions(root string, opts *Options) (*Options, error) {
	if opts == nil {
		opts = &Options{}
	}

	// Resolve MaxDepth from config when not explicitly set by the caller.
	if opts.MaxDepth == 0 {
		cfgPath := opts.ConfigPath
		if cfgPath == "" {
			cfgPath = resolveConfigPath()
		}
		cfg, _, _ := config.Load(cfgPath) // lenient: ignore warnings and errors
		if cfg != nil {
			opts.MaxDepth = cfg.Output.Signals.MaxDepth
		}
		// Final fallback handled inside ScanTreeWithOptions (defaultMaxDepth).
	}

	if len(opts.ExcludeGlobs) == 0 && len(opts.GeneratedGlobs) == 0 {
		excl, gen, err := readSignalsIgnore(root)
		if err != nil {
			return nil, err
		}
		opts.ExcludeGlobs = excl
		opts.GeneratedGlobs = gen
	}
	return opts, nil
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

	// When OutDir is set, redirect both the substrate and the prev-file backup
	// to that directory so the scanned repo is never written to.
	outputRoot := root
	if opts.OutDir != "" {
		outputRoot = opts.OutDir
	}
	outPath := filepath.Join(outputRoot, signalsFile)
	prevPath := filepath.Join(outputRoot, prevFile)

	// Read existing file (if any) to check idempotency.
	existingRaw, readErr := os.ReadFile(outPath)

	var genAt string
	rewrite := true

	if readErr == nil {
		oldMeta, oldBody, parseErr := frontmatter.Parse(string(existingRaw))
		if parseErr == nil && oldBody == body {
			// Body unchanged — keep existing generated_at and skip rewrite.
			if oldMeta != nil {
				if v, ok := oldMeta["generated_at"]; ok {
					genAt, _ = v.(string)
				}
			}
			rewrite = false
		}
	}

	if rewrite {
		// Back up the existing file before overwriting.
		if readErr == nil {
			if err := os.MkdirAll(filepath.Dir(prevPath), 0o755); err != nil {
				return fmt.Errorf("signals scan: create prev dir: %w", err)
			}
			if err := os.WriteFile(prevPath, existingRaw, 0o644); err != nil {
				return fmt.Errorf("signals scan: write prev file: %w", err)
			}
		}

		genAt = opts.clock().Format(time.RFC3339)

		// Order: generated_at first, then atomic_version (matches spec examples).
		doc, err := frontmatter.EmitOrdered([]frontmatter.KV{
			{Key: "generated_at", Value: genAt},
			{Key: "atomic_version", Value: version.Version},
		}, body)
		if err != nil {
			return fmt.Errorf("signals scan: emit: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("signals scan: create output dir: %w", err)
		}
		if err := os.WriteFile(outPath, []byte(doc), 0o644); err != nil {
			return fmt.Errorf("signals scan: write output: %w", err)
		}
	}

	return nil
}

// assembleBody builds the body of the signals document (without frontmatter).
func assembleBody(root string, opts *Options) (string, error) {
	tree, err := ScanTreeWithOptions(root, opts)
	if err != nil {
		return "", fmt.Errorf("tree scanner: %w", err)
	}

	manifests, err := ScanManifests(root)
	if err != nil {
		return "", fmt.Errorf("manifests scanner: %w", err)
	}

	langs, err := ScanLanguages(root)
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

// StaleInfo carries evidence about why the signals file is stale. The CLI
// turns it into imperative output because the staleness gate is consumed by an
// LLM orchestrator that can rationalize a silent exit code away — a concrete
// magnitude of drift makes the staleness real, not dismissable. Zero when fresh.
type StaleInfo struct {
	// ChangedLines is how many deterministic-body lines would change (added +
	// removed) if the signals file were re-scanned now.
	ChangedLines int
}

// Stale reports whether the signals file is out of date. It is content-based:
// it assembles the deterministic body exactly as Scan would and compares it to
// the stored body, so it is stale only when a fresh scan would actually differ.
// Pure mtime cannot tell an idempotent regeneration (same bytes, newer mtime)
// from a real edit; content comparison can, which avoids the commit-time-regen
// false-positive treadmill.
//
// Returns (zero, nil) when fresh, (info, ErrStale) when a re-scan would differ
// — info carries evidence for the caller's output — and (zero, error) on a hard
// failure such as a missing signals file. The three outcomes map to CLI exit
// codes 0 / 1 / 2 respectively.
func Stale(root string) (StaleInfo, error) {
	path := SignalsPath(root)
	existingRaw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StaleInfo{}, fmt.Errorf("signals stale: file not found at %s — run 'atomic signals scan' first", path)
		}
		return StaleInfo{}, fmt.Errorf("signals stale: %w", err)
	}
	_, oldBody, err := frontmatter.Parse(string(existingRaw))
	if err != nil {
		return StaleInfo{}, fmt.Errorf("signals stale: parse %s: %w", path, err)
	}

	opts, err := resolveScanOptions(root, nil)
	if err != nil {
		return StaleInfo{}, fmt.Errorf("signals stale: %w", err)
	}
	newBody, err := assembleBody(root, opts)
	if err != nil {
		return StaleInfo{}, fmt.Errorf("signals stale: %w", err)
	}

	if newBody != oldBody {
		return StaleInfo{ChangedLines: lineDelta(oldBody, newBody)}, ErrStale
	}
	return StaleInfo{}, nil
}

// ErrStale is returned by Stale when a fresh scan would differ from the stored
// signals file.
var ErrStale = fmt.Errorf("signals stale: a fresh scan would differ from the stored signals file")

// lineDelta counts how many lines differ (added + removed) between two bodies,
// as a multiset symmetric difference — a cheap magnitude of drift, not a true
// edit distance.
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

// Diff prints a unified diff between the previous and current signals files.
// Diff output is written to out (caller may pass os.Stdout).
// Exit codes (returned as special errors):
//   - nil → exit 0 (no diff)
//   - ErrDiffPresent → exit 1 (diff present, out has content)
//   - ErrNoPrior → exit 2 (no prior version)
func Diff(root string, out io.Writer) error {
	currentPath := SignalsPath(root)
	prevPath := PrevPath(root)

	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		return fmt.Errorf("signals diff: signals file not found at %s — run 'atomic signals scan' first", currentPath)
	}

	// Try git diff first.
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
	// Make the path relative to root for git.
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

// LinkifyFiles linkifies .claude/project/signals.md and every *.md file under
// .claude/project/signals/ in the repo at root, using root as the base directory
// for path resolution. Each file is read, linkified, and written back in place.
// If a file's content is unchanged after linkification, it is not rewritten.
// Idempotent: re-running on already-linkified content is a no-op.
func LinkifyFiles(root string) error {
	return LinkifyFilesWithBase(root, root)
}

// LinkifyFilesWithBase is like LinkifyFiles but accepts an explicit base directory.
// Exported so tests can inject a temp directory without needing a git repo.
func LinkifyFilesWithBase(root, base string) error {
	routerPath := filepath.Join(root, ".claude", "project", "signals.md")
	domainDir := filepath.Join(root, ".claude", "project", "signals")

	var targets []string

	if _, err := os.Stat(routerPath); err == nil {
		targets = append(targets, routerPath)
	}

	entries, err := os.ReadDir(domainDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				// Recurse one level for sub-domain directories.
				subDir := filepath.Join(domainDir, e.Name())
				subs, rerr := os.ReadDir(subDir)
				if rerr != nil {
					continue
				}
				for _, se := range subs {
					if !se.IsDir() && strings.HasSuffix(se.Name(), ".md") {
						targets = append(targets, filepath.Join(subDir, se.Name()))
					}
				}
				continue
			}
			if strings.HasSuffix(e.Name(), ".md") {
				targets = append(targets, filepath.Join(domainDir, e.Name()))
			}
		}
	}

	for _, target := range targets {
		if err := linkifyFile(target, base); err != nil {
			return fmt.Errorf("linkify %s: %w", target, err)
		}
	}
	return nil
}

// linkifyFile reads a file, linkifies it with the given base, and writes it
// back only if the content changed.
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
