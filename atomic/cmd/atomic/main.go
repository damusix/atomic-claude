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
	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/dockerinit"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/followups"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/signals"
	"github.com/damusix/atomic-claude/atomic/internal/updatedoctor"
	"github.com/damusix/atomic-claude/atomic/internal/validate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

func main() {
	fs := flag.NewFlagSet("atomic", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: atomic [flags] <command> [args]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  claude install   [--dry-run] [--target ~/.claude]  Install artifact bundle\n")
		fmt.Fprintf(os.Stderr, "  claude update    [--dry-run] [--target ~/.claude]  Update artifact bundle\n")
		fmt.Fprintf(os.Stderr, "  claude list                                         List bundled artifacts\n")
		fmt.Fprintf(os.Stderr, "  claude diff      [--target ~/.claude]               Diff bundle vs on-disk\n")
		fmt.Fprintf(os.Stderr, "  claude uninstall [--target ~/.claude]               Generate uninstall prompt\n")
		fmt.Fprintf(os.Stderr, "  config get <key>        Print resolved config value\n")
		fmt.Fprintf(os.Stderr, "  config set <key> <val>  Set config value; re-renders config.resolved.md\n")
		fmt.Fprintf(os.Stderr, "  config unset <key>      Revert key to built-in default\n")
		fmt.Fprintf(os.Stderr, "  config list [--json]    List all resolved key=value pairs\n")
		fmt.Fprintf(os.Stderr, "  config path             Print path to config.toml\n")
		fmt.Fprintf(os.Stderr, "  docker init [--target ./atomic-docker] [--force]  Scaffold Docker eval environment\n")
		fmt.Fprintf(os.Stderr, "  doctor [--fix] [--json] [--only <cat>] [--skip <cat>] [--stale-days N] [--verbose]  Integrity check\n")
		fmt.Fprintf(os.Stderr, "  hooks session-start [--format=text]  Print session-start hook payload\n")
		fmt.Fprintf(os.Stderr, "  hooks install [--scope user|project]  Install session-start hook\n")
		fmt.Fprintf(os.Stderr, "  hooks uninstall [--scope user|project]  Remove session-start hook\n")
		fmt.Fprintf(os.Stderr, "  reminder add <text> Create a reminder file; prints assigned id\n")
		fmt.Fprintf(os.Stderr, "  reminder list       List all reminders\n")
		fmt.Fprintf(os.Stderr, "  reminder show <id>  Print body of a reminder\n")
		fmt.Fprintf(os.Stderr, "  reminder rm <id>    Delete a reminder\n")
		fmt.Fprintf(os.Stderr, "  signals scan        Walk repo and write deterministic-signals.md\n")
		fmt.Fprintf(os.Stderr, "  signals show        Print deterministic-signals.md to stdout\n")
		fmt.Fprintf(os.Stderr, "  signals stale       Exit 0 fresh, 1 stale, 2 error\n")
		fmt.Fprintf(os.Stderr, "  signals diff        Print unified diff of signals file\n")
		fmt.Fprintf(os.Stderr, "  update [--check] [--channel stable|prerelease]   Self-update the atomic binary\n")
		fmt.Fprintf(os.Stderr, "  followups list [--stale] [--json]                 List open follow-up entries\n")
		fmt.Fprintf(os.Stderr, "  followups add --id <id> --title <t> --severity <s> --origin <o>  Create entry\n")
		fmt.Fprintf(os.Stderr, "  followups close <id> [--reason <r>]               Close an entry\n")
		fmt.Fprintf(os.Stderr, "  followups render                                  Regenerate INDEX.md\n")
		fmt.Fprintf(os.Stderr, "  followups path                                    Print followups folder path\n")
		fmt.Fprintf(os.Stderr, "  validate [flags] [spec|config|bundle] [paths...]  Lint repo artifacts\n")
		fmt.Fprintf(os.Stderr, "  docs scan                                         Scan docs and write doc-surfaces.md\n")
		fmt.Fprintf(os.Stderr, "  docs stale                                        Exit 0 fresh, 1 stale, 2 error\n")
		fmt.Fprintf(os.Stderr, "  profile refresh [--if-stale <dur>]               Refresh ## Environment in profile.md\n")
		fmt.Fprintf(os.Stderr, "  wiki scan [--root=<path>]                         Scaffold wiki/, scan repos, register in ~/.claude/CLAUDE.md\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
	}

	var showVersion bool
	var repoOverride string
	var noUpdateCheck bool
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showVersion, "v", false, "print version and exit (short)")
	fs.StringVar(&repoOverride, "repo", "", "repo root override (default: detect via git)")
	// Registered for --help documentation only; the actual value is set by
	// scanNoUpdateCheck (which pre-scans all argv positions before flag.Parse,
	// since flag.FlagSet stops at the first non-flag argument).
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
	case "doctor":
		runDoctor(args[1:])
	case "docker":
		runDocker(args[1:])
	case "update":
		runUpdate(args[1:])
	case "config":
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic config: resolve home dir: %v\n", err)
			os.Exit(2)
		}
		os.Exit(config.Run(args[1:], filepath.Join(home, ".claude"), os.Stdout, os.Stderr))
	case "followups":
		runFollowups(args[1:], repoOverride)
	case "validate":
		os.Exit(validate.Run(args[1:]))
	case "docs":
		runDocs(args[1:], repoOverride)
	case "profile":
		runProfile(args[1:])
	case "wiki":
		runWiki(args[1:])
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

