package wiki

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WikiAction is the exported entry point for `atomic wiki` used by cmd/atomic/main.go.
// It delegates to wikiAction, which is the unexported testable seam.
func WikiAction(args []string, claudeHome, cwd string, out io.Writer) int {
	return wikiAction(args, claudeHome, cwd, out)
}

// wikiAction is the testable seam for `atomic wiki` subcommand dispatch.
//
// Parameters:
//   - args: the arguments after "wiki" (e.g. ["scan", "--root=/path"])
//   - claudeHome: path to ~/.claude (injected; never os.UserHomeDir() here)
//   - cwd: current working directory used as root when --root is absent
//   - out: writer for stdout handoff (injected for testability)
//
// Returns an exit code: 0 on success, 1 on usage/soft error, 2 on hard error.
func wikiAction(args []string, claudeHome, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki <scan> [flags]\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "scan":
		return wikiScanAction(args[1:], claudeHome, cwd, out)
	case "stale":
		return wikiStaleAction(args[1:], cwd, out)
	case "stamp":
		return wikiStampAction(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki: unknown verb %q\n", verb)
		return 1
	}
}

// wikiStaleAction implements `atomic wiki stale [--root=<path>]`.
// Read-only freshness check. Exit 0 fresh / 1 stale / 2 hard error.
// Stdout is injected via out so tests can capture the DRIFT/STALE lines.
func wikiStaleAction(args []string, cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-stale", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "root directory to check (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if root == "" {
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki stale: resolve root: %v\n", err)
		return 2
	}

	code, err := Stale(absRoot, out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki stale: %v\n", err)
	}
	return code
}

// wikiStampAction implements:
//
//	atomic wiki stamp <file> --repo <path>                        (summary mode)
//	atomic wiki stamp <file> --root <wiki-root> --cites a,b,c    (concern mode)
//
// It is an INTERNAL helper invoked by /refresh-wiki — not surfaced in /atomic-help.
func wikiStampAction(args []string) int {
	fs := flag.NewFlagSet("wiki-stamp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var repo string  // summary mode: repo whose HEAD to stamp
	var root string  // concern mode: wiki root
	var cites string // concern mode: comma-separated cited repo ids

	fs.StringVar(&repo, "repo", "", "repo path (summary mode)")
	fs.StringVar(&root, "root", "", "wiki root (concern mode)")
	fs.StringVar(&cites, "cites", "", "comma-separated cited repo ids (concern mode)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	positional := fs.Args()
	if len(positional) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: atomic wiki stamp <file> --repo <path>\n")
		fmt.Fprintf(os.Stderr, "       atomic wiki stamp <file> --root <wiki-root> --cites <ids>\n")
		return 1
	}

	filePath := positional[0]

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki stamp: resolve file path: %v\n", err)
		return 1
	}

	switch {
	case repo != "" && root == "" && cites == "":
		// Summary mode.
		absRepo, err := filepath.Abs(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: resolve --repo: %v\n", err)
			return 1
		}
		if err := StampSummary(absFile, absRepo); err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: %v\n", err)
			return 1
		}
		return 0

	case root != "" && cites != "":
		// Concern mode.
		absRoot, err := filepath.Abs(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: resolve --root: %v\n", err)
			return 1
		}
		ids := splitCites(cites)
		if err := StampConcern(absFile, absRoot, ids); err != nil {
			fmt.Fprintf(os.Stderr, "atomic wiki stamp: %v\n", err)
			return 1
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr, "atomic wiki stamp: supply either --repo (summary) or --root + --cites (concern)\n")
		return 1
	}
}

// splitCites splits a comma-separated cites string into a slice of trimmed ids.
// Empty elements are discarded.
func splitCites(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// wikiScanAction implements `atomic wiki scan [--root=<path>]`.
// It resolves the root, runs Scan (CP1), registers the wiki in CLAUDE.md,
// and prints the deterministic stdout handoff.
func wikiScanAction(args []string, claudeHome, cwd string, out io.Writer) int {
	fs := flag.NewFlagSet("wiki-scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var root string
	fs.StringVar(&root, "root", "", "root directory to scan (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Resolve root: --root flag or cwd.
	if root == "" {
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: resolve root: %v\n", err)
		return 1
	}

	// Inject a stable clock for tests via Options; real callers use wall clock.
	opts := Options{
		Clock: func() time.Time { return time.Now().UTC() },
	}

	// Run CP1 Scan: discover repos, scaffold wiki/, write <wiki-scan> block.
	// Scan returns the classified members directly — no second filesystem walk needed.
	members, err := Scan(absRoot, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %v\n", err)
		return 1
	}

	wikiDir := filepath.Join(absRoot, "wiki")

	// Register the wiki's index.md in ~/.claude/CLAUDE.md.
	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	wikiIndexPath := filepath.Join(wikiDir, "index.md")
	if err := RegisterWiki(claudeMDPath, wikiIndexPath); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: register wiki: %v\n", err)
		return 1
	}

	// Print deterministic stdout handoff.
	PrintHandoff(out, members)

	return 0
}
