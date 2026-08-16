package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/spf13/cobra"
)

// buildProfileCmd builds the "profile" parent + refresh child.
func buildProfileCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "profile",
		Short: "User profile management (refresh)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runProfile(args); return nil },
	}
	refreshCmd := &cobra.Command{
		Use:                "refresh",
		Short:              "Refresh ## Environment in profile.md",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runProfile(append([]string{"refresh"}, args...))
			return nil
		},
	}
	refreshCmd.Flags().String("if-stale", "", "skip refresh when lastcheck is within this window (e.g. 7d, 30d)")
	parent.AddCommand(refreshCmd)
	return parent
}

// profileAction executes the profile subcommand logic and returns an exit code.
// Extracted from runProfile so tests can exercise dispatch without os.Exit.
// home is the user's home directory (config.ProfilePath resolves it to
// <home>/.atomic/profile.md); today is YYYY-MM-DD (injected, never time.Now here).
func profileAction(args []string, home, today string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic profile <refresh> [flags]\n")
		return 2
	}

	verb := args[0]
	switch verb {
	case "refresh":
		fs := flag.NewFlagSet("profile-refresh", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic profile refresh [--if-stale <Nd>]")
		fs.SetOutput(os.Stderr)
		var ifStale string
		fs.StringVar(&ifStale, "if-stale", "", "skip refresh when lastcheck is within this window (e.g. 7d, 30d)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}

		if ifStale != "" {
			days, err := profile.ParseDuration(ifStale)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic profile refresh: %v\n", err)
				return 1
			}
			wrote, err := profile.RefreshIfStale(home, today, days)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic profile refresh: %v\n", err)
				return 1
			}
			if wrote {
				fmt.Fprintf(os.Stderr, "profile refreshed: %s\n", config.ProfilePath(home))
			}
			return 0
		}

		// Unconditional refresh.
		_, err := profile.Refresh(home, today)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic profile refresh: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "profile refreshed: %s\n", config.ProfilePath(home))
		return 0

	default:
		fmt.Fprintf(os.Stderr, "atomic profile: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic profile <refresh> [flags]\n")
		return 2
	}
}

func runProfile(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic profile: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	today := time.Now().UTC().Format("2006-01-02")
	os.Exit(profileAction(args, home, today))
}
