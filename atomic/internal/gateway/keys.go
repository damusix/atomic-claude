package gateway

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

// Record is one enrolled machine: its key, the name it was enrolled under,
// and when.
type Record struct {
	ID         string    `json:"id"`
	Key        []byte    `json:"key"`
	Name       string    `json:"name"`
	EnrolledAt time.Time `json:"enrolled_at"`
}

// Store is keys.json: key id to key, name and enrollment time. There are no
// roles here — a key holder may do anything a local process can, minus
// shutdown.
//
// Enroll and Revoke run as the CLI, in a separate process from the gateway
// holding this Store for Lookup, so Lookup re-reads the file whenever its
// mtime or size has moved rather than caching forever. Size guards a case
// mtime alone misses: two writes landing in the same mtime tick (an enroll
// immediately followed by a revoke) still change the file's length. That is
// what lets enrollment and revocation take effect without a gateway restart
// and without a filesystem watcher.
type Store struct {
	path string

	mu      sync.Mutex
	mtime   time.Time
	size    int64
	loaded  bool
	records map[string]Record // by id
}

// NewStore returns a Store backed by the keys.json at path. The file need
// not exist yet; it is created on the first Enroll.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Enroll generates a 32-byte key from crypto/rand, derives its id, persists
// the record, and returns it so the caller can print a [bus.remotes] TOML
// block once.
func (s *Store) Enroll(name string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.reload(); err != nil {
		return Record{}, err
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return Record{}, fmt.Errorf("gateway: generate key: %w", err)
	}

	rec := Record{
		ID:         keyID(key),
		Key:        key,
		Name:       name,
		EnrolledAt: time.Now(),
	}
	s.records[rec.ID] = rec

	if err := s.save(); err != nil {
		return Record{}, err
	}
	return cloneRecord(rec), nil
}

// Revoke deletes every record enrolled under name.
func (s *Store) Revoke(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.reload(); err != nil {
		return err
	}
	for id, rec := range s.records {
		if rec.Name == name {
			delete(s.records, id)
		}
	}
	return s.save()
}

// Lookup returns the record for keyID, re-reading keys.json first if it has
// changed since the last read.
func (s *Store) Lookup(keyID string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.reload(); err != nil {
		return Record{}, false, err
	}
	rec, ok := s.records[keyID]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

// cloneRecord copies Key onto a new backing array so a caller zeroing the
// returned key after use — ordinary secret hygiene — cannot corrupt the
// cached copy Enroll or reload populated.
func cloneRecord(r Record) Record {
	r.Key = append([]byte(nil), r.Key...)
	return r
}

// reload re-reads keys.json when its mtime has advanced or its size has
// changed since what was last seen, or on first use. A missing file is an
// empty store, not an error — Enroll creates it.
func (s *Store) reload() error {
	info, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		if !s.loaded {
			s.records = map[string]Record{}
			s.loaded = true
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("gateway: stat keys.json: %w", err)
	}
	if s.loaded && !info.ModTime().After(s.mtime) && info.Size() == s.size {
		return nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("gateway: read keys.json: %w", err)
	}
	records := map[string]Record{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &records); err != nil {
			return fmt.Errorf("gateway: parse keys.json: %w", err)
		}
	}
	s.records = records
	s.mtime = info.ModTime()
	s.size = info.Size()
	s.loaded = true
	return nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("gateway: create keys.json directory: %w", err)
	}
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return fmt.Errorf("gateway: encode keys.json: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("gateway: write keys.json: %w", err)
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return fmt.Errorf("gateway: stat keys.json: %w", err)
	}
	s.mtime = info.ModTime()
	s.size = info.Size()
	return nil
}

func keyID(key []byte) string {
	return string(remote.KeyID(key))
}
