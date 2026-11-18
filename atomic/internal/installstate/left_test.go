package installstate

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestSetLeftReplacesTheTargetsSetInOneDir(t *testing.T) {
	dir := filepath.Join("root", "agents")
	other := filepath.Join("root", "rules")
	l := &Ledger{Left: []LeftResource{
		{Target: "omp:a", Path: filepath.Join(dir, "old.md")},
		{Target: "omp:a", Path: filepath.Join(other, "keep.md")},
		{Target: "omp:b", Path: filepath.Join(dir, "keep.md")},
	}}

	if !l.SetLeft("omp:a", dir, []string{filepath.Join(dir, "new.md")}) {
		t.Fatal("SetLeft reported no change for a different set")
	}
	if got := l.LeftIn("omp:a", dir); !slices.Equal(got, []string{filepath.Join(dir, "new.md")}) {
		t.Errorf("LeftIn = %v, want only the new path", got)
	}
	if !l.IsLeft("omp:a", filepath.Join(other, "keep.md")) || !l.IsLeft("omp:b", filepath.Join(dir, "keep.md")) {
		t.Errorf("SetLeft touched another dir or target: %+v", l.Left)
	}
	if l.SetLeft("omp:a", dir, []string{filepath.Join(dir, "new.md")}) {
		t.Error("SetLeft reported a change for the same set")
	}
	if !l.SetLeft("omp:a", dir, nil) || l.LeftIn("omp:a", dir) != nil {
		t.Errorf("an empty set did not clear the dir: %+v", l.Left)
	}
	if l.IsLeft("omp:a", filepath.Join(dir, "new.md")) {
		t.Error("IsLeft still reports a cleared path")
	}
}

func TestLeftRoundTripsAndIsOmittedWhenEmpty(t *testing.T) {
	empty, err := json.Marshal(Ledger{Header: NewHeader()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(empty), `"left"`) {
		t.Errorf("an empty left set was serialized: %s", empty)
	}

	path := filepath.Join(t.TempDir(), "ledger.json")
	want := []LeftResource{{Target: "codex:x", Path: "/x/agents/a.toml"}}
	if err := (&Ledger{Left: want}).Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Left, want) {
		t.Errorf("left = %+v, want %+v", got.Left, want)
	}
}

func TestCleanupForgetsWhatARemovedTargetLeft(t *testing.T) {
	home := newHome(t)
	gone := TargetRecord{Harness: "omp", Instance: "gone", NativeRoot: filepath.Join(home, ".omp", "agent"), Status: "enrolled"}
	ledger := &Ledger{
		Header:  NewHeader(),
		Targets: []TargetRecord{gone},
		Left:    []LeftResource{{Target: gone.Key(), Path: filepath.Join(gone.NativeRoot, "agents", "a.md")}},
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	if _, err := Cleanup(home, ledger, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Targets) != 0 || len(saved.Left) != 0 {
		t.Errorf("after cleanup targets %+v left %+v; a removed target's leave decision must go with it", saved.Targets, saved.Left)
	}
}
