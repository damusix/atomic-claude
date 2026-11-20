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

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// ActionOptions provides realm-sharing resolution for wiki commands.
type ActionOptions struct {
	ResolveImplicitRoot      func(string) (string, error)
	ResolveRealmScaffoldRoot func(root string, explicit bool) (string, error)
	Buckets                  func(string) (BucketView, error)
	AddBucket                func(realmRoot, name string, prepare func(string) error, legacy func() error) error
}

// WikiAction dispatches `atomic wiki`. Only scan uses steering.
func WikiAction(args []string, home string, steering func() ([]string, error), cwd string, out io.Writer, options ...ActionOptions) int {
	var o ActionOptions
	if len(options) > 0 {
		o = options[0]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki <scan|stale|linkify|bucket|init> [flags]\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "scan":
		return o.scanAction(args[1:], home, steering, cwd, out)
	case "stale":
		return wikiStaleAction(args[1:], cwd, out)
	case "linkify":
		return wikiLinkifyAction(args[1:], cwd)
	case "bucket":
		return o.bucketAction(args[1:], cwd, out)
	case "init":
		return o.wikiInitAction(args[1:], cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki: unknown verb %q\n", verb)
		return 1
	}
}

func wikiAction(args []string, home string, steering func() ([]string, error), cwd string, out io.Writer) int {
	return WikiAction(args, home, steering, cwd, out)
}

func (o ActionOptions) implicitRoot(cwd string) (string, error) {
	if o.ResolveImplicitRoot == nil {
		return cwd, nil
	}
	return o.ResolveImplicitRoot(cwd)
}

func (o ActionOptions) resolveScaffoldRoot(root, cwd string, realm bool) (string, error) {
	explicit := root != ""
	if !explicit {
		root = cwd
		if realm && o.ResolveRealmScaffoldRoot == nil {
			var err error
			root, err = o.implicitRoot(cwd)
			if err != nil {
				return "", err
			}
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	if realm && o.ResolveRealmScaffoldRoot != nil {
		return o.ResolveRealmScaffoldRoot(absRoot, explicit)
	}
	return absRoot, nil
}

func (o ActionOptions) bucketView(root string) (BucketView, error) {
	if o.Buckets == nil {
		return BucketView{}, nil
	}
	return o.Buckets(root)
}

func (o ActionOptions) bucketEntries(root string) (BucketView, []BucketEntry, error) {
	view, err := o.bucketView(root)
	if err != nil {
		return view, nil, err
	}
	if view.Bound {
		return view, view.entries(), nil
	}
	indexPath := filepath.Join(root, "wiki", "index.md")
	if isBucketSymlink(indexPath) {
		return view, nil, nil
	}
	entries, err := readBucketEntries(indexPath)
	if err != nil {
		return view, nil, fmt.Errorf("read bucket registry: %w", err)
	}
	return view, resolveBucketEntries(root, entries), nil
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

// wikiInitAction implements `atomic wiki init --scope repo|realm [--root=<path>]`.
// A marker already declaring a different scope exits 1 with nothing touched.
func (o ActionOptions) wikiInitAction(args []string, cwd string, out io.Writer) int {
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

	absRoot, err := o.resolveScaffoldRoot(root, cwd, scope == "realm")
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki init: %v\n", err)
		return 1
	}

	var wikiDir string
	var sharedWiki bool
	if scope == "realm" {
		wikiDir = filepath.Join(absRoot, "wiki")
		sharedWiki, err = checkRealmWiki(wikiDir, o.Buckets)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki init: %v\n", err)
			return 1
		}
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

	var created []string
	switch scope {
	case "repo":
		created, err = InitRepoScope(absRoot)
	case "realm":
		if !sharedWiki {
			created, err = InitRealmScope(absRoot)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki init: %v\n", err)
		return 1
	}

	if markerOutcome == config.ScopeMarkerCreated || markerOutcome == config.ScopeMarkerAdded {
		fmt.Fprintf(out, "created %s\n", config.RepoConfigPath(absRoot))
	}
	if sharedWiki {
		fmt.Fprintf(out, "using existing shared wiki %s\n", wikiDir)
	}
	for _, path := range created {
		fmt.Fprintf(out, "created %s\n", path)
	}
	if scope != "repo" {
		return 0
	}
	stateRoot, err := refreshRepoCards(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki init: %v\n", err)
		return 1
	}
	// A target conflict leaves the canonical cards valid, so it only warns.
	if ProjectRepoCards != nil {
		if err := ProjectRepoCards(absRoot, stateRoot); err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki init: warning: %v\n", err)
		}
	}
	return 0
}

func (o ActionOptions) bucketAction(args []string, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki bucket <add|list|doc|skill|index> [--root=<path>] [args]\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "add":
		return o.bucketAddAction(args[1:], cwd, out)
	case "list":
		return o.bucketListAction(args[1:], cwd, out)
	case "doc":
		return o.bucketDocAction(args[1:], cwd, out)
	case "skill":
		return o.bucketSkillAction(args[1:], cwd, out)
	case "index":
		return o.bucketIndexAction(args[1:], cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki bucket: unknown verb %q\n", verb)
		return 1
	}
}

func resolveRegisteredBucket(entries []BucketEntry, name string) (string, error) {
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
func resolveWikiRoot(args []string, cwd string, implicit ...func(string) (string, error)) (absRoot string, positional []string, err error) {
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
		if len(implicit) > 0 {
			root, err = implicit[0](cwd)
			if err != nil {
				return "", nil, err
			}
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve root: %w", err)
	}
	return abs, positional, nil
}

func (o ActionOptions) bucketAddAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket add [--root=<path>] <name>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd, o.implicitRoot)
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

	legacy := func() error { return addLegacyBucket(absRoot, name) }
	if o.AddBucket == nil {
		err = legacy()
	} else {
		err = o.AddBucket(absRoot, name, func(bucketDir string) error {
			if err := prepareBucketDirectory(bucketDir, name); err != nil {
				return err
			}
			return rebuildAddedBucket(bucketDir)
		}, legacy)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %v\n", err)
		return 1
	}
	return 0
}

func addLegacyBucket(absRoot, name string) error {
	wikiDir := filepath.Join(absRoot, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")
	bucketDir := filepath.Join(absRoot, name)

	// Registration validates the name and rejects a double-register, so it
	// gates every mutation below.
	if err := RegisterBucket(wikiDir, name); err != nil {
		return err
	}

	if err := prepareBucketDirectory(bucketDir, name); err != nil {
		return err
	}

	if err := spliceBucketEntry(indexPath, name, bucketDir); err != nil {
		return fmt.Errorf("splice block: %w", err)
	}

	mode, err := config.ResolveSteeringMode(config.ProjectHome(), absRoot)
	if err != nil {
		return err
	}
	steering := captureSurfacesFile(absRoot, mode)
	if filepath.Base(steering) == bundlespec.ScopeSteering.Source && mode == bundlespec.SteeringLoader {
		if loader := bundlespec.ScopeSteering.LoaderWithoutImport(absRoot); loader != "" {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %s needs an `@AGENTS.md` line for Claude to load the capture section\n", loader)
		} else if _, _, err := initSteeringPair(absRoot, "", mode); err != nil {
			return fmt.Errorf("write CLAUDE.md loader: %w", err)
		}
	}
	if err := writeCaptureSurfacesSection(steering, name, bucketDir); err != nil {
		return fmt.Errorf("write %s section: %w", filepath.Base(steering), err)
	}

	return rebuildAddedBucket(bucketDir)
}

func prepareBucketDirectory(bucketDir, name string) error {
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		return fmt.Errorf("create bucket dir: %w", err)
	}
	if err := createBucketIndexStub(bucketDir, name); err != nil {
		return fmt.Errorf("create index stub: %w", err)
	}
	return nil
}

func rebuildAddedBucket(bucketDir string) error {
	_, errs := RebuildBucketTree(bucketDir)
	var hard []error
	for _, err := range errs {
		if errors.Is(err, errUnpairedRegion) {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket add: %v\n", err)
		} else {
			hard = append(hard, err)
		}
	}
	return errors.Join(hard...)
}

// reportTreeErrors prints each error on its own line and reports whether any
// is worse than an unpaired region, which only warns.
func reportTreeErrors(verb string, errs []error) (hard bool) {
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "%s: %v\n", verb, err)
		if !errors.Is(err, errUnpairedRegion) {
			hard = true
		}
	}
	return hard
}

