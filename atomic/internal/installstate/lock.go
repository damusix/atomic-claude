package installstate

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// WriterIdentity names the process holding the lifecycle lock, so a concurrent
// operator can tell an in-flight operation from an abandoned lock file.
type WriterIdentity struct {
	OperationID   string    `json:"operation_id"`
	WriterVersion string    `json:"writer_version"`
	PID           int       `json:"pid"`
	AcquiredAt    time.Time `json:"acquired_at"`
}

// Lock is a held exclusive advisory lock on ~/.atomic/install/operation.lock.
// The lock file is inert on disk: holding the flock, never the path's
// existence, is what serializes lifecycle operations.
type Lock struct {
	f    *os.File
	path string
}

// AcquireLock takes a blocking exclusive flock on the lifecycle lock and
// records identity inside it. Every call opens a fresh descriptor: POSIX flock
// locks belong to the open file description, not the path, so two callers
// racing for one path through separate opens genuinely contend rather than each
// locking its own private view.
func AcquireLock(home string, identity WriterIdentity) (*Lock, error) {
	if err := config.ValidateSegment("operation id", identity.OperationID); err != nil {
		return nil, err
	}
	path := config.OperationLockPath(home)
	dir := config.InstallDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("installstate: mkdir %s: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("installstate: open lock file %s: %w", path, err)
	}
	if err := flockExclusive(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("installstate: flock %s: %w", path, err)
	}

	if identity.PID == 0 {
		identity.PID = os.Getpid()
	}
	if identity.AcquiredAt.IsZero() {
		identity.AcquiredAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(identity, "", "    ")
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("installstate: marshal writer identity: %w", err)
	}
	data = append(data, '\n')
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, fmt.Errorf("installstate: truncate lock file: %w", err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("installstate: record writer identity: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, fmt.Errorf("installstate: sync lock file: %w", err)
	}
	return &Lock{f: f, path: path}, nil
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }

// Release drops the lock. The file itself remains; its existence carries no
// ownership meaning.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := flockUnlock(l.f)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

// ReadLockIdentity reads the writer identity currently recorded in the lock
// file. A lock file with no identity yet reports an empty identity, not an
// error.
func ReadLockIdentity(path string) (WriterIdentity, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return WriterIdentity{}, nil
	}
	if err != nil {
		return WriterIdentity{}, fmt.Errorf("installstate: read lock file %s: %w", path, err)
	}
	if len(data) == 0 {
		return WriterIdentity{}, nil
	}
	var identity WriterIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return WriterIdentity{}, fmt.Errorf("installstate: decode lock file %s: %w", path, err)
	}
	return identity, nil
}
