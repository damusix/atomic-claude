package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/coldprompt"
	"github.com/spf13/cobra"
)

// buildPromptCmd builds the "prompt" parent + git-cleanup|claude-merge|implementer|reviewer children.
func buildPromptCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "prompt",
		Short: "Emit cold-op briefs (git-cleanup|claude-merge|implementer|reviewer)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runPrompt(args); return nil },
	}
	addSub := func(verb, short string) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": ""},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				runPrompt(append([]string{verb}, args...))
				return nil
			},
		}
		parent.AddCommand(c)
	}
	addSub("git-cleanup", "Emit the git-cleanup cold-op brief")
	addSub("claude-merge", "Emit the CLAUDE.md merge cold-op brief")
	addSub("implementer", "Emit the implementer subagent prompt brief")
	addSub("reviewer", "Emit the reviewer subagent prompt brief")
	return parent
}

// promptAction executes the prompt subcommand logic and returns an exit code.
// Extracted from runPrompt so tests can exercise dispatch without os.Exit.
// out receives the brief text on success; errOut receives error messages.
func promptAction(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(errOut, "Usage: atomic prompt <name>\n")
		fmt.Fprintf(errOut, "Valid names: %s\n", strings.Join(coldprompt.Names(), ", "))
		return 1
	}
	text, err := coldprompt.Get(args[0])
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		return 1
	}
	fmt.Fprint(out, text)
	return 0
}

// runPrompt is the os.Exit-aware entry point for the prompt top-level verb.
func runPrompt(args []string) {
	os.Exit(promptAction(args, os.Stdout, os.Stderr))
}
