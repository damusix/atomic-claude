package main

import (
	"cmp"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/workflow"
	"github.com/spf13/cobra"
)

func buildWorkflowCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runWorkflow(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "workflow",
		Short: "Source-control, issue-tracker, and worktree preferences (resolve|set)",
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
		flagFn(c)
		parent.AddCommand(c)
	}
	addSub("resolve", "Print the resolved workflow preferences", "", func(c *cobra.Command) {
		c.Flags().String("cwd", "", workflowCwdHelp)
		c.Flags().String("harness", "", workflowHarnessHelp)
		c.Flags().Bool("json", false, "print JSON instead of the block")
	})
	addSub("set", "Write one workflow preference line", "<key> <value>", func(c *cobra.Command) {
		c.Flags().String("level", "", workflowLevelHelp)
	})
	return parent
}

const (
	workflowCwdHelp     = "directory to resolve from (default: --repo, else process directory)"
	workflowHarnessHelp = "harness kind: claude, omp, or codex (default: detect)"
	workflowLevelHelp   = "level to write: profile, realm, or repo (default profile)"
	workflowUsage       = "Usage: atomic workflow <resolve|set> [flags]\n"
)

type workflowEnv struct {
	home, cwd, repo string
	detect          func() harness.Detection
}

func runWorkflow(args []string, repoOverride string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic workflow: resolve home dir: %v\n", err)
		os.Exit(1)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic workflow: %v\n", err)
		os.Exit(1)
	}
	os.Exit(workflowAction(args, workflowEnv{
		home: home, cwd: cwd, repo: repoOverride,
		detect: func() harness.Detection { return harness.Detect(harness.Environ()) },
	}, os.Stdout, os.Stderr))
}

func workflowAction(args []string, e workflowEnv, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, workflowUsage)
		return 2
	}
	switch args[0] {
	case "resolve":
		return workflowResolve(args[1:], e, stdout, stderr)
	case "set":
		return workflowSet(args[1:], e, stdout, stderr)
	}
	fmt.Fprintf(stderr, "atomic workflow: unknown verb %q\n%s", args[0], workflowUsage)
	return 2
}

func workflowResolve(args []string, e workflowEnv, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("workflow resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic workflow resolve [--cwd DIR] [--harness claude|omp|codex] [--json]")
	var cwd, harnessFlag string
	var asJSON bool
	fs.StringVar(&cwd, "cwd", "", workflowCwdHelp)
	fs.StringVar(&harnessFlag, "harness", "", workflowHarnessHelp)
	fs.BoolVar(&asJSON, "json", false, "print JSON instead of the block")
	positional, err := cliutil.ParseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(positional) > 0 {
		cliutil.UnexpectedArgs(fs, "workflow resolve", positional)
		return 2
	}
	chosen, err := harness.Choose(harnessFlag, e.detect)
	if err != nil {
		fmt.Fprintf(stderr, "atomic workflow resolve: %v\n", err)
		return 2
	}
	if cwd == "" {
		cwd = cmp.Or(e.repo, e.cwd)
	}

	r, err := workflow.Resolve(workflow.Options{Dir: cwd, Home: e.home, Harness: string(chosen.Kind)})
	if err != nil {
		fmt.Fprintf(stderr, "atomic workflow resolve: %v\n", err)
		return 1
	}
	if !asJSON {
		fmt.Fprint(stdout, workflow.RenderText(r))
		return 0
	}
	out, err := workflow.RenderJSON(r)
	if err != nil {
		fmt.Fprintf(stderr, "atomic workflow resolve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", out)
	return 0
}

func workflowSet(args []string, e workflowEnv, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("workflow set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic workflow set <key> <value> [--level profile|realm|repo]")
	var levelFlag string
	fs.StringVar(&levelFlag, "level", string(workflow.LevelProfile), workflowLevelHelp)
	positional, err := cliutil.ParseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(positional) != 2 {
		fmt.Fprintf(stderr, "atomic workflow set: want <key> <value>, got %d argument(s)\n", len(positional))
		fs.Usage()
		return 2
	}
	key, value := positional[0], positional[1]
	level := workflow.Level(levelFlag)
	switch level {
	case workflow.LevelProfile, workflow.LevelRealm, workflow.LevelRepo:
	default:
		fmt.Fprintf(stderr, "atomic workflow set: unknown level %q: want profile, realm, or repo\n", levelFlag)
		return 2
	}
	if err := workflow.ValidateSetting(key, value); err != nil {
		fmt.Fprintf(stderr, "atomic workflow set: %v\n", err)
		return 2
	}

	fail := func(err error) int {
		fmt.Fprintf(stderr, "atomic workflow set: %v\n", err)
		return 1
	}
	path, shared, err := workflow.TargetPath(level, cmp.Or(e.repo, e.cwd), e.home)
	if err != nil {
		return fail(err)
	}
	if level == workflow.LevelProfile {
		unlock, err := profile.LockProfile(e.home)
		if err != nil {
			return fail(err)
		}
		defer unlock()
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	content := string(raw)
	if level == workflow.LevelProfile {
		if content, err = profile.EnsureWorkflowBlock(content); err != nil {
			return fail(fmt.Errorf("%s: %w", path, err))
		}
	}
	content, err = workflow.SetValue(content, level, workflow.Key(key), value, shared)
	if err != nil {
		return fail(fmt.Errorf("%s: %w", path, err))
	}
	if err := managedfile.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return 0
}
