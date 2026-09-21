// Package installstate owns the durable lifecycle state under ~/.atomic/install:
// the advisory operation lock with writer identity, the schema-versioned
// enrollment ledger, the per-operation journal, transaction staging and
// backups, digest-driven recovery, and the retention contract a full uninstall
// follows.
package installstate

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/version"
)

const (
	// SchemaVersion is the state schema this binary writes.
	SchemaVersion = 1
	// MinimumReader is the oldest reader schema that can read what this binary
	// writes. Raising it makes older binaries refuse the state outright instead
	// of writing a shape they only partly understand.
	MinimumReader = 1
)

// Header is the version stamp every ledger and journal carries.
type Header struct {
	SchemaVersion int    `json:"schema_version"`
	WriterVersion string `json:"writer_version"`
	MinimumReader int    `json:"minimum_reader_version"`
}

// NewHeader stamps state with this binary's schema and writer identity.
func NewHeader() Header {
	return Header{
		SchemaVersion: SchemaVersion,
		WriterVersion: version.Version,
		MinimumReader: MinimumReader,
	}
}

// Validate refuses state this reader cannot understand, in both directions:
// state written by a newer schema than this binary supports, and state that
// demands a reader newer than this binary.
func (h Header) Validate() error {
	if h.SchemaVersion == 0 {
		return fmt.Errorf("installstate: state carries no schema version")
	}
	if h.SchemaVersion > SchemaVersion {
		return fmt.Errorf("installstate: state schema %d is newer than supported schema %d", h.SchemaVersion, SchemaVersion)
	}
	if h.MinimumReader > SchemaVersion {
		return fmt.Errorf("installstate: state requires reader schema %d, this binary reads schema %d", h.MinimumReader, SchemaVersion)
	}
	return nil
}
