package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

func buildHooksCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runHooks(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "hooks",
		Short: "Manage session-start hooks (session-start|install|uninstall)",
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
	addSub("session-start", "Print session-start hook payload", "", func(c *cobra.Command) {
		c.Flags().String("format", "", "output format: json or text")
	})
	addSub("install", "Install session-start hook", "", func(c *cobra.Command) {
		c.Flags().String("scope", "", "scope: user or project")
	})
	addSub("uninstall", "Remove session-start hook", "", func(c *cobra.Command) {
		c.Flags().String("scope", "", "scope: user or project")
	})
	return parent
}

func runHooks(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|install|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "session-start":
		fs := flag.NewFlagSet("hooks session-start", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks session-start [--format json|text]")
		var format string
		fs.StringVar(&format, "format", "json", "output format: json or text")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}

		now := time.Now().UTC()
		var out string
		if format == "text" {
			out, err = hooks.SessionStartText(root, now)
		} else {
			out, err = hooks.SessionStart(root, now)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}
		if out != "" {
			fmt.Println(out)
		}

	case "install":
		fs := flag.NewFlagSet("hooks install", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks install [--scope user|project]")
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		if err := hooks.Install(root, scopeRoot); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "hooks installed (scope=%s)\n", scope)

	case "uninstall":
		fs := flag.NewFlagSet("hooks uninstall", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks uninstall [--scope user|project]")
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		if err := hooks.Uninstall(root, scopeRoot); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "hooks uninstalled (scope=%s)\n", scope)

	default:
		fmt.Fprintf(os.Stderr, "atomic hooks: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|install|uninstall> [flags]\n")
		os.Exit(2)
	}
}

// resolveScopeRoot maps "user" to $HOME/.claude and "project" to repoRoot.
func resolveScopeRoot(scope, repoRoot string) (string, error) {
	switch scope {
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve scope: get home dir: %w", err)
		}
		return home, nil
	case "project":
		return repoRoot, nil
	default:
		return "", fmt.Errorf("unknown scope %q: must be \"user\" or \"project\"", scope)
	}
}
