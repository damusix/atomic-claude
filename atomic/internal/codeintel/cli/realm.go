package cli

// realm.go — CP3: realm fan-out orchestrator for `atomic code` verbs.
//
// RunCodeWithRealm is the new entry point called by main.go:runCode.  It
// resolves the scope via realm.Resolve BEFORE calling repoctx.Resolve (which
// errors at a realm root that has no git repo).  Repo-scope and ScopeNoIndex
// paths are forwarded to the existing RunCode unchanged, preserving SC 2.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
)

// RunCodeWithRealm is the scope-aware entry point for `atomic code` verbs.
//
//   - projectRoot is the cwd (resolved by main.go before the call — note: this
//     may be a realm root, so we MUST NOT call repoctx.Resolve before branching).
//   - claudeMDPath is the path to ~/.claude/CLAUDE.md used to find <wikis> realm registrations.
//
// Repo-scope (local index present) and ScopeNoIndex (not in a realm) forward to
// RunCode unchanged so the single-repo path is byte-for-byte identical (SC 2).
func RunCodeWithRealm(args []string, projectRoot, claudeMDPath string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printCodeUsage(stderr)
		return 0
	}

	res, err := realm.Resolve(projectRoot, claudeMDPath)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code: realm resolve: %v\n", err)
		return 1
	}

	switch res.Scope {
	case realm.ScopeRepo:
		// A local index exists at projectRoot (the resolver verified
		// <projectRoot>/.claude/.atomic-index/atomic.db). Query it directly — the
		// indexed root IS projectRoot, so there is no need to consult git or the
		// process cwd. main.go passes os.Getwd() as projectRoot, so this is
		// byte-for-byte today's single-repo behavior (SC 2), and it does not depend
		// on the process working directory (which made this path untestable before).
		return RunCode(args, projectRoot, stdout, stderr, stdin)

	case realm.ScopeNoIndex:
		// No local index at projectRoot and not under a realm. Resolve the git root
		// of projectRoot so a subdir invocation targets the whole repo: a query from
		// a subdir of an indexed repo must find the git-root index, and
		// `atomic code index` from a subdir must index the git root. projectRoot ==
		// cwd in production (main.go passes os.Getwd), so repoctx.Resolve("") runs
		// `git rev-parse --show-toplevel` against the right tree, exactly as today's
		// single-repo path does (SC 2).
		root, err := repoctx.Resolve("")
		if err != nil {
			// Not inside a git repo (and not a realm) — surface the same error the
			// user would have seen before CP3.
			fmt.Fprintf(stderr, "atomic code: %v\n", err)
			return 1
		}
		return RunCode(args, root, stdout, stderr, stdin)

	case realm.ScopeRealmMember:
		return runRealmMember(args, res, stdout, stderr, stdin)

	case realm.ScopeRealmAll:
		return runRealmAll(args, projectRoot, res, claudeMDPath, stdout, stderr, stdin)

	default:
		root, err := repoctx.Resolve("")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code: %v\n", err)
			return 1
		}
		return RunCode(args, root, stdout, stderr, stdin)
	}
}

// ─── ScopeRealmMember ────────────────────────────────────────────────────────

// runRealmMember runs a verb against the single matched member's keyed db.
// Output is not [key]-wrapped (single target — matches spec "returns only foo's results").
func runRealmMember(args []string, res realm.Resolution, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(res.Members) != 1 {
		fmt.Fprintf(stderr, "atomic code: realm member: unexpected member count %d\n", len(res.Members))
		return 1
	}
	m := res.Members[0]
	memberAbs := filepath.Join(res.RealmRoot, m.Path)
	dbPath := res.DBPath(m.Key)

	verb := args[0]

	// index in realm-member scope must not write into the member repo (SC 3).
	// Route through indexRealmAll which uses NewWithDBPath + no EnsureGitignore.
	if verb == "index" {
		return indexRealmAll(res.RealmRoot, []realm.MemberEntry{m}, args[1:], stdout, stderr)
	}

	// mcp / __serve start a long-lived server over a whole tree; they have no
	// per-member meaning. sync and status DO operate on a single member — against
	// its realm db (the NewWithDBPath engine below), writing nothing into the
	// member repo — so they fall through to dispatchVerb like the query verbs.
	if verb == "mcp" || verb == "__serve" {
		fmt.Fprintf(stderr, "atomic code %s: not available in realm-member scope; run from a standalone repo\n", verb)
		return 1
	}

	eng, err := engine.NewWithDBPath(memberAbs, dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code: create engine for %s: %v\n", m.Key, err)
		return 1
	}
	defer eng.Close()

	ctx := context.Background()
	return dispatchVerb(ctx, verb, args[1:], eng, memberAbs, stdout, stderr, stdin)
}

