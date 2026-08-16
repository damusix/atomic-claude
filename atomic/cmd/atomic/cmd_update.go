// Self-update: the `atomic update` verb plus the state-driven fast path
// main() runs before Cobra, which is why both live here.

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

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/updatedoctor"
	"github.com/damusix/atomic-claude/atomic/internal/version"
	"github.com/spf13/cobra"
)

// updateCheckInterval bounds how often selfupdateFastPath spawns a detached
// child to perform the GitHub lookup: at most once per hour, regardless of
// how many invocations happen in between.
const updateCheckInterval = time.Hour

// backgroundCheckMarker is appended to argv by selfupdateFastPath's spawn to
// mark a `atomic update --check` invocation as auto-spawned rather than
// manually typed. runUpdate strips it before flag parsing — it is never
// registered on a flag.FlagSet — so it never appears in `atomic update
// --help` output or the cliusage surface.
const backgroundCheckMarker = "--__background-check"

// stripBackgroundCheckMarker removes backgroundCheckMarker from args if
// present, reporting whether it was found. CP4 wires the returned bool into
// the once-only background-staging gate; this checkpoint only guarantees the
// marker parses cleanly.
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

// selfupdateFastPath implements the parent fast path (docs/spec/selfupdate-state.md,
// Flow "parent fast path", steps 2-4): read state, render the update-available
// banner from state alone when due, and — stamp-before-spawn — launch a
// detached background-check child at most once per hour. current, spawn, and
// now are all injected so tests exercise every gate without forking a real
// process, depending on the wall clock, or the test binary's own unparseable
// "dev" version.Version.
func selfupdateFastPath(home, verb, current string, noUpdateCheck bool, w io.Writer, now func() time.Time, spawn func(exe string) error) {
	statePath := config.StatePath(home)
	state := selfupdate.LoadState(statePath)
	nowVal := now()

	if selfupdate.ShouldNotify(current, state.Update.LatestVersion, state.Update.LastNotified, nowVal) {
		// F-1: defense-in-depth normalization. The check branch already
		// writes latest_version pre-stripped of any "v" prefix, but the
		// banner strips again here so a stray legacy or hand-edited value
		// in state.json can never surface a "vX.Y.Z" string to the user.
		fmt.Fprintf(w, "update available: %s (current: %s). run: atomic update\n", selfupdate.DisplayVersion(state.Update.LatestVersion), current)
		state.Update.LastNotified = nowVal
		if err := selfupdate.WriteState(statePath, state); err != nil {
			fmt.Fprintf(w, "atomic: write update state: %v\n", err)
		}
	}

	// The spawn gate: the child's own `update` invocation must never
	// re-spawn a grandchild, --no-update-check opts out explicitly, and
	// config update.check must be enabled.
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

	// Stamp and persist last_check BEFORE spawning: a crash or a racing
	// invocation must never leave the hourly budget unspent.
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

// executableFn resolves the path to the running binary for the spawn gate.
// Package-level var seam (mirrors config.renameState/symlinkState and
// doctor.binaryLookupFn elsewhere in this codebase) so tests can force the
// otherwise practically-unreachable os.Executable failure path.
var executableFn = os.Executable

// defaultUpdateSpawn launches a detached `atomic update --check` child that
// performs the GitHub lookup this process never does. Setsid puts the child
// in its own session so it outlives this invocation; its standard streams
// are nil so nothing blocks it once this process exits — mirrors
// atomic/internal/repl/spawn.go's DefaultSpawn. Never waits on the child.
func defaultUpdateSpawn(exe string) error {
	cmd := exec.Command(exe, "update", "--check", backgroundCheckMarker)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("selfupdate: start %s: %w", exe, err)
	}
	// Nothing here will ever Wait: the child outlives this process, and
	// releasing the handle hands reaping to init.
	_ = cmd.Process.Release()
	return nil
}

// buildUpdateCmd returns the "update" top-level command with flag metadata.
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

