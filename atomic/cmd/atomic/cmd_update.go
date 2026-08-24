// The `atomic update` verb plus the state-driven fast path main() runs before
// Cobra reaches any command, which is why both live here.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	charmterm "github.com/charmbracelet/x/term"
	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/updatedoctor"
	"github.com/damusix/atomic-claude/atomic/internal/version"
	"github.com/spf13/cobra"
)

// updateCheckInterval caps background GitHub lookups at one per hour, however
// many invocations happen in between.
const updateCheckInterval = time.Hour

// backgroundCheckMarker distinguishes an auto-spawned `atomic update --check`
// from a manually typed one. Never registered on a FlagSet and stripped before
// parsing, so it stays out of --help and the cliusage surface.
const backgroundCheckMarker = "--__background-check"

// stripBackgroundCheckMarker reports whether the marker was present; the bool
// gates once-only background staging.
func stripBackgroundCheckMarker(args []string) (found bool, cleaned []string) {
	cleaned = make([]string, 0, len(args))
	for _, a := range args {
		if a == backgroundCheckMarker {
			found = true
			continue
		}
		cleaned = append(cleaned, a)
	}
	return found, cleaned
}

// selfupdateFastPath renders the update banner from state alone and stamps
// last_check *before* launching the detached check child, so a crash or a
// racing invocation cannot leave the hourly budget unspent. current, spawn and
// now are injected so tests reach every gate without forking or waiting on the
// clock. See docs/spec/selfupdate-state.md.
func selfupdateFastPath(home, verb, current string, noUpdateCheck bool, w io.Writer, now func() time.Time, spawn func(exe string) error) {
	statePath := config.StatePath(home)
	state := selfupdate.LoadState(statePath)
	nowVal := now()

	if selfupdate.ShouldNotify(current, state.Update.LatestVersion, state.Update.LastNotified, nowVal) {
		// latest_version is stored pre-stripped; stripping again only guards a
		// legacy or hand-edited state.json.
		fmt.Fprintf(w, "update available: %s (current: %s). run: atomic update\n", selfupdate.DisplayVersion(state.Update.LatestVersion), current)
		state.Update.LastNotified = nowVal
		if err := selfupdate.WriteState(statePath, state); err != nil {
			fmt.Fprintf(w, "atomic: write update state: %v\n", err)
		}
	}

	// The child's own `update` invocation must never re-spawn a grandchild.
	if verb == "update" || noUpdateCheck {
		return
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil || !cfg.Update.Check {
		return
	}
	if !state.Update.LastCheck.IsZero() && nowVal.Sub(state.Update.LastCheck) < updateCheckInterval {
		return
	}

	state.Update.LastCheck = nowVal
	if err := selfupdate.WriteState(statePath, state); err != nil {
		fmt.Fprintf(w, "atomic: write update state: %v\n", err)
		return
	}

	exe, err := executableFn()
	if err != nil {
		fmt.Fprintf(w, "atomic: resolve executable: %v\n", err)
		return
	}
	if err := spawn(exe); err != nil {
		fmt.Fprintf(w, "atomic: spawn background update check: %v\n", err)
	}
}

// executableFn is a seam so tests can force the otherwise unreachable
// os.Executable failure path.
var executableFn = os.Executable

// defaultUpdateSpawn launches the detached `atomic update --check` child that
// performs the GitHub lookup this process never does. Setsid puts it in its own
// session so it outlives this invocation; nil streams keep it from blocking.
func defaultUpdateSpawn(exe string) error {
	cmd := exec.Command(exe, "update", "--check", backgroundCheckMarker)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("selfupdate: start %s: %w", exe, err)
	}
	// Nothing ever Waits on the child; releasing the handle hands reaping to init.
	_ = cmd.Process.Release()
	return nil
}

func buildUpdateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "update",
		Short:              "Self-update the atomic binary, then refresh ~/.claude artifacts",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runUpdate(args)
			return nil
		},
	}
	c.Flags().Bool("check", false, "only check if update available; do not apply")
	c.Flags().String("channel", "stable", "release channel: stable or prerelease")
	c.Flags().Bool("no-doctor", false, "skip post-update doctor self-check")
	c.Flags().Bool("skip-claude-update", false, "skip the ~/.claude artifact refresh after binary swap")
	c.Flags().Bool("force", false, "bypass the update lock; never weakens checksum verification")
	return c
}