// ─── ScopeRealmAll ───────────────────────────────────────────────────────────

// runRealmAll handles the realm-root cwd case: seed config if absent, then
// fan out across non-excluded members.
func runRealmAll(args []string, cwd string, res realm.Resolution, claudeMDPath string, stdout, stderr io.Writer, stdin io.Reader) int {
	verb := args[0]
	restArgs := args[1:]

	// Server verbs bind to a single tree and cannot fan out across members.
	// sync and status are NOT in this set — they fan out per member like the
	// query verbs (sync updates each member's realm db; status reports each).
	switch verb {
	case "mcp", "__serve":
		fmt.Fprintf(stderr, "atomic code %s: not available in realm scope; cd into a member repo or pass --repo <member>\n", verb)
		return 1
	}

	// Strip --only/--exclude from restArgs before passing them to verb runners.
	only, excl, cleanArgs := extractRealmFlags(restArgs)

	// Determine the target member set.
	members, cfgRes, code := prepareMembers(verb, cwd, res, claudeMDPath, only, excl, stdout, stderr, stdin, cleanArgs)
	if code >= 0 {
		return code
	}

	// Verb is "index": fan out over the filtered member set (respects --only/--exclude),
	// then write the <code-index> awareness block into the realm CLAUDE.md (SC 7).
	if verb == "index" {
		code := indexRealmAll(cfgRes.RealmRoot, members, cleanArgs, stdout, stderr)
		// Write the <code-index> block even on partial failure — membership that was
		// already indexed should still surface for Claude awareness.
		// Pass the config-non-excluded set (cfgRes.Members), NOT the CLI-filtered
		// members slice, so the block always reflects full realm membership regardless
		// of --only/--exclude flags on this invocation (SC 7).
		if err := realm.WriteCodeIndexBlock(cfgRes.RealmRoot, cfgRes.Members); err != nil {
			fmt.Fprintf(stderr, "atomic code index: write <code-index> block: %v\n", err)
			// Non-fatal: the index itself succeeded; only awareness wiring failed.
			if code == 0 {
				code = 1
			}
		}
		return code
	}

	// Fan-out query verbs.
	return fanOutQuery(verb, cleanArgs, members, cfgRes, stdout, stderr, stdin)
}

// prepareMembers resolves the member list for fan-out, seeding code.toml if
// absent (for the index verb only).  Returns (members, resolution, earlyExit)
// where earlyExit >= 0 means return that code immediately.
func prepareMembers(
	verb, cwd string,
	res realm.Resolution,
	claudeMDPath string,
	only, excl []string,
	stdout, stderr io.Writer,
	stdin io.Reader,
	cleanArgs []string,
) ([]realm.MemberEntry, realm.Resolution, int) {
	// If code.toml is absent and this is the index verb, seed it first.
	if res.Config == nil && verb == "index" {
		wikiIndexPath := filepath.Join(res.RealmRoot, "wiki", "index.md")
		cfg, err := realm.SeedConfig(res.RealmRoot, wikiIndexPath)
		if err != nil {
			fmt.Fprintf(stderr, "atomic code index: seed config: %v\n", err)
			return nil, res, 1
		}
		if cfg == nil {
			// No wiki-scan block — cannot seed. Run index at cwd directly.
			// Fall back to single-repo index at the realm root (unusual but safe).
			fmt.Fprintf(stderr, "atomic code index: no code.toml and no wiki/index.md with <wiki-scan> block; falling back to single-repo index at %s\n", cwd)
			return nil, res, RunCode(append([]string{"index"}, cleanArgs...), cwd, stdout, stderr, stdin)
		}
		// Reload resolution with seeded config.
		res.Config = cfg
		res.Members = nonExcludedMembers(cfg.Members)
	} else if res.Config == nil {
		// Query verb with no config — no members to fan out.
		fmt.Fprintf(stderr, "atomic code %s: no realm config at %s/.atomic/code.toml — run `atomic code index` first\n", verb, res.RealmRoot)
		return nil, res, 1
	}

	// Apply --only/--exclude filters.
	members := filterMembers(res.Members, only, excl)
	return members, res, -1 // -1 = no early exit
}

