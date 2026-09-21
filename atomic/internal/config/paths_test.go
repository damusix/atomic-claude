package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathHelpers(t *testing.T) {
	home := "/home/user"

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Dir", Dir(home), filepath.Join(home, ".atomic")},
		{"TOMLPath", TOMLPath(home), filepath.Join(home, ".atomic", "config.toml")},
		{"BackupDir", BackupDir(home), filepath.Join(home, ".atomic", "backups")},
		{"ProposedCLAUDEMD", ProposedCLAUDEMD(home), filepath.Join(home, ".atomic", "proposed", "CLAUDE.md")},
		{"PreInstallDir", PreInstallDir(home), filepath.Join(home, ".atomic", "pre-install")},
		{"ProfilePath", ProfilePath(home), filepath.Join(home, ".atomic", "profile.md")},
		{"StatePath", StatePath(home), filepath.Join(home, ".atomic", "state.json")},
		{"InstallDir", InstallDir(home), filepath.Join(home, ".atomic", "install")},
		{"OperationLockPath", OperationLockPath(home), filepath.Join(home, ".atomic", "install", "operation.lock")},
		{"LedgerPath", LedgerPath(home), filepath.Join(home, ".atomic", "install", "ledger.json")},
		{"JournalsDir", JournalsDir(home), filepath.Join(home, ".atomic", "install", "journals")},
		{"JournalPath", JournalPath(home, "op-1"), filepath.Join(home, ".atomic", "install", "journals", "op-1.json")},
		{"TransactionsDir", TransactionsDir(home), filepath.Join(home, ".atomic", "install", "transactions")},
		{"TransactionDir", TransactionDir(home, "op-1"), filepath.Join(home, ".atomic", "install", "transactions", "op-1")},
		{"TransactionStageDir", TransactionStageDir(home, "op-1"), filepath.Join(home, ".atomic", "install", "transactions", "op-1", "stage")},
		{"TransactionBackupDir", TransactionBackupDir(home, "op-1"), filepath.Join(home, ".atomic", "install", "transactions", "op-1", "backup")},
		{"BackupStampDir", BackupStampDir(home, "20260916-120000"), filepath.Join(home, ".atomic", "backups", "20260916-120000")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestBackupStampsAndRemoval(t *testing.T) {
	home := t.TempDir()

	if stamps, err := BackupStamps(home); err != nil || len(stamps) != 0 {
		t.Fatalf("missing backups root: %v %v", stamps, err)
	}

	for _, stamp := range []string{"20260916-140000", "20260914-090000"} {
		dir := BackupStampDir(home, stamp)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(BackupDir(home), "notes.txt"), []byte("ignore\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stamps, err := BackupStamps(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(stamps) != 2 || stamps[0] != "20260914-090000" || stamps[1] != "20260916-140000" {
		t.Errorf("stamps = %v, want oldest-first", stamps)
	}

	if err := RemoveBackupStamp(home, "20260914-090000"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBackupStamp(home, "20260914-090000"); err != nil {
		t.Errorf("removing an absent stamp should be a no-op: %v", err)
	}
	if stamps, _ := BackupStamps(home); len(stamps) != 1 || stamps[0] != "20260916-140000" {
		t.Errorf("stamps after removal = %v", stamps)
	}
}
