package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/spf13/cobra"
)

func buildWikiCmd() *cobra.Command {
	dispatch := func(args []string) { runWiki(args) }
	parent := &cobra.Command{
		Use:   "wiki",
		Short: "Wiki management (scan|stale|linkify|bucket|init)",
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
	addSub("scan", "Scaffold wiki/, scan repos, mark realm scope, register in ~/.claude/CLAUDE.md", "", func(c *cobra.Command) {
		c.Flags().String("root", "", "root directory to scan (default: cwd)")
	})
	addSub("stale", "Report membership drift: exit 0 none, 1 drift, 2 error (DRIFT lines on stdout)", "", func(c *cobra.Command) {
		c.Flags().String("root", "", "root directory to check (default: cwd)")
	})
	addSub("linkify", "Linkify path tokens in wiki artifacts in-place", "", func(c *cobra.Command) {
		c.Flags().String("root", "", "realm root directory (default: cwd)")
	})
	addSub("init", "Write the fixed-content CLAUDE.md scaffold and the scope marker for --scope repo|realm (idempotent)", "", func(c *cobra.Command) {
		c.Flags().String("scope", "", "scaffold scope: repo or realm (required)")
		c.Flags().String("root", "", "root directory (default: cwd)")
	})

	// The bucket intermediate routes through dispatch too, so the no-args
	// usage path still reaches wiki.WikiAction.
	bucketParent := &cobra.Command{
		Use:   "bucket",
		Short: "Manage capture buckets (add|list|doc|skill|index)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dispatch(append([]string{"bucket"}, args...))
			return nil
		},
	}
	addBucketSub := func(verb, short, argsHint string, flagFn func(*cobra.Command)) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": argsHint},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				dispatch(append([]string{"bucket", verb}, args...))
				return nil
			},
		}
		c.Flags().String("root", "", "realm root directory (default: cwd)")
		if flagFn != nil {
			flagFn(c)
		}
		bucketParent.AddCommand(c)
	}
	addBucketSub("add", "Register a capture bucket; create its folder and index.md stub", "<name>", nil)
	addBucketSub("list", "List registered buckets as name<TAB>path", "", nil)
	addBucketSub("doc", "Scaffold <bucket>/<slug>/index.md and its 01_raw/ stage, then rebuild the bucket's indexes", "<bucket> <slug>", nil)
	addBucketSub("skill", "Scaffold the realm per-bucket SKILL.md for <bucket> (no-op if present)", "<bucket>", nil)
	addBucketSub("index", "Rebuild the <bucket-docs> region in every directory of one bucket (or all when omitted) plus the realm bucket list", "[<bucket>]", nil)
	parent.AddCommand(bucketParent)

	return parent
}

func runWiki(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	claudeHome := filepath.Join(home, ".claude")

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki: resolve cwd: %v\n", err)
		os.Exit(2)
	}

	os.Exit(wiki.WikiAction(args, claudeHome, cwd, os.Stdout))
}
