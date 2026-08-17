package wiki

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// WikiAction is the `atomic wiki` entry point for cmd/atomic/main.go.
func WikiAction(args []string, claudeHome, cwd string, out io.Writer) int {
	return wikiAction(args, claudeHome, cwd, out)
}

// wikiAction is the testable seam behind WikiAction: args are everything after
// "wiki", claudeHome and cwd are injected rather than looked up, and the exit
// code is 0 success / 1 usage or soft error / 2 hard error.
func wikiAction(args []string, claudeHome, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki <scan|stale|linkify|bucket|init> [flags]\n")
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
	case "init":
		return wikiInitAction(args[1:], cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki: unknown verb %q\n", verb)
		return 1
	}
}

// wikiStaleAction implements `atomic wiki stale [--root=<path>]`: read-only,
// exit 0 fresh / 1 stale / 2 hard error.
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

// knowledgeTopicRE is the kebab-case naming contract for knowledge topic files.
var knowledgeTopicRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.md$`)

// wikiStampAction implements:
//
//	atomic wiki stamp <file> --repo <path>                          (summary mode)
//	atomic wiki stamp <file> --root <wiki-root> --cites a,b,c      (concern mode)
//	atomic wiki stamp <file> --knowledge --sources <entries>        (knowledge mode)
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

	// flag.FlagSet stops at the first non-flag token, so flags placed after
	// <file> would be dropped. Re-parse in a loop, peeling one positional at a
	// time, so flags may appear anywhere.
	//
	// The bare "--" is split off first: fs.Parse honors a terminator only
	// within the call that consumes it, so re-parsing the tail would revive
	// flags the user meant as positional.
	head := args
	var tail []string
	for i, a := range args {
		if a == "--" {
			head = args[:i]
			tail = args[i+1:]
			break
		}
	}

	var positional []string
	rest := head
	for {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	positional = append(positional, tail...)
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
		if sources == "" {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: --knowledge requires --sources\n")
			return 1
		}
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

// wikiMarkDirtyAction implements `atomic wiki mark-dirty`, an internal helper
// for the signals-gate partial and not surfaced in /atomic-help. Exits 0 when
// no registered root matches cwd.
func wikiMarkDirtyAction(args []string, claudeHome, cwd string) int {
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

// wikiLinkifyAction linkifies every wiki artifact under <root>/wiki/ in place.
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

// wikiInitAction implements `atomic wiki init --scope repo|realm [--root=<path>]`:
// it declares the scope in .claude/atomic.toml and writes the CLAUDE.md
// scaffold for it — docs/wiki/CLAUDE.md (steering) for repo, wiki/CLAUDE.md
// (self-reference) for realm. Both writes no-op when the target exists.
//
// A marker already declaring a different scope is never rewritten: that exits 1
// with nothing touched.
func wikiInitAction(args []string, cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var scope, root string
	fs.StringVar(&scope, "scope", "", "scaffold scope: repo or realm")
	fs.StringVar(&root, "root", "", "root directory (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if scope != "repo" && scope != "realm" {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki init --scope repo|realm [--root=<path>]\n")
		return 1
	}

	if root == "" {
		root = cwd
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki init: resolve root: %v\n", err)
		return 1
	}

	markerOutcome, err := config.EnsureScopeMarker(absRoot, scope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki init: scope marker: %v\n", err)
		return 1
	}
	if markerOutcome == config.ScopeMarkerConflict {
		fmt.Fprintf(os.Stderr, "atomic wiki init: %s already declares a different scope — refusing to overwrite it\n", config.RepoConfigPath(absRoot))
		return 1
	}

	var path string
	var created bool
	switch scope {
	case "repo":
		path = filepath.Join(absRoot, "docs", "wiki", "CLAUDE.md")
		created, err = InitRepoScope(absRoot)
	case "realm":
		path = filepath.Join(absRoot, "wiki", "CLAUDE.md")
		created, err = InitRealmScope(absRoot)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki init: %v\n", err)
		return 1
	}

	if markerOutcome == config.ScopeMarkerCreated || markerOutcome == config.ScopeMarkerAdded {
		fmt.Fprintf(out, "created %s\n", config.RepoConfigPath(absRoot))
	}
	if created {
		fmt.Fprintf(out, "created %s\n", path)
	}
	return 0
}

// splitCites splits a comma-separated list into trimmed, non-empty ids.
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

func wikiBucketAction(args []string, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket <add|list|diff|promote|doc|skill|index> [--root=<path>] [args]\n")
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
	case "doc":
		return wikiBucketDocAction(args[1:], cwd, out)
	case "skill":
		return wikiBucketSkillAction(args[1:], cwd, out)
	case "index":
		return wikiBucketIndexAction(args[1:], cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki bucket: unknown verb %q\n", verb)
		return 1
	}
}

// resolveRegisteredBucket returns name's bucket dir from the <wiki-buckets>
// registry at indexPath. An unregistered name errors with the sorted list of
// registered names, so the caller's message is actionable.
func resolveRegisteredBucket(indexPath, name string) (string, error) {
	entries, err := readBucketEntries(indexPath)
	if err != nil {
		return "", fmt.Errorf("read bucket registry: %w", err)
	}

	for _, e := range entries {
		if e.Name == name {
			return e.Path, nil
		}
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("bucket %q is not registered — no buckets registered (run: atomic wiki bucket add <name>)", name)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("bucket %q is not registered — registered buckets: %s", name, strings.Join(names, ", "))
}

// errUsageRequested mirrors flag.ErrHelp: callers check it with errors.Is,
// print their own usage, and return 0 without touching the filesystem.
var errUsageRequested = errors.New("usage requested")

// resolveWikiRoot resolves --root (either spelling, any position) against cwd
// and returns the absolute root plus the remaining positionals. The hand-rolled
// scan exists because flag.FlagSet stops at the first non-flag token.
//
// A help token short-circuits with errUsageRequested. Any other dash-prefixed
// token is rejected: a bucket name can never begin with "-", so a flag typo
// must not silently become the name.
func resolveWikiRoot(args []string, cwd string) (absRoot string, positional []string, err error) {
	var root string
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "-h" || arg == "-help" || arg == "--help" {
			return "", nil, errUsageRequested
		}
		if arg == "--root" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("flag --root requires a value")
			}
			root = args[i+1]
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "--root=") {
			val := arg[len("--root="):]
			if val == "" {
				return "", nil, fmt.Errorf("flag --root requires a value")
			}
			root = val
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", nil, fmt.Errorf("unrecognized flag %q", arg)
		}
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

func wikiBucketAddAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket add [--root=<path>] <name>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, usage)
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

	// Registration validates the name and rejects a double-register, so it
	// gates every mutation below.
	if err := RegisterBucket(wikiDir, name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: create bucket dir: %v\n", err)
		return 1
	}

	if err := createBucketIndexStub(bucketDir, name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: create index stub: %v\n", err)
		return 1
	}

	if err := spliceBucketEntry(indexPath, name, bucketDir); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: splice block: %v\n", err)
		return 1
	}

	realmCLAUDE := filepath.Join(absRoot, "CLAUDE.md")
	if err := writeCaptureSurfacesSection(realmCLAUDE, name, bucketDir); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: write CLAUDE.md section: %v\n", err)
		return 1
	}

	return 0
}

// wikiBucketListAction prints one tab-separated line per bucket —
// name, path, file count or "(no baseline)", pending/fresh — and exits 0 even
// when there are none.
func wikiBucketListAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket list [--root=<path>]\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
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
			// Never promoted, so every content file is pending — but an empty
			// bucket is still fresh, which only the diff can tell us.
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

		// bucketDiffReadOnly never writes the current manifest: list is a
		// status verb and must leave no trace on disk.
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

// wikiBucketDiffAction prints "new|changed|removed <relpath>" lines and exits 1
// when any were printed.
func wikiBucketDiffAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket diff [--root=<path>] <name>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket diff: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, usage)
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

func wikiBucketPromoteAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket promote [--root=<path>] <name>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket promote: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, usage)
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

// parseBucketDocArgs peels off the valueless --router and delegates the rest to
// resolveWikiRoot, so flag and help classification has one implementation.
func parseBucketDocArgs(args []string, cwd string) (absRoot string, positional []string, router bool, err error) {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--router" {
			router = true
			continue
		}
		filtered = append(filtered, arg)
	}
	absRoot, positional, err = resolveWikiRoot(filtered, cwd)
	if err != nil {
		return "", nil, false, err
	}
	return absRoot, positional, router, nil
}

func wikiBucketDocAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket doc [--root=<path>] <bucket> <slug> [--router]\n"

	absRoot, positional, router, err := parseBucketDocArgs(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: %v\n", err)
		return 2
	}
	if len(positional) < 2 {
		fmt.Fprintf(os.Stderr, usage)
		return 1
	}
	if len(positional) > 2 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: unexpected extra arguments: %v\n", positional[2:])
		return 1
	}
	bucketName, slug := positional[0], positional[1]

	indexPath := filepath.Join(absRoot, "wiki", "index.md")
	bucketDir, err := resolveRegisteredBucket(indexPath, bucketName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: %v\n", err)
		return 1
	}

	path, err := ScaffoldBucketDoc(bucketDir, slug, router, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: %v\n", err)
		return 1
	}

	fmt.Fprintln(out, path)
	return 0
}

func wikiBucketSkillAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket skill [--root=<path>] <bucket>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: %v\n", err)
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, usage)
		return 1
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: unexpected extra arguments: %v\n", positional[1:])
		return 1
	}
	bucketName := positional[0]

	indexPath := filepath.Join(absRoot, "wiki", "index.md")
	bucketDir, err := resolveRegisteredBucket(indexPath, bucketName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: %v\n", err)
		return 1
	}

	// ScaffoldBucketSkill returns (path, nil) whether it wrote the file or found
	// it already there, so distinguish the two before calling.
	target := filepath.Join(absRoot, ".claude", "skills", bucketName+"-management", "SKILL.md")
	_, statErr := os.Lstat(target)
	alreadyExists := statErr == nil

	path, err := ScaffoldBucketSkill(absRoot, bucketName, bucketDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: %v\n", err)
		return 1
	}

	if alreadyExists {
		fmt.Fprintf(out, "already exists: %s\n", path)
		return 0
	}

	fmt.Fprintln(out, path)
	fmt.Fprintln(out, "note: this skill loads for Claude Code sessions started at the realm root")
	return 0
}

// printBucketIndexCounts writes one "<name>: N indexed, M unindexed" line,
// walking topics the way RebuildBucketIndex does. A walk failure warns but
// never blocks the caller's rebuild.
func printBucketIndexCounts(out io.Writer, name, bucketDir string) {
	topics, err := walkBucketTopics(bucketDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket index: count %s: %v\n", name, err)
		return
	}
	var indexed, unindexed int
	for _, t := range topics {
		if t.Indexed {
			indexed++
		} else {
			unindexed++
		}
	}
	fmt.Fprintf(out, "%s: %d indexed, %d unindexed\n", name, indexed, unindexed)
}

// wikiBucketIndexAction rebuilds every registered bucket, or just the named
// one. The single-bucket path refreshes only the realm <wiki-bucket-list>
// splice, never re-walking sibling <bucket-docs> regions, so a broken sibling
// is not re-reported. errUnpairedRegion warns and continues, as in Scan.
func wikiBucketIndexAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket index [--root=<path>] [<bucket>]\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd)
	if errors.Is(err, errUsageRequested) {
		fmt.Fprintf(out, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", err)
		return 2
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket index: unexpected extra arguments: %v\n", positional[1:])
		return 1
	}

	wikiDir := filepath.Join(absRoot, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")

	if len(positional) == 1 {
		name := positional[0]
		bucketDir, rerr := resolveRegisteredBucket(indexPath, name)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rerr)
			return 1
		}

		printBucketIndexCounts(out, name, bucketDir)

		if rebErr := RebuildBucketIndex(bucketDir); rebErr != nil {
			if errors.Is(rebErr, errUnpairedRegion) {
				fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rebErr)
			} else {
				fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rebErr)
				return 1
			}
		}

		entries, rerr := readBucketEntries(indexPath)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rerr)
			return 1
		}
		if rebErr := rebuildRealmBucketList(wikiDir, entries); rebErr != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rebErr)
		}

		return 0
	}

	entries, rerr := readBucketEntries(indexPath)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rerr)
		return 1
	}
	for _, e := range entries {
		if !fileExists(e.Path) {
			continue
		}
		printBucketIndexCounts(out, e.Name, e.Path)
	}

	if rebErr := RebuildAllBucketIndexes(absRoot, wikiDir); rebErr != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rebErr)
	}

	return 0
}

// wikiScanAction runs Scan, registers the wiki in ~/.claude/CLAUDE.md, and
// prints the deterministic stdout handoff.
func wikiScanAction(args []string, claudeHome, cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "root directory to scan (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if root == "" {
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: resolve root: %v\n", err)
		return 1
	}

	opts := Options{
		Clock: func() time.Time { return time.Now().UTC() },
	}

	members, err := Scan(absRoot, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}

	wikiDir := filepath.Join(absRoot, "wiki")

	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	wikiIndexPath := filepath.Join(wikiDir, "index.md")
	if err := RegisterWiki(claudeMDPath, wikiIndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: register wiki: %v\n", err)
		return 1
	}

	PrintHandoff(out, members)

	return 0
}
