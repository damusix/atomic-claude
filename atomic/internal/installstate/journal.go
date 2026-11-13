package installstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// UnitState is one step in a commit unit's progress. Progress is append-only, so
// an interruption between adjacent states is observable afterwards.
type UnitState string

const (
	// StatePlanned is a mutation recorded but not yet staged.
	StatePlanned UnitState = "planned"
	// StateStaged is a validated projection waiting to be published.
	StateStaged UnitState = "staged"
	// StateApplied is native mutation written but not yet verified.
	StateApplied UnitState = "applied"
	// StateVerified is effective content confirmed against the intended digest.
	StateVerified UnitState = "verified"
	// StateCommitted is journaled before Complete writes the ledger row, so recovery
	// writes a missing row. Completed generations are never implicitly rolled back.
	StateCommitted UnitState = "committed"
)

var stateRank = map[UnitState]int{
	StatePlanned:   0,
	StateStaged:    1,
	StateApplied:   2,
	StateVerified:  3,
	StateCommitted: 4,
}

// Mutation describes one intended native mutation and everything recovery needs
// to judge it later.
type Mutation struct {
	Unit       string           `json:"unit"`
	Resource   string           `json:"resource"`
	Target     string           `json:"target,omitempty"`
	Consumer   string           `json:"consumer,omitempty"`
	Generation string           `json:"generation,omitempty"`
	Tier       string           `json:"tier,omitempty"`
	Kind       managedfile.Kind `json:"kind"`
	Path       string           `json:"path"`
	Stage      string           `json:"stage,omitempty"`
	Intended   string           `json:"intended_digest"`
	Prior      string           `json:"prior_digest,omitempty"`
	Backup     string           `json:"backup,omitempty"`
	BackupSum  string           `json:"backup_digest,omitempty"`
	// PriorObserved records that the pre-mutation observation was journaled; a
	// unit without it was never published, so its staging is provably unused.
	PriorObserved bool `json:"prior_observed,omitempty"`
	// AppliedSum is the whole-resource digest Atomic published. Rollback compares
	// it, because a block digest cannot prove the rest of the file is unchanged.
	AppliedSum string `json:"applied_digest,omitempty"`
	// Consumers are the other targets sharing this physical resource, whose
	// rows the commit rewrites onto the same applied value.
	Consumers []string `json:"consumers,omitempty"`
}

// LedgerRow builds the ownership row this mutation commits once verified.
func (m Mutation) LedgerRow() Row {
	return Row{
		Target:     m.Target,
		Resource:   m.Resource,
		Consumer:   m.Consumer,
		Generation: m.Generation,
		Tier:       m.Tier,
		Applied:    AppliedValue{Path: m.Path, Kind: m.Kind, Digest: m.Intended},
	}
}

// Progress is one recorded state transition for a commit unit.
type Progress struct {
	Unit   string    `json:"unit"`
	State  UnitState `json:"state"`
	Digest string    `json:"digest,omitempty"`
	At     time.Time `json:"at"`
}

// Journal is one operation's authority, written and fsynced before any
// mutation.
type Journal struct {
	Header
	OperationID           string                     `json:"operation_id"`
	Created               time.Time                  `json:"created"`
	Updated               time.Time                  `json:"updated,omitempty"`
	Observations          []managedfile.Observation  `json:"observations"`
	Mutations             []Mutation                 `json:"mutations"`
	Backups               []managedfile.BackupRecord `json:"backups,omitempty"`
	SelectionDependencies []string                   `json:"selection_dependencies,omitempty"`
	Progress              []Progress                 `json:"progress,omitempty"`
	Completed             bool                       `json:"completed"`
	// Targets lets recovery restore the enrollment records a publication that
	// never reached its ledger commit still needs.
	Targets []TargetRecord `json:"targets,omitempty"`
	// Left, when LeftSnapshot is set, is the committed targets' whole
	// left-unowned set; an older journal has none and leaves the ledger's alone.
	Left         []LeftResource `json:"left,omitempty"`
	LeftSnapshot bool           `json:"left_snapshot,omitempty"`
}