// indexRealmAll indexes each member in the provided (already-filtered) list,
// storing each db at <realm>/.atomic/<key>.db.  Partial failure: a member that
// fails to index gets a warning line; the run continues.
//
// members is the --only/--exclude-filtered slice from prepareMembers so that
// index respects the same filters as query verbs (SC 5).
func indexRealmAll(realmRoot string, members []realm.MemberEntry, extraArgs []string, stdout, stderr io.Writer) int {
	ctx := context.Background()
	overallOK := true

	for _, m := range members {
		memberAbs := filepath.Join(realmRoot, m.Path)
		// DB lives at <realm>/.atomic/<key>.db, never inside the member repo (SC 3).
		dbPath := filepath.Join(realmRoot, ".atomic", m.Key+".db")

		eng, err := engine.NewWithDBPath(memberAbs, dbPath)
		if err != nil {
			fmt.Fprintf(stderr, "[%s] create engine: %v (skipping)\n", m.Key, err)
			overallOK = false
			continue
		}

		fmt.Fprintf(stdout, "[%s] indexing %s…\n", m.Key, memberAbs)

		if err := eng.Init(ctx); err != nil {
			fmt.Fprintf(stderr, "[%s] init: %v (skipping)\n", m.Key, err)
			eng.Close()
			overallOK = false
			continue
		}
		if err := eng.IndexAll(ctx); err != nil {
			fmt.Fprintf(stderr, "[%s] index: %v (skipping)\n", m.Key, err)
			eng.Close()
			overallOK = false
			continue
		}
		if _, fwErr := eng.ExtractFrameworkNodes(ctx); fwErr != nil {
			fmt.Fprintf(stderr, "[%s] framework nodes: %v (non-fatal)\n", m.Key, fwErr)
		}
		if err := eng.ResolveReferences(ctx); err != nil {
			fmt.Fprintf(stderr, "[%s] resolve refs: %v (non-fatal)\n", m.Key, err)
		}
		stats, err := eng.GetStats(ctx)
		if err == nil {
			fmt.Fprintf(stdout, "[%s] indexed: %d files, %d nodes, %d edges\n",
				m.Key, stats.FileCount, stats.NodeCount, stats.EdgeCount)
		}
		eng.Close()
	}

	if !overallOK {
		return 1
	}
	return 0
}

