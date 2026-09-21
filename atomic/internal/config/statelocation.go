package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// stateDirDefault is the built-in repository-state segment — the historical
// `.claude` layout, kept as the last rung of the neutral ladder.
const stateDirDefault = ".claude"

// StateDirEnvVar is the process-only repository-state override. It never
// rewrites the persisted selection and is read before every other rung.
const StateDirEnvVar = "ATOMIC_STATE_DIR"

// ObsoleteHarnessEnvVar is the retired harness-selection variable. It is never
// treated as an alias for StateDirEnvVar; resolution refuses while it is set.
const ObsoleteHarnessEnvVar = "ATOMIC_HARNESS"

// ErrObsoleteHarnessEnv is returned when the retired harness variable is set.
var ErrObsoleteHarnessEnv = errors.New("config: obsolete harness environment")

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
	// StateSourceConfig is the user state.dir value (or legacy harness.dir evidence).
	StateSourceConfig StateSource = "config-default"
	// StateSourceDefault is the built-in `.claude` fallback.
	StateSourceDefault StateSource = "builtin-default"
)

// StateLocation is the resolved repository-state directory for one repository.
// Root is absolute when a rung named an absolute directory, otherwise joined
// onto the repository root.
type StateLocation struct {
	Root   string
	Source StateSource
}

// StateSelection is the persisted per-project selection record. It is shared by
// every worktree of one clone because the project key flattens the main
// checkout root.
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

// ValidateStateDirSegment rejects value unless it is a single safe path segment.
// It is the one enforcement point for the `state.dir` key and for a relative
// selection record.
func ValidateStateDirSegment(value string) error {
	return ValidateSegment("state.dir", value)
}

// resolveStateDirValue turns a recorded or configured value into a directory.
// An absolute value is used as-is; anything else must be one safe segment and is
// joined onto repoRoot (which may be empty for a repo-relative caller).
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
	if err := ValidateStateDirSegment(value); err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, value), nil
}

// CheckObsoleteHarnessEnv reports the retired-harness refusal, or nil. The root
// command calls it once so an obsolete ATOMIC_HARNESS surfaces with instructions
// before any verb runs, instead of the quiet repo-local resolver ignoring it.
func CheckObsoleteHarnessEnv() error {
	return refuseObsoleteHarnessEnv()
}

// refuseObsoleteHarnessEnv blocks resolution while the retired harness variable
// is set. It is not an alias for StateDirEnvVar: the user must migrate.
func refuseObsoleteHarnessEnv() error {
	raw := os.Getenv(ObsoleteHarnessEnvVar)
	if raw == "" {
		return nil
	}
	return fmt.Errorf("%w: %s=%q no longer selects the repository-state directory; unset it and re-run, or set %s=<dir> for a one-process override (or `atomic config set state.dir <segment>` to persist one)",
		ErrObsoleteHarnessEnv, ObsoleteHarnessEnvVar, raw, StateDirEnvVar)
}

// ResolveStateLocation walks the neutral ladder, most specific first:
//
//  1. the process-only ATOMIC_STATE_DIR override;
//  2. the persisted ~/.atomic/<project-key>/state-location.json selection;
//  3. the user state.dir default (legacy explicit harness.dir supplies evidence
//     while state.dir is unset);
//  4. the built-in `.claude` fallback.
//
// No rung consults a harness fingerprint. The retired ATOMIC_HARNESS variable is
// never an alias: while it is set (and no process override is given) resolution
// refuses with instructions.
func ResolveStateLocation(repoRoot string) (StateLocation, error) {
	if raw := os.Getenv(StateDirEnvVar); raw != "" {
		root, err := resolveStateDirValue(repoRoot, raw)
		if err != nil {
			return StateLocation{}, fmt.Errorf("config: %s: %w", StateDirEnvVar, err)
		}
		return StateLocation{Root: root, Source: StateSourceProcessEnv}, nil
	}

	if err := refuseObsoleteHarnessEnv(); err != nil {
		return StateLocation{}, err
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
	}

	if seg, ok := configuredStateDirSegment(); ok {
		return StateLocation{Root: filepath.Join(repoRoot, seg), Source: StateSourceConfig}, nil
	}

	return StateLocation{Root: filepath.Join(repoRoot, stateDirDefault), Source: StateSourceDefault}, nil
}

