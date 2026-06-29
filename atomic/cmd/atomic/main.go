package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"
	"github.com/damusix/atomic-claude/atomic/internal/coldprompt"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/dockerinit"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/followups"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
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
		cliusage.RenderCommandsBlock(os.Stderr)
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
	case "code":
		runCode(args[1:], repoOverride)
	case "wiki":
		runWiki(args[1:])
	case "prompt":
		runPrompt(args[1:])
	case "serve":
		os.Exit(serve.Run(args[1:], os.Stdout, os.Stderr))
	case "migrate":
		runMigrate(args[1:])
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

	// Wire claudeMDPath for realm detection in check 11 (code-index).
	opts.ClaudeMDPath = filepath.Join(home, ".claude", "CLAUDE.md")

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
	cliutil.SetUsage(fs, "atomic update [--check] [--channel stable|prerelease] [--no-doctor] [--skip-claude-update]")
	var check bool
	var channel string
	var noDoctor bool
	var skipClaudeUpdate bool
	fs.BoolVar(&check, "check", false, "only check if an update is available; do not apply")
	fs.StringVar(&channel, "channel", "stable", "release channel: stable or prerelease")
	fs.BoolVar(&noDoctor, "no-doctor", false, "skip post-update doctor self-check")
	fs.BoolVar(&skipClaudeUpdate, "skip-claude-update", false, "skip the ~/.claude artifact refresh after the binary swap")
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

	// Refresh ~/.claude artifacts by default, so doctor below runs against
	// the refreshed state instead of flagging drift the user then has to fix
	// by hand. Anyone running `atomic update` is assumed to want the whole
	// product current; --skip-claude-update opts out. Re-exec of the freshly
	// swapped binary is load-bearing: this process still embeds the OLD
	// bundle after the swap, so an in-process claudeinstall.Update would
	// install stale artifacts. Best-effort: a refresh failure warns and
	// never blocks the update success path.
	if !skipClaudeUpdate {
		hooksInstalled := false
		if home, herr := os.UserHomeDir(); herr == nil {
			if installed, _, ierr := hooks.IsInstalled(home); ierr == nil {
				hooksInstalled = installed
			}
		}
		if err := defaultRunCmd(exe, artifactRefreshArgs(hooksInstalled)...); err != nil {
			fmt.Fprintf(os.Stderr, "atomic update: artifact refresh failed: %v\nrun `atomic claude update` manually.\n", err)
		}
	}

	// Run install-scope migrations after the artifact refresh so they see the
	// new bundle. Best-effort: failure warns and never blocks the update path.
	if home, herr := os.UserHomeDir(); herr == nil {
		if err := runMigrateInstall(filepath.Join(home, ".claude")); err != nil {
			fmt.Fprintf(os.Stderr, "atomic update: migrations failed: %v\nrun `atomic migrate` manually.\n", err)
		}
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

// defaultRunCmd executes name with args, streaming output to this process's
// stdout/stderr.
func defaultRunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// artifactRefreshArgs builds the argv (minus the binary path) for the
// post-swap artifact refresh. When the session-start hook is not currently
// registered, --no-hooks is appended so the refresh preserves the user's
// hook state — it must never be the thing that first registers hooks or
// overrides an explicit `--no-hooks` install choice.
func artifactRefreshArgs(hooksInstalled bool) []string {
	args := []string{"claude", "update", "--no-update-check"}
	if !hooksInstalled {
		args = append(args, "--no-hooks")
	}
	return args
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
		fs := flag.NewFlagSet("signals-scan", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		var outDir string
		fs.StringVar(&outDir, "out", "", "write substrate to <dir> instead of <root>/.claude/project/")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}
		opts := &signals.Options{}
		if outDir != "" {
			absOut, err := filepath.Abs(outDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic signals scan: resolve --out: %v\n", err)
				os.Exit(1)
			}
			opts.OutDir = absOut
		}
		if err := signals.ScanWithOptions(root, opts); err != nil {
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
	case "linkify":
		fs := flag.NewFlagSet("signals-linkify", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}
		// Root follows the signals convention (cwd / global --repo), like
		// scan and stale. There is no per-verb --root flag here.
		if err := signals.LinkifyFiles(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
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

func runCode(args []string, repoOverride string) {
	// Resolve scope BEFORE calling repoctx.Resolve, because repoctx.Resolve
	// runs `git rev-parse --show-toplevel` which errors at a realm root (a
	// plain container directory, not a git repo).  realm.Resolve position-senses
	// the cwd and branches to the correct engine path without git.
	if repoOverride == "" {
		// Inject cwd + claudeMD path for realm detection.
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic code: get cwd: %v\n", err)
			os.Exit(1)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic code: get home dir: %v\n", err)
			os.Exit(1)
		}
		claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
		os.Exit(codecli.RunCodeWithRealm(args, cwd, claudeMDPath, os.Stdout, os.Stderr, os.Stdin))
	}

	// --repo override: user explicitly specified a path. Normalise it to an
	// absolute path, then use the realm-aware dispatcher so a member path gets
	// its realm db and a standalone repo gets its local index. We avoid
	// repoctx.Resolve here because it runs `git rev-parse --show-toplevel` which
	// fails when the cwd is a realm root (no git repo there).
	absRepo, err := filepath.Abs(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic code: resolve --repo path: %v\n", err)
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic code: get home dir: %v\n", err)
		os.Exit(1)
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	os.Exit(codecli.RunCodeWithRealm(args, absRepo, claudeMDPath, os.Stdout, os.Stderr, os.Stdin))
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

// promptAction executes the prompt subcommand logic and returns an exit code.
// Extracted from runPrompt so tests can exercise dispatch without os.Exit.
// out receives the brief text on success; errOut receives error messages.
func promptAction(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(errOut, "Usage: atomic prompt <name>\n")
		fmt.Fprintf(errOut, "Valid names: %s\n", strings.Join(coldprompt.Names(), ", "))
		return 1
	}
	text, err := coldprompt.Get(args[0])
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		return 1
	}
	fmt.Fprint(out, text)
	return 0
}

// runPrompt is the os.Exit-aware entry point for the prompt top-level verb.
func runPrompt(args []string) {
	os.Exit(promptAction(args, os.Stdout, os.Stderr))
}

// runMigrate is the os.Exit-aware entry point for the migrate top-level verb.
//
//	atomic migrate                  → install-scope steps against ~/.claude
//	atomic migrate --repo <path>    → repo-scope steps on that repo
//	atomic migrate --realm <path>   → install-scope + fan-out to member repos
func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic migrate [--repo <path>] [--realm <path>]")
	var repoPath string
	var realmPath string
	fs.StringVar(&repoPath, "repo", "", "run repo-scope migrations on this path")
	fs.StringVar(&realmPath, "realm", "", "run install-scope + repo fan-out under this realm root")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	switch {
	case repoPath != "":
		absRepo, err := filepath.Abs(repoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve --repo path: %v\n", err)
			os.Exit(1)
		}
		if err := migrateRepoAction(absRepo); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: %v\n", err)
			os.Exit(1)
		}

	case realmPath != "":
		absRealm, err := filepath.Abs(realmPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve --realm path: %v\n", err)
			os.Exit(1)
		}
		// Install-scope first.
		home, herr := os.UserHomeDir()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve home dir: %v\n", herr)
			os.Exit(1)
		}
		if err := runMigrateInstall(filepath.Join(home, ".claude")); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: install-scope: %v\n", err)
			os.Exit(1)
		}
		// Fan-out to member repos.
		if err := runMigrateRealm(absRealm); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: realm: %v\n", err)
			os.Exit(1)
		}

	default:
		// Install-scope only.
		home, herr := os.UserHomeDir()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve home dir: %v\n", herr)
			os.Exit(1)
		}
		if err := runMigrateInstall(filepath.Join(home, ".claude")); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: %v\n", err)
			os.Exit(1)
		}
	}
}

