package installstate

import (
	"errors"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// crashAtLedgerWrite publishes one block unit and runs Complete with a ledger
// write that dies, optionally after the ledger reached disk.
func crashAtLedgerWrite(t *testing.T, home string, ledgerLanded bool) *Transaction {
	t.Helper()
	tx := publishedBlock(t, home, "op-crash", "unit", "claude:default", nil, nil)
	tx.WriteLedgerFn = func(path string, l *Ledger) error {
		if ledgerLanded {
			if err := l.Save(path); err != nil {
				return err
			}
		}
		return errors.New("simulated crash at the ledger write")
	}
	if err := tx.Complete(); err == nil {
		t.Fatal("Complete survived the simulated crash")
	}
	return tx
}

// assertLedgerMatchesNative proves the ledger row and the native bytes describe
// the same content, which is what drift detection and a later uninstall trust.
func assertLedgerMatchesNative(t *testing.T, home string, tx *Transaction) {
	t.Helper()
	m := tx.Journal.Mutations[0]
	obs, err := managedfile.Observe(m.Path, m.Kind)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find(m.Target, m.Resource)
	if !ok {
		t.Fatalf("no ledger row for %s; native digest %s", m.Resource, obs.Digest)
	}
	if row.Applied.Digest != obs.Digest {
		t.Errorf("ledger digest %s disagrees with native digest %s", row.Applied.Digest, obs.Digest)
	}
}

func TestRollbackAfterCompleteCrashLeavesLedgerAgreeingWithNative(t *testing.T) {
	for _, landed := range []bool{true, false} {
		name := map[bool]string{true: "ledger landed", false: "ledger lost"}[landed]
		t.Run(name, func(t *testing.T) {
			home := newHome(t)
			tx := crashAtLedgerWrite(t, home, landed)
			if _, err := RollbackJournals(home); err != nil {
				t.Fatalf("RollbackJournals: %v", err)
			}
			assertLedgerMatchesNative(t, home, tx)
		})
	}
}

func TestRecoverAfterCompleteCrashRecordsCommittedRows(t *testing.T) {
	home := newHome(t)
	tx := crashAtLedgerWrite(t, home, false)
	if _, err := RecoverJournals(home); err != nil {
		t.Fatalf("RecoverJournals: %v", err)
	}
	assertLedgerMatchesNative(t, home, tx)
}
