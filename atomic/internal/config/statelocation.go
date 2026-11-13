package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// stateDirDefault is the built-in repository-state directory, the last rung of
// the neutral ladder.
const stateDirDefault = ".atomic"

// stateDirLegacy is the former default. It stays a candidate so state written
// there keeps resolving.
const stateDirLegacy = ".claude"

var stateWarnOut io.Writer = os.Stderr

// SetStateWarnOutForTest redirects resolution warnings and returns a restore
// func.
func SetStateWarnOutForTest(w io.Writer) func() {
	prev := stateWarnOut
	stateWarnOut = w
	return func() { stateWarnOut = prev }
}

// StateDirEnvVar is the process-only repository-state override. It never
// rewrites the persisted selection and is read before every other rung.
const StateDirEnvVar = "ATOMIC_STATE_DIR"

// ErrAmbiguousStateRoot is returned when adoption finds more than one populated
// repository-state candidate and cannot pick one without user selection.
var ErrAmbiguousStateRoot = errors.New("config: ambiguous repository-state root")

// ErrStateSelectionConflict is returned when a guarded selection withdraw finds
// bytes other than the ones it recorded — a later state write used the record.
var ErrStateSelectionConflict = errors.New("config: state selection changed since it was recorded")

// StateSource identifies which rung of the neutral ladder decided the selected
// repository-state directory.
type StateSource string

const (
	// StateSourceProcessEnv is an ATOMIC_STATE_DIR process override.
	StateSourceProcessEnv StateSource = "process-env"
	// StateSourceRecord is the persisted ~/.atomic/<project-key>/state-location.json.
	StateSourceRecord StateSource = "selection-record"
	// StateSourceCandidate is the one populated candidate that answered in
	// place because the destination held no state.
	StateSourceCandidate StateSource = "populated-candidate"
	// StateSourceConfig is the user state.dir value.
	StateSourceConfig StateSource = "config-default"
	// StateSourceDefault is the built-in `.atomic` fallback.
	StateSourceDefault StateSource = "builtin-default"
)

// StateLocation is the resolved repository-state directory for one repository.
type StateLocation struct {
	Root   string
	Source StateSource
}

// StateSelection is the persisted per-project selection record, shared by every
// worktree of one clone.
type StateSelection struct {
	SchemaVersion int    `json:"schema_version"`
	StateDir      string `json:"state_dir"`
}

// stateSelectionSchemaVersion is the record schema this binary writes and reads.
const stateSelectionSchemaVersion = 1

// StateLocationRecordPath returns ~/.atomic/<project-key>/state-location.json.
func StateLocationRecordPath(repoRoot string) string {
	return filepath.Join(ProjectStateDir(repoRoot), "state-location.json")
}

// resolveStateDirValue maps a recorded or overridden value to a directory.
// Relative values join repoRoot, so each linked worktree gets its own copy.
func resolveStateDirValue(repoRoot, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("config: state.dir is empty")
	}
	if filepath.IsAbs(value) {
		clean := filepath.Clean(value)
		if clean == string(filepath.Separator) {
			return "", fmt.Errorf("config: state.dir %q: filesystem root is not a state directory", value)
		}
		return clean, nil
	}
	if err := ValidateStateDirPath(value); err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, value), nil
}

// ValidateStateDirEnv refuses an unusable ATOMIC_STATE_DIR. The override is
// process-global, so the root command checks it once before any verb runs.
func ValidateStateDirEnv() error {
	raw := os.Getenv(StateDirEnvVar)
	if raw == "" {
		return nil
	}
	if _, err := resolveStateDirValue("", raw); err != nil {
		return fmt.Errorf("config: %s: %w", StateDirEnvVar, err)
	}
	return nil
}

// ResolveStateLocation walks the ladder: ATOMIC_STATE_DIR, the selection
// record, the populated candidate, state.dir, `.atomic`. Invalid input refuses.
func ResolveStateLocation(repoRoot string) (StateLocation, error) {
	return resolveLadder(repoRoot, true)
}

