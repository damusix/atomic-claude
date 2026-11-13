package installstate

import (
	"encoding/json"
	"fmt"
	"io"
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

// Lock is a held exclusive flock on ~/.atomic/install/operation.lock. Holding
// the flock, never the file's existence, serializes lifecycle operations.
type Lock struct {
	f    *os.File
	path string
}

// AcquireLock takes the lifecycle flock through a fresh descriptor, naming any
// holder on stderr before it waits, and records identity inside the lock file.
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
	held, err := flockTryExclusive(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("installstate: flock %s: %w", path, err)
	}
	if held {
		fmt.Fprintln(lockWaitNotice, waitNotice(path))
		if err := flockExclusive(f); err != nil {
			f.Close()
			return nil, fmt.Errorf("installstate: flock %s: %w", path, err)
		}
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

var lockWaitNotice io.Writer = os.Stderr

func waitNotice(path string) string {
	holder, err := ReadLockIdentity(path)
	if err != nil || holder.OperationID == "" {
		return fmt.Sprintf("atomic: waiting for another Atomic operation to release %s", path)
	}
	return fmt.Sprintf("atomic: waiting on %s; last recorded holder: operation %s (pid %d, atomic %s, since %s)",
		path,
		holder.OperationID, holder.PID, holder.WriterVersion, holder.AcquiredAt.Format(time.RFC3339))
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

// ReadLockIdentity reads the writer identity recorded in the lock file. A file
// with no identity yet reports an empty identity, not an error.
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
