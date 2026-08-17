package main

import (
	"fmt"
	"os"
	"path/filepath"

	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"

	"github.com/spf13/cobra"
)

// buildCodeCmd builds the "code" parent + index|sync|status|search|callers|
// callees|impact|node|files|affected|explore|mcp children.
// Dispatch is runCode (→ codecli.RunCodeWithRealm); the handler's own
// flag.NewFlagSet parses flags at runtime — Cobra flag registrations here are
// for deriveCommands, which reads cmd.Flags; they are not parsed here.
func buildCodeCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runCode(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "code",
		Short: "Code-intel engine (index|sync|status|search|callers|callees|impact|node|files|affected|explore|mcp)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { dispatch(args); return nil },
	}
	addSub := func(verb, short, argsHint string, flagFn func(*cobra.Command)) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": argsHint},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				dispatch(append([]string{verb}, args...))
				return nil
			},
		}
		if flagFn != nil {
			flagFn(c)
		}
		parent.AddCommand(c)
	}
	addSub("index", "Index all source files", "", func(c *cobra.Command) {
		c.Flags().Bool("profile", false, "emit per-phase wall-time to stderr")
		c.Flags().String("only", "", "include only files matching pattern")
		c.Flags().String("exclude", "", "exclude files matching pattern")
	})
	addSub("sync", "Incrementally re-index changed files", "", nil)
	addSub("status", "Show index status", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit machine-readable JSON")
	})
	addSub("search", "Search indexed nodes", "<query>", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().Int("limit", 20, "max results")
		c.Flags().String("only", "", "include only files matching pattern")
		c.Flags().String("exclude", "", "exclude files matching pattern")
	})
	addSub("callers", "Find callers of symbol", "<symbol>", func(c *cobra.Command) {
		c.Flags().Int("depth", 3, "BFS depth")
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("only", "", "include only files matching pattern")
		c.Flags().String("exclude", "", "exclude files matching pattern")
	})
	addSub("callees", "Find callees of symbol", "<symbol>", func(c *cobra.Command) {
		c.Flags().Int("depth", 3, "BFS depth")
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("only", "", "include only files matching pattern")
		c.Flags().String("exclude", "", "exclude files matching pattern")
	})
	addSub("impact", "Find impact radius of symbol", "<symbol>", func(c *cobra.Command) {
		c.Flags().Int("depth", 3, "BFS depth")
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("only", "", "include only files matching pattern")
		c.Flags().String("exclude", "", "exclude files matching pattern")
	})
	addSub("node", "Show node detail", "<symbol>", func(c *cobra.Command) {
		c.Flags().String("file", "", "filter by file path")
		c.Flags().Int("line", 0, "filter by line number")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("files", "List indexed files", "[pattern]", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("affected", "Find affected test files", "", func(c *cobra.Command) {
		c.Flags().Int("depth", 5, "BFS depth for dependency traversal")
		c.Flags().String("test-glob", "", "glob pattern to identify test files")
		c.Flags().Bool("stdin", false, "read changed file paths from stdin")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("explore", "Gather context for a query", "<query>", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("only", "", "include only files matching pattern")
		c.Flags().String("exclude", "", "exclude files matching pattern")
	})
	addSub("mcp", "Run the MCP server over stdio (proxy by default; --daemon --source --db runs the daemon itself; --no-watch disables sync poller)", "", func(c *cobra.Command) {
		c.Flags().Bool("daemon", false, "run as the daemon itself, bound to --source/--db")
		c.Flags().String("source", "", "source root to serve (requires --daemon)")
		c.Flags().String("db", "", "absolute path to the SQLite index db (requires --daemon)")
		c.Flags().Duration("watch-interval", 0, "override the daemon's sync interval")
		c.Flags().Bool("no-watch", false, "disable background sync poller in the daemon")
	})
	return parent
}

func runCode(args []string, repoOverride string) {
	// Resolve scope BEFORE calling repoctx.Resolve, because repoctx.Resolve
	// runs `git rev-parse --show-toplevel` which errors at a realm root (a
	// plain container directory, not a git repo).  realm.Resolve position-senses
	// the cwd and branches to the correct engine path without git.
	if repoOverride == "" {
		// Inject cwd + claudeMD path for realm detection.
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic code: get cwd: %v\n", err)
			os.Exit(1)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic code: get home dir: %v\n", err)
			os.Exit(1)
		}
		claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
		os.Exit(codecli.RunCodeWithRealm(args, cwd, claudeMDPath, os.Stdout, os.Stderr, os.Stdin))
	}

	// --repo override: user explicitly specified a path. Normalise it to an
	// absolute path, then use the realm-aware dispatcher so a member path gets
	// its realm db and a standalone repo gets its local index. We avoid
	// repoctx.Resolve here because it runs `git rev-parse --show-toplevel` which
	// fails when the cwd is a realm root (no git repo there).
	absRepo, err := filepath.Abs(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic code: resolve --repo path: %v\n", err)
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic code: get home dir: %v\n", err)
		os.Exit(1)
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	os.Exit(codecli.RunCodeWithRealm(args, absRepo, claudeMDPath, os.Stdout, os.Stderr, os.Stdin))
}