func (o ActionOptions) bucketListAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket list [--root=<path>]\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd, o.implicitRoot)
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

	_, entries, err := o.bucketEntries(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket list: %v\n", err)
		return 1
	}
	for _, e := range entries {
		fmt.Fprintf(out, "%s\t%s\n", e.Name, e.Path)
	}
	return 0
}

func (o ActionOptions) bucketDocAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket doc [--root=<path>] <bucket> <slug>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd, o.implicitRoot)
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

	_, entries, err := o.bucketEntries(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: %v\n", err)
		return 1
	}
	bucketDir, err := resolveRegisteredBucket(entries, bucketName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: %v\n", err)
		return 1
	}

	path, err := ScaffoldBucketDoc(bucketDir, slug, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket doc: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, path)
	if _, errs := RebuildBucketTree(bucketDir); reportTreeErrors("atomic wiki bucket doc", errs) {
		return 1
	}
	return 0
}

func (o ActionOptions) bucketSkillAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket skill [--root=<path>] <bucket>\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd, o.implicitRoot)
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

	view, entries, err := o.bucketEntries(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: %v\n", err)
		return 1
	}
	bucketDir, err := resolveRegisteredBucket(entries, bucketName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: %v\n", err)
		return 1
	}

	// ScaffoldBucketSkill returns (path, nil) whether it wrote the file or found
	// it already there, so distinguish the two before calling.
	target := filepath.Join(absRoot, ".claude", "skills", bucketName+"-management", "SKILL.md")
	_, statErr := os.Lstat(target)
	alreadyExists := statErr == nil
	if indexPath := filepath.Join(bucketDir, "index.md"); view.Bound && !alreadyExists && isBucketSymlink(indexPath) {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket skill: refusing to read symlink %s\n", indexPath)
		return 1
	}

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

