package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func buildHarnessCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "harness",
		Short: "Enroll, adopt, repair, diff, or uninstall harness targets (list|status|enroll|adopt|repair|diff|uninstall|rules)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runHarness(args); return nil },
	}

	addHarnessSub(parent, []string{"list"}, "List discovered and enrolled instances", "", func(fs *pflag.FlagSet) {
		fs.Bool("json", false, "emit machine-readable JSON output")
	})
	addHarnessSub(parent, []string{"status"}, "Report enrolled target and shared-resource state", "[<target-key>]", func(fs *pflag.FlagSet) {
		fs.Bool("json", false, "emit machine-readable JSON output")
	})
	addHarnessSub(parent, []string{"enroll"}, "Explicitly enroll a harness instance", "<claude|omp|codex>", func(fs *pflag.FlagSet) {
		registerInstallFlags(fs)
	})
	addHarnessSub(parent, []string{"adopt"}, "Adopt a verified legacy Claude install", "[claude]", func(fs *pflag.FlagSet) {
		registerAdoptFlags(fs)
	})
	addHarnessSub(parent, []string{"repair"}, "Reconverge already-enrolled targets", "", func(fs *pflag.FlagSet) {
		registerRepairFlags(fs)
	})
	addHarnessSub(parent, []string{"diff"}, "Report native difference from the selected generation", "", func(fs *pflag.FlagSet) {
		registerRepairFlags(fs)
	})
	addHarnessSub(parent, []string{"uninstall"}, "Remove one enrolled target, or every target with --all", "<target-key>", func(fs *pflag.FlagSet) {
		registerUninstallFlags(fs)
	})

	rules := &cobra.Command{
		Use:   "rules",
		Short: "Report or sync per-target rule surfaces (status|sync)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHarness(append([]string{"rules"}, args...))
			return nil
		},
	}
	addHarnessSub(rules, []string{"rules", "status"}, "Report rule tier, digest, coverage, and conflict state", "", func(fs *pflag.FlagSet) {
		registerRepairFlags(fs)
	})
	addHarnessSub(rules, []string{"rules", "sync"}, "Converge rule and steering projections for enrolled targets", "", func(fs *pflag.FlagSet) {
		registerRepairFlags(fs)
	})
	parent.AddCommand(rules)
	return parent
}

// addHarnessSub registers one leaf. path is the full verb path, so nested verbs
// still reach the dispatcher with every token it must switch on.
func addHarnessSub(parent *cobra.Command, path []string, short, argsHint string, flagFn func(*pflag.FlagSet)) {
	c := &cobra.Command{
		Use:                path[len(path)-1],
		Short:              short,
		Annotations:        map[string]string{"args_hint": argsHint},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHarness(append(append([]string{}, path...), args...))
			return nil
		},
	}
	if flagFn != nil {
		flagFn(c.Flags())
	}
	parent.AddCommand(c)
}

// The register* helpers declare flags on the cobra tree so `atomic --help` and
// cliusage derivation see them. They are never parsed by cobra: every leaf
// disables flag parsing and each handler owns its own stdlib FlagSet.

func registerInstallFlags(fs *pflag.FlagSet) {
	fs.String("harness", "", "harness to install (claude, omp, or codex)")
	fs.String("instance", "", "native root of the instance")
	fs.Bool("all", false, "operate on every discovered instance")
	fs.Bool("replace", false, "replace unowned older-version artifacts")
	fs.Bool("leave-unowned", false, "preserve unowned older-version artifacts")
	fs.Bool("dry-run", false, "print what would happen; make no changes")
	fs.Bool("yes", false, "approve the printed plan without prompting")
	fs.Bool("json", false, "emit machine-readable JSON output")
}

func registerRepairFlags(fs *pflag.FlagSet) {
	fs.String("harness", "", "restrict to one harness (claude, omp, or codex)")
	fs.String("instance", "", "restrict to one native root")
	fs.Bool("dry-run", false, "print what would happen; make no changes")
	fs.Bool("yes", false, "approve the printed plan without prompting")
	fs.Bool("json", false, "emit machine-readable JSON output")
}

func registerUninstallFlags(fs *pflag.FlagSet) {
	fs.Bool("all", false, "remove every enrolled target and completed operational state")
	fs.Bool("dry-run", false, "print what would happen; make no changes")
	fs.Bool("yes", false, "approve the printed plan without prompting")
	fs.Bool("json", false, "emit machine-readable JSON output")
}

