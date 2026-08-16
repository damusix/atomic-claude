package main

import (
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
	"github.com/spf13/cobra"
)

// buildValidateCmd returns the "validate" top-level command with flag metadata.
func buildValidateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "validate",
		Short:              "Lint repo artifacts",
		Annotations:        map[string]string{"args_hint": "[flags] [spec|config|bundle|artifacts] [paths...]"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(validate.Run(args))
			return nil
		},
	}
	c.Flags().Bool("json", false, "emit JSON output ({schema_version:1, findings:[...]})")
	c.Flags().Bool("suggest", false, "print structural templates for content-FAIL rules")
	return c
}