func printBucketIndexCounts(out io.Writer, name string, counts TopicCounts) {
	fmt.Fprintf(out, "%s: %d indexed, %d unindexed\n", name, counts.Indexed, counts.Unindexed)
}

func (o ActionOptions) bucketIndexAction(args []string, cwd string, out io.Writer) int {
	const usage = "Usage: atomic wiki bucket index [--root=<path>] [<bucket>]\n"

	absRoot, positional, err := resolveWikiRoot(args, cwd, o.implicitRoot)
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
	view, entries, err := o.bucketEntries(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", err)
		return 1
	}

	if len(positional) == 1 {
		name := positional[0]
		bucketDir, rerr := resolveRegisteredBucket(entries, name)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", rerr)
			return 1
		}

		counts, rebErrs := RebuildBucketTree(bucketDir)
		printBucketIndexCounts(out, name, counts)
		if reportTreeErrors("atomic wiki bucket index", rebErrs) {
			return 1
		}

		if !view.Bound {
			if err := rebuildRealmBucketList(wikiDir, entries); err != nil {
				fmt.Fprintf(os.Stderr, "atomic wiki bucket index: %v\n", err)
			}
		}
		return 0
	}

	results, rebErrs := rebuildBucketTrees(entries, "")
	if !view.Bound {
		if err := rebuildRealmBucketList(wikiDir, entries); err != nil {
			rebErrs = append(rebErrs, err)
		}
	}
	for _, e := range entries {
		if res, ok := results[e.Name]; ok {
			printBucketIndexCounts(out, e.Name, res)
		}
	}
	reportTreeErrors("atomic wiki bucket index", rebErrs)

	return 0
}

func (o ActionOptions) scanAction(args []string, home string, steering func() ([]string, error), cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "root directory to scan (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	absRoot, err := o.resolveScaffoldRoot(root, cwd, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}

	view, err := o.bucketView(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}

	opts := Options{
		Clock:   func() time.Time { return time.Now().UTC() },
		buckets: view,
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

	paths, err := steering()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: read enrolled targets: %v\n", err)
		return 1
	}
	wikiIndexPath := filepath.Join(wikiDir, "index.md")
	if err := RegisterAndProject(home, wikiIndexPath, paths); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}
	if err := reportProjectionOnlyEntries(home, paths); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}

	PrintHandoff(out, members)

	return 0
}

// reportProjectionOnlyEntries warns on stderr about each steering-file entry
// the authority does not register; the entry stays where it is.
func reportProjectionOnlyEntries(home string, steering []string) error {
	registry := NewWikiRegistry(home)
	authority, err := registry.Load()
	if err != nil {
		return err
	}
	for _, path := range steering {
		extra, err := ProjectionOnlyEntries(path, authority)
		if err != nil {
			return err
		}
		for _, entry := range extra {
			fmt.Fprintf(os.Stderr, "atomic wiki scan: warning: %s <wikis> block diverges from %s: %s is not registered there; left in place, not imported\n", path, registry.Path(), entry)
		}
	}
	return nil
}
