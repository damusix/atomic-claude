package installstate

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// AppliedValue is the last-applied native value for a resource: where those
// bytes live, what kind of resource they are, and the digest that proves it.
type AppliedValue struct {
	Path   string           `json:"path"`
	Kind   managedfile.Kind `json:"kind"`
	Digest string           `json:"digest"`
}

// Row is one ownership record. One physical resource has one row even when
// several targets or unenrolled instances can discover it; visibility and
// consumer relations are separate fields, not separate rows.
type Row struct {
	Target     string       `json:"target"`
	Resource   string       `json:"resource"`
	Consumer   string       `json:"consumer"`
	Generation string       `json:"generation"`
	Tier       string       `json:"tier"`
	Applied    AppliedValue `json:"applied"`
}

// Ledger is the enrollment, resource, consumer, generation, tier, and
// last-applied authority at ~/.atomic/install/ledger.json.
type Ledger struct {
	Header
	Rows []Row `json:"rows"`
}

// LoadLedger reads the ledger at path. A missing ledger is an empty ledger
// stamped with this binary's header, not an error. A newer schema is refused
// before anything can mutate.
func LoadLedger(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Ledger{Header: NewHeader()}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("installstate: read ledger %s: %w", path, err)
	}
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("installstate: decode ledger %s: %w", path, err)
	}
	if err := l.Header.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// Save writes the ledger durably: atomic rename, fsynced bytes, fsynced
// directory. Ledger state is committed only after native content verifies, so
// this is the durability point recovery resumes from.
func (l *Ledger) Save(path string) error {
	l.Header = NewHeader()
	data, err := json.MarshalIndent(l, "", "    ")
	if err != nil {
		return fmt.Errorf("installstate: marshal ledger: %w", err)
	}
	data = append(data, '\n')
	if err := managedfile.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("installstate: write ledger %s: %w", path, err)
	}
	return nil
}

// Upsert inserts or replaces the row for a target and resource. It reports
// whether anything changed.
func (l *Ledger) Upsert(row Row) bool {
	for i := range l.Rows {
		if l.Rows[i].Target == row.Target && l.Rows[i].Resource == row.Resource {
			if l.Rows[i] == row {
				return false
			}
			l.Rows[i] = row
			return true
		}
	}
	l.Rows = append(l.Rows, row)
	return true
}

// Find returns the row owning resource for target.
func (l *Ledger) Find(target, resource string) (Row, bool) {
	for _, row := range l.Rows {
		if row.Target == target && row.Resource == resource {
			return row, true
		}
	}
	return Row{}, false
}
