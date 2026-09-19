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
	"github.com/damusix/atomic-claude/atomic/internal/install"
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

// targetConvergeMarker marks the re-exec of the freshly swapped binary that owns
// post-swap target convergence. Like backgroundCheckMarker it is stripped before
// parsing and never registered on a FlagSet, so it stays out of --help and the
// cliusage surface.
const targetConvergeMarker = "--__target-converge"

// stripTargetConvergeMarker reports whether the marker was present.
func stripTargetConvergeMarker(args []string) (found bool, cleaned []string) {
	cleaned = make([]string, 0, len(args))
	for _, a := range args {
		if a == targetConvergeMarker {
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

	// The banner's install decision is channel-dependent, so the config is read
	// before it. An unreadable config still banners, on stable.
	cfg, _, err := config.Load(config.TOMLPath(home))
	channel := selfupdate.ChannelStable
	if err == nil && cfg.Update.Channel != "" {
		channel = cfg.Update.Channel
	}

	if selfupdate.ShouldNotify(channel, current, state.Update.LatestVersion, state.Update.LastNotified, nowVal) {
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
	c.Flags().Bool("pre", false, "shorthand for --channel prerelease")
	c.Flags().Bool("no-doctor", false, "skip post-update doctor self-check")
	c.Flags().Bool("skip-claude-update", false, "skip the post-swap enrolled-target convergence")
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

// resolveUpdateChannel applies the channel precedence: an explicit --pre or
// --channel, then update.channel from config, then stable. --pre never writes
// config, so it stays a one-shot override of a persisted channel.
func resolveUpdateChannel(home string, fs *flag.FlagSet, channel string, pre bool) (string, error) {
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	switch {
	case pre && explicit["channel"] && channel != selfupdate.ChannelPrerelease:
		return "", fmt.Errorf("--pre conflicts with --channel %s", channel)
	case pre:
		return selfupdate.ChannelPrerelease, nil
	case explicit["channel"]:
		if !selfupdate.ValidChannel(channel) {
			return "", fmt.Errorf("--channel %q is not one of: prerelease, stable", channel)
		}
		return channel, nil
	}
	if home == "" {
		return selfupdate.ChannelStable, nil
	}
	// A config that fails to load or carries an invalid channel falls back to
	// stable rather than blocking the update.
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil || !selfupdate.ValidChannel(cfg.Update.Channel) {
		return selfupdate.ChannelStable, nil
	}
	return cfg.Update.Channel, nil
}

// updateDeps are the collaborators one `atomic update` invocation reaches.
// Production wiring is defaultUpdateDeps; tests replace the network, the binary
// swap, and the re-exec so the stale/replacement boundary is observable without
// a second binary.
type updateDeps struct {
	client       *selfupdate.Client
	executable   func() (string, error)
	evalSymlinks func(string) (string, error)
	runChild     func(exe string, args ...string) error
	apply        func(ctx context.Context, home string, c *selfupdate.Client, channel, currentVersion, currentBinary string, force bool, now func() time.Time, w io.Writer) (bool, error)
	converge     func(home string, w io.Writer) error
	migrate      func(home string) error
	doctor       func(w io.Writer)
	terminal     func() bool
	out          io.Writer
	errOut       io.Writer
	now          func() time.Time
}

// defaultUpdateDeps wires the production collaborators.
func defaultUpdateDeps() updateDeps {
	return updateDeps{
		client:       &selfupdate.Client{},
		executable:   os.Executable,
		evalSymlinks: filepath.EvalSymlinks,
		runChild:     defaultRunCmd,
		apply:        runUpdateApplyOutcome,
		converge:     convergeEnrolledTargets,
		migrate:      runMigrateInstall,
		doctor:       func(w io.Writer) { updatedoctor.Run(doctor.Run, w) },
		terminal:     func() bool { return charmterm.IsTerminal(os.Stdout.Fd()) },
		out:          os.Stdout,
		errOut:       os.Stderr,
		now:          time.Now,
	}
}

func runUpdate(args []string) {
	os.Exit(runUpdateWith(args, defaultUpdateDeps()))
}

// runUpdateWith is the testable core of `atomic update`. It returns the process
// exit code instead of exiting, so the re-exec boundary is exercised in-process.
func runUpdateWith(args []string, deps updateDeps) int {
	// Stripped before flag parsing so they never surface as unknown flags.
	background, args := stripBackgroundCheckMarker(args)
	convergeChild, args := stripTargetConvergeMarker(args)

	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic update [--check] [--pre | --channel stable|prerelease] [--no-doctor] [--skip-claude-update] [--force]")
	var check bool
	var channel string
	var pre bool
	var noDoctor bool
	var skipTargets bool
	var force bool
	fs.BoolVar(&check, "check", false, "only check if an update is available; do not apply")
	fs.StringVar(&channel, "channel", "stable", "release channel: stable or prerelease")
	fs.BoolVar(&pre, "pre", false, "shorthand for --channel prerelease")
	fs.BoolVar(&noDoctor, "no-doctor", false, "skip post-update doctor self-check")
	fs.BoolVar(&skipTargets, "skip-claude-update", false, "skip the post-swap enrolled-target convergence")
	fs.BoolVar(&force, "force", false, "bypass the update lock; never weakens checksum verification")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// An unresolvable home degrades to a raw run with no state or config I/O.
	home, _ := os.UserHomeDir()
	channel, err := resolveUpdateChannel(home, fs, channel, pre)
	if err != nil {
		fmt.Fprintf(deps.errOut, "atomic update: %v\n", err)
		return 2
	}

	ctx := context.Background()

	if check {
		newer, tag, err := runUpdateCheck(ctx, home, background, deps.client, channel, version.Version, deps.now, deps.errOut)
		if err != nil {
			// Exit 2 for a hard error, distinct from the exit-1 "available" signal.
			fmt.Fprintf(deps.errOut, "atomic update: %v\n", err)
			return 2
		}
		if newer {
			// Exit 1 signals "available", the diff(1) idiom.
			fmt.Fprintf(deps.out, "update available: %s (current: %s)\n", tag, version.Version)
			return 1
		}
		fmt.Fprintf(deps.out, "atomic is up to date (%s)\n", tag)
		return 0
	}

	// The replacement binary owns post-swap convergence: it embeds the selected
	// generation, which is the only corpus allowed to publish.
	if convergeChild {
		return runUpdatePostSwap(home, noDoctor, skipTargets, deps)
	}

	exe, err := deps.executable()
	if err != nil {
		fmt.Fprintf(deps.errOut, "atomic update: resolve executable: %v\n", err)
		return 1
	}
	exe, err = deps.evalSymlinks(exe)
	if err != nil {
		fmt.Fprintf(deps.errOut, "atomic update: resolve symlinks: %v\n", err)
		return 1
	}

	deps.client.OnProgress = downloadProgressRenderer(deps.out, deps.terminal())

	swapped, err := deps.apply(ctx, home, deps.client, channel, version.Version, exe, force, deps.now, deps.out)
	if err != nil {
		fmt.Fprintf(deps.errOut, "atomic update: %v\n", err)
		return 1
	}

	if swapped {
		// The swap replaced the binary on disk; this process still embeds the
		// stale corpus, so it must refuse every convergence path and hand all
		// post-swap work to the replacement. Only when nothing was swapped is
		// this binary the selected generation and allowed to converge in place.
		childArgs := []string{"update", targetConvergeMarker}
		if skipTargets {
			childArgs = append(childArgs, "--skip-claude-update")
		}
		if noDoctor {
			childArgs = append(childArgs, "--no-doctor")
		}
		if err := deps.runChild(exe, childArgs...); err != nil {
			fmt.Fprintf(deps.errOut, "atomic update: target convergence failed: %v\nrun `atomic harness repair` manually.\n", err)
		}
		return 0
	}

	// Already current: this binary embeds the selected generation.
	return runUpdatePostSwap(home, noDoctor, skipTargets, deps)
}

// runUpdatePostSwap converges every enrolled target, then runs the migrations
// and doctor that follow a completed update. It runs only in a process whose
// embedded corpus is the selected generation: the re-exec'd replacement after a
// swap, or the already-current binary when no swap was needed. A convergence
// failure warns and never blocks the update success path. With an unresolvable
// home there is no install root, so both convergence and migrations are
// skipped: the update stays a raw run with no state or config I/O.
func runUpdatePostSwap(home string, noDoctor, skipTargets bool, deps updateDeps) int {
	if !skipTargets {
		if err := deps.converge(home, deps.out); err != nil {
			fmt.Fprintf(deps.errOut, "atomic update: target convergence failed: %v\nrun `atomic harness repair` manually.\n", err)
		}
	}
	if home != "" {
		if err := deps.migrate(home); err != nil {
			fmt.Fprintf(deps.errOut, "atomic update: migrations failed: %v\nrun `atomic migrate` manually.\n", err)
		}
	}
	cfgRunDoctor := true // safe default when config is unreadable
	if home != "" {
		if cfg, _, cerr := config.Load(config.TOMLPath(home)); cerr == nil {
			cfgRunDoctor = cfg.Update.RunDoctor
		}
	}
	if shouldRunPostUpdateDoctor(noDoctor, cfgRunDoctor) {
		deps.doctor(deps.out)
	}
	return 0
}

// convergeEnrolledTargets runs the CP7A install engine over every enrolled
// target. `atomic update` is the user's consent, so the plan is auto-approved;
// adapters re-acquire the lifecycle lock and recover unresolved journals before
// they mutate. A home with no enrollment is a no-op, never an error.
func convergeEnrolledTargets(home string, w io.Writer) error {
	if home == "" {
		return nil
	}
	steps := install.DefaultSteps(home)
	steps.AssumeYes = true
	reports, err := steps.ConvergeEnrolled()
	if err != nil {
		return err
	}
	for _, r := range reports {
		switch {
		case len(r.Blockers) > 0:
			fmt.Fprintf(w, "%s\t%s\t%s\n", r.Target.Key(), r.Status, r.Blockers[0])
		case r.Applied:
			fmt.Fprintf(w, "%s\t%s\tapplied\n", r.Target.Key(), r.Status)
		default:
			fmt.Fprintf(w, "%s\t%s\n", r.Target.Key(), r.Status)
		}
	}
	return nil
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

// runUpdateApply performs the foreground swap and reports only its error.
func runUpdateApply(ctx context.Context, home string, c *selfupdate.Client, channel, currentVersion, currentBinary string, force bool, now func() time.Time, w io.Writer) error {
	_, err := runUpdateApplyOutcome(ctx, home, c, channel, currentVersion, currentBinary, force, now, w)
	return err
}

// runUpdateApplyOutcome performs the foreground swap: lock acquire/takeover, a
// fresh GitHub lookup (state's own latest_version is never trusted for this
// call), then a staged fast-path swap or a fallback to the full download. It
// reports whether the binary was replaced: a false outcome with a nil error
// means the running binary is already the selected generation. Callers must pass
// currentBinary already symlink-resolved. State I/O is skipped, never blocking
// the swap, when home is unresolvable.
func runUpdateApplyOutcome(ctx context.Context, home string, c *selfupdate.Client, channel, currentVersion, currentBinary string, force bool, now func() time.Time, w io.Writer) (bool, error) {
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
		return false, err
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
		return false, err
	}

	if !selfupdate.ShouldInstall(channel, currentVersion, rel.TagName) {
		writeState(releaseLock(nil))
		fmt.Fprintf(w, "atomic is up to date (%s)\n", selfupdate.DisplayVersion(rel.TagName))
		return false, nil
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
			return false, aerr
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
	return true, nil
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
