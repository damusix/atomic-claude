package main

import (
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	// flag.FlagSet stops at the first non-flag argument, so a trailing
	// --no-update-check would reach neither Cobra's root nor the sub-handler's
	// own parser. Strip it from argv in any position instead.
	var noUpdateCheck bool
	noUpdateCheck, os.Args = scanNoUpdateCheck(os.Args)

	// Handled before Cobra to preserve the exact output format and exit 0.
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" {
			fmt.Printf("atomic %s (%s)\n", version.Version, version.Commit)
			os.Exit(0)
		}
	}

	// Every leaf sets DisableFlagParsing, so Cobra never parses the persistent
	// --repo flag; scanning raw argv here is what makes --repo do anything.
	// Exempt: verbs whose own --repo already means something else.
	var repoOverrideVal string
	if !repoFlagExempt(os.Args[1:]) {
		val, cleaned, rerr := scanRepoOverride(os.Args)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "atomic: %v\n", rerr)
			os.Exit(2)
		}
		repoOverrideVal = val
		os.Args = cleaned
	}

	// StringVar resets the pointer to its default at registration time, so the
	// pre-scanned value must be assigned after buildRootCmd, not before.
	var repoOverride string
	rootCmd := buildRootCmd(&repoOverride)
	repoOverride = repoOverrideVal

	// Derives the cliusage surface from the live tree, not a hardcoded table.
	cliusage.SetRoot(rootCmd)

	// Best-effort: a failed state migration warns and never blocks the verb.
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		if err := config.MigrateUserState(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic: user state migration: %v\n", err)
		}
	}

	// Banner comes from state.json alone; this process performs no network I/O.
	if homeErr == nil {
		selfupdateFastPath(home, findFirstVerb(os.Args[1:]), version.Version, noUpdateCheck, os.Stderr, time.Now, defaultUpdateSpawn)
	}

	// rootCmd sets SilenceErrors so the exit code is controlled here.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "atomic: %v\n", err)
		os.Exit(1)
	}
}

// buildRootCmd assembles the verb tree. Every verb sets DisableFlagParsing so
// the full arg slice reaches its handler unmodified, leaving the handler's own
// flag.NewFlagSet and sub-switch in charge.
func buildRootCmd(repoOverride *string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "atomic",
		Short:         "Holistic coding-agent configuration CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	// Cobra's generated "completion" and "help" verbs are suppressed so the
	// visible verb list is exactly the tree built below.
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "help [command]",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	})

	// Cobra's default template special-cases `help`, showing it despite
	// Hidden:true. This copy drops that exception.
	rootCmd.SetHelpTemplate(`{{with .Short}}{{. | trimRightSpace}}{{end}}

Usage:
  {{.UseLine}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

Flags:
{{.LocalFlags.FlagUsages | trimRightSpace}}

Use "{{.CommandPath}} [command] --help" for more information about a command.
`)

	// None of these are parsed at runtime — main()'s pre-scans consume all
	// three from argv. They exist for `atomic --help` and cliusage derivation.
	rootCmd.PersistentFlags().StringVar(repoOverride, "repo", "", "repo root override (default: detect via git)")
	rootCmd.PersistentFlags().Bool("no-update-check", false, "suppress background update check")
	rootCmd.PersistentFlags().BoolP("version", "v", false, "print version and exit")

	rootCmd.AddCommand(buildBusCmd())

	rootCmd.AddCommand(buildSignalsCmd(repoOverride))

	rootCmd.AddCommand(buildReminderCmd(repoOverride))

	rootCmd.AddCommand(buildScratchpadCmd(repoOverride))

	rootCmd.AddCommand(buildHooksCmd(repoOverride))

	rootCmd.AddCommand(buildClaudeCmd())

	rootCmd.AddCommand(buildDoctorCmd())

	rootCmd.AddCommand(buildWhereCmd(repoOverride))

	rootCmd.AddCommand(buildDockerCmd())

	rootCmd.AddCommand(buildUpdateCmd())

	rootCmd.AddCommand(buildConfigCmd())

	rootCmd.AddCommand(buildFollowupsCmd(repoOverride))

	rootCmd.AddCommand(buildValidateCmd())

	rootCmd.AddCommand(buildDocsCmd(repoOverride))

	rootCmd.AddCommand(buildProfileCmd())

	rootCmd.AddCommand(buildCodeCmd(repoOverride))

	rootCmd.AddCommand(buildWikiCmd())

	rootCmd.AddCommand(buildPromptCmd())

	rootCmd.AddCommand(buildTemplateCmd())

	rootCmd.AddCommand(buildServeCmd())

	rootCmd.AddCommand(buildMigrateCmd())

	rootCmd.AddCommand(buildRepoCmd(repoOverride))

	rootCmd.AddCommand(buildReplCmd(repoOverride))

	return rootCmd
}

// Convention for the parent commands in cmd_*.go: the parent takes
// Args:cobra.ArbitraryArgs so unknown and empty verbs fall through to the
// handler with its exit codes intact; each child prepends its own name so the
// handler still receives [verb, ...rest]. Flags declared via cmd.Flags() are
// for cliusage derivation only, never parsed by Cobra.

// main is the composition root: internal/config cannot import
// internal/claudeinstall (claudeinstall already imports config, so that would
// cycle), so the wiring lives here, in the one package that imports both.
func init() {
	config.ApplyAgentsHook = func(home string) ([]string, int, error) {
		// A custom `claude install --target <dir>` root is recorded nowhere the
		// config command can read, so those installs report "no installed agents
		// found" and the user re-runs install instead.
		target, err := claudeinstall.ResolveTarget("~/.claude")
		if err != nil {
			return nil, 0, err
		}
		return claudeinstall.ReapplyAgents(target, home)
	}
}