// runMigrateInstall runs install-scope migrations against claudeHome.
// Reads the recorded version from config.toml [install].version, applies any
// pending install-scope steps, and writes the new version back on success.
// Returns an error; the caller decides whether it is fatal.
func runMigrateInstall(claudeHome string) error {
	cfgPath := config.TOMLPath(claudeHome)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx := &migrate.Context{Root: claudeHome}
	installSteps := scopedMigrations("install", migrate.Registry)
	newVer, err := migrate.Run(cfg.Install.Version, installSteps, ctx)
	if err != nil {
		return err
	}
	if newVer == cfg.Install.Version {
		return nil // no steps applied
	}
	cfg.Install.Version = newVer
	return config.WritePersist(cfgPath, cfg)
}

// migrateRepoAction is the testable seam for `atomic migrate --repo <path>`.
// It reads the repo's wiki schema, runs repo-scope migrations, and stamps the
// new schema back into docs/wiki/index.md on success.
func migrateRepoAction(repoPath string) error {
	schema := migrate.ReadWikiSchema(repoPath)
	recorded := schemaToSemver(schema)
	ctx := &migrate.Context{Root: repoPath}
	repoSteps := scopedMigrations("repo", migrate.Registry)
	newVer, err := migrate.Run(recorded, repoSteps, ctx)
	if err != nil {
		return fmt.Errorf("repo %s: %w", repoPath, err)
	}
	newSchema := semverToSchema(newVer)
	if newSchema == schema {
		return nil // nothing changed
	}
	// WriteWikiSchema is a no-op when docs/wiki/index.md does not exist
	// (e.g. no-signals repo where the step ran but created no file).
	return migrate.WriteWikiSchema(repoPath, newSchema)
}