// resolveLadder is ResolveStateLocation; warn false silences the leftover
// warning for callers that probe directories the user is not working in.
func resolveLadder(repoRoot string, warn bool) (StateLocation, error) {
	if raw := os.Getenv(StateDirEnvVar); raw != "" {
		root, err := resolveStateDirValue(repoRoot, raw)
		if err != nil {
			return StateLocation{}, fmt.Errorf("config: %s: %w", StateDirEnvVar, err)
		}
		return StateLocation{Root: root, Source: StateSourceProcessEnv}, nil
	}

	if repoRoot != "" {
		sel, ok, err := ReadStateSelection(repoRoot)
		if err != nil {
			return StateLocation{}, err
		}
		if ok {
			root, err := resolveStateDirValue(repoRoot, sel.StateDir)
			if err != nil {
				return StateLocation{}, fmt.Errorf("config: selection record %s: %w", StateLocationRecordPath(repoRoot), err)
			}
			return StateLocation{Root: root, Source: StateSourceRecord}, nil
		}

		chosen, source, ok, err := chooseCandidate(repoRoot, warn)
		if err != nil {
			return StateLocation{}, err
		}
		if ok {
			return StateLocation{Root: chosen.Root, Source: source}, nil
		}
	}

	if seg, ok := configuredStateDir(); ok {
		return StateLocation{Root: filepath.Join(repoRoot, seg), Source: StateSourceConfig}, nil
	}

	return StateLocation{Root: filepath.Join(repoRoot, stateDirDefault), Source: StateSourceDefault}, nil
}

// QuietStateDir resolves repoRoot without warning or caching, for repositories a
// caller merely visits; ok is false when the ladder refuses.
func QuietStateDir(repoRoot string) (string, bool) {
	if stateDirOverride != nil {
		return filepath.Join(repoRoot, *stateDirOverride), true
	}
	loc, err := resolveLadder(repoRoot, false)
	return loc.Root, err == nil
}

// QuietStateRoots is QuietStateDir for a reader probing a visited repository:
// when the ladder refuses, every populated candidate root instead of none.
func QuietStateRoots(repoRoot string) []string {
	if root, ok := QuietStateDir(repoRoot); ok {
		return []string{root}
	}
	candidates, _ := DiscoverStateCandidates(repoRoot)
	roots := make([]string, 0, len(candidates))
	for _, c := range candidates {
		roots = append(roots, c.Root)
	}
	return roots
}

// RepositoryStateDir is the cached root. Repo resolution calls it so a refusal
// stops a verb before the error-less path helpers run.
func RepositoryStateDir(repoRoot string) (string, error) {
	return repositoryStateDir(repoRoot)
}

// repositoryStateDir is the process-lifetime cached resolver behind every path
// helper; the override is read first.
func repositoryStateDir(repoRoot string) (string, error) {
	if stateDirOverride != nil {
		return filepath.Join(repoRoot, *stateDirOverride), nil
	}
	if os.Getenv(StateDirEnvVar) != "" {
		loc, err := ResolveStateLocation(repoRoot)
		return loc.Root, err
	}
	stateCacheMu.Lock()
	defer stateCacheMu.Unlock()
	if cached, ok := stateDirCached[repoRoot]; ok {
		return cached.root, cached.err
	}
	loc, err := ResolveStateLocation(repoRoot)
	stateDirCached[repoRoot] = cachedState{root: loc.Root, err: err}
	return loc.Root, err
}

type cachedState struct {
	root string
	err  error
}

var (
	stateCacheMu   sync.Mutex
	stateDirCached = map[string]cachedState{}
)

// stateRefusalExit backstops a refusal that reaches a path helper: only a verb
// that skips the root check and repoctx gets here. It must not return.
var stateRefusalExit = func(err error) {
	fmt.Fprintf(stateWarnOut, "atomic: %v\n", err)
	os.Exit(1)
}

var (
	warnedMu sync.Mutex
	warned   = map[string]bool{}
)

func warnOnce(key string, print func()) {
	warnedMu.Lock()
	defer warnedMu.Unlock()
	if warned[key] {
		return
	}
	warned[key] = true
	print()
}

// segmentCached caches the configured state.dir (else the built-in default);
// ATOMIC_STATE_DIR never feeds it.
var (
	segmentMu     sync.Mutex
	segmentCached *string
)

