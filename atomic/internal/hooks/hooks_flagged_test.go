package hooks_test

import (
	"os"
	"path/filepath"
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

func TestRemoveOwnedMembersPreservesSharedSessionStartHandlers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command func(string) string
		adopt   bool
	}{
		{"adopted", func(string) string { return unflaggedCmd }, true},
		{"current", func(string) string { return flaggedCmd }, false},
		{"unflagged", func(string) string { return unflaggedCmd }, false},
		{"legacy script", func(dir string) string { return filepath.Join(dir, "hooks", "session-start-reminders.sh") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := hooks.SettingsPathInDir(dir)
			command := tc.command(dir)
			unrelated := `"permissions" : { "allow" : [ "Read", "Bash" ] }`
			userHandler := `{"type":"command","command":"my-notify.sh"}`
			body := "{\n  // user settings\n  " + unrelated + ",\n  \"hooks\": {\"SessionStart\": [{\"matcher\":\".*\",\"hooks\":[" +
				`{"type":"command","command":"` + command + `"},` + userHandler + `]}]}` + "\n}\n"
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.adopt {
				if skipped, err := hooks.InstallInDir(dir); err != nil || skipped {
					t.Fatalf("adopt: skipped=%v err=%v", skipped, err)
				}
			}
			if skipped, err := hooks.RemoveOwnedMembers(path); err != nil || skipped {
				t.Fatalf("remove: skipped=%v err=%v", skipped, err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := string(raw)
			for _, want := range []string{"// user settings", unrelated, userHandler, `"matcher":".*"`} {
				if !strings.Contains(got, want) {
					t.Errorf("missing preserved bytes %q in %s", want, got)
				}
			}
			for _, removed := range []string{flaggedCmd, unflaggedCmd, filepath.Join(dir, "hooks", "session-start-reminders.sh")} {
				if strings.Contains(got, removed) {
					t.Errorf("Atomic command %q survived in %s", removed, got)
				}
			}
		})
	}
}