// repositoryStateDir is ResolveStateLocation's no-error form for repo-local path
// helpers that have no error channel. It honors the same rungs except that the
// retired harness variable is ignored rather than refused, and an unreadable or
// invalid value degrades to the next rung instead of aborting a path lookup.
// Results are cached per repository root for the process lifetime.
func repositoryStateDir(repoRoot string) string {
	if harnessDirOverride != nil {
		return filepath.Join(repoRoot, *harnessDirOverride)
	}
	stateCacheMu.Lock()
	defer stateCacheMu.Unlock()
	if cached, ok := stateDirCached[repoRoot]; ok {
		return cached
	}
	root := resolveRepositoryStateDir(repoRoot)
	stateDirCached[repoRoot] = root
	return root
}

var (
	stateCacheMu   sync.Mutex
	stateDirCached = map[string]string{}
)

// resolveRepositoryStateDir is the uncached quiet resolution. It walks the same
// ladder as ResolveStateLocation, plus one read-only rung between the record and
// the configured default: a repository with exactly one populated state
// candidate resolves to it in place, so state left under a former harness root
// (for example `.pi`) keeps being read instead of a fresh default directory
// sprouting beside it. Two populated candidates are ambiguous and fall through
// to the configured default; `atomic state adopt` is the explicit resolver.
func resolveRepositoryStateDir(repoRoot string) string {
	if raw := os.Getenv(StateDirEnvVar); raw != "" {
		if root, err := resolveStateDirValue(repoRoot, raw); err == nil {
			return root
		}
	}
	if repoRoot != "" {
		if sel, ok, err := ReadStateSelection(repoRoot); err == nil && ok {
			if root, err := resolveStateDirValue(repoRoot, sel.StateDir); err == nil {
				return root
			}
		}
		if cands, err := DiscoverStateCandidates(repoRoot); err == nil && len(cands) == 1 {
			return cands[0].Root
		}
	}
	if seg, ok := configuredStateDirSegment(); ok {
		return filepath.Join(repoRoot, seg)
	}
	return filepath.Join(repoRoot, stateDirDefault)
}

// resolvedSegment caches the process-global configured segment (state.dir, else
// legacy harness.dir evidence, else the built-in default). Env is never a rung.
var (
	segmentMu     sync.Mutex
	segmentCached *string
)

// configuredStateDirSegment returns the configured segment and whether the user
// configured one at all. An unset state.dir with a non-default legacy harness.dir
// still counts as configured — that value is the migration evidence.
func configuredStateDirSegment() (string, bool) {
	segmentMu.Lock()
	defer segmentMu.Unlock()
	if segmentCached == nil {
		seg := readConfiguredStateDirSegment()
		segmentCached = &seg
	}
	if *segmentCached == stateDirDefault {
		return stateDirDefault, false
	}
	return *segmentCached, true
}

// resetStateResolutionCacheForTest clears both process caches so a test can
// change config.toml or a record and observe the new resolution.
func resetStateResolutionCacheForTest() {
	segmentMu.Lock()
	segmentCached = nil
	segmentMu.Unlock()
	stateCacheMu.Lock()
	stateDirCached = map[string]string{}
	stateCacheMu.Unlock()
}

func readConfiguredStateDirSegment() string {
	home := projectHome()
	if home == "" {
		return stateDirDefault
	}
	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		return stateDirDefault
	}
	if cfg.State.Dir != "" && ValidateStateDirSegment(cfg.State.Dir) == nil {
		return cfg.State.Dir
	}
	// A legacy harness.dir is evidence only while it names something other than
	// the built-in default — Load backfills an absent value to the default, so a
	// non-default value is necessarily explicit.
	if cfg.Harness.Dir != "" && cfg.Harness.Dir != harnessDirDefault && validateHarnessDir(cfg.Harness.Dir) == nil {
		return cfg.Harness.Dir
	}
	return stateDirDefault
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

