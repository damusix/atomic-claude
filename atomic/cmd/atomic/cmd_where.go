package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/spf13/cobra"
)

func buildWhereCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "where",
		Short:              "Report cwd's wiki/realm/code-index position",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runWhere(args)
			return nil
		},
	}
	c.Flags().Bool("json", false, "emit machine-readable JSON output")
	return c
}

// runWhere reports cwd across three independent axes — repo-scope wiki
// presence, realm position, code-index scope — descriptively, with no severity.
func runWhere(args []string) {
	fs := flag.NewFlagSet("where", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic where [--json]")
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: get cwd: %v\n", err)
		os.Exit(2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: get home dir: %v\n", err)
		os.Exit(2)
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")

	report, err := where.Resolve(cwd, claudeMDPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: %v\n", err)
		os.Exit(2)
	}

	if jsonOut {
		data, jerr := where.FormatJSON(report)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "atomic where: marshal json: %v\n", jerr)
			os.Exit(2)
		}
		fmt.Println(data)
		os.Exit(0)
	}

	fmt.Print(where.FormatHuman(report))
	os.Exit(0)
}
