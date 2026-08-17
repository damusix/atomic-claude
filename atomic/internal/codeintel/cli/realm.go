package cli

// Realm fan-out for `atomic code` verbs.

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

// RunCodeWithRealm resolves scope before touching repoctx: projectRoot may be a
// realm root, where repoctx.Resolve errors for want of a git repo. claudeMDPath
// supplies the <wikis> realm registrations.
func RunCodeWithRealm(args []string, projectRoot, claudeMDPath string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printCodeUsage(stderr)
		return 0
	}

	// A spawned daemon carries explicit --source/--db, so it must bypass realm
	// resolution entirely — otherwise the realm-verb-reject gate would kill it
	// based on whatever cwd it happened to inherit.
	if args[0] == "mcp" && containsFlag(args[1:], "daemon") {
		return RunCode(args, projectRoot, stdout, stderr, stdin)
	}

	res, err := realm.Resolve(projectRoot, claudeMDPath)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code: realm resolve: %v\n", err)
		return 1
	}

	switch res.Scope {
	case realm.ScopeRepo:
		// The resolver already verified a local index here, so the indexed root
		// is projectRoot — no need to consult git or the process cwd.
		return RunCode(args, projectRoot, stdout, stderr, stdin)

	case realm.ScopeNoIndex:
		// Resolve the git root so a subdir invocation still targets the whole
		// repo. projectRoot first, so --repo works from a non-git cwd; then cwd,
		// which covers a projectRoot whose toplevel sits above it.
		root, err := repoctx.Resolve(projectRoot)
		if err != nil {
			root, err = repoctx.Resolve("")
		}
		if err != nil {
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

// runRealmMember runs a verb against one member's keyed db. Output is not
// [key]-wrapped: there is only one target.
func runRealmMember(args []string, res realm.Resolution, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(res.Members) != 1 {
		fmt.Fprintf(stderr, "atomic code: realm member: unexpected member count %d\n", len(res.Members))
		return 1
	}
	m := res.Members[0]
	memberAbs := filepath.Join(res.RealmRoot, m.Path)
	dbPath := res.DBPath(m.Key)

	verb := args[0]

	// Routed through indexRealmAll because index must not write into the member
	// repo: no EnsureGitignore, db path supplied explicitly.
	if verb == "index" {
		return indexRealmAll(res.RealmRoot, []realm.MemberEntry{m}, args[1:], stdout, stderr)
	}

	// Explicit paths so the daemon serves the member's realm db without writing
	// into the member source tree.
	if verb == "mcp" {
		ctx := context.Background()
		return runMCP(ctx, memberAbs, dbPath, args[1:], stderr)
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

// runRealmAll seeds config if absent, then fans out across non-excluded members.
func runRealmAll(args []string, cwd string, res realm.Resolution, claudeMDPath string, stdout, stderr io.Writer, stdin io.Reader) int {
	verb := args[0]
	restArgs := args[1:]

	// A server binds to one tree and so cannot fan out. sync and status can:
	// each acts per member.
	switch verb {
	case "mcp":
		fmt.Fprintf(stderr, "atomic code mcp: not available in realm scope; pass --repo <member> to serve a specific repo\n")
		return 1
	}

	only, excl, cleanArgs := extractRealmFlags(restArgs)

	members, cfgRes, code := prepareMembers(verb, cwd, res, claudeMDPath, only, excl, stdout, stderr, stdin, cleanArgs)
	if code >= 0 {
		return code
	}

	if verb == "index" {
		code := indexRealmAll(cfgRes.RealmRoot, members, cleanArgs, stdout, stderr)
		// Written even on partial failure, so already-indexed members still
		// surface. Uses the full config membership rather than the CLI-filtered
		// slice: the block describes the realm, not this one invocation.
		if err := realm.WriteCodeIndexBlock(cfgRes.RealmRoot, cfgRes.Members); err != nil {
			fmt.Fprintf(stderr, "atomic code index: write <code-index> block: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
		return code
	}

	return fanOutQuery(verb, cleanArgs, members, cfgRes, stdout, stderr, stdin)
}

// prepareMembers resolves the fan-out member list, seeding code.toml when the
// verb is index. A returned exit code >= 0 means return it immediately.
func prepareMembers(
	verb, cwd string,
	res realm.Resolution,
	claudeMDPath string,
	only, excl []string,
	stdout, stderr io.Writer,
	stdin io.Reader,
	cleanArgs []string,
) ([]realm.MemberEntry, realm.Resolution, int) {
	if res.Config == nil && verb == "index" {
		wikiIndexPath := filepath.Join(res.RealmRoot, "wiki", "index.md")
		cfg, err := realm.SeedConfig(res.RealmRoot, wikiIndexPath)
		if err != nil {
			fmt.Fprintf(stderr, "atomic code index: seed config: %v\n", err)
			return nil, res, 1
		}
		if cfg == nil {
			// No <wiki-scan> block to seed from; a single-repo index at the
			// realm root is unusual but harmless.
			fmt.Fprintf(stderr, "atomic code index: no code.toml and no wiki/index.md with <wiki-scan> block; falling back to single-repo index at %s\n", cwd)
			return nil, res, RunCode(append([]string{"index"}, cleanArgs...), cwd, stdout, stderr, stdin)
		}
		res.Config = cfg
		res.Members = nonExcludedMembers(cfg.Members)
	} else if res.Config == nil {
		fmt.Fprintf(stderr, "atomic code %s: no realm config at %s/.atomic/code.toml — run `atomic code index` first\n", verb, res.RealmRoot)
		return nil, res, 1
	}

	members := filterMembers(res.Members, only, excl)
	return members, res, -1
}

// indexRealmAll indexes each member into <realm>/.atomic/<key>.db. A member
// that fails is warned about and skipped; the rest of the run continues.
func indexRealmAll(realmRoot string, members []realm.MemberEntry, extraArgs []string, stdout, stderr io.Writer) int {
	ctx := context.Background()
	overallOK := true

	for _, m := range members {
		memberAbs := filepath.Join(realmRoot, m.Path)
		// Never inside the member repo.
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

// fanOutQuery runs a non-index verb per member, skipping un-indexed ones.
// Human output puts each member under a [key] header; JSON output keys each
// member's raw output by member key.
func fanOutQuery(verb string, args []string, members []realm.MemberEntry, res realm.Resolution, stdout, stderr io.Writer, stdin io.Reader) int {
	asJSON := containsFlag(args, "json")

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

		// Stat first to skip building an engine for an absent db; IsInitialized
		// below then catches a db file left zero-byte by a failed run.
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

		var memberBuf bytes.Buffer
		memberStderr := &bytes.Buffer{}
		exitCode := dispatchVerb(ctx, verb, args, eng, memberAbs, &memberBuf, memberStderr, strings.NewReader(""))
		eng.Close()

		if memberStderr.Len() > 0 {
			fmt.Fprintf(stderr, "[%s] %s", m.Key, memberStderr.String())
		}

		if exitCode != 0 {
			overallCode = exitCode
		}

		if asJSON {
			raw := bytes.TrimSpace(memberBuf.Bytes())
			if len(raw) == 0 {
				raw = []byte("null")
			}
			jsonParts[m.Key] = json.RawMessage(raw)
		} else {
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

// dispatchVerb routes one verb to its runner against an already-built engine,
// returning an exit code rather than calling os.Exit.
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

// extractRealmFlags scans linearly rather than using flag.FlagSet, which stops
// at the first positional argument and so would miss a flag placed after a
// search query.
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

// filterMembers gives --only precedence: when set, --exclude is ignored.
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

// nonExcludedMembers mirrors realm.nonExcluded, which is unexported.
func nonExcludedMembers(members []realm.MemberEntry) []realm.MemberEntry {
	var out []realm.MemberEntry
	for _, m := range members {
		if !m.Exclude {
			out = append(out, m)
		}
	}
	return out
}

func containsFlag(args []string, flag string) bool {
	needle := "--" + flag
	for _, a := range args {
		if a == needle || strings.HasPrefix(a, needle+"=") {
			return true
		}
	}
	return false
}

// valueFlags always consume the next token even when it starts with '-', so
// `--depth -1` parses as a value rather than leaving "-1" positional.
var valueFlags = map[string]bool{
	"--depth":   true,
	"--limit":   true,
	"--only":    true,
	"--exclude": true,
	"-depth":    true,
	"-limit":    true,
}

// hoistFlags moves flags ahead of positionals so a verb runner's flag.FlagSet,
// which stops at the first non-flag argument, still sees flags the user typed
// after the query.
func hoistFlags(args []string) []string {
	var flags, positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if strings.Contains(a, "=") {
				flags = append(flags, a)
				i++
			} else if valueFlags[a] && i+1 < len(args) {
				flags = append(flags, a, args[i+1])
				i += 2
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// An unknown flag followed by a non-flag: assume it takes a value.
				flags = append(flags, a, args[i+1])
				i += 2
			} else {
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
