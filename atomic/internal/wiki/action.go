package wiki

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// WikiAction is the exported entry point for `atomic wiki` used by cmd/atomic/main.go.
// It delegates to wikiAction, which is the unexported testable seam.
func WikiAction(args []string, claudeHome, cwd string, out io.Writer) int {
	return wikiAction(args, claudeHome, cwd, out)
}

// wikiAction is the testable seam for `atomic wiki` subcommand dispatch.
//
// Parameters:
//   - args: the arguments after "wiki" (e.g. ["scan", "--root=/path"])
//   - claudeHome: path to ~/.claude (injected; never os.UserHomeDir() here)
//   - cwd: current working directory used as root when --root is absent
//   - out: writer for stdout handoff (injected for testability)
//
// Returns an exit code: 0 on success, 1 on usage/soft error, 2 on hard error.
func wikiAction(args []string, claudeHome, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki <scan|stale|linkify|bucket> [flags]\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "scan":
		return wikiScanAction(args[1:], claudeHome, cwd, out)
	case "stale":
		return wikiStaleAction(args[1:], cwd, out)
	case "stamp":
		return wikiStampAction(args[1:])
	case "mark-dirty":
		return wikiMarkDirtyAction(args[1:], claudeHome, cwd)
	case "linkify":
		return wikiLinkifyAction(args[1:], cwd)
	case "bucket":
		return wikiBucketAction(args[1:], cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki: unknown verb %q\n", verb)
		return 1
	}
}

