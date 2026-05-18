package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestClaudeHomeMissingWhenAbsent verifies ClaudeHomeMissing returns true when
// <home>/.claude does not exist — this is the short-circuit condition.
func TestClaudeHomeMissingWhenAbsent(t *testing.T) {
	home := t.TempDir()
	if !doctor.ClaudeHomeMissing(home) {
		t.Error("ClaudeHomeMissing = false, want true when ~/.claude absent")
	}
}

// TestClaudeHomeMissingWhenPresent verifies ClaudeHomeMissing returns false when
// <home>/.claude exists — no short-circuit, normal check flow applies.
func TestClaudeHomeMissingWhenPresent(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if doctor.ClaudeHomeMissing(home) {
		t.Error("ClaudeHomeMissing = true, want false when ~/.claude present")
	}
}