func runDoctor(args []string) {
	opts, err := doctor.ParseFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	// Resolve home directory for the missing-~/.claude/ short-circuit.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic doctor: resolve home dir: %v\n", err)
		os.Exit(2)
	}

	if doctor.ClaudeHomeMissing(home) {
		msg := doctor.MissingHomeMessage()
		if opts.JSON {
			data, jerr := doctor.FormatJSONMissingHome(msg)
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "atomic doctor: marshal json: %v\n", jerr)
				os.Exit(2)
			}
			fmt.Println(string(data))
		} else {
			fmt.Println(msg)
		}
		os.Exit(0)
	}

	// Resolve project name: git toplevel basename, or cwd basename on failure.
	project := doctorProjectName()

	results, err := doctor.Run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic doctor: %v\n", err)
		os.Exit(2)
	}

	exitCode := doctor.ExitCode(results)

	if opts.JSON {
		data, jerr := doctor.FormatJSON(results, project, exitCode)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "atomic doctor: marshal json: %v\n", jerr)
			os.Exit(2)
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(doctor.FormatHuman(results, opts, project))
	}

	if opts.Fix {
		p := doctor.NewStdinPrompter(os.Stdin, os.Stdout)
		doctor.Repair(results, opts, p, os.Stdout)
	}

	os.Exit(exitCode)
}

// doctorProjectName returns the project name to display in doctor output.
// Uses the git toplevel directory basename; falls back to cwd basename.
func doctorProjectName() string {
	out, err := repoctx.Resolve("")
	if err == nil && out != "" {
		return filepath.Base(out)
	}
	cwd, err := os.Getwd()
	if err == nil {
		return filepath.Base(cwd)
	}
	return "unknown"
}

func runUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic update [--check] [--channel stable|prerelease] [--no-doctor]")
	var check bool
	var channel string
	var noDoctor bool
	fs.BoolVar(&check, "check", false, "only check if an update is available; do not apply")
	fs.StringVar(&channel, "channel", "stable", "release channel: stable or prerelease")
	fs.BoolVar(&noDoctor, "no-doctor", false, "skip post-update doctor self-check")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	c := &selfupdate.Client{}

	ctx := context.Background()

	if check {
		newer, tag, err := c.Check(ctx, channel, version.Version)
		if err != nil {
			// Hard error (network, parse): exit 2, distinct from the exit-1
			// "update available" signal — see the check-family exit convention.
			fmt.Fprintf(os.Stderr, "atomic update: %v\n", err)
			os.Exit(2)
		}
		if newer {
			// Actionable signal: a newer version exists. Exit 1 (diff(1) idiom).
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

	// Post-update doctor: load config to check user preference, then run.
	// Ignore home-dir errors and config warnings — doctor will catch real issues.
	cfgRunDoctor := true // safe default when config is unreadable
	if home, herr := os.UserHomeDir(); herr == nil {
		cfgPath := config.TOMLPath(filepath.Join(home, ".claude"))
		if cfg, _, cerr := config.Load(cfgPath); cerr == nil {
			cfgRunDoctor = cfg.Update.RunDoctor
		}
	}
	if shouldRunPostUpdateDoctor(noDoctor, cfgRunDoctor) {
		updatedoctor.Run(doctor.Run, os.Stdout)
	}
}

// shouldRunPostUpdateDoctor returns true when the post-update doctor should run.
// Precedence: --no-doctor flag (highest) > config update.run_doctor > default true.
func shouldRunPostUpdateDoctor(noDoctor, cfgRunDoctor bool) bool {
	if noDoctor {
		return false
	}
	return cfgRunDoctor
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

func runHooks(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|install|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "session-start":
		fs := flag.NewFlagSet("hooks session-start", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks session-start [--format json|text]")
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
		cliutil.SetUsage(fs, "atomic hooks install [--scope user|project]")
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
		cliutil.SetUsage(fs, "atomic hooks uninstall [--scope user|project]")
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
		info, err := signals.Stale(root)
		if err == nil {
			return // fresh → exit 0, silent
		}
		if err == signals.ErrStale {
			// Imperative, evidence-bearing output. The staleness gate is read by
			// an LLM orchestrator that can rationalize a silent exit code away, so
			// the tool states the directive and the evidence, not just the state.
			// Deliberate model-safeguard layer over the deterministic exit code —
			// see the prefer-code-over-model exception in CLAUDE.md.
			fmt.Printf("signals: STALE — a fresh scan would change the deterministic snapshot (~%d lines)\n", info.ChangedLines)
			fmt.Printf("→ refresh required; dispatch atomic-signals-inferrer. do not skip.\n")
			os.Exit(1)
		}
		// Hard error (e.g. missing signals file): exit 2, distinct from the
		// exit-1 stale signal so callers can tell "out of date" from "broken".
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
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
		// Hard error: exit 2, alongside ErrNoPrior — distinct from the exit-1
		// "diff present" signal. See the check-family exit convention.
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "atomic signals: unknown verb %q\n", verb)
		os.Exit(1)
	}
}

func runFollowups(args []string, repoOverride string) {
	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic followups: %v\n", err)
		os.Exit(1)
	}
	clock := func() time.Time { return time.Now().UTC() }
	os.Exit(followups.Run(args, root, os.Stdout, os.Stderr, clock))
}

