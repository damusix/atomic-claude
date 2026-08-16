package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

// buildReminderCmd builds the "reminder" parent + add|list|show|rm children.
// The undocumented "set-due" verb is not a cliusage entry; it routes via the
// parent's ArbitraryArgs fallback to the existing handler.
func buildReminderCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runReminder(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "reminder",
		Short: "Manage session reminders (add|list|show|rm|set-due)",
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
	addSub("add", "Create a reminder file; prints assigned id", "<text>", func(c *cobra.Command) {
		c.Flags().String("due", "", "RFC3339 due timestamp")
		c.Flags().String("transport", "", "transport kind: cron, routine, or none")
	})
	addSub("list", "List all reminders", "", nil)
	addSub("show", "Print body of a reminder", "<id>", nil)
	addSub("rm", "Delete a reminder", "<id>", nil)
	return parent
}

func runReminder(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic reminder <add|list|show|rm|set-due> [args]\n")
		os.Exit(1)
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic reminder: %v\n", err)
		os.Exit(1)
	}

	verb := args[0]
	switch verb {
	case "add":
		fs := flag.NewFlagSet("reminder add", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic reminder add <text> [--due <RFC3339>] [--transport cron|routine|none]")
		var due string
		var transport string
		fs.StringVar(&due, "due", "", "RFC3339 due timestamp (e.g. 2026-05-24T09:00:00Z)")
		fs.StringVar(&transport, "transport", "", "transport kind: cron, routine, or none")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		remaining := fs.Args()
		if len(remaining) == 0 {
			fmt.Fprintf(os.Stderr, "Usage: atomic reminder add [--due <iso>] [--transport <kind>] <text>\n")
			os.Exit(1)
		}
		text := strings.Join(remaining, " ")
		var opts []reminder.Option
		if due != "" {
			opts = append(opts, reminder.WithDue(due))
		}
		if transport != "" {
			opts = append(opts, reminder.WithTransport(transport))
		}
		id, err := reminder.Add(root, text, opts...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println(id)
	case "list":
		rows, err := reminder.List(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		for _, r := range rows {
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", r.ID, r.Created, r.Due, r.Transport, r.Preview)
		}
	case "show":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: atomic reminder show <id>\n")
			os.Exit(1)
		}
		body, err := reminder.Show(root, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Print(body)
	case "rm":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: atomic reminder rm <id>\n")
			os.Exit(1)
		}
		if err := reminder.Rm(root, args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "set-due":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: atomic reminder set-due <id> <iso>\n")
			os.Exit(1)
		}
		if err := reminder.SetDue(root, args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "atomic reminder: unknown verb %q\n", verb)
		os.Exit(1)
	}
}
