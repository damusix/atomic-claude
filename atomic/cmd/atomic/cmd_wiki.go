package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
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
	addSub("scan", "Scaffold wiki/, scan repos, mark realm scope, register in ~/.atomic/wikis.md", "", func(c *cobra.Command) {
		c.Flags().String("root", "", "root directory to scan (default: cwd)")
	})
	addSub("stale", "Report membership drift: exit 0 none, 1 drift, 2 error (DRIFT lines on stdout)", "", func(c *cobra.Command) {
		c.Flags().String("root", "", "root directory to check (default: cwd)")
	})
	addSub("linkify", "Linkify path tokens in wiki artifacts in-place", "", func(c *cobra.Command) {
		c.Flags().String("root", "", "realm root directory (default: cwd)")
	})
	addSub("init", "Write the steering loader pair and the scope marker for --scope repo|realm (idempotent)", "", func(c *cobra.Command) {
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
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki: resolve cwd: %v\n", err)
		os.Exit(2)
	}

	steering := func() ([]string, error) { return wikiSteeringPaths(home) }
	os.Exit(wiki.WikiAction(args, home, steering, cwd, os.Stdout))
}

// wikiSteeringPaths lists each enrolled target's global steering. With no
// target enrolled, an existing CLAUDE.md in Claude's config root gets it.
func wikiSteeringPaths(home string) ([]string, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}
	if len(ledger.Targets) == 0 {
		unadopted := filepath.Join(config.ClaudeConfigDir(home), "CLAUDE.md")
		if _, err := os.Stat(unadopted); err == nil {
			return []string{unadopted}, nil
		}
		return nil, nil
	}
	var paths []string
	for _, t := range ledger.Targets {
		switch harness.Kind(t.Harness) {
		case harness.KindClaude:
			paths = append(paths, claude.GlobalSteeringPath(t.NativeRoot))
		case harness.KindOMP:
			paths = append(paths, omp.SteeringPath(t.NativeRoot))
		case harness.KindCodex:
			paths = append(paths, codex.SteeringPath(t.NativeRoot))
		}
	}
	return paths, nil
}