// runUpdateCheck writes latest_version/last_result to state.json whatever the
// lookup returns, then runs the once-only staging gate — but only for the
// auto-spawned invocation, never a manually typed --check. Returns exactly what
// c.Check returns and never exits the process, so every branch is testable. An
// empty home degrades to a bare lookup with no state I/O.
func runUpdateCheck(ctx context.Context, home string, background bool, c *selfupdate.Client, channel, currentVersion string, now func() time.Time, w io.Writer) (newer bool, tag string, lookupErr error) {
	newer, tag, lookupErr = c.Check(ctx, channel, currentVersion)
	if home == "" {
		return newer, tag, lookupErr
	}

	statePath := config.StatePath(home)
	state := selfupdate.LoadState(statePath)
	if lookupErr != nil {
		state.Update.LastResult = lookupErr.Error()
	} else {
		// c.Check normalizes tag, so latest_version is stored without a "v".
		state.Update.LatestVersion = tag
		state.Update.LastResult = ""
	}
	if err := selfupdate.WriteState(statePath, state); err != nil {
		fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
	}

	// A manually typed --check never stages; only the auto-spawned child does.
	if !background || lookupErr != nil || !newer {
		return newer, tag, lookupErr
	}
	cfg, _, cerr := config.Load(config.TOMLPath(home))
	if cerr != nil || !cfg.Update.Stage {
		return newer, tag, lookupErr
	}

	// Re-read: a concurrent writer may already have superseded the write above.
	gate := selfupdate.LoadState(statePath)
	if gate.Update.StageAttemptedFor == tag {
		return newer, tag, lookupErr
	}

	lockedAt := now()
	locked, acquired := selfupdate.AcquireLock(gate, lockedAt)
	if !acquired {
		// Deliberately does not stamp stage_attempted_for: the once-only budget
		// is spent on a real download attempt, never on lock contention.
		return newer, tag, lookupErr
	}
	locked.Update.StageAttemptedFor = tag
	if err := selfupdate.WriteState(statePath, locked); err != nil {
		fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
		return newer, tag, lookupErr
	}

	// c.Check returns no Assets, so staging needs the full Release. Only the
	// rare about-to-download path pays for this second lookup.
	rel, relErr := c.Lookup(ctx, channel, os.Getenv("GITHUB_TOKEN"))
	var staged selfupdate.StagedInfo
	stageErr := relErr
	if relErr == nil {
		staged, stageErr = c.Stage(ctx, rel, selfupdate.StageDir(home))
	}

	final := selfupdate.LoadState(statePath)
	final.Update.StageAttemptedFor = tag
	if stageErr != nil {
		final.Update.LastResult = stageErr.Error()
	} else {
		final.Update.Staged = staged
	}
	// Fenced on lockedAt: if a foreground swap took the lock mid-download,
	// ReleaseLock leaves its fields alone instead of clobbering it.
	final = selfupdate.ReleaseLock(final, lockedAt)
	if err := selfupdate.WriteState(statePath, final); err != nil {
		fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
	}

	return newer, tag, lookupErr
}

func runUpdate(args []string) {
	// Stripped before flag parsing so it never surfaces as an unknown flag.
	background, args := stripBackgroundCheckMarker(args)

	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic update [--check] [--channel stable|prerelease] [--no-doctor] [--skip-claude-update] [--force]")
	var check bool
	var channel string
	var noDoctor bool
	var skipClaudeUpdate bool
	var force bool
	fs.BoolVar(&check, "check", false, "only check if an update is available; do not apply")
	fs.StringVar(&channel, "channel", "stable", "release channel: stable or prerelease")
	fs.BoolVar(&noDoctor, "no-doctor", false, "skip post-update doctor self-check")
	fs.BoolVar(&skipClaudeUpdate, "skip-claude-update", false, "skip the ~/.claude artifact refresh after the binary swap")
	fs.BoolVar(&force, "force", false, "bypass the update lock; never weakens checksum verification")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	c := &selfupdate.Client{}

	ctx := context.Background()

	if check {
		// An unresolvable home degrades to a raw check with no state I/O.
		home, _ := os.UserHomeDir()
		newer, tag, err := runUpdateCheck(ctx, home, background, c, channel, version.Version, time.Now, os.Stderr)
		if err != nil {
			// Exit 2 for a hard error, distinct from the exit-1 "available" signal.
			fmt.Fprintf(os.Stderr, "atomic update: %v\n", err)
			os.Exit(2)
		}
		if newer {
			// Exit 1 signals "available", the diff(1) idiom.
			fmt.Printf("update available: %s (current: %s)\n", tag, version.Version)
			os.Exit(1)
		}
		fmt.Printf("atomic is up to date (%s)\n", tag)
		return
	}

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

	c.OnProgress = downloadProgressRenderer(os.Stdout, charmterm.IsTerminal(os.Stdout.Fd()))

	home, _ := os.UserHomeDir()
	if err := runUpdateApply(ctx, home, c, channel, version.Version, exe, force, time.Now, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "atomic update: %v\n", err)
		os.Exit(1)
	}

	// Re-exec of the freshly swapped binary, not an in-process call: this
	// process still embeds the OLD bundle, so it would install stale artifacts.
	// Best-effort — a refresh failure warns and never blocks the update.
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

	// After the refresh, so migrations see the new bundle. Best-effort.
	if home, herr := os.UserHomeDir(); herr == nil {
		if err := runMigrateInstall(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic update: migrations failed: %v\nrun `atomic migrate` manually.\n", err)
		}
	}

	cfgRunDoctor := true // safe default when config is unreadable
	if home, herr := os.UserHomeDir(); herr == nil {
		cfgPath := config.TOMLPath(home)
		if cfg, _, cerr := config.Load(cfgPath); cerr == nil {
			cfgRunDoctor = cfg.Update.RunDoctor
		}
	}
	if shouldRunPostUpdateDoctor(noDoctor, cfgRunDoctor) {
		updatedoctor.Run(doctor.Run, os.Stdout)
	}
}

