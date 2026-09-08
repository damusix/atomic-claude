package gateway

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_Enroll_ReturnsUsableRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	rec, err := s.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if rec.Name != "web-api" {
		t.Fatalf("Name = %q, want %q", rec.Name, "web-api")
	}
	if len(rec.Key) != 32 {
		t.Fatalf("Key length = %d, want 32", len(rec.Key))
	}
	if rec.ID == "" {
		t.Fatalf("ID must not be empty")
	}
	if rec.EnrolledAt.IsZero() {
		t.Fatalf("EnrolledAt must not be zero")
	}
}

func TestStore_Enroll_PersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	if _, err := s.Enroll("web-api"); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("keys.json was not written: %v", err)
	}
}

func TestStore_Lookup_FindsEnrolledKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	rec, err := s.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	got, ok, err := s.Lookup(rec.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatalf("Lookup did not find the enrolled key")
	}
	if !bytes.Equal(got.Key, rec.Key) {
		t.Fatalf("Key mismatch: got %x want %x", got.Key, rec.Key)
	}
	if got.Name != "web-api" {
		t.Fatalf("Name = %q, want %q", got.Name, "web-api")
	}
}

func TestStore_Lookup_UnknownIDNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	_, ok, err := s.Lookup("does-not-exist")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatalf("Lookup found a record for an id that was never enrolled")
	}
}

func TestStore_Lookup_MissingFileIsNotFoundNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway", "keys.json")
	s := NewStore(path)

	_, ok, err := s.Lookup("anything")
	if err != nil {
		t.Fatalf("Lookup on a store with no file yet: %v", err)
	}
	if ok {
		t.Fatalf("Lookup found a record before any Enroll")
	}
}

func TestStore_Revoke_DeletesTheRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	rec, err := s.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if err := s.Revoke("web-api"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, ok, err := s.Lookup(rec.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatalf("Lookup still finds a revoked key")
	}
}

// Enroll and Revoke run as separate processes from the gateway's own lookup
// path — each holds its own Store over the same file — so a change one makes
// must reach the other's next Lookup without a restart.
func TestStore_Lookup_SeesAnotherProcessEnroll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	gatewayView := NewStore(path)
	enrollProcess := NewStore(path)

	if _, ok, _ := gatewayView.Lookup("anything"); ok {
		t.Fatalf("gatewayView found a record before any Enroll")
	}

	rec, err := enrollProcess.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	got, ok, err := gatewayView.Lookup(rec.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatalf("gatewayView did not see the other process's enroll")
	}
	if got.Name != "web-api" {
		t.Fatalf("Name = %q, want %q", got.Name, "web-api")
	}
}

func TestStore_Lookup_SeesAnotherProcessRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	enrollProcess := NewStore(path)
	gatewayView := NewStore(path)

	rec, err := enrollProcess.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if _, ok, _ := gatewayView.Lookup(rec.ID); !ok {
		t.Fatalf("gatewayView did not see the enroll")
	}

	if err := enrollProcess.Revoke("web-api"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, ok, err := gatewayView.Lookup(rec.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatalf("gatewayView still sees a key revoked by another process")
	}
}

// Two writes landing in the same filesystem mtime tick — an enroll
// immediately followed by a revoke, or two scripted CLI calls — must not
// leave the cache serving the pre-write state. Forcing an identical mtime
// on disk is what makes this deterministic rather than dependent on real
// OS timing gaps.
func TestStore_Lookup_DetectsChangeWithinSameMtimeTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	rec, err := s.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if _, ok, err := s.Lookup(rec.ID); err != nil || !ok {
		t.Fatalf("Lookup after enroll: ok=%v err=%v", ok, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mtime := info.ModTime()

	revoker := NewStore(path)
	if err := revoker.Revoke("web-api"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	_, ok, err := s.Lookup(rec.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatalf("Lookup served a revoked key after a same-mtime rewrite")
	}
}

// Enroll and Lookup must return a Record whose Key does not share a backing
// array with the cached copy — a caller zeroing the key after use, ordinary
// secret hygiene, would otherwise corrupt what Lookup serves next.
func TestStore_Lookup_KeyDoesNotAliasCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	rec, err := s.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	original := append([]byte(nil), rec.Key...)

	got, ok, err := s.Lookup(rec.ID)
	if err != nil || !ok {
		t.Fatalf("Lookup: ok=%v err=%v", ok, err)
	}
	for i := range got.Key {
		got.Key[i] = 0
	}

	again, ok, err := s.Lookup(rec.ID)
	if err != nil || !ok {
		t.Fatalf("Lookup: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(again.Key, original) {
		t.Fatalf("zeroing a returned Key corrupted the cached record: got %x, want %x", again.Key, original)
	}
}

func TestStore_Enroll_DifferentKeysGetDifferentIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s := NewStore(path)

	a, err := s.Enroll("machine-a")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	b, err := s.Enroll("machine-b")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("two enrollments produced the same key id")
	}
}
