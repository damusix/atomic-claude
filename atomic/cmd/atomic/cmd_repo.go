package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/repoinit"
	"github.com/spf13/cobra"
)

// buildRepoCmd builds the "repo" parent + init child.
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
		Short:              "Scaffold .claude/ layout: dirs + nested .claude/.gitignore + root ignore rules (idempotent)",
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

		actions, err := repoinit.Init(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic repo init: %v\n", err)
			os.Exit(1)
		}

		for _, a := range actions {
			fmt.Printf("%-8s %s\n", string(a.Kind), a.Name)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic repo: unknown subcommand %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic repo <init> [flags]\n")
		os.Exit(2)
	}
}
