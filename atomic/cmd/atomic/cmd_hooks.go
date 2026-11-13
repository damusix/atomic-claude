package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

func buildHooksCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runHooks(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "hooks",
		Short: "Manage atomic hooks (session-start|pre-tool-use|install|uninstall)",
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
		c.Flags().String("harness", "", "harness the hook runs under: claude, omp, or codex (default claude)")
		c.Flags().String("cwd", "", cwdHelp)
	})
	// Replaces the root's state-dir preflight: a refusal would fail the edit.
	addSub("pre-tool-use", "Print the wiki cards matching a Codex edit", "", func(c *cobra.Command) {
		c.PersistentPreRunE = func(*cobra.Command, []string) error { return nil }
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
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|pre-tool-use|install|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "session-start":
		fs := flag.NewFlagSet("hooks session-start", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks session-start [--format json|text] [--harness claude|omp|codex] [--cwd <dir>]")
		var format, harnessFlag, cwdFlag string
		fs.StringVar(&format, "format", "json", "output format: json or text")
		fs.StringVar(&harnessFlag, "harness", "claude", "harness the hook runs under: claude, omp, or codex")
		fs.StringVar(&cwdFlag, "cwd", "", cwdHelp)
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}
		kind, err := harness.ParseKind(harnessFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(2)
		}
		harnessFlag = string(kind)

		root, err := resolveSessionRoot(cwdFlag, pipedStdin(os.Stdin), repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}

		now := time.Now().UTC()
		var out string
		if format == "text" {
			out = hooks.SessionStartText(root, harnessFlag, now)
		} else if out, err = hooks.SessionStart(root, harnessFlag, now); err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}
		if out != "" {
			fmt.Println(out)
		}

	case "pre-tool-use":
		runPreToolUse(os.Stdin)

	case "install":
		fs := flag.NewFlagSet("hooks install", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks install [--scope user|project]")
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := resolveHooksRoot(scope, repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		skipped, err := hooks.Install(root, scopeRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if skipped {
			fmt.Fprintf(os.Stderr, "warning: settings.json is read-only; hooks not installed (scope=%s, non-fatal)\n", scope)
		} else {
			fmt.Fprintf(os.Stderr, "hooks installed (scope=%s)\n", scope)
		}

	case "uninstall":
		fs := flag.NewFlagSet("hooks uninstall", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks uninstall [--scope user|project]")
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := resolveHooksRoot(scope, repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		skipped, err := hooks.Uninstall(root, scopeRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if skipped {
			fmt.Fprintf(os.Stderr, "warning: settings.json is read-only; hooks not uninstalled (scope=%s, non-fatal)\n", scope)
		} else {
			fmt.Fprintf(os.Stderr, "hooks uninstalled (scope=%s)\n", scope)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic hooks: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|pre-tool-use|install|uninstall> [flags]\n")
		os.Exit(2)
	}
}

// preToolUse is a seam so a test can make the hook panic.
var preToolUse = hooks.PreToolUse

// runPreToolUse never fails: a panic or error prints nothing and exits 0, so a
// wiki card can never fail the edit it precedes.
func runPreToolUse(stdin io.Reader) {
	defer func() { _ = recover() }()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if out := preToolUse(stdin, home, time.Now().UTC()); out != "" {
		fmt.Println(out)
	}
}

// resolveSessionRoot resolves the session directory through the scope marker
// and Git rather than taking the directory as the root.
func resolveSessionRoot(flagCwd string, stdin io.Reader, repoOverride string) (string, error) {
	dir := sessionStartCwd(flagCwd, stdin, stdinWait)
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	root, _, err := repoctx.ResolveFrom(dir, repoOverride)
	return root, err
}

const (
	stdinWait = time.Second
	cwdHelp   = "directory to resolve the repository from (default: stdin cwd, else process directory)"
)

// pipedStdin returns nil for a terminal or /dev/null, which would otherwise
// wait on or consume typed input.
func pipedStdin(f *os.File) io.Reader {
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	return f
}

// sessionStartCwd gives up on r after wait: a harness may leave stdin open.
func sessionStartCwd(flagCwd string, r io.Reader, wait time.Duration) string {
	if flagCwd != "" || r == nil {
		return flagCwd
	}
	got := make(chan string, 1)
	go func() {
		var payload struct {
			Cwd string `json:"cwd"`
		}
		if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&payload); err != nil {
			got <- ""
			return
		}
		got <- payload.Cwd
	}()
	select {
	case cwd := <-got:
		return cwd
	case <-time.After(wait):
		return ""
	}
}

// resolveHooksRoot: user scope writes no repository state, so it skips the
// state-root check that refuses an ambiguous repository.
func resolveHooksRoot(scope, override string) (string, error) {
	if scope == "user" {
		return repoctx.ResolveUnchecked(override)
	}
	return repoctx.Resolve(override)
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
