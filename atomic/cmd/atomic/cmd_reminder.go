package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

// The undocumented "set-due" verb has no cliusage entry; it reaches the handler
// through the parent.s ArbitraryArgs fallback.
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
		add, code := parseReminderAddArgs(args[1:], os.Stderr)
		if code != 0 {
			os.Exit(code)
		}
		var opts []reminder.Option
		if add.due != "" {
			opts = append(opts, reminder.WithDue(add.due))
		}
		if add.transport != "" {
			opts = append(opts, reminder.WithTransport(add.transport))
		}
		id, err := reminder.Add(root, add.text, opts...)
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

type reminderAddOpts struct {
	text, due, transport string
}

// parseReminderAddArgs keeps exit 1, the reminder verb's usage-error code, rather than the 2 other verbs use.
func parseReminderAddArgs(args []string, stderr io.Writer) (opts reminderAddOpts, exitCode int) {
	fs := flag.NewFlagSet("reminder add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic reminder add <text> [--due <RFC3339>] [--transport cron|routine|none]")
	fs.StringVar(&opts.due, "due", "", "RFC3339 due timestamp (e.g. 2026-05-24T09:00:00Z)")
	fs.StringVar(&opts.transport, "transport", "", "transport kind: cron, routine, or none")
	positional, err := cliutil.ParseInterspersed(fs, args)
	if err != nil {
		return opts, 1
	}
	if len(positional) == 0 {
		fs.Usage()
		return opts, 1
	}
	opts.text = strings.Join(positional, " ")
	return opts, 0
}
