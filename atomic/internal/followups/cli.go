package followups

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// Run is the entry point for `atomic followups <verb>`. clock is injected for
// tests; production passes time.Now. Exit codes: 0 ok, 1 error, 2 usage.
func Run(args []string, repoRoot string, stdout, stderr io.Writer, clock func() time.Time) int {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	dir := config.FollowupsDir(repoRoot)
	verb := args[0]
	rest := args[1:]

	switch verb {
	case "path":
		if code := rejectArgs(bareFlagSet(verb, stderr), verb, rest); code != 0 {
			return code
		}
		fmt.Fprintln(stdout, dir)
		return 0

	case "render":
		if code := rejectArgs(bareFlagSet(verb, stderr), verb, rest); code != 0 {
			return code
		}
		return runRender(dir, stdout, stderr, clock())

	case "list":
		return runList(rest, dir, stdout, stderr, clock())

	case "add":
		return runAdd(rest, dir, repoRoot, stdout, stderr, clock())

	case "close":
		return runClose(rest, dir, stdout, stderr, clock())

	default:
		fmt.Fprintf(stderr, "atomic followups: unknown verb %q\n", verb)
		printUsage(stderr)
		return 2
	}
}

func runRender(dir string, stdout, stderr io.Writer, today time.Time) int {
	entries, err := LoadEntries(dir)
	if err != nil {
		fmt.Fprintf(stderr, "atomic followups render: %v\n", err)
		return 1
	}
	content := Render(entries, today)
	indexPath := filepath.Join(dir, "INDEX.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "atomic followups render: mkdir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		fmt.Fprintf(stderr, "atomic followups render: write INDEX.md: %v\n", err)
		return 1
	}
	return 0
}

func runList(args []string, dir string, stdout, stderr io.Writer, today time.Time) int {
	fs := flag.NewFlagSet("followups-list", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic followups list [--stale] [--json]")
	fs.SetOutput(stderr)
	var stale bool
	var asJSON bool
	fs.BoolVar(&stale, "stale", false, "show only stale entries")
	fs.BoolVar(&asJSON, "json", false, "output as JSON array")
	if code := rejectArgs(fs, "list", args); code != 0 {
		return code
	}

	entries, err := ListEntries(dir, ListOpts{StaleOnly: stale, Today: today})
	if err != nil {
		fmt.Fprintf(stderr, "atomic followups list: %v\n", err)
		return 1
	}

	if asJSON {
		out, err := FormatListJSON(entries)
		if err != nil {
			fmt.Fprintf(stderr, "atomic followups list: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, out)
		return 0
	}

	fmt.Fprint(stdout, FormatListHuman(entries, today))
	return 0
}

func runAdd(args []string, dir, repoRoot string, stdout, stderr io.Writer, today time.Time) int {
	fs := flag.NewFlagSet("followups-add", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic followups add --id <id> --title <t> [--kind finding|plan] [--severity risk|nit|question] [--origin <o>] [--file <f>] [--body <b|->]")
	fs.SetOutput(stderr)
	var id, title, kind, severity, origin, file, body string
	fs.StringVar(&id, "id", "", "entry id (kebab-case)")
	fs.StringVar(&title, "title", "", "entry title")
	fs.StringVar(&kind, "kind", "", "kind: finding (default) or plan")
	fs.StringVar(&severity, "severity", "", "severity: risk, nit, or question")
	fs.StringVar(&origin, "origin", "", "origin text")
	fs.StringVar(&file, "file", "", "optional file:lines reference")
	fs.StringVar(&body, "body", "", "body content; use '-' to read from stdin")
	if code := rejectArgs(fs, "add", args); code != 0 {
		return code
	}

	// --kind is validated first, or a bad kind surfaces as "missing --severity".
	if kind != "" {
		if _, err := parseKind(kind); err != nil {
			fmt.Fprintf(stderr, "atomic followups add: %v\n", err)
			return 1
		}
	}

	var missing []string
	if title == "" {
		missing = append(missing, "--title")
	}
	if origin == "" {
		missing = append(missing, "--origin")
	}
	if severity == "" && kind != string(KindPlan) {
		missing = append(missing, "--severity")
	}
	if len(missing) > 0 {
		for _, m := range missing {
			fmt.Fprintf(stderr, "atomic followups add: missing required flag %s\n", m)
		}
		return 1
	}

	if body == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(stderr, "atomic followups add: read stdin: %v\n", err)
			return 1
		}
		body = string(raw)
	}

	path, err := Add(dir, AddOpts{
		ID:       id,
		Title:    title,
		Kind:     kind,
		Severity: severity,
		Origin:   origin,
		File:     file,
		Body:     body,
		Today:    today,
	})
	if err != nil {
		fmt.Fprintf(stderr, "atomic followups add: %v\n", err)
		return 1
	}

	_ = runRender(dir, io.Discard, io.Discard, today)

	fmt.Fprintln(stdout, path)
	return 0
}

func runClose(args []string, dir string, stdout, stderr io.Writer, today time.Time) int {
	fs := flag.NewFlagSet("followups-close", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic followups close <id> [--reason <r>]")
	fs.SetOutput(stderr)
	var reason string
	fs.StringVar(&reason, "reason", "", "optional closure reason")
	positional, err := cliutil.ParseInterspersed(fs, args)
	if err != nil {
		return 2
	}

	if len(positional) == 0 {
		fs.Usage()
		return 2
	}
	if len(positional) > 1 {
		cliutil.UnexpectedArgs(fs, "followups close", positional[1:])
		return 2
	}
	id := positional[0]

	if err := CloseEntry(dir, id, reason, today); err != nil {
		fmt.Fprintf(stderr, "atomic followups close: %v\n", err)
		return 1
	}
	return 0
}

func bareFlagSet(verb string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("followups-"+verb, flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic followups "+verb)
	fs.SetOutput(stderr)
	return fs
}

func rejectArgs(fs *flag.FlagSet, verb string, args []string) int {
	positional, err := cliutil.ParseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(positional) > 0 {
		cliutil.UnexpectedArgs(fs, "followups "+verb, positional)
		return 2
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: atomic followups <list|add|close|render|path> [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  list [--stale] [--json]           List open follow-up entries")
	fmt.Fprintln(w, "  add --id <id> --title <t> --severity <s> --origin <o> [--file <f>] [--body -]")
	fmt.Fprintln(w, "                                    Create a new follow-up entry")
	fmt.Fprintln(w, "  close <id> [--reason <r>]         Close an entry; appends to CLOSED.md")
	fmt.Fprintln(w, "  render                            Regenerate INDEX.md from open entries")
	fmt.Fprintln(w, "  path                              Print absolute path to the followups folder")
}
