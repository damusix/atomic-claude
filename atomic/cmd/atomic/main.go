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
	// Pre-scan 1: strip --no-update-check from os.Args in any position before
	// Cobra runs. flag.FlagSet stops at the first non-flag argument (the
	// subcommand), so "atomic signals scan --no-update-check" would not be
	// seen by Cobra's persistent flag on the root. Stripping it here also
	// prevents the sub-handler's own flag parser from tripping over it.
	var noUpdateCheck bool
	noUpdateCheck, os.Args = scanNoUpdateCheck(os.Args)

	// Pre-scan 2: handle --version / -v before Cobra so the output format
	// ("atomic X.Y.Z (commit)") and exit code (0) are preserved exactly.
	// These flags are also registered as persistent flags for --help docs.
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" {
			fmt.Printf("atomic %s (%s)\n", version.Version, version.Commit)
			os.Exit(0)
		}
	}

	// Pre-scan 3: extract a global --repo override from argv, in any
	// position, before Cobra or any leaf's own flag.NewFlagSet ever sees it.
	// Every leaf sets DisableFlagParsing:true (see buildRootCmd), which makes
	// Cobra's ParseFlags a no-op regardless of where --repo sits — the
	// persistent flag registered on rootCmd below is never actually parsed
	// at runtime, so this scan is what makes --repo do anything at all.
	// Skipped for verbs whose own --repo flag already carries different,
	// established semantics (migrate, config resolve, wiki stamp).
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

	// Build the Cobra command tree. repoOverride starts at "" via the
	// persistent-flag registration in buildRootCmd (kept for --help docs and
	// cliusage's live-tree derivation only) and is then set to the
	// pre-scanned value above — StringVar resets the pointer to its default
	// at registration time, so the assignment must happen after.
	var repoOverride string
	rootCmd := buildRootCmd(&repoOverride)
	repoOverride = repoOverrideVal

	// Derive the cliusage surface from the live Cobra tree so Commands(),
	// LookupByPath(), and TopLevelVerbs() all reflect the real flag metadata
	// rather than the static hardcoded table.
	cliusage.SetRoot(rootCmd)

	// Migrate legacy per-user state (~/.claude/.atomic -> ~/.atomic, issue #150)
	// once, before any verb runs. Best-effort: failure warns and never blocks
	// the invoked verb — the migration itself is not what the user asked for.
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		if err := config.MigrateUserState(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic: user state migration: %v\n", err)
		}
	}

	// Self-update fast path (docs/spec/selfupdate-state.md): read state.json,
	// render an at-most-once-per-24h banner from state alone, and — stamp
	// last_check before spawning — launch a detached child that owns the
	// GitHub lookup at most once per hour. This process performs zero network
	// I/O of its own and never waits on the spawned child. Skipped entirely
	// when home could not be resolved (best-effort, like the migration above).
	if homeErr == nil {
		selfupdateFastPath(home, findFirstVerb(os.Args[1:]), version.Version, noUpdateCheck, os.Stderr, time.Now, defaultUpdateSpawn)
	}

	// Execute the Cobra command tree. SilenceErrors is set on rootCmd so we
	// handle the error ourselves and control the exit code explicitly.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "atomic: %v\n", err)
		os.Exit(1)
	}
}

// buildRootCmd constructs the Cobra root command with all 22 top-level verb
// stubs. Each stub delegates to the existing runXxx handler with the post-verb
// args, preserving all existing dispatch behavior unchanged. The nested
// sub-switches inside handlers (code, wiki, signals, etc.) stay intact and are
// ported to Cobra sub-commands in later checkpoints.
//
// DisableFlagParsing: true on every verb passes the full arg slice
// (sub-verbs + flags) through to the existing handler unmodified, so the
// handler's own flag.NewFlagSet / sub-switch continues to work identically.
func buildRootCmd(repoOverride *string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "atomic",
		Short:         "Holistic coding-agent configuration CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		// Run is called when no subcommand is given. Print help and exit 1,
		// matching the old behavior of fs.Usage() + os.Exit(1).
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	// Suppress the auto-generated "completion" subcommand so the visible
	// verb set stays exactly the current 17.
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Replace the auto-generated "help" subcommand with a hidden no-op so it
	// does not appear in the visible verb list.
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "help [command]",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	})

	// Override the help template to omit the hard-coded "(eq .Name "help")"
	// exception that Cobra's default template uses. Without this, the hidden
	// "help" command appears in `atomic --help` output even though Hidden: true.
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

	// Persistent flags: registered on root for --help documentation.
	//
	// --no-update-check is stripped from os.Args by scanNoUpdateCheck() before
	// Execute() runs, so Cobra's persistent-flag parser never sees it in the
	// argv. The Bool registration below is purely for `atomic --help` docs.
	//
	// --version / -v is handled by a pre-scan in main() that exits before
	// Execute(); the BoolP registration is for `atomic --help` docs only.
	//
	// --repo is extracted from argv by scanRepoOverride() in main() before
	// Execute() runs (same reason as --no-update-check above, plus every
	// leaf's DisableFlagParsing:true — see main()); the StringVar
	// registration below is for `atomic --help` docs and cliusage's
	// live-tree derivation only, matching the pattern above.
	rootCmd.PersistentFlags().StringVar(repoOverride, "repo", "", "repo root override (default: detect via git)")
	rootCmd.PersistentFlags().Bool("no-update-check", false, "suppress background update check")
	rootCmd.PersistentFlags().BoolP("version", "v", false, "print version and exit")

	// --- 22 top-level verb stubs -----------------------------------------

	rootCmd.AddCommand(buildBusCmd())

	rootCmd.AddCommand(buildSignalsCmd(repoOverride))

	rootCmd.AddCommand(buildReminderCmd(repoOverride))

	rootCmd.AddCommand(buildHooksCmd(repoOverride))

	rootCmd.AddCommand(buildClaudeCmd())

	rootCmd.AddCommand(buildDoctorCmd())

	rootCmd.AddCommand(buildWhereCmd())

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

// --- parent commands with Cobra subcommands ---------------------------
//
// Each builder creates a parent *cobra.Command whose children correspond to the
// nested verb switch in the handler. The parent uses Args:cobra.ArbitraryArgs
// so unknown verbs (and the empty-args case) fall through to the existing
// handler, preserving exit codes and error messages. Each child sets
// DisableFlagParsing:true and prepends its name so the existing handler
// receives [verb, ...rest], identical to the previous call shape.
// Flags registered via cmd.Flags() are for derivation only — not parsed
// by Cobra at runtime; the handler's own flag.NewFlagSet parses them.

// init wires config.ApplyAgentsHook to claudeinstall.ReapplyAgents. internal/config
// cannot import internal/claudeinstall directly (claudeinstall already imports
// config, which would be a cycle), so main — which imports both — closes the loop.
func init() {
	config.ApplyAgentsHook = func(home string) ([]string, int, error) {
		// Auto-apply targets the default install root (~/.claude). A custom
		// `atomic claude install --target <dir>` root is not recorded anywhere the
		// config command can read, so those installs are not auto-applied here and
		// report "no installed agents found" — the user re-runs install for them.
		target, err := claudeinstall.ResolveTarget("~/.claude")
		if err != nil {
			return nil, 0, err
		}
		return claudeinstall.ReapplyAgents(target, home)
	}
}
