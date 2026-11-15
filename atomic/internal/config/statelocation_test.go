package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points projectHome at a temp dir and clears the resolution caches, so
// a test can change config.toml and observe the new resolution.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	restore := SetHomeDirForTest(home)
	t.Cleanup(restore)
	return home
}

func captureStateWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	t.Cleanup(SetStateWarnOutForTest(&buf))
	return &buf
}

func populate(t *testing.T, repo, name string, entry ...string) {
	t.Helper()
	if len(entry) == 0 {
		entry = []string{"project"}
	}
	if err := os.MkdirAll(filepath.Join(repo, name, filepath.Join(entry...)), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeStateDirConfig(t *testing.T, home, value string) {
	t.Helper()
	cfg := Default()
	if value != "" {
		if err := Set(cfg, "state.dir", value); err != nil {
			t.Fatalf("Set(state.dir, %q): %v", value, err)
		}
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	resetStateResolutionCacheForTest()
}

// A verb that records or withdraws a selection and then builds a path in the
// same process must get the root it just chose, not the cached one.
func TestRepositoryStateDir_SeesASelectionWrittenMidProcess(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	if got, _ := RepositoryStateDir(repo); got != filepath.Join(repo, stateDirDefault) {
		t.Fatalf("before = %q, want the default root", got)
	}
	written, err := PersistStateSelection(repo, ".record")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := RepositoryStateDir(repo); got != filepath.Join(repo, ".record") {
		t.Errorf("after persist = %q, want the recorded root", got)
	}
	if err := WithdrawStateSelection(repo, written); err != nil {
		t.Fatal(err)
	}
	if got, _ := RepositoryStateDir(repo); got != filepath.Join(repo, stateDirDefault) {
		t.Errorf("after withdraw = %q, want the default root again", got)
	}
}

// The ladder is process override, record, configured segment, built-in default,
// most specific first.
func TestResolveStateLocation_Order(t *testing.T) {
	home := withHome(t)
	repo := t.TempDir()
	writeStateDirConfig(t, home, ".state")

	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, ".state") || got.Source != StateSourceConfig {
		t.Fatalf("configured resolution = %+v, want %s/.state (config)", got, repo)
	}

	if _, err := PersistStateSelection(repo, ".record"); err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
	got, err = ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation after record: %v", err)
	}
	if got.Root != filepath.Join(repo, ".record") || got.Source != StateSourceRecord {
		t.Fatalf("record resolution = %+v, want %s/.record (record)", got, repo)
	}

	override := t.TempDir()
	t.Setenv(StateDirEnvVar, override)
	got, err = ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation with override: %v", err)
	}
	if got.Root != override || got.Source != StateSourceProcessEnv {
		t.Fatalf("process resolution = %+v, want %s (process-env)", got, override)
	}
}

func TestResolveStateLocation_BuiltinDefault(t *testing.T) {
	withHome(t)
	repo := t.TempDir()

	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, ".atomic") || got.Source != StateSourceDefault {
		t.Fatalf("default resolution = %+v, want %s/.atomic", got, repo)
	}
}

// A process override affects one process only: it never writes the persisted
// selection record.
func TestResolveStateLocation_ProcessOverridePersistsNothing(t *testing.T) {
	home := withHome(t)
	repo := t.TempDir()
	override := t.TempDir()
	t.Setenv(StateDirEnvVar, override)

	if _, err := ResolveStateLocation(repo); err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("process override created a selection record at %s", StateLocationRecordPath(repo))
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("process override wrote under the real state home")
	}
}

