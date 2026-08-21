package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
	"github.com/spf13/cobra"
)

// buildScratchpadCmd registers the scratchpad verb: new, path, list, archive.
// --repo is the shared global root override (main.go), resolved through
// repoOverride like buildReminderCmd.
func buildScratchpadCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runScratchpad(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "scratchpad",
		Short: "Manage slug-keyed scratchpad bundles (new|path|list|archive)",
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
	addSub("new", "Create or extend a slug's bundle", "<slug>", func(c *cobra.Command) {
		c.Flags().String("purpose", "", "plan|implement|fix|diagnose|review")
	})
	addSub("path", "Print a slug's bundle path", "<slug>", nil)
	addSub("list", "List bundles (live, or --archived)", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "print JSON instead of a table")
		c.Flags().Bool("archived", false, "list the archive home instead of the live scratchpad root")
	})
	addSub("archive", "Archive a slug's bundle", "<slug>", nil)
	return parent
}

func runScratchpad(args []string, repoOverride string) {
	os.Exit(scratchpadDispatch(args, repoOverride, os.Stdout, os.Stderr))
}

// scratchpadDispatch is the testable seam for the real argv path: it returns
// an exit code instead of calling os.Exit, so a test can drive it through a
// raw []string{"new", slug, "--purpose", "plan"} without killing the test
// binary on the error paths. Mirrors repl.ReplAction's shape.
func scratchpadDispatch(args []string, repoOverride string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "Usage: atomic scratchpad <new|path|list|archive> [args]\n")
		return 1
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(stderr, "atomic scratchpad: %v\n", err)
		return 1
	}

	verb := args[0]
	switch verb {
	case "new":
		return scratchpadNewDispatch(args[1:], root, stdout, stderr)

	case "path":
		if len(args) < 2 || args[1] == "-h" || args[1] == "--help" {
			fmt.Fprintf(stderr, "Usage: atomic scratchpad path <slug>\n")
			return boolToExit(len(args) >= 2)
		}
		if err := config.ValidateSegment("slug", args[1]); err != nil {
			fmt.Fprintf(stderr, "atomic scratchpad path: %v\n", err)
			return 1
		}
		bundleRoot, err := scratchpadPathAction(root, args[1])
		if err != nil {
			fmt.Fprintf(stderr, "atomic scratchpad path: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, bundleRoot)
		return 0

	case "list":
		return scratchpadListDispatch(args[1:], root, stdout, stderr)

	case "archive":
		if len(args) < 2 || args[1] == "-h" || args[1] == "--help" {
			fmt.Fprintf(stderr, "Usage: atomic scratchpad archive <slug>\n")
			return boolToExit(len(args) >= 2)
		}
		if err := config.ValidateSegment("slug", args[1]); err != nil {
			fmt.Fprintf(stderr, "atomic scratchpad archive: %v\n", err)
			return 1
		}
		dest, err := scratchpad.Archive(root, args[1])
		if err != nil {
			fmt.Fprintf(stderr, "atomic scratchpad archive: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, dest)
		return 0

	default:
		fmt.Fprintf(stderr, "atomic scratchpad: unknown verb %q (want new|path|list|archive)\n", verb)
		return 1
	}
}

// scratchpadNewDispatch parses `new`'s arguments and runs the action. The
// slug is taken positionally before fs.Parse sees anything, because
// flag.FlagSet.Parse stops at the first non-flag token — parsing
// ["<slug>", "--purpose", "plan"] would otherwise never consume --purpose.
// Flags-first order (["--purpose", "plan", "<slug>"]) still works via
// fs.Args() below, so both orders the spec and this command's own usage
// string document are accepted.
func scratchpadNewDispatch(args []string, root string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratchpad new", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic scratchpad new <slug> --purpose <plan|implement|fix|diagnose|review>")
	var purpose string
	fs.StringVar(&purpose, "purpose", "", "plan|implement|fix|diagnose|review")

	rest := args
	var slug string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		slug = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if slug == "" {
		remaining := fs.Args()
		if len(remaining) == 0 {
			fmt.Fprintf(stderr, "Usage: atomic scratchpad new <slug> --purpose <p>\n")
			return 1
		}
		slug = remaining[0]
	}
	if purpose == "" {
		fmt.Fprintf(stderr, "atomic scratchpad new: --purpose is required\n")
		return 1
	}
	if err := config.ValidateSegment("slug", slug); err != nil {
		fmt.Fprintf(stderr, "atomic scratchpad new: %v\n", err)
		return 1
	}

	if archiveDir, ok := scratchpad.HasArchive(root, slug); ok {
		fmt.Fprintf(stdout, "prior work for %q is archived at %s; creating a fresh bundle\n", slug, archiveDir)
	}

	bundleRoot, extended, err := scratchpadNewAction(root, slug, purpose)
	if err != nil {
		fmt.Fprintf(stderr, "atomic scratchpad new: %v\n", err)
		return 1
	}
	if extended {
		fmt.Fprintf(stdout, "extending existing bundle for %q\n", slug)
	}
	fmt.Fprintln(stdout, bundleRoot)
	return 0
}

// scratchpadNewAction is the testable seam for `atomic scratchpad new`.
func scratchpadNewAction(root, slug, purpose string) (bundleRoot string, extended bool, err error) {
	_, extended, err = scratchpad.New(root, slug, purpose)
	if err != nil {
		return "", false, err
	}
	return scratchpad.BundleRoot(root, slug), extended, nil
}

// scratchpadPathAction is the testable seam for `atomic scratchpad path`.
func scratchpadPathAction(root, slug string) (string, error) {
	bundleRoot := scratchpad.BundleRoot(root, slug)
	if _, err := os.Stat(filepath.Join(bundleRoot, "meta.toml")); err != nil {
		return "", fmt.Errorf("no bundle for %q", slug)
	}
	return bundleRoot, nil
}

// scratchpadListDispatch parses `list`'s flags and prints its result as a
// table or, with --json, as JSON.
func scratchpadListDispatch(args []string, root string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratchpad list", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic scratchpad list [--json] [--archived]")
	var asJSON, archived bool
	fs.BoolVar(&asJSON, "json", false, "print JSON instead of a table")
	fs.BoolVar(&archived, "archived", false, "list the archive home instead of the live scratchpad root")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	entries, warnings, err := scratchpadListAction(root, archived)
	if err != nil {
		fmt.Fprintf(stderr, "atomic scratchpad list: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, w)
	}

	if asJSON {
		printScratchpadListJSON(stdout, entries, archived)
		return 0
	}
	printScratchpadListTable(stdout, entries, archived)
	return 0
}

// scratchpadListAction is the testable seam for `atomic scratchpad list`.
func scratchpadListAction(root string, archived bool) ([]scratchpad.Entry, []string, error) {
	listRoot := config.ScratchpadDir(root)
	if archived {
		listRoot = scratchpad.ArchiveRoot(root)
	}
	return scratchpad.List(listRoot)
}

// scratchpadListRow is the list --json shape: every field is a meta.toml
// field, plus the bundle's own path. Created is only meaningful for an
// archived row (the archive directory's own date key), so it is omitted for
// a live row rather than claiming to be one.
type scratchpadListRow struct {
	Slug        string   `json:"slug"`
	Purposes    []string `json:"purposes"`
	Status      string   `json:"status"`
	Created     string   `json:"created,omitempty"`
	Updated     string   `json:"updated"`
	Description string   `json:"description,omitempty"`
	Path        string   `json:"path"`
}

func printScratchpadListJSON(w io.Writer, entries []scratchpad.Entry, archived bool) {
	rows := make([]scratchpadListRow, 0, len(entries))
	for _, e := range entries {
		row := scratchpadListRow{
			Slug:        e.Meta.Slug,
			Purposes:    e.Meta.Purposes,
			Status:      e.Meta.Status,
			Updated:     e.Meta.Updated,
			Description: e.Meta.Description,
			Path:        e.Path,
		}
		if archived {
			row.Created = e.Meta.Created
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rows)
}

func printScratchpadListTable(w io.Writer, entries []scratchpad.Entry, archived bool) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	if archived {
		fmt.Fprintln(tw, "SLUG\tCREATED\tPURPOSES\tSTATUS\tUPDATED")
	} else {
		fmt.Fprintln(tw, "SLUG\tPURPOSES\tSTATUS\tUPDATED")
	}
	for _, e := range entries {
		purposes := strings.Join(e.Meta.Purposes, ",")
		if archived {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Meta.Slug, e.Meta.Created, purposes, e.Meta.Status, e.Meta.Updated)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Meta.Slug, purposes, e.Meta.Status, e.Meta.Updated)
		}
	}
	_ = tw.Flush()
}

// boolToExit maps "this was an explicit help request" to exit 0 and
// "this was a usage error" to exit 1, so --help on a positional-first verb
// behaves like --help everywhere else in the CLI.
func boolToExit(isHelp bool) int {
	if isHelp {
		return 0
	}
	return 1
}
