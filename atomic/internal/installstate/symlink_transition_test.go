package installstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func TestSymlinkToFileTransactionRecovery(t *testing.T) {
	for _, phase := range []string{"before-write", "after-write", "rollback", "rollback-preview"} {
		t.Run(phase, func(t *testing.T) {
			home := newHome(t)
			dir := t.TempDir()
			canonical := filepath.Join(dir, "canonical.md")
			path := filepath.Join(dir, "card.md")
			writeBlockFile(t, canonical, "canonical card\n")
			if err := os.Symlink("canonical.md", path); err != nil {
				t.Fatal(err)
			}
			// The persisted intent identifies the exact owned link to replace.
			var m Mutation
			data, err := json.Marshal(map[string]any{
				"unit": "card", "resource": path, "path": path,
				"kind": managedfile.KindFile, "intended_digest": managedfile.Digest([]byte("projected copy\n")),
				"prior_symlink_digest": managedfile.Digest([]byte("canonical.md")),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			tx, err := NewTransaction(home, "symlink-transition", Plan{Mutations: []Mutation{m}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.StageFile("card", []byte("projected copy\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			stop := StepBackedUp
			if phase != "before-write" {
				stop = StepPublished
			}
			tx.OnStep = func(step Step, _ string) {
				if step == stop {
					panic("simulated crash")
				}
			}
			func() {
				defer func() {
					if recover() != "simulated crash" {
						t.Fatal("publication did not reach the crash point")
					}
				}()
				if err := tx.PublishFile("card"); err != nil {
					t.Fatal(err)
				}
			}()
			if phase == "rollback-preview" {
				before := homeFingerprint(t, home)
				sims, blocked, err := SimulateRollbacks(home)
				if err != nil || blocked || len(sims) != 1 || !hasDecisionIn(sims[0].Actions, DecisionRestoreBackup) {
					t.Fatalf("rollback preview = %+v, blocked=%v, %v", sims, blocked, err)
				}
				if after := homeFingerprint(t, home); after != before {
					t.Error("rollback preview changed lifecycle state")
				}
				fi, err := os.Lstat(path)
				if err != nil || !fi.Mode().IsRegular() {
					t.Fatalf("rollback preview replaced the copy: %v, %v", fi, err)
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != "projected copy\n" {
					t.Fatalf("rollback preview changed the copy: %q, %v", got, err)
				}
				return
			}
			journal, err := LoadJournal(tx.JournalPath)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := LoadLedger(tx.LedgerPath)
			if err != nil {
				t.Fatal(err)
			}
			recovery := NewRecovery(home, journal, ledger)
			var result RecoveryResult
			if phase == "rollback" {
				result, err = recovery.Rollback()
			} else {
				result, err = recovery.Recover()
			}
			if err != nil || len(result.Conflicts) != 0 {
				t.Fatalf("recovery = %+v, %v", result, err)
			}
			if got, err := os.ReadFile(canonical); err != nil || string(got) != "canonical card\n" {
				t.Fatalf("canonical card = %q, %v", got, err)
			}
			if phase == "after-write" {
				fi, err := os.Lstat(path)
				if err != nil || !fi.Mode().IsRegular() {
					t.Fatalf("published copy = %v, %v", fi, err)
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != "projected copy\n" {
					t.Fatalf("published copy = %q, %v", got, err)
				}
			} else if got, err := os.Readlink(path); err != nil || got != "canonical.md" {
				t.Fatalf("restored link = %q, %v", got, err)
			}
		})
	}
}