// PersistStateSelection records repoRoot's state directory durably — the file
// bytes and the parent directory are fsynced before the caller switches
// resolution. The exact bytes written are returned so a failed migration can
// withdraw the record later, but only while nothing else has rewritten it.
//
// An adoption run that journals this write also records StateLocationRecordPath
// in the journal's selection dependencies, so recovery retention keeps the
// record while an unresolved journal still references it.
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
	return data, nil
}

// WithdrawStateSelection removes the selection record only while its bytes still
// equal expected — the ones PersistStateSelection returned. Any later write
// makes this a conflict, so rollback can never discard a newer choice.
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
	return syncDir(filepath.Dir(path))
}

// StateCandidate is one existing directory carrying Atomic repository-state
// signatures that adoption could record without moving files.
type StateCandidate struct {
	// Root is the candidate directory's absolute path.
	Root string
	// StateDir is what the record stores: the segment when the root sits
	// directly under the repository, else the absolute path.
	StateDir string
}

// stateSignatureEntries prove a directory really holds Atomic repository state.
// Harness-native files (settings, agents, skills) are deliberately absent: an
// empty or harness-only `.claude` or `.pi` is not a candidate.
var stateSignatureEntries = []string{
	"atomic.toml",
	".atomic-index",
	".scratchpad",
	"project",
	"worktrees",
}

// candidateDirNames returns the repository-local directory names adoption may
// inspect, most specific first, without duplicates.
func candidateDirNames() []string {
	names := []string{stateDirDefault, ".pi"}
	if seg, ok := configuredStateDirSegment(); ok {
		names = append([]string{seg}, names...)
	}
	seen := map[string]bool{}
	out := names[:0]
	for _, n := range names {
		if ValidateStateDirSegment(n) != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// DiscoverStateCandidates returns the populated repository-state candidates for
// repoRoot. It is read-only: empty directories and directories without Atomic
// state signatures are ignored, and nothing is created.
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
	for _, entry := range stateSignatureEntries {
		if _, err := os.Stat(filepath.Join(dir, entry)); err == nil {
			return true
		}
	}
	return false
}

// AdoptStateRoot adopts one unambiguous populated state root in place, without
// moving files, and persists the selection before returning. An empty repository
// has no candidate: nothing is written, and the neutral ladder's current
// resolution (configured segment or built-in default) is returned. Two or more
// populated candidates are ambiguous and refuse until the user runs
// `atomic state adopt`. The retired harness variable blocks the operation.
func AdoptStateRoot(repoRoot string) (StateLocation, error) {
	if err := refuseObsoleteHarnessEnv(); err != nil {
		return StateLocation{}, err
	}
	candidates, err := DiscoverStateCandidates(repoRoot)
	if err != nil {
		return StateLocation{}, err
	}
	switch len(candidates) {
	case 0:
		// Nothing to adopt: return what the ladder would resolve right now, so a
		// state write at the returned root can never land somewhere other than
		// the directory readers resolve to. Reached before the record rung in the
		// common empty-repo case, it honors a configured state.dir segment (or
		// legacy harness.dir evidence) instead of assuming `.claude`.
		return ResolveStateLocation(repoRoot)
	case 1:
		chosen := candidates[0]
		if _, err := PersistStateSelection(repoRoot, chosen.StateDir); err != nil {
			return StateLocation{}, err
		}
		return StateLocation{Root: chosen.Root, Source: StateSourceRecord}, nil
	default:
		roots := make([]string, 0, len(candidates))
		for _, c := range candidates {
			roots = append(roots, c.Root)
		}
		return StateLocation{}, fmt.Errorf("%w: found %s; run `atomic state adopt` to choose one",
			ErrAmbiguousStateRoot, strings.Join(roots, ", "))
	}
}

// MutablePaths enumerates the ~/.atomic authorities and Atomic-owned roots. One
// path has exactly one writer:
//
//	ConfigTOML  user configuration          — `atomic config` and lifecycle settings writes
//	ProfileMD   mutable user authority      — profile refresh and explicit edits
//	WikisMD     mutable registry authority  — wiki registration/removal, then projected
//	InstallDir  lifecycle authority         — lock, ledger, journals, transactions
//	BackupsDir  user recovery history       — pre-mutation backups, preserved on full uninstall
//	PackagesDir generated harness packages  — the owning adapter
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
