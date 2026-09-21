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

// UnitState is one step in a commit unit's progress. Progress is append-only,
// so an interruption between any two adjacent states is observable after the
// fact.
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
	// StateCommitted is a ledger row durably recorded. Completed generations
	// are never implicitly rolled back.
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
// to judge it later: the intended digest, the pre-mutation digest, and the
// transaction backup holding the displaced bytes.
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
	// RowKind, when set, is the ownership kind the ledger row records; Kind
	// governs how the staged bytes are published. The two differ only for a
	// relocation that publishes merged whole-file bytes while Atomic owns just
	// the managed block: the row must strip the block on removal, never delete
	// the user prose it moved.
	RowKind managedfile.Kind `json:"row_kind,omitempty"`
	// RowDigest, when set, is the digest the ledger row records instead of
	// Intended — the block digest for such a relocation.
	RowDigest string `json:"row_digest,omitempty"`
	Prior     string `json:"prior_digest,omitempty"`
	Backup    string `json:"backup,omitempty"`
	BackupSum string `json:"backup_digest,omitempty"`
	// PriorObserved records that the pre-mutation observation was journaled. The
	// journal is written before every native mutation, so a unit that carries no
	// such observation cannot have been published, which is what makes its
	// staging provably unused.
	PriorObserved bool `json:"prior_observed,omitempty"`
	// AppliedSum is the whole-resource digest of the bytes Atomic published: the
	// whole file for a file or block resource, the tree digest for a generated
	// tree. Rollback compares this, because a block digest cannot prove the rest
	// of the file is unchanged.
	AppliedSum string `json:"applied_digest,omitempty"`
}

// LedgerRow builds the ownership row this mutation commits once verified. A
// mutation that publishes whole-file bytes but owns only a managed block records
// its RowKind and RowDigest, so removal strips the block rather than deleting the
// file.
func (m Mutation) LedgerRow() Row {
	kind := m.Kind
	if m.RowKind != "" {
		kind = m.RowKind
	}
	digest := m.Intended
	if m.RowDigest != "" {
		digest = m.RowDigest
	}
	return Row{
		Target:     m.Target,
		Resource:   m.Resource,
		Consumer:   m.Consumer,
		Generation: m.Generation,
		Tier:       m.Tier,
		Applied:    AppliedValue{Path: m.Path, Kind: kind, Digest: digest},
	}
}

// Progress is one recorded state transition for a commit unit.
type Progress struct {
	Unit   string    `json:"unit"`
	State  UnitState `json:"state"`
	Digest string    `json:"digest,omitempty"`
	At     time.Time `json:"at"`
}

// Journal is one operation's authority: the observations it took, the mutations
// it intends, the backups it captured, the selections it depends on, and the
// progress of each commit unit. It is written and fsynced before any mutation.
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

// JournalPaths lists the journal files in dir, oldest-first by creation time
// with the file name breaking ties, so recovery always replays in a stable
// order. An unreadable journal is refused: recovery must never silently skip
// one that could own native bytes.
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

// journalEntry is one journal-directory entry: its path and the loaded journal,
// which is nil when the file could not be read. Recovery refuses that error
// because it cannot replay a journal it cannot parse; classification records
// the path as evidence instead, because an unreadable journal could still own
// native bytes.
type journalEntry struct {
	path   string
	loaded *Journal
	err    error
}

// journalEntries lists dir's journal files in replay order: oldest-first by
// creation time, with the path breaking ties. An entry that carries no readable
// creation time sorts before the readable ones, and the read error is returned
// on the entry rather than aborting the listing.
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