func runDocker(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docker <init> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "init":
		fs := flag.NewFlagSet("docker init", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic docker init [--target <dir>] [--force]")
		var target string
		var force bool
		fs.StringVar(&target, "target", "./atomic-docker", "target directory for scaffolded files")
		fs.BoolVar(&force, "force", false, "overwrite existing files")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		absTarget, err := filepath.Abs(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic docker init: resolve target: %v\n", err)
			os.Exit(1)
		}

		if version.Version == "dev" {
			fmt.Fprintf(os.Stderr, "warning: atomic version is \"dev\" — generated Dockerfile pins ATOMIC_VERSION=dev which will fail at docker build. Use a released atomic binary or override with --version later.\n")
		}

		opts := dockerinit.Options{
			TargetDir:     absTarget,
			Force:         force,
			AtomicVersion: version.Version,
			HostUID:       os.Getuid(),
		}

		actions, err := dockerinit.Init(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic docker init: %v\n", err)
			os.Exit(1)
		}

		for _, a := range actions {
			fmt.Printf("%-12s %s\n", string(a.Kind), a.Path)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic docker: unknown subcommand %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic docker <init> [flags]\n")
		os.Exit(2)
	}
}

// installResult bundles what runClaudeInstall did. HooksError is non-fatal
// at the cmd layer — the caller decides whether to surface it as a warning.
type installResult struct {
	Plan           []claudeinstall.FileAction
	HooksInstalled bool
	HooksError     error
}

// runClaudeInstall performs the bundle install/update and, by default, also
// registers the session-start hook. Extracted from the cmd switch so it can be
// tested without invoking os.Exit. Hook registration is skipped under dry-run
// and when noHooks is true.
//
// scopeRoot for the hook is the parent of targetDir: ~/.claude → $HOME (user
// scope), <repo>/.claude → <repo> (project scope). This mirrors the mapping
// used by `atomic hooks install --scope user|project`.
func runClaudeInstall(targetDir, verb string, dryRun, noHooks bool) (installResult, error) {
	var plan []claudeinstall.FileAction
	var err error
	if verb == "update" {
		plan, err = claudeinstall.Update(targetDir, dryRun, claudeinstall.RealClock)
	} else {
		plan, err = claudeinstall.Install(targetDir, dryRun, claudeinstall.RealClock)
	}
	if err != nil {
		return installResult{}, err
	}

	result := installResult{Plan: plan}
	if dryRun || noHooks {
		return result, nil
	}

	scopeRoot := filepath.Dir(targetDir)
	if err := hooks.Install(scopeRoot, scopeRoot); err != nil {
		result.HooksError = err
		return result, nil
	}
	result.HooksInstalled = true
	return result, nil
}

// runClaudeUninstall builds the uninstall plan for targetDir and returns the
// structured markdown prompt Claude should execute. When out is a TTY the
// caller should print a human-readable hint before the prompt. Extracted from
// the cmd switch so it can be tested without invoking os.Exit.
func runClaudeUninstall(targetDir string, out *os.File) (string, error) {
	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		return "", err
	}

	// TTY detection: if out is a character device, we're in an interactive
	// terminal — print a hint so the user knows what to do with the output.
	info, statErr := out.Stat()
	if statErr == nil && (info.Mode()&os.ModeCharDevice != 0) {
		fmt.Fprintln(os.Stderr, "hint: run this inside a Claude Code session, or paste the output below into Claude.")
		fmt.Fprintln(os.Stderr, "      alternatively: ask Claude to run `atomic claude uninstall`")
		fmt.Fprintln(os.Stderr, "")
	}

	return claudeinstall.GenerateUninstallPrompt(targetDir, plan), nil
}

// printPostInstallHint surfaces the manual steps `atomic claude install` cannot
// automate: output style activation (Claude Code requires user opt-in) and
// per-repo signals initialization.
func printPostInstallHint(verb string) {
	if verb != "install" {
		return
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "next steps:")
	fmt.Fprintln(os.Stderr, "  1. open claude code and run /config → output style → Atomic")
	fmt.Fprintln(os.Stderr, "     (claude code requires explicit user opt-in for output styles)")
	fmt.Fprintln(os.Stderr, "  2. in each repo where you want project signals, run /refresh-signals")
}

