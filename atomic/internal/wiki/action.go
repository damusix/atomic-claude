package wiki

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// exit 0 no drift / 1 drift / 2 hard error.
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

func wikiBucketAction(args []string, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket <add|list|doc|skill|index> [--root=<path>] [args]\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "add":
		return wikiBucketAddAction(args[1:], cwd, out)
	case "list":
		return wikiBucketListAction(args[1:], cwd, out)
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

// wikiBucketListAction prints one "<name>\t<path>" line per registered bucket
// and exits 0 even when there are none.
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
	for _, e := range entries {
		fmt.Fprintf(out, "%s\t%s\n", e.Name, e.Path)
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

// wikiScanAction runs Scan, declares the realm scope marker, registers the wiki
// in ~/.claude/CLAUDE.md, and prints the deterministic stdout handoff.
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

	switch markerOutcome, err := config.EnsureScopeMarker(absRoot, "realm"); {
	case err != nil:
		fmt.Fprintf(os.Stderr, "atomic wiki scan: warning: scope marker not written: %v\n", err)
	case markerOutcome == config.ScopeMarkerConflict:
		fmt.Fprintf(os.Stderr, "atomic wiki scan: warning: %s already declares a different scope — left unchanged\n", config.RepoConfigPath(absRoot))
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
