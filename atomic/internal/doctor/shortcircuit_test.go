package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func TestClaudeHomeMissingWhenAbsent(t *testing.T) {
	home := t.TempDir()
	if !doctor.ClaudeHomeMissing(home) {
		t.Error("ClaudeHomeMissing = false, want true when ~/.claude absent")
	}
}

func TestClaudeHomeMissingWhenPresent(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if doctor.ClaudeHomeMissing(home) {
		t.Error("ClaudeHomeMissing = true, want false when ~/.claude present")
	}
}