// configuredStateDir returns the configured state.dir path and whether the user
// configured one other than the built-in default.
func configuredStateDir() (string, bool) {
	segmentMu.Lock()
	defer segmentMu.Unlock()
	if segmentCached == nil {
		seg := readConfiguredStateDir()
		segmentCached = &seg
	}
	if *segmentCached == stateDirDefault {
		return stateDirDefault, false
	}
	return *segmentCached, true
}

// resetStateResolutionCacheForTest clears the process caches so a test can
// change config.toml or a record and observe the new resolution.
func resetStateResolutionCacheForTest() {
	segmentMu.Lock()
	segmentCached = nil
	segmentMu.Unlock()
	stateCacheMu.Lock()
	stateDirCached = map[string]cachedState{}
	stateCacheMu.Unlock()
	warnedMu.Lock()
	warned = map[string]bool{}
	warnedMu.Unlock()
}

func readConfiguredStateDir() string {
	home := projectHome()
	if home == "" {
		return stateDirDefault
	}
	return segmentFromConfig(TOMLPath(home))
}

// ReadStateSelection reads the persisted selection record for repoRoot. A missing
// record is (zero, false, nil). A record written by a newer schema is refused.
func ReadStateSelection(repoRoot string) (StateSelection, bool, error) {
	path := StateLocationRecordPath(repoRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StateSelection{}, false, nil
		}
		return StateSelection{}, false, fmt.Errorf("config: read state selection %s: %w", path, err)
	}

	var sel StateSelection
	if err := json.Unmarshal(raw, &sel); err != nil {
		return StateSelection{}, false, fmt.Errorf("config: parse state selection %s: %w", path, err)
	}
	if sel.SchemaVersion > stateSelectionSchemaVersion {
		return StateSelection{}, false, fmt.Errorf("config: state selection %s: schema version %d is newer than this binary reads (%d)", path, sel.SchemaVersion, stateSelectionSchemaVersion)
	}
	if sel.StateDir == "" {
		return StateSelection{}, false, fmt.Errorf("config: state selection %s: state_dir is empty", path)
	}
	return sel, true, nil
}

