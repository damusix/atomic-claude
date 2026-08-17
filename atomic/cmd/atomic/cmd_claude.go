package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/spf13/cobra"
)

func buildClaudeCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "claude",
		Short: "Install, update, or manage ~/.claude artifacts (install|update|list|diff|uninstall)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runClaude(args); return nil },
	}
	addSub := func(verb, short, argsHint string, flagFn func(*cobra.Command)) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": argsHint},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				runClaude(append([]string{verb}, args...))
				return nil
			},
		}
		if flagFn != nil {
			flagFn(c)
		}
		parent.AddCommand(c)
	}
	addSub("install", "Install artifact bundle", "", func(c *cobra.Command) {
		c.Flags().Bool("dry-run", false, "print what would happen; make no changes")
		c.Flags().String("target", "", "target directory (default ~/.claude)")
		c.Flags().Bool("no-hooks", false, "skip session-start hook installation")
	})
	addSub("update", "Update artifact bundle", "", func(c *cobra.Command) {
		c.Flags().Bool("dry-run", false, "print what would happen; make no changes")
		c.Flags().String("target", "", "target directory (default ~/.claude)")
		c.Flags().Bool("no-hooks", false, "skip session-start hook installation")
	})
	addSub("list", "List bundled artifacts", "", nil)
	addSub("diff", "Diff bundle vs on-disk", "", func(c *cobra.Command) {
		c.Flags().String("target", "", "target directory (default ~/.claude)")
	})
	addSub("uninstall", "Generate uninstall prompt", "", func(c *cobra.Command) {
		c.Flags().String("target", "", "target directory (default ~/.claude)")
	})
	return parent
}

// HooksError is non-fatal here; the caller decides whether to warn.
type installResult struct {
	Plan           []claudeinstall.FileAction
	HooksInstalled bool
	HooksError     error
}

// runClaudeInstall is split out of the cmd switch so it is testable without
// os.Exit. targetDir (the artifact install root, possibly --target-overridden)
// and home (the fixed root of atomic state under ~/.atomic) resolve
// independently: a custom --target does not move where config state lives. The
// hook's scopeRoot is targetDir's parent, mirroring `hooks install --scope`.
func runClaudeInstall(targetDir, home, verb string, dryRun, noHooks bool) (installResult, error) {
	var plan []claudeinstall.FileAction
	var err error
	if verb == "update" {
		plan, err = claudeinstall.Update(targetDir, home, dryRun, claudeinstall.RealClock)
	} else {
		plan, err = claudeinstall.Install(targetDir, home, dryRun, claudeinstall.RealClock)
	}
	if err != nil {
		return installResult{}, err
	}

	result := installResult{Plan: plan}
	if dryRun || noHooks {
		return result, nil
	}

	scopeRoot := filepath.Dir(targetDir)
	if err := hooks.Install(scopeRoot, scopeRoot); err != nil {
		result.HooksError = err
		return result, nil
	}
	result.HooksInstalled = true
	return result, nil
}

// runClaudeUninstall returns the markdown prompt Claude should execute. Split
// out of the cmd switch so it is testable without os.Exit.
func runClaudeUninstall(targetDir, home string, out *os.File) (string, error) {
	plan, err := claudeinstall.BuildUninstallPlan(targetDir, home)
	if err != nil {
		return "", err
	}

	// A character device means an interactive terminal, so hint at what to do
	// with the output that follows.
	info, statErr := out.Stat()
	if statErr == nil && (info.Mode()&os.ModeCharDevice != 0) {
		fmt.Fprintln(os.Stderr, "hint: run this inside a Claude Code session, or paste the output below into Claude.")
		fmt.Fprintln(os.Stderr, "      alternatively: ask Claude to run `atomic claude uninstall`")
		fmt.Fprintln(os.Stderr, "")
	}

	return claudeinstall.GenerateUninstallPrompt(targetDir, home, plan), nil
}

// printPostInstallHint covers what install cannot automate: output style
// activation, which Claude Code requires the user to opt into, and per-repo
// signals initialization.
func printPostInstallHint(verb string) {
	if verb != "install" {
		return
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "next steps:")
	fmt.Fprintln(os.Stderr, "  1. open claude code and run /config → output style → Atomic")
	fmt.Fprintln(os.Stderr, "     (claude code requires explicit user opt-in for output styles)")
	fmt.Fprintln(os.Stderr, "  2. in each repo where you want project signals, run /refresh-wiki")
}

func runClaude(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic claude <install|update|list|diff|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]

	switch verb {
	case "install", "update":
		fs := flag.NewFlagSet("claude "+verb, flag.ContinueOnError)
		cliutil.SetUsage(fs, fmt.Sprintf("atomic claude %s [--dry-run] [--target <dir>] [--no-hooks]", verb))
		var dryRun bool
		var target string
		var noHooks bool
		fs.BoolVar(&dryRun, "dry-run", false, "print what would happen; make no changes")
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		fs.BoolVar(&noHooks, "no-hooks", false, "skip session-start hook installation")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: %v\n", verb, err)
			os.Exit(1)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: resolve home dir: %v\n", verb, err)
			os.Exit(1)
		}

		result, err := runClaudeInstall(targetDir, home, verb, dryRun, noHooks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: %v\n", verb, err)
			os.Exit(1)
		}

		if dryRun {
			fmt.Println("(dry-run — no changes written)")
		}
		fmt.Print(claudeinstall.Report(result.Plan, targetDir))

		if !dryRun {
			if result.HooksInstalled {
				fmt.Fprintln(os.Stderr, "session-start hook installed.")
			} else if result.HooksError != nil {
				fmt.Fprintf(os.Stderr, "warning: hook install failed (non-fatal): %v\n", result.HooksError)
				fmt.Fprintln(os.Stderr, "         retry later with: atomic hooks install")
			}
			printPostInstallHint(verb)
		}

	case "list":
		rows := claudeinstall.List()
		for _, r := range rows {
			fmt.Printf("%s\t%s\t%s\n", r.Kind, r.Target, r.SHA256)
		}

	case "diff":
		fs := flag.NewFlagSet("claude diff", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic claude diff [--target <dir>]")
		var target string
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude diff: %v\n", err)
			os.Exit(1)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude diff: resolve home dir: %v\n", err)
			os.Exit(1)
		}

		rows, err := claudeinstall.Diff(targetDir, home)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude diff: %v\n", err)
			os.Exit(1)
		}
		for _, r := range rows {
			fmt.Printf("%s\t%s\n", r.Status, r.Artifact.Target)
		}

	case "uninstall":
		fs := flag.NewFlagSet("claude uninstall", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic claude uninstall [--target <dir>]")
		var target string
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude uninstall: %v\n", err)
			os.Exit(1)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude uninstall: resolve home dir: %v\n", err)
			os.Exit(1)
		}

		prompt, err := runClaudeUninstall(targetDir, home, os.Stdout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude uninstall: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(prompt)

	default:
		fmt.Fprintf(os.Stderr, "atomic claude: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic claude <install|update|list|diff|uninstall> [flags]\n")
		os.Exit(2)
	}
}
