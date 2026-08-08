package repl

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeta_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "work.meta.json")

	want := Meta{
		Name:      "work",
		Lang:      LangPython,
		Bin:       "/usr/bin/python3",
		PID:       4242,
		Socket:    filepath.Join(dir, "work.sock"),
		StartedAt: time.Now().Truncate(time.Second).UTC(),
		Root:      "/repo",
	}
	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadMeta(path)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v — the timeout escalation compares this against the live process", got.StartedAt, want.StartedAt)
	}
	got.StartedAt, want.StartedAt = time.Time{}, time.Time{}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestMetaSave_WritesAt0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "work.meta.json")

	if err := (Meta{Name: "work", PID: 1}).Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat meta: %v", err)
	}
	// Meta names the pid the timeout escalation signals; another user must not
	// be able to read it, and the temp-then-rename must not leave the house
	// 0644 behind.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("meta mode = %o, want 600", perm)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat meta dir: %v", err)
	}
	if perm := parent.Mode().Perm(); perm != 0o700 {
		t.Errorf("meta parent dir mode = %o, want 700", perm)
	}
}

func TestMetaSave_ReplacesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "work.meta.json")

	if err := (Meta{Name: "work", PID: 1}).Save(path); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := (Meta{Name: "work", PID: 2}).Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := LoadMeta(path)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.PID != 2 {
		t.Errorf("PID = %d, want 2 — a re-`start` must not leave the dead pid on disk", got.PID)
	}
	// The temp file the rename came from is not left as debris that `list`
	// would have to learn to ignore.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("session dir holds %v, want only the meta file", names)
	}
}

func TestLoadMeta_AbsentIsAnIsNotExistError(t *testing.T) {
	_, err := LoadMeta(filepath.Join(t.TempDir(), "gone.meta.json"))
	// The verbs distinguish "no session by that name" from a real read
	// failure by this, so the wrapping has to preserve it.
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadMeta on an absent file = %v, want an os.ErrNotExist error", err)
	}
}

func TestLoadMeta_CorruptFileNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.meta.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadMeta(path)
	if err == nil {
		t.Fatal("LoadMeta accepted a corrupt file")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Error("a corrupt meta file reports as absent; it must not read as a never-started session")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestMeta_CarriesNoEnvironment(t *testing.T) {
	// `list` and `status` render meta, and neither may ever echo an --env
	// value. The cheapest guarantee is that meta has nowhere to put one.
	raw, err := json.Marshal(Meta{Name: "work"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "env") {
		t.Errorf("meta wire form %s carries an env field; --env values must never reach disk", raw)
	}
}
