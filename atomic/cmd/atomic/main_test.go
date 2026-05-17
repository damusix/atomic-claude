package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanNoUpdateCheck(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantFound bool
		wantArgs  []string
	}{
		{
			name:      "flag before subcommand",
			argv:      []string{"atomic", "--no-update-check", "signals", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag after subcommand",
			argv:      []string{"atomic", "signals", "scan", "--no-update-check"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag equals true",
			argv:      []string{"atomic", "--no-update-check=true", "signals", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag equals false strips token but returns false",
			argv:      []string{"atomic", "--no-update-check=false", "signals", "scan"},
			wantFound: false,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag absent",
			argv:      []string{"atomic", "signals", "scan"},
			wantFound: false,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag between subcommand and sub-verb",
			argv:      []string{"atomic", "signals", "--no-update-check", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, cleaned := scanNoUpdateCheck(tc.argv)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if len(cleaned) != len(tc.wantArgs) {
				t.Errorf("cleaned = %v, want %v", cleaned, tc.wantArgs)
				return
			}
			for i, a := range cleaned {
				if a != tc.wantArgs[i] {
					t.Errorf("cleaned[%d] = %q, want %q", i, a, tc.wantArgs[i])
				}
			}
		})
	}
}

// hookScriptName mirrors hooks.scriptName for assertions.
// If the constant moves, this test fails loudly — that's intended.
const hookScriptName = "session-start-reminders.sh"

// TestRunClaudeInstallWiresHooks proves that `atomic claude install` lays the
// bundle AND registers the session-start hook in one shot. Encodes the WHY:
// the previous flow required users to chain `atomic hooks install` separately,
// which was undocumented in the curl|bash output and a real onboarding gap.
func TestRunClaudeInstallWiresHooks(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, "install", false, false)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if len(result.Plan) == 0 {
		t.Fatal("expected non-empty install plan")
	}
	if !result.HooksInstalled {
		t.Errorf("expected HooksInstalled=true, got false; hookError=%v", result.HooksError)
	}

	scriptPath := filepath.Join(scope, ".claude", "hooks", hookScriptName)
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("expected hook script at %s: %v", scriptPath, err)
	}

	settingsPath := filepath.Join(scope, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("expected settings.json at %s: %v", settingsPath, err)
	}
}

// TestRunClaudeInstallNoHooksFlag verifies the opt-out path. Users with their
// own hook config need a way to install the bundle without atomic touching
// settings.json.
func TestRunClaudeInstallNoHooksFlag(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, "install", false, true)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false when noHooks=true")
	}

	scriptPath := filepath.Join(scope, ".claude", "hooks", hookScriptName)
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Errorf("expected no hook script at %s, got err=%v", scriptPath, err)
	}
}

// TestRunClaudeInstallDryRunSkipsHooks dry-run must be observation-only;
// touching settings.json under dry-run would defeat its purpose.
func TestRunClaudeInstallDryRunSkipsHooks(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, "install", true, false)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false under dry-run")
	}

	scriptPath := filepath.Join(scope, ".claude", "hooks", hookScriptName)
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Errorf("expected no hook script under dry-run, got err=%v", err)
	}
}
