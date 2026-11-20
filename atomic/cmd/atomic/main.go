package main

import (
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	var noUpdateCheck bool
	noUpdateCheck, os.Args = scanNoUpdateCheck(os.Args)

	if slices.Equal(os.Args[1:], []string{"hooks", "session-start", "--harness", "claude", "--realm-instructions"}) {
		if err := runClaudeRealmSessionStart(pipedStdin(os.Stdin), os.Getenv("CLAUDE_PROJECT_DIR")); err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
		}
		return
	}

	// Handled before Cobra to preserve the exact output format and exit 0.
	for _, a := range os.Args[1:] {
		if a == "--" {
			break
		}
		if a == "--version" || a == "-v" {
			fmt.Printf("atomic %s (%s)\n", version.Version, version.Commit)
			os.Exit(0)
		}
	}

	// Delegated leaves parse their own flags, so scan --repo before dispatch.
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

	verb := findFirstVerb(os.Args[1:])
	// Realm previews and unapproved operations must not change user state.
	if home, err := os.UserHomeDir(); verb != "realm" && err == nil {
		if err := config.MigrateUserState(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic: user state migration: %v\n", err)
		}

		selfupdateFastPath(home, verb, version.Version, noUpdateCheck, os.Stderr, time.Now, defaultUpdateSpawn)
	}

	// rootCmd sets SilenceErrors so the exit code is controlled here.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "atomic: %v\n", err)
		os.Exit(1)
	}
}

func buildRootCmd(repoOverride *string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "atomic",
		Short:         "Holistic coding-agent configuration CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		// Refuses before any verb can resolve a path through the override.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return config.ValidateStateDirEnv()
		},
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
	rootCmd.SetHelpTemplate(`{{if .Long}}{{.Long | trimRightSpace}}{{else}}{{.Short | trimRightSpace}}{{end}}

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

	rootCmd.AddCommand(buildScratchpadCmd(repoOverride))

	rootCmd.AddCommand(buildHooksCmd(repoOverride))

	rootCmd.AddCommand(buildWorkflowCmd(repoOverride))

	rootCmd.AddCommand(buildClaudeCmd())

	rootCmd.AddCommand(buildInstallCmd())

	rootCmd.AddCommand(buildHarnessCmd())

	rootCmd.AddCommand(buildStateCmd(repoOverride))

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

	rootCmd.AddCommand(buildRealmCmd(repoOverride))

	rootCmd.AddCommand(buildPromptCmd())

	rootCmd.AddCommand(buildTemplateCmd())

	rootCmd.AddCommand(buildServeCmd())

	rootCmd.AddCommand(buildMigrateCmd())

	rootCmd.AddCommand(buildRepoCmd(repoOverride))

	rootCmd.AddCommand(buildReplCmd(repoOverride))

	rootCmd.AddCommand(buildRetroCmd())

	return rootCmd
}

// For command families with a shared dispatcher, the parent takes
// Args:cobra.ArbitraryArgs so unknown and empty verbs fall through to the
// handler with its exit codes intact; each child prepends its own name so the
// handler still receives [verb, ...rest]. Flags declared via cmd.Flags() are
// for cliusage derivation only, never parsed by Cobra.
