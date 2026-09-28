package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/spf13/cobra"
)

func buildDocsCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runDocs(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "docs",
		Short: "Docs surface scanning (scan|stale|index)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { dispatch(args); return nil },
	}
	addSub := func(verb, argsHint, short string) *cobra.Command {
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
		parent.AddCommand(c)
		return c
	}
	addSub("scan", "", "Scan docs and write doc-surfaces.md")
	addSub("stale", "", "Exit 0 fresh, 1 stale, 2 error")
	indexCmd := addSub("index", "[--check] <dir>...", "Rebuild each <dir>/index.md <bucket-docs> region; --check exits 0 fresh, 1 stale, 2 error")
	indexCmd.Flags().Bool("check", false, "compare without writing")
	return parent
}

// docsAction is split out of runDocs so tests reach dispatch without os.Exit.
func docsAction(args []string, root string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docs <scan|stale|index>\n")
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
	case "index":
		return docsIndex(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "atomic docs: unknown verb %q\n", verb)
		return 1
	}
}

func docsIndex(args []string) int {
	fs := flag.NewFlagSet("docs-index", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic docs index [--check] <dir>...")
	fs.SetOutput(os.Stderr)
	check := fs.Bool("check", false, "compare without writing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	dirs := fs.Args()
	terminated := len(args) > len(dirs) && args[len(args)-len(dirs)-1] == "--"
	for _, dir := range dirs {
		if !terminated && strings.HasPrefix(dir, "-") {
			fmt.Fprintf(os.Stderr, "atomic docs index: flag %q must come before the directories\n", dir)
			fs.Usage()
			return 2
		}
	}

	errCode := 1
	if *check {
		errCode = 2
	}

	code := 0
	for _, dir := range dirs {
		res, err := wiki.IndexDir(dir, *check)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "atomic docs index: %v\n", err)
			code = max(code, errCode)
		case *check && res.Stale:
			fmt.Printf("STALE %s\n", filepath.Join(dir, "index.md"))
			code = max(code, 1)
		case !*check:
			fmt.Printf("%s: %d indexed, %d unindexed\n", dir, res.Indexed, res.Unindexed)
		}
	}
	return code
}

func runDocs(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docs <scan|stale|index>\n")
		os.Exit(1)
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic docs: %v\n", err)
		os.Exit(1)
	}

	os.Exit(docsAction(args, root))
}
