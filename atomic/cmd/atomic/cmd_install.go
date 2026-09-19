package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/spf13/cobra"
)

func buildInstallCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "install",
		Short:              "Install Atomic into explicitly selected harness instances",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runInstall(args)
			return nil
		},
	}
	c.Flags().String("harness", "", "harness to install (claude or omp)")
	c.Flags().String("instance", "", "native root of the instance to install")
	c.Flags().Bool("all", false, "install every discovered instance")
	c.Flags().Bool("replace", false, "replace unowned older-version artifacts the selected generation cannot prove")
	c.Flags().Bool("leave-unowned", false, "preserve unowned older-version artifacts and record no ownership")
	c.Flags().Bool("dry-run", false, "print what would happen; make no changes")
	c.Flags().Bool("yes", false, "approve the printed plan without prompting")
	c.Flags().Bool("json", false, "emit machine-readable JSON output")
	return c
}

// installFlags is the parsed flag set shared by install and harness enroll.
type installFlags struct {
	harness  string
	instance string
	all      bool
	replace  bool
	leave    bool
	dryRun   bool
	yes      bool
	jsonOut  bool
}

func addInstallFlags(fs *flag.FlagSet) *installFlags {
	out := &installFlags{}
	fs.StringVar(&out.harness, "harness", "", "harness to install (claude or omp)")
	fs.StringVar(&out.instance, "instance", "", "native root of the instance")
	fs.BoolVar(&out.all, "all", false, "operate on every discovered instance")
	fs.BoolVar(&out.replace, "replace", false, "replace unowned older-version artifacts")
	fs.BoolVar(&out.leave, "leave-unowned", false, "preserve unowned older-version artifacts")
	fs.BoolVar(&out.dryRun, "dry-run", false, "print what would happen; make no changes")
	fs.BoolVar(&out.yes, "yes", false, "approve the printed plan without prompting")
	fs.BoolVar(&out.jsonOut, "json", false, "emit machine-readable JSON output")
	return out
}

// selection maps the parsed flags onto an install selection.
func (f *installFlags) selection() (install.Selection, error) {
	sel := install.Selection{Instance: f.instance, All: f.all}
	if f.harness != "" {
		kind, err := harness.ParseKind(f.harness)
		if err != nil {
			return sel, err
		}
		sel.Kind = kind
	}
	return sel, nil
}

// batchDecision maps --replace/--leave-unowned onto the adoption decision.
func (f *installFlags) batchDecision() (installstate.Decision, error) {
	switch {
	case f.replace && f.leave:
		return "", fmt.Errorf("--replace and --leave-unowned are mutually exclusive")
	case f.replace:
		return installstate.DecisionReplace, nil
	case f.leave:
		return installstate.DecisionLeaveUnowned, nil
	default:
		return "", nil
	}
}

// steps builds the engine's seams from parsed flags.
func (f *installFlags) steps(home string) (install.Steps, error) {
	decision, err := f.batchDecision()
	if err != nil {
		return install.Steps{}, err
	}
	steps := install.DefaultSteps(home)
	steps.DryRun = f.dryRun
	steps.AssumeYes = f.yes
	steps.BatchDecision = decision
	return steps, nil
}

func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic install --harness <claude|omp> [--instance <root>] [--all] [--replace|--leave-unowned] [--dry-run] [--yes] [--json]")
	flags := addInstallFlags(fs)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fatal("install", err)
	}
	steps, err := flags.steps(home)
	if err != nil {
		fatal("install", err)
	}
	sel, err := flags.selection()
	if err != nil {
		fatal("install", err)
	}

	if sel.Kind == "" && !sel.All {
		fmt.Fprintln(os.Stderr, "atomic install: select a harness with --harness <claude|omp>, or pass --all")
		os.Exit(2)
	}

	reports, err := steps.Converge(install.ConvergeRequest{Selection: sel, Enroll: true})
	if err != nil {
		fatal("install", err)
	}
	printConvergeReports("install", reports, flags.dryRun, flags.jsonOut)
}

// printConvergeReports renders convergence outcomes. A blocked target exits
// non-zero so a scripted install never reads a refusal as success.
func printConvergeReports(verb string, reports []install.ConvergeReport, dryRun, jsonOut bool) {
	if jsonOut {
		encodeJSON(map[string]any{"verb": verb, "dry_run": dryRun, "targets": reports})
		return
	}
	blocked := false
	for _, r := range reports {
		switch {
		case len(r.Blockers) > 0:
			blocked = true
			fmt.Printf("%s\t%s\t%s\n", r.Target.Key(), r.Status, r.Blockers[0])
			for _, b := range r.Blockers[1:] {
				fmt.Printf("\t\t%s\n", b)
			}
		case r.Applied:
			fmt.Printf("%s\t%s\tapplied\n", r.Target.Key(), r.Status)
		default:
			fmt.Printf("%s\t%s\n", r.Target.Key(), r.Status)
		}
	}
	if dryRun {
		fmt.Println("(dry-run — no changes written)")
	}
	if blocked {
		os.Exit(1)
	}
}

// encodeJSON writes one JSON document to stdout, aborting the verb on failure.
func encodeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal("json", err)
	}
}

// fatal reports a verb error and exits non-zero.
func fatal(verb string, err error) {
	fmt.Fprintf(os.Stderr, "atomic %s: %v\n", verb, err)
	os.Exit(1)
}
