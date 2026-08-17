package main

import (
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
	"github.com/spf13/cobra"
)

// buildServeCmd returns the "serve" top-level command with flag metadata.
func buildServeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "serve",
		Short:              "Start a local read-only HTTP server for exploring wiki + code-intel",
		Annotations:        map[string]string{"args_hint": "[path]"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(serve.Run(args, os.Stdout, os.Stderr))
			return nil
		},
	}
	c.Flags().Int("port", 4500, "TCP port to listen on (0 = OS-assigned free port)")
	c.Flags().String("host", "127.0.0.1", "bind address")
	c.Flags().Bool("open", false, "open the browser after startup (best-effort)")
	return c
}
