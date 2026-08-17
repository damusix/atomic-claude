package main

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

func buildDocsCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runDocs(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "docs",
		Short: "Docs surface scanning (scan|stale)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { dispatch(args); return nil },
	}
	addSub := func(verb, short string) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": ""},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				dispatch(append([]string{verb}, args...))
				return nil
			},
		}
		parent.AddCommand(c)
	}
	addSub("scan", "Scan docs and write doc-surfaces.md")
	addSub("stale", "Exit 0 fresh, 1 stale, 2 error")
	return parent
}

// docsAction is split out of runDocs so tests reach dispatch without os.Exit.
func docsAction(args []string, root string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docs <scan|stale>\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "scan":
		if err := docs.Scan(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		return 0
	case "stale":
		err := docs.Stale(root)
		if err == nil {
			return 0 // fresh
		}
		if err == docs.ErrStale {
			return 1
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	default:
		fmt.Fprintf(os.Stderr, "atomic docs: unknown verb %q\n", verb)
		return 1
	}
}

func runDocs(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docs <scan|stale>\n")
		os.Exit(1)
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic docs: %v\n", err)
		os.Exit(1)
	}

	os.Exit(docsAction(args, root))
}