func registerAdoptFlags(fs *pflag.FlagSet) {
	fs.String("instance", "", "native root of the instance to adopt")
	fs.Bool("replace", false, "replace unowned older-version artifacts")
	fs.Bool("leave-unowned", false, "preserve unowned older-version artifacts")
	fs.Bool("acknowledge-snapshot", false, "accept an absent or corrupt legacy snapshot")
	fs.Bool("dry-run", false, "print what would happen; make no changes")
	fs.Bool("yes", false, "approve the printed plan without prompting")
	fs.Bool("json", false, "emit machine-readable JSON output")
}

func runHarness(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic harness <list|status|enroll|adopt|repair|diff|uninstall|rules> [flags]")
		os.Exit(2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fatal("harness", err)
	}
	steps := install.DefaultSteps(home)

	switch args[0] {
	case "list":
		runHarnessList(steps, args[1:])
	case "status":
		runHarnessStatus(steps, args[1:])
	case "enroll":
		runHarnessEnroll(steps, args[1:])
	case "adopt":
		runHarnessAdopt(steps, args[1:])
	case "repair":
		runHarnessRepair(steps, args[1:])
	case "diff":
		runHarnessDiff(steps, args[1:])
	case "uninstall":
		runHarnessUninstall(steps, args[1:])
	case "rules":
		runHarnessRules(steps, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "atomic harness: unknown verb %q\n", args[0])
		os.Exit(2)
	}
}

