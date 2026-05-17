package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/signals"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

func main() {
	fs := flag.NewFlagSet("atomic", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: atomic [flags] <command> [args]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  signals scan        Walk repo and write deterministic-signals.md\n")
		fmt.Fprintf(os.Stderr, "  signals show        Print deterministic-signals.md to stdout\n")
		fmt.Fprintf(os.Stderr, "  signals stale       Exit 0 if fresh, 1 if stale\n")
		fmt.Fprintf(os.Stderr, "  signals diff        Print unified diff of signals file\n")
		fmt.Fprintf(os.Stderr, "  reminder add <text> Create a reminder file; prints assigned id\n")
		fmt.Fprintf(os.Stderr, "  reminder list       List all reminders\n")
		fmt.Fprintf(os.Stderr, "  reminder show <id>  Print body of a reminder\n")
		fmt.Fprintf(os.Stderr, "  reminder rm <id>    Delete a reminder\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
	}

	var showVersion bool
	var repoOverride string
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showVersion, "v", false, "print version and exit (short)")
	fs.StringVar(&repoOverride, "repo", "", "repo root override (default: detect via git)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	if showVersion {
		fmt.Printf("atomic %s (%s)\n", version.Version, version.Commit)
		return
	}

	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	switch args[0] {
	case "signals":
		runSignals(args[1:], repoOverride)
	case "reminder":
		runReminder(args[1:], repoOverride)
	default:
		fmt.Fprintf(os.Stderr, "atomic: unknown command %q\n", args[0])
		os.Exit(1)
	}
}

func runReminder(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic reminder <add|list|show|rm> [args]\n")
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
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: atomic reminder add <text>\n")
			os.Exit(1)
		}
		text := strings.Join(args[1:], " ")
		id, err := reminder.Add(root, text)
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
			fmt.Printf("%s\t%s\t%s\n", r.ID, r.Created, r.Preview)
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
	default:
		fmt.Fprintf(os.Stderr, "atomic reminder: unknown verb %q\n", verb)
		os.Exit(1)
	}
}

func runSignals(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic signals <scan|show|stale|diff>\n")
		os.Exit(1)
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic signals: %v\n", err)
		os.Exit(1)
	}

	verb := args[0]
	switch verb {
	case "scan":
		if err := signals.Scan(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := signals.Show(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "stale":
		err := signals.Stale(root)
		if err == nil {
			return // fresh → exit 0
		}
		if err == signals.ErrStale {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	case "diff":
		err := signals.Diff(root)
		if err == nil {
			return // no diff → exit 0
		}
		if err == signals.ErrDiffPresent {
			os.Exit(1)
		}
		if err == signals.ErrNoPrior {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "atomic signals: unknown verb %q\n", verb)
		os.Exit(1)
	}
}
