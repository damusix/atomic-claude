package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
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
		fmt.Fprintf(os.Stderr, "  hooks session-start [--format=text]  Print session-start hook payload\n")
		fmt.Fprintf(os.Stderr, "  hooks install [--scope user|project]  Install session-start hook\n")
		fmt.Fprintf(os.Stderr, "  hooks uninstall [--scope user|project]  Remove session-start hook\n")
		fmt.Fprintf(os.Stderr, "  claude install [--dry-run] [--target ~/.claude]  Install artifact bundle\n")
		fmt.Fprintf(os.Stderr, "  claude update  [--dry-run] [--target ~/.claude]  Update artifact bundle\n")
		fmt.Fprintf(os.Stderr, "  claude list                                       List bundled artifacts\n")
		fmt.Fprintf(os.Stderr, "  claude diff    [--target ~/.claude]               Diff bundle vs on-disk\n")
		fmt.Fprintf(os.Stderr, "  update [--check] [--channel stable|prerelease]   Self-update the atomic binary\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
	}

	var showVersion bool
	var repoOverride string
	var noUpdateCheck bool
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showVersion, "v", false, "print version and exit (short)")
	fs.StringVar(&repoOverride, "repo", "", "repo root override (default: detect via git)")
	fs.BoolVar(&noUpdateCheck, "no-update-check", false, "suppress background update check")

	// Pre-scan all argv for --no-update-check before flag.Parse, because
	// flag.FlagSet stops at the first non-flag argument (the subcommand), so
	// "atomic signals scan --no-update-check" would not set noUpdateCheck via
	// fs.Parse alone. We strip the flag from args so subcommands don't see it.
	noUpdateCheck, os.Args = scanNoUpdateCheck(os.Args)

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

	// Background update check: spawned for every command except "update" and
	// when --no-update-check is set. The goroutine writes to the cache; only
	// the main thread prints the banner after the subcommand finishes.
	var bgUpdateCh <-chan selfupdate.Result
	var cacheEntry selfupdate.CacheEntry
	var cachePath string
	if !noUpdateCheck && args[0] != "update" {
		cp, err := selfupdate.DefaultCachePath()
		if err == nil {
			cachePath = cp
			cacheEntry, _ = selfupdate.ReadCache(cachePath)
			c := &selfupdate.Client{}
			bgUpdateCh = c.BackgroundCheck(context.Background(), cachePath, version.Version, "stable")
		}
	}

	switch args[0] {
	case "signals":
		runSignals(args[1:], repoOverride)
	case "reminder":
		runReminder(args[1:], repoOverride)
	case "hooks":
		runHooks(args[1:], repoOverride)
	case "claude":
		runClaude(args[1:])
	case "update":
		runUpdate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "atomic: unknown command %q\n", args[0])
		os.Exit(1)
	}

	// Check if the background goroutine has completed and print banner if due.
	if bgUpdateCh != nil {
		select {
		case res := <-bgUpdateCh:
			if res.Err == nil && res.Latest != "" {
				// Re-read cache after the goroutine finalization budget so we use
				// the entry the goroutine wrote (updated checked_at / latest_version),
				// not the snapshot taken before the goroutine ran.
				if updated, err := selfupdate.ReadCache(cachePath); err == nil {
					cacheEntry = updated
				}
				selfupdate.MaybeBanner(os.Stderr, version.Version, res.Latest, cacheEntry, cachePath, time.Now())
			}
		case <-time.After(100 * time.Millisecond):
			// goroutine not done yet — skip banner this run
		}
	}
}

// scanNoUpdateCheck pre-scans argv for --no-update-check (and
// --no-update-check=true/false) in any position. It returns the resolved flag
// value and a cleaned argv with the flag tokens removed so subcommand parsers
// don't trip over an unknown flag.
func scanNoUpdateCheck(argv []string) (found bool, cleaned []string) {
	cleaned = make([]string, 0, len(argv))
	for _, a := range argv {
		switch {
		case a == "--no-update-check" || a == "--no-update-check=true":
			found = true
		case a == "--no-update-check=false":
			// explicit false — leave found as-is, strip the token
		default:
			cleaned = append(cleaned, a)
		}
	}
	return found, cleaned
}

func runUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	var check bool
	var channel string
	fs.BoolVar(&check, "check", false, "only check if an update is available; do not apply")
	fs.StringVar(&channel, "channel", "stable", "release channel: stable or prerelease")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	c := &selfupdate.Client{}

	ctx := context.Background()

	if check {
		newer, tag, err := c.Check(ctx, channel, version.Version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic update: %v\n", err)
			os.Exit(1)
		}
		if newer {
			fmt.Printf("update available: %s (current: %s)\n", tag, version.Version)
			os.Exit(1)
		}
		fmt.Printf("atomic is up to date (%s)\n", tag)
		return
	}

	// apply update
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic update: resolve executable: %v\n", err)
		os.Exit(1)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic update: resolve symlinks: %v\n", err)
		os.Exit(1)
	}

	if err := c.Update(ctx, channel, version.Version, exe); err != nil {
		fmt.Fprintf(os.Stderr, "atomic update: %v\n", err)
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

func runHooks(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|install|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "session-start":
		fs := flag.NewFlagSet("hooks session-start", flag.ContinueOnError)
		var format string
		fs.StringVar(&format, "format", "json", "output format: json or text")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}

		now := time.Now().UTC()
		var out string
		if format == "text" {
			out, err = hooks.SessionStartText(root, now)
		} else {
			out, err = hooks.SessionStart(root, now)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}
		if out != "" {
			fmt.Println(out)
		}

	case "install":
		fs := flag.NewFlagSet("hooks install", flag.ContinueOnError)
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		if err := hooks.Install(root, scopeRoot); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "hooks installed (scope=%s)\n", scope)

	case "uninstall":
		fs := flag.NewFlagSet("hooks uninstall", flag.ContinueOnError)
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := repoctx.Resolve(repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		if err := hooks.Uninstall(root, scopeRoot); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "hooks uninstalled (scope=%s)\n", scope)

	default:
		fmt.Fprintf(os.Stderr, "atomic hooks: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|install|uninstall> [flags]\n")
		os.Exit(2)
	}
}

// resolveScopeRoot returns the directory against which hook files are written.
// "user" → $HOME/.claude (user scope), "project" → repoRoot (project scope).
func resolveScopeRoot(scope, repoRoot string) (string, error) {
	switch scope {
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve scope: get home dir: %w", err)
		}
		return home, nil
	case "project":
		return repoRoot, nil
	default:
		return "", fmt.Errorf("unknown scope %q: must be \"user\" or \"project\"", scope)
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
		err := signals.Diff(root, os.Stdout)
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

func runClaude(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic claude <install|update|list|diff> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]

	switch verb {
	case "install", "update":
		fs := flag.NewFlagSet("claude "+verb, flag.ContinueOnError)
		var dryRun bool
		var target string
		fs.BoolVar(&dryRun, "dry-run", false, "print what would happen; make no changes")
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: %v\n", verb, err)
			os.Exit(1)
		}

		plan, err := claudeinstall.Install(targetDir, dryRun, claudeinstall.RealClock)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: %v\n", verb, err)
			os.Exit(1)
		}

		if dryRun {
			fmt.Println("(dry-run — no changes written)")
		}
		fmt.Print(claudeinstall.Report(plan, targetDir))

	case "list":
		rows := claudeinstall.List()
		for _, r := range rows {
			fmt.Printf("%s\t%s\t%s\n", r.Kind, r.Target, r.SHA256)
		}

	case "diff":
		fs := flag.NewFlagSet("claude diff", flag.ContinueOnError)
		var target string
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude diff: %v\n", err)
			os.Exit(1)
		}

		rows, err := claudeinstall.Diff(targetDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude diff: %v\n", err)
			os.Exit(1)
		}
		for _, r := range rows {
			fmt.Printf("%s\t%s\n", r.Status, r.Artifact.Target)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic claude: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic claude <install|update|list|diff> [flags]\n")
		os.Exit(2)
	}
}
