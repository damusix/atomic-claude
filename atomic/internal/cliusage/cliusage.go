// Package cliusage defines the complete atomic command surface as structured
// data. It serves two consumers: (1) main.go renders --help from it, and
// (2) the validate artifacts rule (Checkpoint 2) checks artifact citations
// against it. Define the surface once here; callers never hand-write usage
// lines or maintain parallel flag lists.
package cliusage

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Command describes one entry in the atomic command surface.
type Command struct {
	// Path is the ordered verb tokens, e.g. ["code", "search"] or ["doctor"].
	Path []string
	// Args is the positional-argument hint shown in --help, e.g. "<query>".
	// May be empty.
	Args string
	// Flags lists every --flag accepted by this command, each including the
	// leading "--", e.g. ["--json", "--limit"]. Universal flags (--help,
	// --repo, etc.) are not listed here; they are implicitly accepted everywhere.
	Flags []string
	// Description is the one-line summary shown in --help.
	Description string
}

// commands is the ordered command surface. Flags reflect the actual
// flag.NewFlagSet registrations in each verb handler — the source of truth
// for the validate-artifacts check. Keep this in sync with the handler when
// adding or removing flags; a mismatch causes CP2 to emit false-positives or
// false-negatives. Edit this slice to change --help and the validate
// artifacts rule simultaneously.
var commands = []Command{
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
		Description: "Set config value; re-renders config.resolved.md",
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
		Flags:       []string{"--check", "--channel", "--no-doctor", "--skip-claude-update"},
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
		Flags:       []string{"--watch-interval", "--no-watch"},
		Description: "Run the MCP server over stdio (proxy + daemon; --no-watch disables sync poller)",
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
		Path:        []string{"serve"},
		Args:        "[path]",
		Flags:       []string{"--port", "--host", "--open"},
		Description: "Start a local read-only HTTP server for exploring wiki + code-intel",
	},
}

// Commands returns the ordered command surface. The returned slice is a copy;
// callers may not mutate the underlying table.
func Commands() []Command {
	out := make([]Command, len(commands))
	copy(out, commands)
	return out
}

// RenderCommandsBlock writes the "Commands:" body — the lines between the
// "Commands:" label and the "\nFlags:" section — to w. Each line is one
// command: "  <verb-path> [args] [flags]  <description>". Columns are aligned
// via text/tabwriter. The label "Commands:" and the "\nFlags:" section are
// NOT written here; main.go owns those surrounding lines.
func RenderCommandsBlock(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		hint := buildHint(c)
		if hint != "" {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", strings.Join(c.Path, " "), hint, c.Description)
		} else {
			fmt.Fprintf(tw, "  %s\t\t%s\n", strings.Join(c.Path, " "), c.Description)
		}
	}
	tw.Flush()
}

// buildHint builds the args+flags hint string for a command, e.g.
// "<query> [--json] [--limit N]". Returns "" when both Args and Flags are
// empty. The rendered form matches the original main.go usage block style.
func buildHint(c Command) string {
	var parts []string
	if c.Args != "" {
		parts = append(parts, c.Args)
	}
	for _, f := range c.Flags {
		parts = append(parts, "["+f+"]")
	}
	return strings.Join(parts, " ")
}

// LookupByPath returns the Command whose Path matches path exactly, or nil
// if no command with that path exists. Used by Checkpoint 2 validate rule.
func LookupByPath(path []string) *Command {
	key := strings.Join(path, "\x00")
	for i := range commands {
		if strings.Join(commands[i].Path, "\x00") == key {
			return &commands[i]
		}
	}
	return nil
}

// TopLevelVerbs returns a set of the distinct first tokens across all commands.
// Used by the validate artifacts scanner to gate which "atomic <token>" spans
// to inspect (avoids false positives for prose uses of "atomic").
func TopLevelVerbs() map[string]bool {
	out := make(map[string]bool)
	for _, c := range commands {
		if len(c.Path) > 0 {
			out[c.Path[0]] = true
		}
	}
	return out
}
