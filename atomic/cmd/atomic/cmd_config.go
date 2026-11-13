package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/spf13/cobra"
)

const configAgentsUsage = "atomic config agents [--harness <claude|omp|codex|all>]"

// chooseAgentsHarness is the interactive harness choice; a variable so tests
// answer it without a terminal.
var chooseAgentsHarness = func(options []harness.Kind) (harness.Kind, error) {
	opts := make([]prompt.Option[harness.Kind], len(options))
	for i, k := range options {
		opts[i] = prompt.Option[harness.Kind]{Label: string(k), Value: k}
	}
	return prompt.Select("Edit agent overrides for which harness?", opts)
}

// reapplyLegacyAgents patches a legacy Claude root no ledger owns, so no
// lifecycle convergence can reach it.
var reapplyLegacyAgents = claudeinstall.ReapplyAgents

// unadoptedLegacyClaude reports whether the Claude root carries a legacy
// install record that no ledger adoption has taken over.
func unadoptedLegacyClaude(home string) (string, bool, error) {
	root := claude.DefaultConfigDir(home)
	enrolled, err := install.TargetEnrolled(home, harness.Target{Kind: harness.KindClaude, Instance: root})
	if err != nil || enrolled {
		return root, false, err
	}
	rec, err := claudeinstall.ReadInstallRecord(home)
	if err != nil {
		return root, false, err
	}
	return root, rec.Present() && !rec.Adopted, nil
}

// agentsReach is what a save can reach: the enrolled kinds, and the Claude root
// an unadopted legacy install still owns, if any.
type agentsReach struct {
	enrolled   []harness.Kind
	legacyRoot string
}

func loadAgentsReach(home string) (agentsReach, error) {
	enrolled, err := harness.EnrolledKinds(home)
	if err != nil {
		return agentsReach{}, err
	}
	reach := agentsReach{enrolled: enrolled}
	if slices.Contains(enrolled, harness.KindClaude) {
		return reach, nil
	}
	root, legacy, err := unadoptedLegacyClaude(home)
	if legacy {
		reach.legacyRoot = root
	}
	return reach, err
}

// candidates counts an unadopted legacy Claude root as enrolled Claude.
func (r agentsReach) candidates() []harness.Kind {
	if r.legacyRoot == "" {
		return r.enrolled
	}
	return append([]harness.Kind{harness.KindClaude}, r.enrolled...)
}

// agentsHarnesses resolves the harnesses `atomic config agents` edits, by the
// agent override convergence flow in docs/spec/omp-plugin-compatibility.md.
func agentsHarnesses(flagValue, home string, env map[string]string) ([]harness.Kind, agentsReach, error) {
	reach, err := loadAgentsReach(home)
	if err != nil {
		return nil, reach, err
	}
	candidates := reach.candidates()
	if flagValue == "all" {
		if len(candidates) == 0 {
			return nil, reach, fmt.Errorf("no harness is enrolled; name one with --harness claude|omp|codex")
		}
		return candidates, reach, nil
	}
	kind, err := harness.SelectAgentsHarness(flagValue, func() harness.Detection { return harness.Detect(env) }, candidates)
	if err == nil {
		return []harness.Kind{kind}, reach, nil
	}
	var unresolved *harness.UnresolvedHarnessError
	if !errors.As(err, &unresolved) {
		return nil, reach, err
	}
	options := candidates
	if len(options) == 0 {
		options = []harness.Kind{harness.KindClaude, harness.KindOMP, harness.KindCodex}
	}
	kind, chooseErr := chooseAgentsHarness(options)
	switch {
	case errors.Is(chooseErr, prompt.ErrNonInteractive):
		return nil, reach, fmt.Errorf("%w; pass --harness claude|omp|codex|all", err)
	case chooseErr != nil:
		return nil, reach, fmt.Errorf("choose a harness: %w", chooseErr)
	}
	return []harness.Kind{kind}, reach, nil
}

// runConfigAgents edits each selected harness's form, then converges the
// enrolled ones through the lifecycle so the ledger owns every agent byte.
func runConfigAgents(args []string, home string, env map[string]string) int {
	fs := flag.NewFlagSet("config-agents", flag.ContinueOnError)
	cliutil.SetUsage(fs, configAgentsUsage)
	fs.SetOutput(os.Stderr)
	harnessFlag := fs.String("harness", "", "claude, omp, codex, or all")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s\n", configAgentsUsage)
		return 2
	}
	if f := *harnessFlag; f != "" && f != "all" {
		if _, err := harness.ParseKind(f); err != nil {
			fmt.Fprintf(os.Stderr, "atomic config agents: %v\nUsage: %s\n", err, configAgentsUsage)
			return 2
		}
	}
	kinds, reach, err := agentsHarnesses(*harnessFlag, home, env)
	switch {
	case errors.Is(err, prompt.ErrAborted):
		fmt.Fprintln(os.Stderr, "atomic config agents: aborted")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "atomic config agents: %v\n", err)
		return 1
	}

	failed := false
	for _, kind := range kinds {
		verb := fmt.Sprintf("config agents --harness %s", kind)
		form, err := config.AgentFormFor(string(kind))
		if err == nil {
			err = config.EditAgents(home, form)
		}
		switch {
		case errors.Is(err, config.ErrNonInteractiveAgents):
			fmt.Fprintf(os.Stderr, "atomic %s: requires an interactive terminal\n", verb)
			return 1
		case errors.Is(err, config.ErrAgentsAborted):
			fmt.Fprintf(os.Stderr, "atomic %s: aborted\n", verb)
			return 1
		case err != nil:
			fmt.Fprintf(os.Stderr, "atomic %s: %v\n", verb, err)
			failed = true
		case !convergeAgents(home, verb, form, reach):
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// convergeAgents applies a saved form to its harness and reports false when that
// failed; the saved config stays saved either way.
func convergeAgents(home, verb string, form config.AgentForm, reach agentsReach) bool {
	kind := harness.Kind(form.Harness)
	if slices.Contains(reach.enrolled, kind) {
		steps := install.DefaultSteps(home)
		steps.AssumeYes = true
		reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: kind, EnrolledOnly: true}})
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic %s: saved, but convergence failed: %v\n", verb, err)
			return false
		}
		if printConvergeReports(verb, reports, false, false) {
			fmt.Fprintf(os.Stderr, "atomic %s: saved; a blocked target keeps its files until `atomic harness repair` converges it\n", verb)
			return false
		}
		fmt.Println(form.RestartNote)
		return true
	}

	if kind == harness.KindClaude && reach.legacyRoot != "" {
		changed, _, err := reapplyLegacyAgents(reach.legacyRoot, home)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic %s: saved, but could not patch %s: %v\n", verb, reach.legacyRoot, err)
			return false
		}
		fmt.Printf("%s: patched %d agent file(s)\n", reach.legacyRoot, len(changed))
		fmt.Println(form.RestartNote)
		return true
	}
	fmt.Printf("%s agent overrides saved; the change applies when %s is enrolled\n", kind, kind)
	return true
}

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
	agents := &cobra.Command{
		Use:                "agents",
		Short:              "Set per-agent model and effort for a harness interactively",
		Annotations:        map[string]string{"args_hint": "[--harness <claude|omp|codex|all>]"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic config: resolve home dir: %v\n", err)
				os.Exit(2)
			}
			os.Exit(runConfigAgents(args, home, harness.Environ()))
			return nil
		},
	}
	agents.Flags().String("harness", "", "claude, omp, codex, or all")
	parent.AddCommand(agents)
	return parent
}
