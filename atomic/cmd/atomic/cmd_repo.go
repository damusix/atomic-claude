package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/repoinit"
	"github.com/spf13/cobra"
)

func buildRepoCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runRepo(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "repo",
		Short: "Repo-scoped scaffolding (init)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { dispatch(args); return nil },
	}
	initCmd := &cobra.Command{
		Use:                "init",
		Short:              "Scaffold the state-root layout: dirs + state-root .gitignore + root ignore rules (idempotent)",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dispatch(append([]string{"init"}, args...))
			return nil
		},
	}
	parent.AddCommand(initCmd)
	return parent
}

// claudeEnrolled reports whether the ledger enrolls a Claude target; an
// unreadable ledger warns and reads as not enrolled.
func claudeEnrolled(home string, warn io.Writer) bool {
	kinds, err := harness.EnrolledKinds(home)
	if err != nil {
		fmt.Fprintf(warn, "atomic repo init: warning: %v; treating Claude as not enrolled\n", err)
		return false
	}
	return slices.Contains(kinds, harness.KindClaude)
}

// repoInit migrates legacy state first, so the layout lands in the new root.
func repoInit(root, home string, out, errOut io.Writer) error {
	if err := migrateStateIfPending(root, errOut); err != nil {
		return err
	}
	enrolled := home != "" && claudeEnrolled(home, errOut)
	actions, err := repoinit.Init(root, enrolled)
	if err != nil {
		return err
	}
	for _, a := range actions {
		fmt.Fprintf(out, "%-8s %s\n", string(a.Kind), a.Name)
		if a.Detail != "" {
			fmt.Fprintf(out, "         %s\n", a.Detail)
		}
	}
	if err := convergeSteeringMode(home, root, out); err != nil {
		return err
	}
	stateRoot, err := config.RepositoryStateDir(root)
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(root, ".claude"))
	return convergeRepoCards(home, root, stateRoot, cardWrite{link: err == nil}, out)
}

func runRepo(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic repo <init> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "init":
		fs := flag.NewFlagSet("repo init", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic repo init")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic repo init: %v\n", err)
			os.Exit(1)
		}
		home, _ := os.UserHomeDir()
		if err := repoInit(root, home, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "atomic repo init: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic repo: unknown subcommand %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic repo <init> [flags]\n")
		os.Exit(2)
	}
}