// state.dir is a repository-relative path of one or more safe segments:
// absolute, empty, and parent-escaping values never name a state directory.
func TestStateDirPathValidation(t *testing.T) {
	for _, valid := range []string{".claude", ".state", "state", ".my-state_1", "state/atomic", ".config/atomic"} {
		if err := ValidateStateDirPath(valid); err != nil {
			t.Errorf("ValidateStateDirPath(%q): unexpected error %v", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "..", "../x", "a/../b", "a/./b", "/abs", "a//b", "a/", "..\\win", "sp ace"} {
		if err := ValidateStateDirPath(invalid); err == nil {
			t.Errorf("ValidateStateDirPath(%q): expected error", invalid)
		}
	}
}

// The config surface accepts, resolves, and rejects state.dir by the same rules.
func TestConfigStateDirKey(t *testing.T) {
	cfg := Default()
	if got, _ := Get(cfg, "state.dir"); got != ".atomic" {
		t.Errorf("default state.dir = %q, want .atomic", got)
	}
	for _, bad := range []string{"/abs", "../x", "a/../b", ""} {
		if err := Set(cfg, "state.dir", bad); err == nil {
			t.Errorf("Set(state.dir, %q) must be rejected", bad)
		}
	}
	if err := Set(cfg, "state.dir", "state/atomic"); err != nil {
		t.Fatalf("Set multi-segment: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate after set: %v", err)
	}
	if err := Unset(cfg, "state.dir"); err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if cfg.State.Dir != "" {
		t.Errorf("state.dir after Unset = %q, want unset", cfg.State.Dir)
	}
}

// An empty repository has no candidate, and resolving it writes no record.
func TestSelectStateCandidate_EmptyRepoHasNone(t *testing.T) {
	withHome(t)
	repo := t.TempDir()

	if _, ok, err := SelectStateCandidate(repo); err != nil || ok {
		t.Fatalf("SelectStateCandidate = ok %v, err %v; want none", ok, err)
	}
	if _, err := ResolveStateLocation(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("empty repo created an adoption record")
	}
}

// An empty state directory is not a candidate: only real Atomic state
// signatures count.
func TestDiscoverStateCandidates_IgnoresEmptyDirectories(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	for _, name := range []string{stateDirDefault, ".claude"} {
		if err := os.MkdirAll(filepath.Join(repo, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := DiscoverStateCandidates(repo)
	if err != nil {
		t.Fatalf("DiscoverStateCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %v, want none for empty harness dirs", candidates)
	}
}

// Two populated candidates beside an unpopulated destination are ambiguous and
// refuse with actionable guidance.
func TestSelectStateCandidate_AmbiguousRefuses(t *testing.T) {
	home := withHome(t)
	writeStateDirConfig(t, home, ".state")
	repo := t.TempDir()
	for _, name := range []string{stateDirDefault, ".claude"} {
		populate(t, repo, name, ".atomic-index")
	}

	_, _, err := SelectStateCandidate(repo)
	if err == nil {
		t.Fatal("expected ambiguity refusal")
	}
	if !errors.Is(err, ErrAmbiguousStateRoot) {
		t.Errorf("error = %v, want ErrAmbiguousStateRoot", err)
	}
	if !strings.Contains(err.Error(), "atomic state adopt") {
		t.Errorf("error %q must name the explicit selection verb", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("ambiguous adoption wrote a selection record")
	}
}

// A persisted selection is withdrawn only while its bytes are unchanged — a
// later state write turns rollback into a conflict.
func TestWithdrawStateSelection_OnlyWhenUnchanged(t *testing.T) {
	withHome(t)
	repo := t.TempDir()

	first, err := PersistStateSelection(repo, ".claude")
	if err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
	if err := WithdrawStateSelection(repo, first); err != nil {
		t.Fatalf("withdraw of an unchanged record: %v", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Fatalf("withdraw left the record in place")
	}

	again, err := PersistStateSelection(repo, ".claude")
	if err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
	if _, err := PersistStateSelection(repo, ".other"); err != nil {
		t.Fatalf("PersistStateSelection (later write): %v", err)
	}
	err = WithdrawStateSelection(repo, again)
	if !errors.Is(err, ErrStateSelectionConflict) {
		t.Fatalf("withdraw after a later write = %v, want ErrStateSelectionConflict", err)
	}
	sel, ok, rerr := ReadStateSelection(repo)
	if rerr != nil || !ok || sel.StateDir != ".other" {
		t.Errorf("later selection was lost: sel=%+v ok=%v err=%v", sel, ok, rerr)
	}
}

// No fingerprint selects state, and a `.pi` tree without Atomic signatures is
// not adopted.
func TestPiTreeIsNotAStateCandidate(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")

	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, stateDirDefault) {
		t.Errorf("fingerprints changed repository state to %q", got.Root)
	}

	piDir := filepath.Join(repo, ".pi", "agents")
	if err := os.MkdirAll(piDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candidates, err := DiscoverStateCandidates(repo)
	if err != nil {
		t.Fatalf("DiscoverStateCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("pi agent tree adopted as state candidate: %v", candidates)
	}
}

// MutablePaths names every ~/.atomic authority with exactly one writer.
func TestMutablePathsFor(t *testing.T) {
	home := "/home/u"
	m := MutablePathsFor(home)
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ConfigTOML", m.ConfigTOML, "/home/u/.atomic/config.toml"},
		{"ProfileMD", m.ProfileMD, "/home/u/.atomic/profile.md"},
		{"WikisMD", m.WikisMD, "/home/u/.atomic/wikis.md"},
		{"InstallDir", m.InstallDir, "/home/u/.atomic/install"},
		{"BackupsDir", m.BackupsDir, "/home/u/.atomic/backups"},
		{"PackagesDir", m.PackagesDir, "/home/u/.atomic/packages"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if got := PackageRoot(home, "omp"); got != "/home/u/.atomic/packages/omp/atomic" {
		t.Errorf("PackageRoot = %q", got)
	}
}

func bothResolvers(t *testing.T, repo string) (StateLocation, string, error) {
	t.Helper()
	strict, serr := ResolveStateLocation(repo)
	cached, cerr := repositoryStateDir(repo)
	if (serr == nil) != (cerr == nil) {
		t.Fatalf("strict err = %v, cached err = %v; must agree", serr, cerr)
	}
	if serr == nil && strict.Root != cached {
		t.Fatalf("strict root = %s, cached root = %s; must agree", strict.Root, cached)
	}
	return strict, cached, serr
}

// A lone populated legacy root resolves in place while the destination is
// empty, so existing state is never orphaned.
func TestLadder_LoneLegacyRootResolvesInPlace(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	populate(t, repo, ".claude")

	got, _, err := bothResolvers(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(repo, ".claude") || got.Source != StateSourceCandidate {
		t.Errorf("resolution = %+v, want %s/.claude (candidate)", got, repo)
	}
}

// worktrees is not a signature, so a .claude holding only linked worktrees is
// unpopulated; rules/wiki is one.
func TestLadder_SignatureSet(t *testing.T) {
	withHome(t)

	onlyWorktrees := t.TempDir()
	populate(t, onlyWorktrees, ".claude", "worktrees")
	got, _, err := bothResolvers(t, onlyWorktrees)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(onlyWorktrees, ".atomic") || got.Source != StateSourceDefault {
		t.Errorf("worktree-only .claude resolved %+v, want the .atomic default", got)
	}

	wikiCards := t.TempDir()
	populate(t, wikiCards, ".claude", "rules", "wiki")
	got, _, err = bothResolvers(t, wikiCards)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(wikiCards, ".claude") {
		t.Errorf("rules/wiki .claude resolved %+v, want .claude", got)
	}
}

// A populated destination answers; a populated .claude beside it draws exactly
// one warning per process that names its leftover entries.
func TestLadder_PopulatedDestinationWinsAndWarnsOnce(t *testing.T) {
	withHome(t)
	warnings := captureStateWarnings(t)
	repo := t.TempDir()
	populate(t, repo, ".atomic", ".scratchpad")
	populate(t, repo, ".claude", "project")
	populate(t, repo, ".claude", "atomic.toml")

	for i := 0; i < 3; i++ {
		got, _, err := bothResolvers(t, repo)
		if err != nil {
			t.Fatal(err)
		}
		if got.Root != filepath.Join(repo, ".atomic") || got.Source != StateSourceDefault {
			t.Fatalf("resolution = %+v, want the populated .atomic destination", got)
		}
	}
	out := warnings.String()
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("got %d warning lines, want exactly 1:\n%s", n, out)
	}
	for _, want := range []string{filepath.Join(repo, ".claude"), "project", "atomic.toml"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning %q must name %q", out, want)
		}
	}
}

// A configured state.dir is the destination: it answers over .atomic and
// .claude, which both draw a warning.
func TestLadder_ConfiguredDestinationWins(t *testing.T) {
	home := withHome(t)
	warnings := captureStateWarnings(t)
	writeStateDirConfig(t, home, "state/atomic")
	repo := t.TempDir()
	populate(t, repo, "state/atomic")
	populate(t, repo, ".atomic")
	populate(t, repo, ".claude")

	got, _, err := bothResolvers(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(repo, "state", "atomic") || got.Source != StateSourceConfig {
		t.Fatalf("resolution = %+v, want the configured multi-segment destination", got)
	}
	if n := strings.Count(warnings.String(), "\n"); n != 2 {
		t.Errorf("got %d warning lines, want one per leftover root:\n%s", n, warnings)
	}
}

// Only two populated non-destination candidates refuse, in both resolvers.
func TestLadder_TwoNonDestinationCandidatesRefuse(t *testing.T) {
	home := withHome(t)
	writeStateDirConfig(t, home, ".state")
	repo := t.TempDir()
	populate(t, repo, ".atomic")
	populate(t, repo, ".claude")

	_, _, err := bothResolvers(t, repo)
	if !errors.Is(err, ErrAmbiguousStateRoot) {
		t.Fatalf("error = %v, want ErrAmbiguousStateRoot", err)
	}
	if !strings.Contains(err.Error(), "atomic state adopt") {
		t.Errorf("error %q must name the explicit selection verb", err)
	}
	if _, _, err := SelectStateCandidate(repo); !errors.Is(err, ErrAmbiguousStateRoot) {
		t.Errorf("adopt error = %v, want ErrAmbiguousStateRoot", err)
	}
}

// Ladder order: process override, selection record, populated candidate.
func TestLadder_RecordOutranksCandidate(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	populate(t, repo, ".claude")
	populate(t, repo, ".atomic")
	if _, err := PersistStateSelection(repo, "chosen/dir"); err != nil {
		t.Fatal(err)
	}

	got, _, err := bothResolvers(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(repo, "chosen", "dir") || got.Source != StateSourceRecord {
		t.Errorf("resolution = %+v, want the recorded root", got)
	}
}

// An invalid ATOMIC_STATE_DIR refuses in the cached resolver exactly as in the
// strict one, never falling through to a populated candidate or the default.
func TestRepositoryStateDir_InvalidEnvRefuses(t *testing.T) {
	for _, bad := range []string{"../escape", "a/../b", "/"} {
		t.Run(bad, func(t *testing.T) {
			withHome(t)
			repo := t.TempDir()
			populate(t, repo, ".claude")
			t.Setenv(StateDirEnvVar, bad)

			_, strictErr := ResolveStateLocation(repo)
			root, cachedErr := repositoryStateDir(repo)
			if strictErr == nil || cachedErr == nil {
				t.Fatalf("strict err = %v, cached err = %v (root %q); both must refuse", strictErr, cachedErr, root)
			}
			if !strings.Contains(cachedErr.Error(), StateDirEnvVar) {
				t.Errorf("cached error %q must name %s", cachedErr, StateDirEnvVar)
			}
		})
	}
}

// A valid override still answers first, even when the cache holds an earlier
// resolution for the same root.
func TestRepositoryStateDir_EnvIsNeverCached(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	if _, err := repositoryStateDir(repo); err != nil {
		t.Fatal(err)
	}
	override := t.TempDir()
	t.Setenv(StateDirEnvVar, override)
	got, err := repositoryStateDir(repo)
	if err != nil || got != override {
		t.Errorf("repositoryStateDir = %q, %v; want %q", got, err, override)
	}
}

// A multi-segment state.dir and a relative record resolve against whichever
// repository root asks, so a linked worktree gets its own copy.
func TestResolveStateDirValue_RelativeFollowsTheAskingRoot(t *testing.T) {
	mainRoot, linked := t.TempDir(), t.TempDir()
	for _, root := range []string{mainRoot, linked} {
		got, err := resolveStateDirValue(root, "state/atomic")
		if err != nil || got != filepath.Join(root, "state", "atomic") {
			t.Errorf("resolveStateDirValue(%s) = %q, %v", root, got, err)
		}
	}
	for _, bad := range []string{"", "../x", "a/../../x", "/"} {
		if _, err := resolveStateDirValue(mainRoot, bad); err == nil {
			t.Errorf("resolveStateDirValue(%q) accepted", bad)
		}
	}
}

func TestValidateStateDirEnv(t *testing.T) {
	t.Setenv(StateDirEnvVar, "")
	if err := ValidateStateDirEnv(); err != nil {
		t.Errorf("unset override refused: %v", err)
	}
	t.Setenv(StateDirEnvVar, "state/atomic")
	if err := ValidateStateDirEnv(); err != nil {
		t.Errorf("valid relative override refused: %v", err)
	}
	t.Setenv(StateDirEnvVar, filepath.Join(t.TempDir(), "x"))
	if err := ValidateStateDirEnv(); err != nil {
		t.Errorf("valid absolute override refused: %v", err)
	}
	for _, bad := range []string{"../escape", "a/../b", "/"} {
		t.Setenv(StateDirEnvVar, bad)
		err := ValidateStateDirEnv()
		if err == nil || !strings.Contains(err.Error(), StateDirEnvVar) {
			t.Errorf("ValidateStateDirEnv(%q) = %v, want an %s refusal", bad, err, StateDirEnvVar)
		}
	}
}

// A path helper has no error to return, so a refusal that reached it stops the
// process instead of resolving somewhere the user's state is not.
func TestStateRoot_RefusalIsFatalNotADefault(t *testing.T) {
	home := withHome(t)
	writeStateDirConfig(t, home, ".state")
	repo := t.TempDir()
	populate(t, repo, ".atomic")
	populate(t, repo, ".claude")
	var refused error
	prev := stateRefusalExit
	stateRefusalExit = func(err error) { refused = err; panic("exit") }
	t.Cleanup(func() { stateRefusalExit = prev })

	func() {
		defer func() { _ = recover() }()
		ScratchpadDir(repo)
	}()
	if !errors.Is(refused, ErrAmbiguousStateRoot) {
		t.Fatalf("helper refusal = %v, want ErrAmbiguousStateRoot", refused)
	}
}

func TestPersistStateSelection_MultiSegmentRelativeAndAbsolute(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	abs := filepath.Join(t.TempDir(), "elsewhere")
	for _, value := range []string{"state/atomic", abs} {
		if _, err := PersistStateSelection(repo, value); err != nil {
			t.Errorf("PersistStateSelection(%q): %v", value, err)
		}
	}
	for _, bad := range []string{"../x", "a/../../x"} {
		if _, err := PersistStateSelection(repo, bad); err == nil {
			t.Errorf("PersistStateSelection(%q) accepted a parent-escaping path", bad)
		}
	}
}

// ATOMIC_HARNESS selects, refuses, and alters nothing.
func TestResolveStateLocation_IgnoresAtomicHarness(t *testing.T) {
	for _, val := range []string{"claude", "omp", "bogus"} {
		t.Run(val, func(t *testing.T) {
			withHome(t)
			repo := t.TempDir()
			t.Setenv("ATOMIC_HARNESS", val)

			got, err := ResolveStateLocation(repo)
			if err != nil {
				t.Fatalf("ResolveStateLocation: %v", err)
			}
			if got.Root != filepath.Join(repo, stateDirDefault) || got.Source != StateSourceDefault {
				t.Errorf("resolution = %+v, want the built-in default", got)
			}
		})
	}
}

// .pi is not a candidate name: a repository whose only state is under it
// resolves the built-in default.
func TestDiscoverStateCandidates_PiIsNotACandidate(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".pi", ".atomic-index"), 0o755); err != nil {
		t.Fatal(err)
	}
	candidates, err := DiscoverStateCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %v, want none", candidates)
	}
	if got, err := repositoryStateDir(repo); err != nil || got != filepath.Join(repo, stateDirDefault) {
		t.Errorf("resolved = %s, %v; want the built-in default", got, err)
	}
}