// downloadProgressRenderer rewrites one status line in place as the archive
// streams down. Nil off-TTY: without \r rewriting, every tick would print its
// own line into redirected output.
func downloadProgressRenderer(w io.Writer, isTTY bool) func(received, total int64) {
	if !isTTY {
		return nil
	}
	const mib = 1024 * 1024
	done := false
	return func(received, total int64) {
		if done {
			return
		}
		switch {
		case total > 0 && received >= total:
			done = true
			fmt.Fprintf(w, "\rdownloaded %.1f MB (100%%)           \n", float64(total)/mib)
		case total > 0:
			fmt.Fprintf(w, "\rdownloading %.1f / %.1f MB (%d%%)   ", float64(received)/mib, float64(total)/mib, received*100/total)
		default:
			fmt.Fprintf(w, "\rdownloading %.1f MB   ", float64(received)/mib)
		}
	}
}

// runUpdateApply performs the foreground swap: lock acquire/takeover, a fresh
// GitHub lookup (state's own latest_version is never trusted for this call),
// then a staged fast-path swap or a fallback to the full download. Callers must
// pass currentBinary already symlink-resolved. State I/O is skipped, never
// blocking the swap, when home is unresolvable.
func runUpdateApply(ctx context.Context, home string, c *selfupdate.Client, channel, currentVersion, currentBinary string, force bool, now func() time.Time, w io.Writer) error {
	var statePath string
	state := selfupdate.State{}
	if home != "" {
		statePath = config.StatePath(home)
		state = selfupdate.LoadState(statePath)
	}
	writeState := func(s selfupdate.State) {
		if statePath == "" {
			return
		}
		if err := selfupdate.WriteState(statePath, s); err != nil {
			fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
		}
	}

	acquiredAt := now()
	locked, err := selfupdate.AcquireOrTakeoverLock(state, acquiredAt, force)
	if err != nil {
		return err
	}
	state = locked
	writeState(state)

	// Fenced on acquiredAt: a newer holder's lock fields are left untouched,
	// while mutate's non-lock changes apply regardless of ownership.
	releaseLock := func(mutate func(selfupdate.State) selfupdate.State) selfupdate.State {
		s := state
		if statePath != "" {
			s = selfupdate.LoadState(statePath)
		}
		if mutate != nil {
			s = mutate(s)
		}
		return selfupdate.ReleaseLock(s, acquiredAt)
	}

	rel, err := c.Lookup(ctx, channel, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		writeState(releaseLock(nil))
		return err
	}

	if !selfupdate.IsNewer(currentVersion, rel.TagName) {
		writeState(releaseLock(nil))
		fmt.Fprintf(w, "atomic is up to date (%s)\n", selfupdate.DisplayVersion(rel.TagName))
		return nil
	}

	tag := selfupdate.DisplayVersion(rel.TagName)
	swapped := false
	if state.Update.Staged.Version == tag && state.Update.Staged.Path != "" {
		if serr := c.ApplyStaged(ctx, rel, state.Update.Staged.Path, currentBinary); serr == nil {
			swapped = true
		} else {
			// Checksum re-verify failed or the file vanished: discard the record
			// so it is never retried, then fall back to the full download.
			state.Update.Staged = selfupdate.StagedInfo{}
			writeState(state)
		}
	}
	if !swapped {
		if aerr := c.Apply(ctx, rel, currentBinary); aerr != nil {
			writeState(releaseLock(nil))
			return aerr
		}
	}

	if p := state.Update.Staged.Path; p != "" {
		_ = os.Remove(p) // best-effort — state.json remains the sole authority on what is staged
	}
	updatedAt := now()
	writeState(releaseLock(func(s selfupdate.State) selfupdate.State {
		s.Update.Staged = selfupdate.StagedInfo{}
		s.Update.UpdatedAt = updatedAt
		return s
	}))

	fmt.Fprintf(w, "updated atomic %s → %s.\n", currentVersion, tag)
	return nil
}

// shouldRunPostUpdateDoctor applies the precedence --no-doctor > config
// update.run_doctor > default true.
func shouldRunPostUpdateDoctor(noDoctor, cfgRunDoctor bool) bool {
	if noDoctor {
		return false
	}
	return cfgRunDoctor
}

func defaultRunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// artifactRefreshArgs builds the post-swap refresh argv. --no-hooks is appended
// when the session-start hook is unregistered: the refresh must never be what
// first registers hooks or overrides an explicit --no-hooks install choice.
func artifactRefreshArgs(hooksInstalled bool) []string {
	args := []string{"claude", "update", "--no-update-check"}
	if !hooksInstalled {
		args = append(args, "--no-hooks")
	}
	return args
}
