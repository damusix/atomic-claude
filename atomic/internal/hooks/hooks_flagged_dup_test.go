package hooks_test

import (
	"os"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

func TestInstallWithBothFormsLeavesOneFlaggedEntry(t *testing.T) {
	dir := t.TempDir()
	path := hooks.SettingsPathInDir(dir)
	entry := func(cmd string) string {
		return `{"matcher":".*","hooks":[{"type":"command","command":"` + cmd + `"}]}`
	}
	body := `{"hooks":{"SessionStart":[` + entry(unflaggedCmd) + `,` + entry(flaggedCmd) + `]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := hooks.InstallInDir(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if n := strings.Count(string(raw), "session-start"); n != 1 {
		t.Errorf("session-start entries = %d, want 1\n%s", n, raw)
	}
	if got := sessionStartCommandIn(t, raw); got != flaggedCmd {
		t.Errorf("command = %q, want %q", got, flaggedCmd)
	}
}