// runUpdateCheck implements the check branch of `atomic update --check`
// (spec Flow "detached child check + stage", steps 2-5): performs the
// GitHub lookup and writes latest_version/last_result to state.json
// regardless of outcome, then — only when background is true, i.e. this is
// the auto-spawned invocation carrying backgroundCheckMarker, never a
// manually-typed --check — runs the once-only background-staging gate.
// Returns exactly what c.Check would have returned so callers reproduce the
// existing stdout/exit-code contract unchanged; performs no process exit
// itself so every branch is directly testable. A home of "" (home dir
// unresolvable) degrades to a bare lookup with no state I/O at all.
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
		// c.Check already normalizes tag via selfupdate's displayVersion —
		// F-1: latest_version is written pre-stripped of any "v" prefix.
		state.Update.LatestVersion = tag
		state.Update.LastResult = ""
	}
	if err := selfupdate.WriteState(statePath, state); err != nil {
		fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
	}

	// Background-marker-only, once-only staging gate (spec Flow "detached
	// child check + stage", steps 4a-4d). A manually-typed --check never
	// reaches past here — spec Non-goal 6.
	if !background || lookupErr != nil || !newer {
		return newer, tag, lookupErr
	}
	cfg, _, cerr := config.Load(config.TOMLPath(home))
	if cerr != nil || !cfg.Update.Stage {
		return newer, tag, lookupErr
	}

	// Re-read right before the once-only check: the base write above may
	// already be superseded by a concurrent writer under the accepted
	// best-effort concurrency model (design: "collisions lose one field
	// update and self-heal on the next cycle").
	gate := selfupdate.LoadState(statePath)
	if gate.Update.StageAttemptedFor == tag {
		return newer, tag, lookupErr
	}

	lockedAt := now()
	locked, acquired := selfupdate.AcquireLock(gate, lockedAt)
	if !acquired {
		// Lock contention: skip staging this cycle WITHOUT stamping
		// stage_attempted_for — the once-only budget is spent only on a
		// real download attempt, never on contention.
		return newer, tag, lookupErr
	}
	locked.Update.StageAttemptedFor = tag
	if err := selfupdate.WriteState(statePath, locked); err != nil {
		fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
		return newer, tag, lookupErr
	}

	// The full Release (with Assets) is needed for staging; c.Check above
	// only returns (newer, tag, err). This second Lookup only happens on
	// the rare about-to-download-a-multi-MB-archive path, not on every
	// check, so it does not multiply the hourly GitHub-lookup budget in
	// practice.
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
	// Owner-checked release (fencing on lockedAt, the token this staging
	// attempt recorded above): if a foreground `atomic update` has since
	// taken over (or --force-stamped) the lock while this download was in
	// flight, final's freshly-reloaded lock fields already reflect that
	// newer holder — ReleaseLock leaves them untouched rather than
	// clobbering an active foreground swap.
	final = selfupdate.ReleaseLock(final, lockedAt)
	if err := selfupdate.WriteState(statePath, final); err != nil {
		fmt.Fprintf(w, "atomic update: write update state: %v\n", err)
	}

	return newer, tag, lookupErr
}

func runUpdate(args []string) {
	// The parent's detached spawn appends backgroundCheckMarker to mark this
	// invocation as auto-spawned; wired into the once-only background-staging
	// gate below. Stripped before flag parsing so it never surfaces as an
	// unrecognized flag.
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
		// Best-effort: an unresolvable home dir degrades to a raw check with
		// no state I/O — the same best-effort pattern already used for every
		// other home-dir lookup in this function.
		home, _ := os.UserHomeDir()
		newer, tag, err := runUpdateCheck(ctx, home, background, c, channel, version.Version, time.Now, os.Stderr)
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

	home, _ := os.UserHomeDir()
	if err := runUpdateApply(ctx, home, c, channel, version.Version, exe, force, time.Now, os.Stdout); err != nil {
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
		if err := runMigrateInstall(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic update: migrations failed: %v\nrun `atomic migrate` manually.\n", err)
		}
	}

	// Post-update doctor: load config to check user preference, then run.
	// Ignore home-dir errors and config warnings — doctor will catch real issues.
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

// runUpdateApply performs the foreground swap flow (spec Flow "staged
// fast-path swap" + "--force"): lock acquire/refuse/takeover (or --force
// bypass), a fresh GitHub lookup (state's own latest_version is never
// trusted for this decision), and either a staged fast-path swap or a
// fallback to the full Apply download flow. currentBinary must already be
// the resolved, symlink-evaluated path to the running executable. Reports
// "up to date" / "updated X → Y" to w exactly as the prior c.Update-based
// implementation did, so callers observe an unchanged contract. State I/O
// is skipped (never blocks the swap) when home is unresolvable — the same
// best-effort degrade used throughout this file.
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

	// releaseLock reloads the freshest on-disk state (falling back to the
	// in-memory state when statePath is unresolvable) and hands it to
	// ReleaseLock fenced on acquiredAt — the token this process stamped
	// above. If a newer holder has since taken over (stale-lock takeover or
	// --force) the lock fields on disk are left untouched; mutate still
	// applies this process's own non-lock field changes regardless of
	// ownership.
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
			// Version matched but the checksum failed to re-verify (or the
			// staged file vanished) — discard the stale record so it is
			// never retried, then fall back to the full download flow.
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