// fanOutQuery runs a non-index verb across the filtered member list — the
// query verbs plus sync/status (sync mutates each member's realm db; both are
// safe per-member). Un-indexed members are skipped with a clear message.
// Human output: each member under a [key] header.
// JSON output: {key: raw_json, ...} assembled from each member's captured output.
func fanOutQuery(verb string, args []string, members []realm.MemberEntry, res realm.Resolution, stdout, stderr io.Writer, stdin io.Reader) int {
	// Detect --json in args for output assembly.
	asJSON := containsFlag(args, "json")

	// Ensure flag args are placed before positional args so that the verb
	// runners' flag.FlagSet (which stops at the first non-flag argument) can
	// see all flags regardless of the order they were passed by the user.
	args = hoistFlags(args)

	ctx := context.Background()
	overallCode := 0

	var jsonParts map[string]json.RawMessage
	if asJSON {
		jsonParts = make(map[string]json.RawMessage, len(members))
	}

	for _, m := range members {
		memberAbs := filepath.Join(res.RealmRoot, m.Path)
		dbPath := res.DBPath(m.Key)

		// Two-phase check: os.Stat avoids creating the engine just to discover the
		// db is absent (fast path); IsInitialized catches a db file that exists but
		// has not been initialised (e.g. zero-byte or schema-empty from a prior
		// failed run).  Both are legitimate "not indexed" signals.
		if _, err := os.Stat(dbPath); err != nil {
			fmt.Fprintf(stderr, "[%s] not indexed — run `atomic code index` first\n", m.Key)
			continue
		}

		eng, err := engine.NewWithDBPath(memberAbs, dbPath)
		if err != nil {
			fmt.Fprintf(stderr, "[%s] create engine: %v (skipping)\n", m.Key, err)
			continue
		}

		if !eng.IsInitialized() {
			fmt.Fprintf(stderr, "[%s] not indexed — run `atomic code index` first\n", m.Key)
			eng.Close()
			continue
		}

		// Capture member output into a buffer.
		var memberBuf bytes.Buffer
		memberStderr := &bytes.Buffer{}
		exitCode := dispatchVerb(ctx, verb, args, eng, memberAbs, &memberBuf, memberStderr, strings.NewReader(""))
		eng.Close()

		// Surface member stderr.
		if memberStderr.Len() > 0 {
			fmt.Fprintf(stderr, "[%s] %s", m.Key, memberStderr.String())
		}

		if exitCode != 0 {
			overallCode = exitCode
		}

		if asJSON {
			// Capture raw JSON from member; wrap as RawMessage in the keyed map.
			raw := bytes.TrimSpace(memberBuf.Bytes())
			if len(raw) == 0 {
				raw = []byte("null")
			}
			jsonParts[m.Key] = json.RawMessage(raw)
		} else {
			// Human: prefix with [key] header.
			fmt.Fprintf(stdout, "[%s]\n", m.Key)
			if memberBuf.Len() > 0 {
				stdout.Write(memberBuf.Bytes()) //nolint:errcheck
				// Ensure trailing newline.
				if !bytes.HasSuffix(memberBuf.Bytes(), []byte("\n")) {
					fmt.Fprintln(stdout)
				}
			}
		}
	}

	if asJSON {
		enc, err := json.MarshalIndent(jsonParts, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code: marshal realm JSON: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	}

	return overallCode
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// dispatchVerb routes a single verb to the appropriate runner, given an already
// constructed engine.  It does not call os.Exit.
func dispatchVerb(ctx context.Context, verb string, args []string, eng *engine.Engine, projectRoot string, stdout, stderr io.Writer, stdin io.Reader) int {
	switch verb {
	case "index":
		return runIndex(ctx, eng, args, projectRoot, stdout, stderr)
	case "sync":
		return runSync(ctx, eng, args, stdout, stderr)
	case "status":
		return runStatus(ctx, eng, args, projectRoot, stdout, stderr)
	case "search":
		return runSearch(ctx, eng, args, stdout, stderr)
	case "callers":
		return runCallers(ctx, eng, args, stdout, stderr)
	case "callees":
		return runCallees(ctx, eng, args, stdout, stderr)
	case "impact":
		return runImpact(ctx, eng, args, stdout, stderr)
	case "node":
		return runNode(ctx, eng, args, stdout, stderr)
	case "files":
		return runFiles(ctx, eng, args, stdout, stderr)
	case "affected":
		return runAffected(ctx, eng, args, stdin, stdout, stderr)
	case "explore":
		return runExplore(ctx, eng, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "atomic code: unknown verb %q\n", verb)
		printCodeUsage(stderr)
		return 1
	}
}

// extractRealmFlags scans args linearly to pull --only and --exclude values,
// returning the values and the cleaned args (with those flags removed).
//
// We cannot use flag.FlagSet here because it stops parsing at the first
// positional argument (e.g. a search query string), so flags appearing after
// the query would be invisible.  Linear scanning handles any order.
func extractRealmFlags(args []string) (only, excl []string, clean []string) {
	var onlyParts, exclParts []string
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--only" && i+1 < len(args):
			onlyParts = append(onlyParts, args[i+1])
			i += 2
		case strings.HasPrefix(a, "--only="):
			onlyParts = append(onlyParts, strings.TrimPrefix(a, "--only="))
			i++
		case a == "--exclude" && i+1 < len(args):
			exclParts = append(exclParts, args[i+1])
			i += 2
		case strings.HasPrefix(a, "--exclude="):
			exclParts = append(exclParts, strings.TrimPrefix(a, "--exclude="))
			i++
		default:
			clean = append(clean, a)
			i++
		}
	}
	for _, v := range onlyParts {
		only = append(only, splitComma(v)...)
	}
	for _, v := range exclParts {
		excl = append(excl, splitComma(v)...)
	}
	return only, excl, clean
}

