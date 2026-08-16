package main

import (
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/followups"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

// buildFollowupsCmd builds the "followups" parent + list|add|close|render|path children.
// Dispatch is runFollowups (→ followups.Run from internal/followups/cli.go).
func buildFollowupsCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runFollowups(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "followups",
		Short: "Manage typed follow-up entries (list|add|close|render|path)",
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
	addSub("list", "List open follow-up entries", "", func(c *cobra.Command) {
		c.Flags().Bool("stale", false, "show only stale entries")
		c.Flags().Bool("json", false, "output as JSON array")
	})
	addSub("add", "Create entry", "", func(c *cobra.Command) {
		c.Flags().String("id", "", "entry id (kebab-case)")
		c.Flags().String("title", "", "entry title")
		c.Flags().String("kind", "", "kind: finding (default) or plan")
		c.Flags().String("severity", "", "severity: risk, nit, or question")
		c.Flags().String("origin", "", "origin text")
		c.Flags().String("file", "", "optional file:lines reference")
		c.Flags().String("body", "", "body content; use '-' to read from stdin")
	})
	addSub("close", "Close an entry", "<id>", func(c *cobra.Command) {
		c.Flags().String("reason", "", "optional closure reason")
	})
	addSub("render", "Regenerate INDEX.md", "", nil)
	addSub("path", "Print followups folder path", "", nil)
	return parent
}

// --- CP4: top-level-only verb builders with flag metadata -----------------
//
// The five verbs below have no Cobra subcommands (they are leaves). Each uses
// DisableFlagParsing:true so the existing handler's own flag.NewFlagSet parses
// flags at runtime; the Flags() registrations here are metadata only, read by
// cliusage.DeriveCommands to populate the Commands() surface for the A1 linter.
// Flag names and the args_hint annotation must match cliusage.go's hardcoded
// golden slice exactly.

func runFollowups(args []string, repoOverride string) {
	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic followups: %v\n", err)
		os.Exit(1)
	}
	clock := func() time.Time { return time.Now().UTC() }
	os.Exit(followups.Run(args, root, os.Stdout, os.Stderr, clock))
}