// NewJournal builds an unresolved journal for one operation.
func NewJournal(operationID string, now time.Time) *Journal {
	return &Journal{
		Header:      NewHeader(),
		OperationID: operationID,
		Created:     now.UTC(),
	}
}

// Mutation returns the mutation for unit.
func (j *Journal) Mutation(unit string) (Mutation, bool) {
	for _, m := range j.Mutations {
		if m.Unit == unit {
			return m, true
		}
	}
	return Mutation{}, false
}

// UpdateMutation replaces the recorded mutation for unit.
func (j *Journal) UpdateMutation(m Mutation) {
	for i := range j.Mutations {
		if j.Mutations[i].Unit == m.Unit {
			j.Mutations[i] = m
			return
		}
	}
	j.Mutations = append(j.Mutations, m)
}

// Record appends a progress entry for a unit.
func (j *Journal) Record(unit string, state UnitState, digest string, at time.Time) {
	j.Progress = append(j.Progress, Progress{Unit: unit, State: state, Digest: digest, At: at.UTC()})
}

// Latest returns a unit's most advanced progress entry.
func (j *Journal) Latest(unit string) (Progress, bool) {
	var (
		found Progress
		ok    bool
	)
	for _, p := range j.Progress {
		if p.Unit != unit {
			continue
		}
		if !ok || stateRank[p.State] > stateRank[found.State] {
			found, ok = p, true
		}
	}
	return found, ok
}

// State returns a unit's most advanced state, defaulting to StatePlanned for a
// mutation that has recorded no progress.
func (j *Journal) State(unit string) UnitState {
	if p, ok := j.Latest(unit); ok {
		return p.State
	}
	return StatePlanned
}

// Backup returns the backup record captured for a unit's resource.
func (j *Journal) Backup(unit string) (managedfile.BackupRecord, bool) {
	m, ok := j.Mutation(unit)
	if !ok || m.Backup == "" {
		return managedfile.BackupRecord{}, false
	}
	for _, b := range j.Backups {
		if b.Path == m.Backup {
			return b, true
		}
	}
	return managedfile.BackupRecord{}, false
}

// WriteJournal writes the journal durably: atomic rename, fsynced bytes,
// fsynced parent directory.
func WriteJournal(path string, j *Journal) error {
	j.Header = NewHeader()
	j.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(j, "", "    ")
	if err != nil {
		return fmt.Errorf("installstate: marshal journal: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("installstate: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := managedfile.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("installstate: write journal %s: %w", path, err)
	}
	return nil
}

// LoadJournal reads one journal, refusing a newer unreadable schema.
func LoadJournal(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("installstate: read journal %s: %w", path, err)
	}
	var j Journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("installstate: decode journal %s: %w", path, err)
	}
	if err := j.Header.Validate(); err != nil {
		return nil, fmt.Errorf("%w (journal %s)", err, path)
	}
	return &j, nil
}

// JournalPaths lists the journal files in dir in stable replay order. An
// unreadable journal is refused, never skipped: it could own native bytes.
func JournalPaths(dir string) ([]string, error) {
	entries, err := journalEntries(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.err != nil {
			return nil, e.err
		}
		paths = append(paths, e.path)
	}
	return paths, nil
}

// journalEntry is one journal-directory entry; loaded is nil when the file could
// not be read, which recovery refuses and classification records as evidence.
type journalEntry struct {
	path   string
	loaded *Journal
	err    error
}

// journalEntries lists dir's journal files oldest-first, path breaking ties. An
// unreadable entry sorts first and carries its error instead of aborting.
func journalEntries(dir string) ([]journalEntry, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("installstate: read journals dir %s: %w", dir, err)
	}
	var found []journalEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		j, err := LoadJournal(path)
		found = append(found, journalEntry{path: path, loaded: j, err: err})
	}
	sort.Slice(found, func(i, k int) bool {
		ci, ck := journalCreated(found[i]), journalCreated(found[k])
		if !ci.Equal(ck) {
			return ci.Before(ck)
		}
		return found[i].path < found[k].path
	})
	return found, nil
}

// journalCreated returns an entry's creation time, zero for an unreadable one.
func journalCreated(e journalEntry) time.Time {
	if e.loaded == nil {
		return time.Time{}
	}
	return e.loaded.Created
}
