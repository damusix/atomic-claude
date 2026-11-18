package retro

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

func TestHistoryDir(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		enrolled    []string
		claude      bool
		unsupported string
	}{
		{"claude session reads claude history", map[string]string{"CLAUDECODE": "1"}, []string{"omp"}, true, ""},
		{"no harness keeps claude history", map[string]string{}, nil, true, ""},
		{"no harness with claude enrolled", map[string]string{}, []string{"claude", "omp"}, true, ""},
		{"no harness on an omp-only home", map[string]string{}, []string{"omp"}, false, "omp"},
		{"no harness on a codex-only home", map[string]string{}, []string{"codex"}, false, "codex"},
		{"omp has no reader", map[string]string{"AGENT": "1", "CI": "true"}, nil, false, "omp"},
		{"codex has no reader", map[string]string{"CODEX_THREAD_ID": "t"}, nil, false, "codex"},
	}
	t.Setenv(config.ClaudeConfigDirEnv, "")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			ledger := &installstate.Ledger{}
			for _, kind := range tt.enrolled {
				root := filepath.Join(home, kind)
				ledger.UpsertTarget(installstate.TargetRecord{Harness: kind, Instance: root, NativeRoot: root})
			}
			if err := ledger.Save(config.LedgerPath(home)); err != nil {
				t.Fatal(err)
			}
			dir, err := HistoryDir(home, harness.Detect(tt.env))
			if tt.claude {
				if want := filepath.Join(home, ".claude", "projects"); err != nil || dir != want {
					t.Fatalf("HistoryDir = %q, %v; want %q", dir, err, want)
				}
				return
			}
			var unsupported *UnsupportedError
			if !errors.As(err, &unsupported) || string(unsupported.Kind) != tt.unsupported {
				t.Fatalf("HistoryDir err = %v, want unsupported %s", err, tt.unsupported)
			}
			if !strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), tt.unsupported) {
				t.Errorf("message %q must say unsupported and name %s", err, tt.unsupported)
			}
		})
	}
}

// retro only reads, so a ledger it cannot read keeps the Claude default
// rather than failing the extract.
func TestHistoryDirFallsBackToClaudeOnAnUnreadableLedger(t *testing.T) {
	t.Setenv(config.ClaudeConfigDirEnv, "")
	home := t.TempDir()
	path := config.LedgerPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := HistoryDir(home, harness.Detect(nil))
	if want := filepath.Join(home, ".claude", "projects"); err != nil || dir != want {
		t.Fatalf("HistoryDir = %q, %v; want %q", dir, err, want)
	}
}

// Claude keeps its transcripts under CLAUDE_CONFIG_DIR when that is set.
func TestHistoryDirFollowsClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-claude")
	t.Setenv(config.ClaudeConfigDirEnv, root)
	dir, err := HistoryDir(home, harness.Detect(map[string]string{"CLAUDECODE": "1"}))
	if want := filepath.Join(root, "projects"); err != nil || dir != want {
		t.Fatalf("HistoryDir = %q, %v; want %q", dir, err, want)
	}
}

// Nested harnesses inherit each other's markers, so reading either history
// would be a guess.
func TestHistoryDirRefusesAmbiguousDetection(t *testing.T) {
	d := harness.Detect(map[string]string{"CLAUDECODE": "1", "CODEX_THREAD_ID": "t"})
	if _, err := HistoryDir(t.TempDir(), d); err == nil || !strings.Contains(err.Error(), "claude, codex") {
		t.Fatalf("HistoryDir err = %v, want an ambiguity refusal naming the candidates", err)
	}
}
