// Package cliusage exposes the atomic command surface as data, so the validate
// artifacts rule can check citations against the same tree Cobra renders help
// from. SetRoot derives it live; the hardcoded slice below is the golden
// fixture and the fallback for tests that never call SetRoot.
package cliusage

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Command is one entry in the atomic command surface. Flags carry their
// leading "--"; universal flags (--help, --repo) are omitted because every
// command accepts them.
type Command struct {
	Path        []string
	Args        string
	Flags       []string
	Description string
}

// commands is replaced by SetRoot in production. The literal below must stay in
// sync with the Cobra tree — TestDeriveCommandsGolden enforces it.
var commands = []Command{
	{
		Path:        []string{"bus", "join"},
		Args:        "<room>",
		Flags:       []string{"--as", "--mode", "--kind", "--session"},
		Description: "Join a room under a name; auto-spawns the daemon",
	},
	{
		Path:        []string{"bus", "leave"},
		Args:        "[<room>]",
		Flags:       []string{"--session"},
		Description: "Leave a room (default: the session's last-joined room)",
	},
	{
		Path:        []string{"bus", "send"},
		Args:        "<room> <text>",
		Flags:       []string{"--to", "--reply-to", "--session", "--json"},
		Description: "Send a message; text \"-\" reads stdin",
	},
	{
		Path:        []string{"bus", "recv"},
		Args:        "<room>",
		Flags:       []string{"--json", "--session"},
		Description: "Receive messages; streams JSON envelopes until SIGTERM",
	},
	{
		Path:        []string{"bus", "who"},
		Args:        "[<room>]",
		Flags:       []string{"--json"},
		Description: "List a room's members (default: the session's last-joined room)",
	},
	{
		Path:        []string{"bus", "rooms"},
		Args:        "",
		Flags:       []string{"--json"},
		Description: "List every room the daemon knows about",
	},
	{
		Path:        []string{"bus", "status"},
		Args:        "",
		Flags:       []string{"--json", "--session"},
		Description: "Report this session's joined rooms and the daemon's state",
	},
	{
		Path:        []string{"bus", "serve"},
		Args:        "",
		Flags:       nil,
		Description: "Run the daemon in the foreground; stopped via bus stop",
	},
	{
		Path:        []string{"bus", "start"},
		Args:        "",
		Flags:       nil,
		Description: "Spawn the daemon if none is listening; idempotent",
	},
	{
		Path:        []string{"bus", "stop"},
		Args:        "",
		Flags:       nil,
		Description: "Stop a running daemon; exit 0 if none is running",
	},
	{
		Path:        []string{"bus", "restart"},
		Args:        "",
		Flags:       nil,
		Description: "Stop then start the daemon; the version-skew remedy",
	},
	{
		Path:        []string{"bus", "tail"},
		Args:        "[<room>]",
		Flags:       []string{"--all-rooms", "--json", "--only-addressed", "--from"},
		Description: "Watch a room's traffic without joining; never appears in who",
	},
	{
		Path:        []string{"bus", "say"},
		Args:        "<room> <text>",
		Flags:       []string{"--to"},
		Description: "Send a one-shot human message without joining; always passes, even halted",
	},
	{
		Path:        []string{"bus", "read"},
		Args:        "<room> <msg-id>",
		Flags:       []string{"--json"},
		Description: "Print one message's full text from the room log; no daemon needed",
	},
	{
		Path:        []string{"bus", "halt"},
		Args:        "<room>",
		Flags:       []string{"--text"},
		Description: "Stop a room: agent send fails with exit 7 until resume",
	},
	{
		Path:        []string{"bus", "resume"},
		Args:        "<room>",
		Flags:       nil,
		Description: "Clear a room's halt flag; restores agent send",
	},
	{
		Path:        []string{"bus", "prune"},
		Args:        "[<room>]",
		Flags:       []string{"--json"},
		Description: "Remove stale members (no live subscription, no recent activity) from a room",
	},
	{
		Path:        []string{"bus", "close"},
		Args:        "<room>",
		Flags:       nil,
		Description: "Publish a closing envelope, evict every member, and drop the room; owner-requested, no session required",
	},
	{
		Path:        []string{"bus", "chat"},
		Args:        "<room>",
		Flags:       []string{"--as", "--session"},
		Description: "Interactive client: joins as a human member; @name, /who, /rooms, /halt, /resume, /quit",
	},
	{
		Path:        []string{"claude", "install"},
		Args:        "",
		Flags:       []string{"--dry-run", "--target", "--no-hooks"},
		Description: "Install artifact bundle",
	},
	{
		Path:        []string{"claude", "update"},
		Args:        "",
		Flags:       []string{"--dry-run", "--target", "--no-hooks"},
		Description: "Update artifact bundle",
	},
	{
		Path:        []string{"claude", "list"},
		Args:        "",
		Flags:       nil,
		Description: "List bundled artifacts",
	},
	{
		Path:        []string{"claude", "diff"},
		Args:        "",
		Flags:       []string{"--target"},
		Description: "Diff bundle vs on-disk",
	},
	{
		Path:        []string{"claude", "uninstall"},
		Args:        "",
		Flags:       []string{"--target"},
		Description: "Generate uninstall prompt",
	},
	{
		Path:        []string{"config", "get"},
		Args:        "<key>",
		Flags:       nil,
		Description: "Print resolved config value",
	},
	{
		Path:        []string{"config", "set"},
		Args:        "<key> <val>",
		Flags:       nil,
		Description: "Set config value",
	},
	{
		Path:        []string{"config", "unset"},
		Args:        "<key>",
		Flags:       nil,
		Description: "Revert key to built-in default",
	},
	{
		Path:        []string{"config", "list"},
		Args:        "",
		Flags:       []string{"--json"},
		Description: "List all resolved key=value pairs",
	},
	{
		Path:        []string{"config", "path"},
		Args:        "",
		Flags:       nil,
		Description: "Print path to config.toml",
	},
	{
		Path:        []string{"config", "agents"},
		Args:        "",
		Flags:       nil,
		Description: "Set per-agent model tiers interactively",
	},
	{
		Path:        []string{"config", "resolve"},
		Args:        "",
		Flags:       []string{"--repo", "--json"},
		Description: "Resolve Pi agent configuration",
	},
	{
		Path:        []string{"docker", "init"},
		Args:        "",
		Flags:       []string{"--target", "--force"},
		Description: "Scaffold Docker eval environment",
	},
	{
		Path:        []string{"doctor"},
		Args:        "",
		Flags:       []string{"--fix", "--json", "--only", "--skip", "--stale-days", "--verbose"},
		Description: "Integrity check",
	},
	{
		Path:        []string{"where"},
		Args:        "",
		Flags:       []string{"--json"},
		Description: "Report cwd's wiki/realm/code-index position",
	},
	{
		Path:        []string{"hooks", "session-start"},
		Args:        "",
		Flags:       []string{"--format"},
		Description: "Print session-start hook payload",
	},
	{
		Path:        []string{"hooks", "install"},
		Args:        "",
		Flags:       []string{"--scope"},
		Description: "Install session-start hook",
	},
	{
		Path:        []string{"hooks", "uninstall"},
		Args:        "",
		Flags:       []string{"--scope"},
		Description: "Remove session-start hook",
	},
	{
		Path:        []string{"reminder", "add"},
		Args:        "<text>",
		Flags:       []string{"--due", "--transport"},
		Description: "Create a reminder file; prints assigned id",
	},
	{
		Path:        []string{"reminder", "list"},
		Args:        "",
		Flags:       nil,
		Description: "List all reminders",
	},
	{
		Path:        []string{"reminder", "show"},
		Args:        "<id>",
		Flags:       nil,
		Description: "Print body of a reminder",
	},
	{
		Path:        []string{"reminder", "rm"},
		Args:        "<id>",
		Flags:       nil,
		Description: "Delete a reminder",
	},
	{
		Path:        []string{"scratchpad", "new"},
		Args:        "<slug>",
		Flags:       []string{"--purpose"},
		Description: "Create or extend a slug's bundle",
	},
	{
		Path:        []string{"scratchpad", "path"},
		Args:        "<slug>",
		Flags:       nil,
		Description: "Print a slug's bundle path",
	},
	{
		Path:        []string{"scratchpad", "list"},
		Args:        "",
		Flags:       []string{"--json", "--archived"},
		Description: "List bundles (live, or --archived)",
	},
	{
		Path:        []string{"scratchpad", "archive"},
		Args:        "<slug>",
		Flags:       nil,
		Description: "Archive a slug's bundle",
	},
	{
		Path:        []string{"signals", "scan"},
		Args:        "",
		Flags:       []string{"--out"},
		Description: "Walk repo and write docs/wiki/scan.md",
	},
	{
		Path:        []string{"signals", "show"},
		Args:        "",
		Flags:       nil,
		Description: "Print docs/wiki/scan.md to stdout",
	},
	{
		Path:        []string{"signals", "stale"},
		Args:        "",
		Flags:       nil,
		Description: "Exit 0 fresh, 1 stale, 2 error",
	},
	{
		Path:        []string{"signals", "diff"},
		Args:        "",
		Flags:       nil,
		Description: "Print unified diff of signals file",
	},
	{
		Path:        []string{"signals", "linkify"},
		Args:        "",
		Flags:       nil,
		Description: "Linkify path tokens in docs/wiki/index.md and docs/wiki/*.md",
	},
	{
		Path:        []string{"update"},
		Args:        "",
		Flags:       []string{"--check", "--channel", "--pre", "--no-doctor", "--skip-claude-update", "--force"},
		Description: "Self-update the atomic binary, then refresh ~/.claude artifacts",
	},
	{
		Path:        []string{"followups", "list"},
		Args:        "",
		Flags:       []string{"--stale", "--json"},
		Description: "List open follow-up entries",
	},
	{
		Path:        []string{"followups", "add"},
		Args:        "",
		Flags:       []string{"--id", "--title", "--kind", "--severity", "--origin", "--file", "--body"},
		Description: "Create entry",
	},
	{
		Path:        []string{"followups", "close"},
		Args:        "<id>",
		Flags:       []string{"--reason"},
		Description: "Close an entry",
	},
	{
		Path:        []string{"followups", "render"},
		Args:        "",
		Flags:       nil,
		Description: "Regenerate INDEX.md",
	},
	{
		Path:        []string{"followups", "path"},
		Args:        "",
		Flags:       nil,
		Description: "Print followups folder path",
	},
	{
		Path:        []string{"validate"},
		Args:        "[flags] [spec|config|bundle|artifacts] [paths...]",
		Flags:       []string{"--json", "--suggest"},
		Description: "Lint repo artifacts",
	},
	{
		Path:        []string{"docs", "scan"},
		Args:        "",
		Flags:       nil,
		Description: "Scan docs and write doc-surfaces.md",
	},
	{
		Path:        []string{"docs", "stale"},
		Args:        "",
		Flags:       nil,
		Description: "Exit 0 fresh, 1 stale, 2 error",
	},
	{
		Path:        []string{"profile", "refresh"},
		Args:        "",
		Flags:       []string{"--if-stale"},
		Description: "Refresh ## Environment in profile.md",
	},
	{
		Path:        []string{"code", "index"},
		Args:        "",
		Flags:       []string{"--profile", "--only", "--exclude"},
		Description: "Index all source files",
	},
	{
		Path:        []string{"code", "sync"},
		Args:        "",
		Flags:       nil,
		Description: "Incrementally re-index changed files",
	},
	{
		Path:        []string{"code", "status"},
		Args:        "",
		Flags:       []string{"--json"},
		Description: "Show index status",
	},
	{
		Path:        []string{"code", "search"},
		Args:        "<query>",
		Flags:       []string{"--json", "--limit", "--only", "--exclude"},
		Description: "Search indexed nodes",
	},
	{
		Path:        []string{"code", "callers"},
		Args:        "<symbol>",
		Flags:       []string{"--depth", "--json", "--only", "--exclude"},
		Description: "Find callers of symbol",
	},
	{
		Path:        []string{"code", "callees"},
		Args:        "<symbol>",
		Flags:       []string{"--depth", "--json", "--only", "--exclude"},
		Description: "Find callees of symbol",
	},
	{
		Path:        []string{"code", "impact"},
		Args:        "<symbol>",
		Flags:       []string{"--depth", "--json", "--only", "--exclude"},
		Description: "Find impact radius of symbol",
	},
	{
		Path:        []string{"code", "node"},
		Args:        "<symbol>",
		Flags:       []string{"--file", "--line", "--json"},
		Description: "Show node detail",
	},
	{
		Path:        []string{"code", "files"},
		Args:        "[pattern]",
		Flags:       []string{"--json"},
		Description: "List indexed files",
	},
	{
		Path:        []string{"code", "affected"},
		Args:        "",
		Flags:       []string{"--depth", "--test-glob", "--stdin", "--json"},
		Description: "Find affected test files",
	},
	{
		Path:        []string{"code", "explore"},
		Args:        "<query>",
		Flags:       []string{"--json", "--only", "--exclude"},
		Description: "Gather context for a query",
	},
	{
		Path:        []string{"code", "mcp"},
		Args:        "",
		Flags:       []string{"--daemon", "--source", "--db", "--watch-interval", "--no-watch"},
		Description: "Run the MCP server over stdio (proxy by default; --daemon --source --db runs the daemon itself; --no-watch disables sync poller)",
	},
	{
		Path:        []string{"wiki", "scan"},
		Args:        "",
		Flags:       []string{"--root"},
		Description: "Scaffold wiki/, scan repos, register in ~/.claude/CLAUDE.md",
	},
	{
		Path:        []string{"wiki", "stale"},
		Args:        "",
		Flags:       []string{"--root"},
		Description: "Exit 0 fresh, 1 stale, 2 error (DRIFT/STALE lines on stdout)",
	},
	{
		Path:        []string{"wiki", "linkify"},
		Args:        "",
		Flags:       []string{"--root"},
		Description: "Linkify path tokens in wiki artifacts in-place",
	},
	{
		Path:        []string{"wiki", "init"},
		Args:        "",
		Flags:       []string{"--scope", "--root"},
		Description: "Write the fixed-content CLAUDE.md scaffold and the scope marker for --scope repo|realm (idempotent)",
	},
	{
		Path:        []string{"wiki", "stamp"},
		Args:        "<file>",
		Flags:       []string{"--repo", "--root", "--cites", "--knowledge", "--sources"},
		Description: "Write reflects_rev/reflects/sources fingerprint frontmatter (summary|concern|knowledge)",
	},
	{
		Path:        []string{"wiki", "bucket", "add"},
		Args:        "<name>",
		Flags:       []string{"--root"},
		Description: "Register a capture bucket; create index.md stub and manifest dir",
	},
	{
		Path:        []string{"wiki", "bucket", "list"},
		Args:        "",
		Flags:       []string{"--root"},
		Description: "List registered buckets with baseline count and pending/fresh status",
	},
	{
		Path:        []string{"wiki", "bucket", "diff"},
		Args:        "<name>",
		Flags:       []string{"--root"},
		Description: "Print new/changed/removed files vs baseline; exit 0 empty, 1 non-empty",
	},
	{
		Path:        []string{"wiki", "bucket", "promote"},
		Args:        "<name>",
		Flags:       []string{"--root"},
		Description: "Snapshot bucket and rotate baseline→previous, current→baseline",
	},
	{
		Path:        []string{"wiki", "bucket", "doc"},
		Args:        "<bucket> <slug>",
		Flags:       []string{"--root", "--router"},
		Description: "Scaffold <bucket>/<slug>.md from the embedded doc template; --router also scaffolds the sibling subtree",
	},
	{
		Path:        []string{"wiki", "bucket", "skill"},
		Args:        "<bucket>",
		Flags:       []string{"--root"},
		Description: "Scaffold the realm per-bucket SKILL.md for <bucket> (no-op if present)",
	},
	{
		Path:        []string{"wiki", "bucket", "index"},
		Args:        "[<bucket>]",
		Flags:       []string{"--root"},
		Description: "Rebuild the <bucket-docs> region for one bucket (or all when omitted) plus the realm bucket list",
	},
	{
		Path:        []string{"prompt", "git-cleanup"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the git-cleanup cold-op brief",
	},
	{
		Path:        []string{"prompt", "claude-merge"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the CLAUDE.md merge cold-op brief",
	},
	{
		Path:        []string{"prompt", "implementer"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the implementer subagent prompt brief",
	},
	{
		Path:        []string{"prompt", "reviewer"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the reviewer subagent prompt brief",
	},
	{
		Path:        []string{"template", "brief"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the brief document template",
	},
	{
		Path:        []string{"template", "design-doc"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the design-doc document template",
	},
	{
		Path:        []string{"template", "diagnose-context"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the diagnose-context document template",
	},
	{
		Path:        []string{"template", "followups"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the followups document template",
	},
	{
		Path:        []string{"template", "implementation-log"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the implementation-log document template",
	},
	{
		Path:        []string{"template", "session-report"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the session-report document template",
	},
	{
		Path:        []string{"template", "spec"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the spec document template",
	},
	{
		Path:        []string{"template", "state"},
		Args:        "",
		Flags:       nil,
		Description: "Emit the state document template",
	},
	{
		Path:        []string{"serve"},
		Args:        "[path]",
		Flags:       []string{"--port", "--host", "--open"},
		Description: "Start a local read-only HTTP server for exploring wiki + code-intel",
	},
	{
		Path:        []string{"migrate"},
		Args:        "",
		Flags:       []string{"--repo", "--realm", "--show-log"},
		Description: "Run versioned atomic migrations",
	},
	{
		Path:        []string{"repo", "init"},
		Args:        "",
		Flags:       nil,
		Description: "Scaffold .claude/ layout: dirs + nested .claude/.gitignore + root ignore rules (idempotent)",
	},
	{
		Path:        []string{"repl", "start"},
		Args:        "",
		Flags:       []string{"--name", "--lang", "--env", "--bin", "--json"},
		Description: "Spawn a persistent py|js interpreter session; --lang accepts python/js/node/javascript aliases",
	},
	{
		Path:        []string{"repl", "eval"},
		Args:        "[--] [<code>]",
		Flags:       []string{"--name", "--timeout", "--json"},
		Description: "Evaluate code against a session; code from the positional arg (use -- before dash-leading code) or piped stdin",
	},
	{
		Path:        []string{"repl", "list"},
		Args:        "",
		Flags:       []string{"--all", "--json"},
		Description: "List sessions in the current repo+realm scope, or every scope with --all",
	},
	{
		Path:        []string{"repl", "status"},
		Args:        "",
		Flags:       []string{"--name", "--all", "--json"},
		Description: "Report one session's liveness, pid, and origin root",
	},
	{
		Path:        []string{"repl", "reset"},
		Args:        "",
		Flags:       []string{"--name", "--json"},
		Description: "Clear a session's interpreter namespace; the process stays alive",
	},
	{
		Path:        []string{"repl", "stop"},
		Args:        "",
		Flags:       []string{"--name", "--json"},
		Description: "End a session and remove its socket + meta",
	},
}

// Commands returns a copy of the ordered command surface.
func Commands() []Command {
	out := make([]Command, len(commands))
	copy(out, commands)
	return out
}

// SetRoot repoints the surface at the live Cobra tree; main() calls it once so
// the accessors stop reading the static table.
func SetRoot(root *cobra.Command) {
	commands = DeriveCommands(root)
}

// DeriveCommands returns the leaf commands under root, root itself excluded.
// Flags come from cmd.Flags().VisitAll and cover registered flags only:
// inherited persistent flags are absent because the FlagSet is built before the
// parent is assigned.
func DeriveCommands(root *cobra.Command) []Command {
	var out []Command
	for _, child := range root.Commands() {
		if child.Hidden {
			continue
		}
		walkLeaves(child, nil, &out)
	}
	return out
}

// walkLeaves accumulates path tokens in prefix, which excludes root.
func walkLeaves(cmd *cobra.Command, prefix []string, out *[]Command) {
	path := make([]string, len(prefix)+1)
	copy(path, prefix)
	path[len(prefix)] = cmd.Name()

	// Cobra injects "help" and "completion" without marking them hidden.
	var subs []*cobra.Command
	for _, s := range cmd.Commands() {
		if s.Hidden || s.Name() == "help" || s.Name() == "completion" {
			continue
		}
		subs = append(subs, s)
	}

	if len(subs) > 0 {
		for _, s := range subs {
			walkLeaves(s, path, out)
		}
		return
	}

	c := Command{
		Path:        path,
		Args:        cmd.Annotations["args_hint"],
		Description: cmd.Short,
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		c.Flags = append(c.Flags, "--"+f.Name)
	})
	if len(c.Flags) == 0 {
		c.Flags = nil
	}
	*out = append(*out, c)
}

func LookupByPath(path []string) *Command {
	key := strings.Join(path, "\x00")
	for i := range commands {
		if strings.Join(commands[i].Path, "\x00") == key {
			return &commands[i]
		}
	}
	return nil
}

// TopLevelVerbs is the set of distinct first tokens, used to gate which
// "atomic <token>" spans the artifacts scanner inspects — prose uses of
// "atomic" would otherwise false-positive.
func TopLevelVerbs() map[string]bool {
	out := make(map[string]bool)
	for _, c := range commands {
		if len(c.Path) > 0 {
			out[c.Path[0]] = true
		}
	}
	return out
}
