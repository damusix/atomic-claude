package hooks_test

import (
	"os"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

const (
	unflaggedCmd = "atomic hooks session-start"
	flaggedCmd   = "atomic hooks session-start --harness claude"
)

func settingsWithCommand(t *testing.T, cmd string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = hooks.SettingsPathInDir(dir)
	body := `{"hooks":{"SessionStart":[{"matcher":".*","hooks":[{"type":"command","command":"` + cmd + `"}]}]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestInstallRewritesUnflaggedEntryInPlace(t *testing.T) {
	dir, path := settingsWithCommand(t, unflaggedCmd)
	if _, err := hooks.InstallInDir(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if got := sessionStartCommandIn(t, raw); got != flaggedCmd {
		t.Errorf("command = %q, want %q", got, flaggedCmd)
	}
	if n := strings.Count(string(raw), "atomic hooks session-start"); n != 1 {
		t.Errorf("registrations = %d, want 1 (no duplicate)\n%s", n, raw)
	}
}

func TestOwnershipRecognizesBothForms(t *testing.T) {
	for _, cmd := range []string{unflaggedCmd, flaggedCmd} {
		dir, _ := settingsWithCommand(t, cmd)
		obs, err := hooks.ObserveSettingsOwnedInDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Digest == "" || obs.Conflict != "" {
			t.Errorf("%q: digest=%q conflict=%q, want owned and conflict-free", cmd, obs.Digest, obs.Conflict)
		}
	}
}

func TestIsInstalledRecognizesBothForms(t *testing.T) {
	for _, cmd := range []string{unflaggedCmd, flaggedCmd} {
		dir, _ := settingsWithCommand(t, cmd)
		installed, drifted, err := hooks.IsInstalledInDir(dir)
		if err != nil || !installed || drifted {
			t.Errorf("%q: installed=%v drifted=%v err=%v", cmd, installed, drifted, err)
		}
	}
}

func TestUninstallRemovesEitherForm(t *testing.T) {
	for _, cmd := range []string{unflaggedCmd, flaggedCmd} {
		dir, path := settingsWithCommand(t, cmd)
		if _, err := hooks.UninstallInDir(dir); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "session-start") {
			t.Errorf("%q survived uninstall: %s", cmd, raw)
		}
	}
}