func runHarnessList(steps install.Steps, args []string) {
	fs := flag.NewFlagSet("harness list", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness list [--json]")
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	rows, err := steps.List()
	if err != nil {
		fatal("harness list", err)
	}
	if jsonOut {
		encodeJSON(rows)
		return
	}
	for _, row := range rows {
		state := "discovered"
		if row.Enrolled {
			state = "enrolled"
		}
		fmt.Printf("%s\t%s\t%s\n", row.Instance.Kind, row.Instance.NativeRoot, state)
	}
}

func runHarnessStatus(steps install.Steps, args []string) {
	fs := flag.NewFlagSet("harness status", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness status [<target-key>] [--json]")
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	flagArgs, positional := splitBoolFlags(args)
	if err := fs.Parse(flagArgs); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	sel, err := harnessSelection(positional, "", "")
	if err != nil {
		fatal("harness status", err)
	}
	report, err := steps.Status(sel)
	if err != nil {
		fatal("harness status", err)
	}
	if jsonOut {
		encodeJSON(report)
		return
	}
	for _, inst := range report.Instances {
		state := "discovered"
		if inst.Enrolled {
			state = "enrolled"
		}
		fmt.Printf("%s\t%s\t%s\n", inst.Instance.Kind, inst.Instance.NativeRoot, state)
	}
	for _, t := range report.Targets {
		fmt.Printf("%s\t%s\n", t.Target.Key(), t.Status)
		for _, r := range t.Resources {
			fmt.Printf("  %s\t%s\n", r.State, r.Resource)
		}
	}
	if len(report.Retention.Journals) > 0 {
		fmt.Printf("retained: %d unresolved journal(s)\n", len(report.Retention.Journals))
	}
	for _, resource := range report.Shared {
		fmt.Printf("resource\t%s\tconsumers=%s\tvisible=%s\n", resource.ID, joinList(resource.Consumers), joinList(resource.VisibleTo))
	}
}

func runHarnessEnroll(steps install.Steps, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic harness enroll <claude|omp|codex> [--instance <root>] [flags]")
		os.Exit(2)
	}
	kind, err := harness.ParseKind(args[0])
	if err != nil {
		fatal("harness enroll", err)
	}

	fs := flag.NewFlagSet("harness enroll", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness enroll <claude|omp|codex> [--instance <root>] [--replace|--leave-unowned] [--dry-run] [--yes] [--json]")
	flags := addInstallFlags(fs)
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	steps, err = flags.steps(steps.Home)
	if err != nil {
		fatal("harness enroll", err)
	}
	flags.harness = string(kind)

	sel, err := flags.selection()
	if err != nil {
		fatal("harness enroll", err)
	}
	reports, err := steps.Converge(install.ConvergeRequest{Selection: sel, Enroll: true})
	if err != nil {
		fatal("harness enroll", err)
	}
	printConvergeReports("harness enroll", reports, flags.dryRun, flags.jsonOut)
}

func runHarnessAdopt(steps install.Steps, args []string) {
	// The harness name is an optional positional token before the flags; only
	// claude has a legacy install to adopt.
	rest := args
	if len(rest) > 0 && rest[0] != "" && rest[0][0] != '-' {
		kind, err := harness.ParseKind(rest[0])
		if err != nil {
			fatal("harness adopt", err)
		}
		if kind != harness.KindClaude {
			fatal("harness adopt", fmt.Errorf("only claude supports legacy adoption"))
		}
		rest = rest[1:]
	}

	fs := flag.NewFlagSet("harness adopt", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness adopt claude [--instance <root>] [--replace|--leave-unowned] [--acknowledge-snapshot] [--dry-run] [--yes] [--json]")
	var instance string
	var replace, leave, ackSnapshot, dryRun, yes, jsonOut bool
	fs.StringVar(&instance, "instance", "", "native root of the instance to adopt")
	fs.BoolVar(&replace, "replace", false, "replace unowned older-version artifacts")
	fs.BoolVar(&leave, "leave-unowned", false, "preserve unowned older-version artifacts")
	fs.BoolVar(&ackSnapshot, "acknowledge-snapshot", false, "accept an absent or corrupt legacy snapshot")
	fs.BoolVar(&dryRun, "dry-run", false, "print what would happen; make no changes")
	fs.BoolVar(&yes, "yes", false, "approve the printed plan without prompting")
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	if err := fs.Parse(rest); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	flags := installFlags{harness: "claude", instance: instance, replace: replace, leave: leave, dryRun: dryRun, yes: yes, jsonOut: jsonOut}
	steps, err := flags.steps(steps.Home)
	if err != nil {
		fatal("harness adopt", err)
	}
	sel, err := flags.selection()
	if err != nil {
		fatal("harness adopt", err)
	}
	reports, err := steps.Adopt(install.AdoptRequest{Selection: sel, AcknowledgeSnapshot: ackSnapshot})
	if err != nil {
		fatal("harness adopt", err)
	}
	printConvergeReports("harness adopt", reports, dryRun, jsonOut)
}

func runHarnessRepair(steps install.Steps, args []string) {
	fs := flag.NewFlagSet("harness repair", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness repair [--harness <claude|omp|codex>] [--instance <root>] [--dry-run] [--yes] [--json]")
	flags := addInstallFlags(fs)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	steps, err := flags.steps(steps.Home)
	if err != nil {
		fatal("harness repair", err)
	}
	sel, err := repairSelection(flags)
	if err != nil {
		fatal("harness repair", err)
	}
	reports, err := steps.Converge(install.ConvergeRequest{Selection: sel})
	if err != nil {
		fatal("harness repair", err)
	}
	printConvergeReports("harness repair", reports, flags.dryRun, flags.jsonOut)
}

func runHarnessDiff(steps install.Steps, args []string) {
	fs := flag.NewFlagSet("harness diff", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness diff [--harness <claude|omp|codex>] [--instance <root>] [--json]")
	flags := addInstallFlags(fs)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	sel, err := repairSelection(flags)
	if err != nil {
		fatal("harness diff", err)
	}
	rows, err := steps.Diff(sel)
	if err != nil {
		fatal("harness diff", err)
	}
	if flags.jsonOut {
		encodeJSON(rows)
		return
	}
	for _, r := range rows {
		fmt.Printf("%s\t%s\t%s\n", r.State, r.Target, r.Resource)
	}
}

func runHarnessUninstall(steps install.Steps, args []string) {
	fs := flag.NewFlagSet("harness uninstall", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic harness uninstall <target-key> [--dry-run] [--yes] [--json]  |  atomic harness uninstall --all")
	var all, dryRun, yes, jsonOut bool
	fs.BoolVar(&all, "all", false, "remove every enrolled target and completed operational state")
	fs.BoolVar(&dryRun, "dry-run", false, "print what would happen; make no changes")
	fs.BoolVar(&yes, "yes", false, "approve the printed plan without prompting")
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	flagArgs, positional := splitBoolFlags(args)
	if err := fs.Parse(flagArgs); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	steps.DryRun = dryRun
	steps.AssumeYes = yes

	if all {
		report, err := steps.UninstallAll()
		if err != nil {
			fatal("harness uninstall", err)
		}
		if jsonOut {
			encodeJSON(report)
			return
		}
		for _, removal := range report.Targets {
			printRemoval(removal)
		}
		printRecovery(report.Recovery)
		if len(report.Blockers) > 0 {
			printBlockers(report.Blockers)
			os.Exit(1)
		}
		fmt.Printf("removed %d ledger row(s), retained %d journal(s)\n", len(report.Cleanup.RemovedRows), len(report.Retention.Journals))
		return
	}

	var key string
	if len(positional) > 0 {
		key = positional[0]
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "Usage: atomic harness uninstall <target-key> [--dry-run] [--yes] [--json]  |  atomic harness uninstall --all")
		os.Exit(2)
	}
	removal, err := steps.UninstallTarget(key)
	if err != nil {
		fatal("harness uninstall", err)
	}
	if jsonOut {
		encodeJSON(removal)
		return
	}
	printRemoval(removal)
	if len(removal.Blockers) > 0 {
		printBlockers(removal.Blockers)
		os.Exit(1)
	}
}

// printRemoval renders one target's removal outcome.
func printRemoval(removal harness.Removal) {
	for _, id := range removal.Removed {
		fmt.Printf("removed\t%s\n", id)
	}
	for _, id := range removal.Retained {
		fmt.Printf("retained\t%s\t(another enrolled consumer depends on it)\n", id)
	}
	printRecovery(removal.Recovery)
}

// printRecovery renders a dry run's in-memory journal preview: what a real run
// would reconcile before planning, without having reconciled it.
func printRecovery(sims []installstate.RecoverySimulation) {
	for _, sim := range sims {
		fmt.Printf("recovery\t%s\t%d action(s), %d conflict(s)\n", sim.Journal, len(sim.Actions), len(sim.Conflicts))
	}
}

// printBlockers renders the observations that forbade a decidable plan.
func printBlockers(blockers []string) {
	for _, b := range blockers {
		fmt.Printf("blocked\t%s\n", b)
	}
}

func runHarnessRules(steps install.Steps, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic harness rules <status|sync> [flags]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("harness rules "+args[0], flag.ContinueOnError)
	cliutil.SetUsage(fs, fmt.Sprintf("atomic harness rules %s [--dry-run] [--yes] [--json]", args[0]))
	flags := addInstallFlags(fs)
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	steps, err := flags.steps(steps.Home)
	if err != nil {
		fatal("harness rules", err)
	}

	switch args[0] {
	case "status":
		statuses, err := steps.RulesStatus()
		if err != nil {
			fatal("harness rules status", err)
		}
		if flags.jsonOut {
			encodeJSON(statuses)
			return
		}
		for _, status := range statuses {
			tier := status.Tier
			if tier == "" {
				tier = "unsupported"
			}
			fmt.Printf("%s\t%s\n", status.Target.Key(), tier)
			for _, r := range status.Resources {
				fmt.Printf("  %s\t%s\n", r.State, r.Resource)
			}
			for _, gap := range status.Gaps {
				fmt.Printf("  gap\t%s\t%s\n", gap.Role, gap.Status)
			}
		}
	case "sync":
		reports, err := steps.RulesSync()
		if err != nil {
			fatal("harness rules sync", err)
		}
		printConvergeReports("harness rules sync", reports, flags.dryRun, flags.jsonOut)
	default:
		fmt.Fprintf(os.Stderr, "atomic harness rules: unknown verb %q\n", args[0])
		os.Exit(2)
	}
}

// joinList renders a name list for one status line.
func joinList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

// splitBoolFlags separates bool flags from positional arguments, so a
// positional target key may appear before or after them. Only bool flags are
// split: a value-taking flag still must precede the positional it follows.
func splitBoolFlags(args []string) (flags, positional []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

// harnessSelection builds a selection from an optional positional target key
// plus optional kind and instance filters.
func harnessSelection(rest []string, kindName, instance string) (install.Selection, error) {
	sel := install.Selection{Instance: instance}
	if kindName != "" {
		kind, err := harness.ParseKind(kindName)
		if err != nil {
			return sel, err
		}
		sel.Kind = kind
	}
	if len(rest) > 0 && rest[0] != "" {
		target, err := harness.ParseKey(rest[0])
		if err != nil {
			return sel, err
		}
		sel.Kind = target.Kind
		sel.Instance = target.Instance
	}
	return sel, nil
}

// repairSelection builds the enrolled-target selection repair, diff, and rules
// operations use.
func repairSelection(flags *installFlags) (install.Selection, error) {
	sel := install.Selection{Instance: flags.instance, EnrolledOnly: true}
	if flags.harness != "" {
		kind, err := harness.ParseKind(flags.harness)
		if err != nil {
			return sel, err
		}
		sel.Kind = kind
	}
	return sel, nil
}
