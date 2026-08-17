package main

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/spf13/cobra"
)

func buildConfigCmd() *cobra.Command {
	dispatch := func(args []string) {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic config: resolve home dir: %v\n", err)
			os.Exit(2)
		}
		os.Exit(config.Run(args, home, os.Stdout, os.Stderr))
	}
	parent := &cobra.Command{
		Use:   "config",
		Short: "Read and write atomic config (get|set|unset|list|path|agents|resolve)",
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
	addSub("get", "Print resolved config value", "<key>", nil)
	addSub("set", "Set config value", "<key> <val>", nil)
	addSub("unset", "Revert key to built-in default", "<key>", nil)
	addSub("list", "List all resolved key=value pairs", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "print as JSON object")
	})
	addSub("path", "Print path to config.toml", "", nil)
	addSub("agents", "Set per-agent model tiers interactively", "", nil)
	addSub("resolve", "Resolve Pi agent configuration", "", func(c *cobra.Command) {
		c.Flags().String("repo", "", "repository root")
		c.Flags().Bool("json", false, "print as JSON object")
	})
	return parent
}