// wikiStaleAction implements `atomic wiki stale [--root=<path>]`.
// Read-only freshness check. Exit 0 fresh / 1 stale / 2 hard error.
// Stdout is injected via out so tests can capture the DRIFT/STALE lines.
func wikiStaleAction(args []string, cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-stale", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "root directory to check (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if root == "" {
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki stale: resolve root: %v\n", err)
		return 2
	}

	code, err := Stale(absRoot, out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki stale: %v\n", err)
	}
	return code
}

// knowledgeTopicRE matches conforming knowledge topic filenames: kebab-case
// [a-z0-9-]+.md — e.g. "vendor-x.md", "auth-patterns.md", "topic.md".
var knowledgeTopicRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.md$`)

// wikiStampAction implements:
//
//	atomic wiki stamp <file> --repo <path>                          (summary mode)
//	atomic wiki stamp <file> --root <wiki-root> --cites a,b,c      (concern mode)
//	atomic wiki stamp <file> --knowledge --sources <entries>        (knowledge mode)
//
// It is an INTERNAL helper invoked by /refresh-wiki — not surfaced in /atomic-help.
func wikiStampAction(args []string) int {
	fs := flag.NewFlagSet("wiki-stamp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var repo string    // summary mode: repo whose HEAD to stamp
	var root string    // concern mode: wiki root
	var cites string   // concern mode: comma-separated cited repo ids
	var knowledge bool // knowledge mode: stamp sources: list
	var sources string // knowledge mode: comma-separated "<bucket>/<file>@<sha256>" entries

	fs.StringVar(&repo, "repo", "", "repo path (summary mode)")
	fs.StringVar(&root, "root", "", "wiki root (concern mode)")
	fs.StringVar(&cites, "cites", "", "comma-separated cited repo ids (concern mode)")
	fs.BoolVar(&knowledge, "knowledge", false, "knowledge page mode: stamp sources: list")
	fs.StringVar(&sources, "sources", "", "comma-separated sources entries (knowledge mode)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	positional := fs.Args()
	if len(positional) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki stamp <file> --repo <path>\n")
		fmt.Fprintf(os.Stderr, "       atomic wiki stamp <file> --root <wiki-root> --cites <ids>\n")
		fmt.Fprintf(os.Stderr, "       atomic wiki stamp <file> --knowledge --sources <entries>\n")
		return 1
	}

	filePath := positional[0]

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki stamp: resolve file path: %v\n", err)
		return 1
	}

	switch {
	case knowledge:
		// Knowledge mode — requires --sources.
		if sources == "" {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: --knowledge requires --sources\n")
			return 1
		}
		// Validate topic name: base filename must match [a-z0-9][a-z0-9-]*.md
		base := filepath.Base(absFile)
		if !knowledgeTopicRE.MatchString(base) {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: knowledge topic name %q does not conform to kebab-case [a-z0-9-]+.md — skipping\n", base)
			return 0
		}
		entries := splitCites(sources)
		if err := StampKnowledge(absFile, entries); err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: %v\n", err)
			return 1
		}
		return 0

	case repo != "" && root == "" && cites == "":
		// Summary mode.
		absRepo, err := filepath.Abs(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: resolve --repo: %v\n", err)
			return 1
		}
		if err := StampSummary(absFile, absRepo); err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: %v\n", err)
			return 1
		}
		return 0

	case root != "" && cites != "":
		// Concern mode.
		absRoot, err := filepath.Abs(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: resolve --root: %v\n", err)
			return 1
		}
		ids := splitCites(cites)
		if err := StampConcern(absFile, absRoot, ids); err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: %v\n", err)
			return 1
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr, "atomic wiki stamp: supply either --repo (summary), --root + --cites (concern), or --knowledge + --sources (knowledge)\n")
		return 1
	}
}

// wikiMarkDirtyAction implements `atomic wiki mark-dirty`.
// It is an INTERNAL helper invoked by the signals-gate partial — not surfaced
// in /atomic-help.  Exits 0 when no registered root matches cwd; exits 1 on
// arg error or MarkDirty failure.
func wikiMarkDirtyAction(args []string, claudeHome, cwd string) int {
	// No flags — mark-dirty is a zero-argument subcommand.
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "atomic wiki mark-dirty: unexpected arguments: %v\n", args)
		return 1
	}

	if err := MarkDirty(claudeHome, cwd); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki mark-dirty: %v\n", err)
		return 1
	}
	return 0
}

// wikiLinkifyAction implements `atomic wiki linkify [--root=<path>]`.
// It linkifies all wiki artifacts under <root>/wiki/ in-place.
func wikiLinkifyAction(args []string, cwd string) int {
	fs := flag.NewFlagSet("wiki-linkify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "realm root directory (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if root == "" {
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki linkify: resolve root: %v\n", err)
		return 2
	}

	if err := LinkifyWiki(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki linkify: %v\n", err)
		return 1
	}
	return 0
}

// splitCites splits a comma-separated cites string into a slice of trimmed ids.
// Empty elements are discarded.
func splitCites(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// wikiBucketAction implements `atomic wiki bucket <add|list|diff|promote> [flags]`.
// Root is resolved from --root flag or cwd.
func wikiBucketAction(args []string, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket <add|list|diff|promote> [--root=<path>] [name]\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "add":
		return wikiBucketAddAction(args[1:], cwd, out)
	case "list":
		return wikiBucketListAction(args[1:], cwd, out)
	case "diff":
		return wikiBucketDiffAction(args[1:], cwd, out)
	case "promote":
		return wikiBucketPromoteAction(args[1:], cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki bucket: unknown verb %q\n", verb)
		return 1
	}
}

// resolveWikiRoot parses a --root flag from args and falls back to cwd.
// Returns the absolute root and the remaining positional args.
//
// Unlike flag.FlagSet.Parse, this scanner handles --root in any position
// relative to positional arguments (e.g. `<name> --root <path>`), because
// flag.FlagSet stops parsing at the first non-flag token. Both forms are
// accepted: --root=<path> and --root <path>.
//
// Unrecognized flags are returned as an error so callers can refuse them
// instead of silently treating them as extra positionals.
func resolveWikiRoot(args []string, cwd string) (absRoot string, positional []string, err error) {
	var root string
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--root" {
			// Space-separated form: --root <value>
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("flag --root requires a value")
			}
			root = args[i+1]
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "--root=") {
			// Equals form: --root=<value>
			val := arg[len("--root="):]
			if val == "" {
				return "", nil, fmt.Errorf("flag --root requires a value")
			}
			root = val
			i++
			continue
		}
		if strings.HasPrefix(arg, "--") {
			// Unrecognized flag — reject loudly.
			return "", nil, fmt.Errorf("unrecognized flag %q", arg)
		}
		// Positional arg.
		positional = append(positional, arg)
		i++
	}
	if root == "" {
		root = cwd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve root: %w", err)
	}
	return abs, positional, nil
}

// wikiBucketAddAction implements `atomic wiki bucket add [--root=<path>] <name>`.
func wikiBucketAddAction(args []string, cwd string, out io.Writer) int {
	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket add [--root=<path>] <name>\n")
		return 1
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: unexpected extra arguments: %v\n", positional[1:])
		return 1
	}
	name := positional[0]

	wikiDir := filepath.Join(absRoot, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")
	bucketDir := filepath.Join(absRoot, name)

	// Register the manifest dir (validates name and double-register).
	if err := RegisterBucket(wikiDir, name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %v\n", err)
		return 1
	}

	// Create the bucket folder if absent.
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: create bucket dir: %v\n", err)
		return 1
	}

	// Write index.md stub (no-op if already exists).
	if err := createBucketIndexStub(bucketDir, name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: create index stub: %v\n", err)
		return 1
	}

	// Splice the <wiki-buckets> entry into wiki/index.md.
	if err := spliceBucketEntry(indexPath, name, bucketDir); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: splice block: %v\n", err)
		return 1
	}

	// Write ## Capture surfaces section to realm CLAUDE.md.
	realmCLAUDE := filepath.Join(absRoot, "CLAUDE.md")
	if err := writeCaptureSurfacesSection(realmCLAUDE, name, bucketDir); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: write CLAUDE.md section: %v\n", err)
		return 1
	}

	return 0
}

// wikiBucketListAction implements `atomic wiki bucket list [--root=<path>]`.
// Prints one line per bucket: "<name>  <abs-path>  <N> files  (<pending|fresh>)"
// or "(no baseline)" when never promoted. Exits 0 even when empty.
func wikiBucketListAction(args []string, cwd string, out io.Writer) int {
	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket list: %v\n", err)
		return 2
	}
	if len(positional) > 0 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket list: unexpected extra arguments: %v\n", positional)
		return 1
	}

	wikiDir := filepath.Join(absRoot, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")

	entries, err := readBucketEntries(indexPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket list: %v\n", err)
		return 1
	}
	if len(entries) == 0 {
		return 0
	}

	for _, e := range entries {
		baselinePath := filepath.Join(wikiDir, ".buckets", e.Name, "baseline")
		baseline, err := readManifest(baselinePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket list: read baseline for %s: %v\n", e.Name, err)
			continue
		}

		if baseline == nil {
			// Never promoted: count field is "(no baseline)"; status field still
			// applies — every content file counts as pending, so we run a
			// read-only diff to determine pending vs fresh.
			diff, err := bucketDiffReadOnly(wikiDir, e.Name, e.Path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic wiki bucket list: diff %s: %v\n", e.Name, err)
				continue
			}
			var noBaselineStatus string
			if len(diff.Added)+len(diff.Changed)+len(diff.Removed) > 0 {
				noBaselineStatus = "pending"
			} else {
				noBaselineStatus = "fresh"
			}
			fmt.Fprintf(out, "%s\t%s\t(no baseline)\t(%s)\n", e.Name, e.Path, noBaselineStatus)
			continue
		}

		// Run a read-only diff to determine pending/fresh status.
		// bucketDiffReadOnly never writes the current manifest — list is a
		// status verb and must have no side effects on disk.
		diff, err := bucketDiffReadOnly(wikiDir, e.Name, e.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket list: diff %s: %v\n", e.Name, err)
			continue
		}

		baselineCount := len(baseline)
		var status string
		if len(diff.Added)+len(diff.Changed)+len(diff.Removed) > 0 {
			status = "pending"
		} else {
			status = "fresh"
		}
		fmt.Fprintf(out, "%s\t%s\t%d files\t(%s)\n", e.Name, e.Path, baselineCount, status)
	}
	return 0
}

// wikiBucketDiffAction implements `atomic wiki bucket diff [--root=<path>] <name>`.
// Prints "new|changed|removed <relpath>" lines. Exits 0 when empty, 1 when non-empty.
func wikiBucketDiffAction(args []string, cwd string, out io.Writer) int {
	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket diff: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket diff [--root=<path>] <name>\n")
		return 1
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket diff: unexpected extra arguments: %v\n", positional[1:])
		return 1
	}
	name := positional[0]

	wikiDir := filepath.Join(absRoot, "wiki")
	bucketDir := filepath.Join(absRoot, name)

	result, err := BucketDiff(wikiDir, name, bucketDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket diff: %v\n", err)
		return 1
	}

	var lines []string
	for _, p := range result.Added {
		lines = append(lines, "new "+p)
	}
	for _, p := range result.Changed {
		lines = append(lines, "changed "+p)
	}
	for _, p := range result.Removed {
		lines = append(lines, "removed "+p)
	}

	for _, l := range lines {
		fmt.Fprintln(out, l)
	}

	if len(lines) > 0 {
		return 1
	}
	return 0
}

// wikiBucketPromoteAction implements `atomic wiki bucket promote [--root=<path>] <name>`.
func wikiBucketPromoteAction(args []string, cwd string, out io.Writer) int {
	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket promote: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket promote [--root=<path>] <name>\n")
		return 1
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket promote: unexpected extra arguments: %v\n", positional[1:])
		return 1
	}
	name := positional[0]

	wikiDir := filepath.Join(absRoot, "wiki")
	bucketDir := filepath.Join(absRoot, name)

	if err := PromoteBucket(wikiDir, name, bucketDir); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket promote: %v\n", err)
		return 1
	}
	return 0
}

// wikiScanAction implements `atomic wiki scan [--root=<path>]`.
// It resolves the root, runs Scan (CP1), registers the wiki in CLAUDE.md,
// and prints the deterministic stdout handoff.
func wikiScanAction(args []string, claudeHome, cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "root directory to scan (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Resolve root: --root flag or cwd.
	if root == "" {
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: resolve root: %v\n", err)
		return 1
	}

	// Inject a stable clock for tests via Options; real callers use wall clock.
	opts := Options{
		Clock: func() time.Time { return time.Now().UTC() },
	}

	// Run CP1 Scan: discover repos, scaffold wiki/, write <wiki-scan> block.
	// Scan returns the classified members directly — no second filesystem walk needed.
	members, err := Scan(absRoot, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}

	wikiDir := filepath.Join(absRoot, "wiki")

	// Register the wiki's index.md in ~/.claude/CLAUDE.md.
	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	wikiIndexPath := filepath.Join(wikiDir, "index.md")
	if err := RegisterWiki(claudeMDPath, wikiIndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: register wiki: %v\n", err)
		return 1
	}

	// Print deterministic stdout handoff.
	PrintHandoff(out, members)

	return 0
}