// PersistStateSelection durably records repoRoot's state directory and returns the
// bytes written, so a failed migration can withdraw exactly that record.
func PersistStateSelection(repoRoot, stateDir string) ([]byte, error) {
	if _, err := resolveStateDirValue(repoRoot, stateDir); err != nil {
		return nil, err
	}
	path := StateLocationRecordPath(repoRoot)
	data, err := json.Marshal(StateSelection{
		SchemaVersion: stateSelectionSchemaVersion,
		StateDir:      stateDir,
	})
	if err != nil {
		return nil, fmt.Errorf("config: marshal state selection: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileDurable(path, data); err != nil {
		return nil, err
	}
	invalidateStateCache()
	return data, nil
}

// WithdrawStateSelection removes the selection record only while its bytes still
// equal expected, so rollback never discards a newer choice.
func WithdrawStateSelection(repoRoot string, expected []byte) error {
	path := StateLocationRecordPath(repoRoot)
	current, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read state selection %s: %w", path, err)
	}
	if !bytes.Equal(current, expected) {
		return fmt.Errorf("%w: %s", ErrStateSelectionConflict, path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("config: remove state selection %s: %w", path, err)
	}
	invalidateStateCache()
	return syncDir(filepath.Dir(path))
}

// StateCandidate is one existing directory carrying Atomic repository-state
// signatures that adoption could record without moving files.
type StateCandidate struct {
	// Root is the candidate directory's absolute path.
	Root string
	// StateDir is the repository-relative path the record stores.
	StateDir string
}

// stateSignatureEntries prove a directory really holds Atomic repository state.
// worktrees is deliberately absent: Claude keeps linked worktrees in `.claude`.
var stateSignatureEntries = []string{
	"atomic.toml",
	".atomic-index",
	".scratchpad",
	"project",
	filepath.Join("rules", "wiki"),
}

// candidateDirNames returns the repository-local directory paths resolution may
// inspect, destination first, without duplicates.
func candidateDirNames() []string {
	names := []string{stateDirDefault, stateDirLegacy}
	if seg, ok := configuredStateDir(); ok {
		names = append([]string{seg}, names...)
	}
	seen := map[string]bool{}
	out := names[:0]
	for _, n := range names {
		if ValidateStateDirPath(n) != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func destinationName() string {
	seg, _ := configuredStateDir()
	return seg
}

// chooseCandidate: a populated destination answers and, when warn is set, names
// leftovers; else one populated candidate answers and two are ambiguous.
func chooseCandidate(repoRoot string, warn bool) (StateCandidate, StateSource, bool, error) {
	populated, err := DiscoverStateCandidates(repoRoot)
	if err != nil {
		return StateCandidate{}, "", false, err
	}
	dest := destinationName()
	var others []StateCandidate
	var destCand *StateCandidate
	for i, c := range populated {
		if c.StateDir == dest {
			destCand = &populated[i]
			continue
		}
		others = append(others, c)
	}
	if destCand != nil {
		if warn {
			for _, c := range others {
				warnLeftover(*destCand, c)
			}
		}
		if _, configured := configuredStateDir(); configured {
			return *destCand, StateSourceConfig, true, nil
		}
		return *destCand, StateSourceDefault, true, nil
	}
	switch len(others) {
	case 0:
		return StateCandidate{}, "", false, nil
	case 1:
		return others[0], StateSourceCandidate, true, nil
	default:
		return StateCandidate{}, "", false, ambiguousStateRootError(others)
	}
}

func ambiguousStateRootError(candidates []StateCandidate) error {
	roots := make([]string, 0, len(candidates))
	for _, c := range candidates {
		roots = append(roots, c.Root)
	}
	return fmt.Errorf("%w: found %s; run `atomic state adopt --dir <path>` to choose one",
		ErrAmbiguousStateRoot, strings.Join(roots, ", "))
}

func warnLeftover(dest, leftover StateCandidate) {
	warnOnce("leftover:"+leftover.Root, func() {
		fmt.Fprintf(stateWarnOut, "atomic: using %s; %s also holds Atomic state (%s). Merge or remove it.\n",
			dest.Root, leftover.Root, strings.Join(signatureEntriesIn(leftover.Root), ", "))
	})
}

func signatureEntriesIn(dir string) []string {
	var found []string
	for _, entry := range stateSignatureEntries {
		if _, err := os.Stat(filepath.Join(dir, entry)); err == nil {
			found = append(found, filepath.ToSlash(entry))
		}
	}
	return found
}

// SelectStateCandidate exposes the candidate rung to adoption, without the
// leftover warning.
func SelectStateCandidate(repoRoot string) (StateCandidate, bool, error) {
	chosen, _, ok, err := chooseCandidate(repoRoot, false)
	return chosen, ok, err
}

// DiscoverStateCandidates returns the repository-state candidates for repoRoot that
// carry Atomic state signatures. It creates nothing.
func DiscoverStateCandidates(repoRoot string) ([]StateCandidate, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("config: resolve %s: %w", repoRoot, err)
	}
	var out []StateCandidate
	for _, name := range candidateDirNames() {
		path := filepath.Join(abs, name)
		if !dirHasStateSignature(path) {
			continue
		}
		out = append(out, StateCandidate{Root: path, StateDir: name})
	}
	return out, nil
}

func dirHasStateSignature(dir string) bool {
	return len(signatureEntriesIn(dir)) > 0
}

// MutablePaths enumerates the ~/.atomic authorities and Atomic-owned roots. Each
// path has exactly one writer; see docs/spec/omp-plugin-compatibility.md.
type MutablePaths struct {
	Home        string
	ConfigTOML  string
	ProfileMD   string
	WikisMD     string
	InstallDir  string
	BackupsDir  string
	PackagesDir string
}

// MutablePathsFor resolves every mutable authority under home.
func MutablePathsFor(home string) MutablePaths {
	return MutablePaths{
		Home:        home,
		ConfigTOML:  TOMLPath(home),
		ProfileMD:   ProfilePath(home),
		WikisMD:     WikisPath(home),
		InstallDir:  InstallDir(home),
		BackupsDir:  BackupDir(home),
		PackagesDir: PackagesDir(home),
	}
}

// writeFileDurable writes data through a temp file in the destination directory,
// fsyncs the bytes, renames into place, then fsyncs the parent directory.
func writeFileDurable(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".state-location-*.json.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: rename to %s: %w", path, err)
	}
	return syncDir(dir)
}

// syncDir fsyncs a directory so a rename inside it survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("config: open %s for fsync: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("config: fsync %s: %w", dir, err)
	}
	return nil
}
