package wiki

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	default:
		fmt.Fprintf(os.Stderr, "atomic wiki: unknown verb %q\n", verb)
		return 1
	}
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
