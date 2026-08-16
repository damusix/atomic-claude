package main

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/repl"
	"github.com/spf13/cobra"
)

// buildReplCmd builds the "repl" parent + start|eval|list|status|reset|stop
// children. Dispatch is runRepl (→ repl.ReplAction from internal/repl/action.go).
func buildReplCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runRepl(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "repl",
		Short: "Persistent Python/Node interpreter sessions (start|eval|list|status|reset|stop)",
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
	addSub("start", "Spawn a persistent py|js interpreter session; --lang accepts python/js/node/javascript aliases", "", func(c *cobra.Command) {
		c.Flags().String("name", "", "session name")
		c.Flags().String("lang", "", "py|js (aliases: python, node, javascript)")
		c.Flags().String("env", "", "KEY=VALUE file merged into the session's environment")
		c.Flags().String("bin", "", "interpreter path override")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("eval", "Evaluate code against a session; code from the positional arg (use -- before dash-leading code) or piped stdin", "[--] [<code>]", func(c *cobra.Command) {
		c.Flags().String("name", "", "session name")
		c.Flags().String("timeout", "", "eval deadline (default 30s)")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("list", "List sessions in the current repo+realm scope, or every scope with --all", "", func(c *cobra.Command) {
		c.Flags().Bool("all", false, "enumerate every session on the machine, across every scope")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("status", "Report one session's liveness, pid, and origin root", "", func(c *cobra.Command) {
		c.Flags().String("name", "", "session name")
		c.Flags().Bool("all", false, "search every scope on the machine, not just the current repo/realm")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("reset", "Clear a session's interpreter namespace; the process stays alive", "", func(c *cobra.Command) {
		c.Flags().String("name", "", "session name")
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("stop", "End a session and remove its socket + meta", "", func(c *cobra.Command) {
		c.Flags().String("name", "", "session name")
		c.Flags().Bool("json", false, "emit JSON")
	})
	return parent
}

// runRepl resolves the process's real home dir and cwd — the two things
// repl.ReplAction needs but must not resolve itself (see ReplAction's doc) —
// and delegates. os.Stdin flows through so eval can read piped code;
// repl.isTerminalReader is what turns "connected to a real terminal" into
// the usage-error path rather than a hang.
func runRepl(args []string, repoOverride string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl: resolve home dir: %v\n", err)
		// 1, not 2: repl's own exit table assigns 2 to ExitNotFound (session
		// not found) — an infra failure before ReplAction even runs must not
		// collide with that code.
		os.Exit(int(repl.ExitUsage))
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl: resolve cwd: %v\n", err)
		os.Exit(int(repl.ExitUsage))
	}
	os.Exit(repl.ReplAction(args, home, cwd, repoOverride, os.Stdin, os.Stdout))
}
