package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/doctemplate"
	"github.com/spf13/cobra"
)

// buildTemplateCmd builds the "template" parent + one child per embedded
// document template (design-doc, spec, brief, state, followups,
// session-report, diagnose-context, implementation-log).
func buildTemplateCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "template",
		Short: "Emit document skeletons (design-doc|spec|brief|state|followups|...)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runTemplate(args); return nil },
	}
	for _, name := range doctemplate.Names() {
		name := name
		c := &cobra.Command{
			Use:                name,
			Short:              "Emit the " + name + " document template",
			Annotations:        map[string]string{"args_hint": ""},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				runTemplate(append([]string{name}, args...))
				return nil
			},
		}
		parent.AddCommand(c)
	}
	return parent
}

// --- package-resident nested switches → Cobra subcommands ---------------
//
// Same pattern: parent has Args:cobra.ArbitraryArgs so unknown verbs and
// the no-args case fall through to the existing handler; each child sets
// DisableFlagParsing:true and prepends its name to reconstruct the arg shape
// the existing package handler expects.

// templateAction executes the template subcommand logic and returns an exit
// code. Extracted from runTemplate so tests can exercise dispatch without
// os.Exit. out receives the template text on success; errOut receives error
// messages.
func templateAction(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(errOut, "Usage: atomic template <name>\n")
		fmt.Fprintf(errOut, "Valid names: %s\n", strings.Join(doctemplate.Names(), ", "))
		return 1
	}
	text, err := doctemplate.Get(args[0])
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		return 1
	}
	fmt.Fprint(out, text)
	return 0
}

// runTemplate is the os.Exit-aware entry point for the template top-level verb.
func runTemplate(args []string) {
	os.Exit(templateAction(args, os.Stdout, os.Stderr))
}