// filterMembers applies --only and --exclude to the member list.
// --only takes precedence: if set, --exclude is ignored.
func filterMembers(members []realm.MemberEntry, only, excl []string) []realm.MemberEntry {
	if len(only) > 0 {
		onlySet := make(map[string]bool, len(only))
		for _, k := range only {
			onlySet[strings.TrimSpace(k)] = true
		}
		var out []realm.MemberEntry
		for _, m := range members {
			if onlySet[m.Key] {
				out = append(out, m)
			}
		}
		return out
	}
	if len(excl) > 0 {
		exclSet := make(map[string]bool, len(excl))
		for _, k := range excl {
			exclSet[strings.TrimSpace(k)] = true
		}
		var out []realm.MemberEntry
		for _, m := range members {
			if !exclSet[m.Key] {
				out = append(out, m)
			}
		}
		return out
	}
	return members
}

// nonExcludedMembers returns the members where Exclude == false.
// Mirrors realm.nonExcluded but accessible here.
func nonExcludedMembers(members []realm.MemberEntry) []realm.MemberEntry {
	var out []realm.MemberEntry
	for _, m := range members {
		if !m.Exclude {
			out = append(out, m)
		}
	}
	return out
}

// containsFlag returns true when "--<flag>" appears in args (with or without value).
func containsFlag(args []string, flag string) bool {
	needle := "--" + flag
	for _, a := range args {
		if a == needle || strings.HasPrefix(a, needle+"=") {
			return true
		}
	}
	return false
}

// valueFlags is the set of flags that always consume the next token as their
// value, regardless of whether that token starts with '-'.  This is required
// so that `--depth -1` is parsed as (flag=depth, value=-1) rather than
// leaving "-1" in the positional list.
var valueFlags = map[string]bool{
	"--depth":   true,
	"--limit":   true,
	"--only":    true,
	"--exclude": true,
	"-depth":    true,
	"-limit":    true,
}

// hoistFlags reorders args so that flag arguments (--foo, --foo=val, --foo val)
// appear before positional arguments.  This lets verb runners' flag.FlagSet
// (which stops at the first non-flag argument) see all flags regardless of
// the original user-supplied order (e.g. "search Hello --json").
//
// Known value-taking flags (depth, limit, only, exclude) always consume the
// next token, even when it starts with '-' (e.g. --depth -1).
func hoistFlags(args []string) []string {
	var flags, positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if strings.Contains(a, "=") {
				// --flag=val: self-contained.
				flags = append(flags, a)
				i++
			} else if valueFlags[a] && i+1 < len(args) {
				// Known value flag: always consume the next token as value,
				// even if it looks like a flag (e.g. --depth -1).
				flags = append(flags, a, args[i+1])
				i += 2
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Unknown flag whose next token is not a flag: hoist as pair.
				flags = append(flags, a, args[i+1])
				i += 2
			} else {
				// Boolean flag or last arg.
				flags = append(flags, a)
				i++
			}
		} else {
			positional = append(positional, a)
			i++
		}
	}
	return append(flags, positional...)
}

// splitComma splits a comma-separated string into a trimmed slice.
func splitComma(s string) []string {
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