// realmConfirmFn is the testable seam for runMigrateRealm's per-repo confirm
// prompt. Tests replace it to avoid spawning a real TTY.
var realmConfirmFn = prompt.Confirm

// runMigrateRealm fans out repo-scope migrations across immediate subdirectory
// member repos under realmPath. A member repo is detected by the presence of
// .claude/project/signals.md (old layout) or docs/wiki/index.md (new layout).
// Each detected member prompts for confirmation before migrating.
//
// Non-interactive context (ErrNonInteractive): the member is skipped. The spec
// requires one explicit confirm per repo; a non-TTY context must migrate nothing.
// ErrAborted: the member is skipped; the realm loop continues.
func runMigrateRealm(realmPath string) error {
	entries, err := os.ReadDir(realmPath)
	if err != nil {
		return fmt.Errorf("read realm dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		memberPath := filepath.Join(realmPath, e.Name())

		hasNew := fileExistsAt(filepath.Join(memberPath, "docs", "wiki", "index.md"))
		hasOld := fileExistsAt(filepath.Join(memberPath, ".claude", "project", "signals.md"))
		if !hasNew && !hasOld {
			continue // not an atomic'd repo
		}

		schema := migrate.ReadWikiSchema(memberPath)
		if schema >= 1 {
			fmt.Printf("migrate: %s already at schema %d, skipping\n", e.Name(), schema)
			continue
		}

		ok, perr := realmConfirmFn(
			fmt.Sprintf("Migrate repo %s?", e.Name()),
			"Move .claude/project/signals.md → docs/wiki/index.md",
			true,
		)
		if perr != nil {
			if errors.Is(perr, prompt.ErrNonInteractive) {
				// Non-TTY context: the spec requires explicit confirm per repo.
				// Skip silently rather than auto-migrating.
				fmt.Printf("migrate: %s skipped (non-interactive)\n", e.Name())
				continue
			} else if errors.Is(perr, prompt.ErrAborted) {
				// User aborted this member's prompt: skip it, continue the loop.
				fmt.Printf("migrate: %s skipped (aborted)\n", e.Name())
				continue
			} else {
				return fmt.Errorf("prompt for %s: %w", e.Name(), perr)
			}
		}
		if !ok {
			fmt.Printf("migrate: skipping %s\n", e.Name())
			continue
		}

		if err := migrateRepoAction(memberPath); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: %s: %v (skipping)\n", e.Name(), err)
			continue
		}
		fmt.Printf("migrate: %s migrated\n", e.Name())
	}
	return nil
}

// scopedMigrations filters migrate.Registry to those with the given Scope.
func scopedMigrations(scope string, registry []migrate.Migration) []migrate.Migration {
	var out []migrate.Migration
	for _, m := range registry {
		if m.Scope == scope {
			out = append(out, m)
		}
	}
	return out
}

// schemaToSemver converts a wiki schema integer to a semver string for
// migrate.Run. 0 returns "" (normalised to floor "0.0.0" by Run); N > 0
// returns "N.0.0".
func schemaToSemver(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n) + ".0.0"
}

// semverToSchema converts the semver string returned by migrate.Run back to an
// integer schema version for WriteWikiSchema. Parses only the major component.
func semverToSchema(v string) int {
	if v == "" || v == "0.0.0" {
		return 0
	}
	idx := strings.IndexByte(v, '.')
	if idx < 0 {
		return 0
	}
	n, _ := strconv.Atoi(v[:idx])
	return n
}

// fileExistsAt returns true when the file at path exists (any type).
func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