func runClaude(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic claude <install|update|list|diff|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]

	switch verb {
	case "install", "update":
		fs := flag.NewFlagSet("claude "+verb, flag.ContinueOnError)
		cliutil.SetUsage(fs, fmt.Sprintf("atomic claude %s [--dry-run] [--target <dir>] [--no-hooks]", verb))
		var dryRun bool
		var target string
		var noHooks bool
		fs.BoolVar(&dryRun, "dry-run", false, "print what would happen; make no changes")
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		fs.BoolVar(&noHooks, "no-hooks", false, "skip session-start hook installation")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: %v\n", verb, err)
			os.Exit(1)
		}

		result, err := runClaudeInstall(targetDir, verb, dryRun, noHooks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude %s: %v\n", verb, err)
			os.Exit(1)
		}

		if dryRun {
			fmt.Println("(dry-run — no changes written)")
		}
		fmt.Print(claudeinstall.Report(result.Plan, targetDir))

		if !dryRun {
			if result.HooksInstalled {
				fmt.Fprintln(os.Stderr, "session-start hook installed.")
			} else if result.HooksError != nil {
				fmt.Fprintf(os.Stderr, "warning: hook install failed (non-fatal): %v\n", result.HooksError)
				fmt.Fprintln(os.Stderr, "         retry later with: atomic hooks install")
			}
			printPostInstallHint(verb)
		}

	case "list":
		rows := claudeinstall.List()
		for _, r := range rows {
			fmt.Printf("%s\t%s\t%s\n", r.Kind, r.Target, r.SHA256)
		}

	case "diff":
		fs := flag.NewFlagSet("claude diff", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic claude diff [--target <dir>]")
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

	case "uninstall":
		fs := flag.NewFlagSet("claude uninstall", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic claude uninstall [--target <dir>]")
		var target string
		fs.StringVar(&target, "target", "~/.claude", "target directory (default ~/.claude)")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		targetDir, err := claudeinstall.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude uninstall: %v\n", err)
			os.Exit(1)
		}

		prompt, err := runClaudeUninstall(targetDir, os.Stdout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic claude uninstall: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(prompt)

	default:
		fmt.Fprintf(os.Stderr, "atomic claude: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic claude <install|update|list|diff|uninstall> [flags]\n")
		os.Exit(2)
	}
}

// docsAction executes the docs subcommand logic and returns an exit code.
// Extracted from runDocs so that tests can exercise dispatch without os.Exit.
func docsAction(args []string, root string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docs <scan|stale>\n")
		return 1
	}

	verb := args[0]
	switch verb {
	case "scan":
		if err := docs.Scan(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		return 0
	case "stale":
		err := docs.Stale(root)
		if err == nil {
			return 0 // fresh
		}
		if err == docs.ErrStale {
			return 1
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	default:
		fmt.Fprintf(os.Stderr, "atomic docs: unknown verb %q\n", verb)
		return 1
	}
}

func runDocs(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docs <scan|stale>\n")
		os.Exit(1)
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic docs: %v\n", err)
		os.Exit(1)
	}

	os.Exit(docsAction(args, root))
}

// profileAction executes the profile subcommand logic and returns an exit code.
// Extracted from runProfile so tests can exercise dispatch without os.Exit.
// claudeHome is the ~/.claude directory; today is YYYY-MM-DD (injected, never time.Now here).
func profileAction(args []string, claudeHome, today string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic profile <refresh> [flags]\n")
		return 2
	}

	verb := args[0]
	switch verb {
	case "refresh":
		fs := flag.NewFlagSet("profile-refresh", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic profile refresh [--if-stale <Nd>]")
		fs.SetOutput(os.Stderr)
		var ifStale string
		fs.StringVar(&ifStale, "if-stale", "", "skip refresh when lastcheck is within this window (e.g. 7d, 30d)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}

		if ifStale != "" {
			days, err := profile.ParseDuration(ifStale)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic profile refresh: %v\n", err)
				return 1
			}
			wrote, err := profile.RefreshIfStale(claudeHome, today, days)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic profile refresh: %v\n", err)
				return 1
			}
			if wrote {
				fmt.Fprintf(os.Stderr, "profile refreshed: %s\n", config.ProfilePath(claudeHome))
			}
			return 0
		}

		// Unconditional refresh.
		_, err := profile.Refresh(claudeHome, today)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic profile refresh: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "profile refreshed: %s\n", config.ProfilePath(claudeHome))
		return 0

	default:
		fmt.Fprintf(os.Stderr, "atomic profile: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic profile <refresh> [flags]\n")
		return 2
	}
}

func runProfile(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic profile: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	claudeHome := filepath.Join(home, ".claude")
	today := time.Now().UTC().Format("2006-01-02")
	os.Exit(profileAction(args, claudeHome, today))
}

func runWiki(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	claudeHome := filepath.Join(home, ".claude")

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki: resolve cwd: %v\n", err)
		os.Exit(2)
	}

	os.Exit(wiki.WikiAction(args, claudeHome, cwd, os.Stdout))
}
