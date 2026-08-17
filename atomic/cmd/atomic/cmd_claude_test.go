package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// Install must lay the bundle and register the session-start hook in one shot;
// requiring a separate `atomic hooks install` was an onboarding gap.
func TestRunClaudeInstallWiresHooks(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, scope, "install", false, false)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if len(result.Plan) == 0 {
		t.Fatal("expected non-empty install plan")
	}
	if !result.HooksInstalled {
		t.Errorf("expected HooksInstalled=true, got false; hookError=%v", result.HooksError)
	}

	installed, drifted, err := hooks.IsInstalled(scope)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed || drifted {
		t.Errorf("IsInstalled = (installed=%v, drifted=%v), want (true, false)", installed, drifted)
	}

	settingsPath := filepath.Join(scope, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("expected settings.json at %s: %v", settingsPath, err)
	}
}

// Users with their own hook config need the bundle without settings.json
// being touched.
func TestRunClaudeInstallNoHooksFlag(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, scope, "install", false, true)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false when noHooks=true")
	}

	installed, _, _ := hooks.IsInstalled(scope)
	if installed {
		t.Error("expected hook not registered when noHooks=true")
	}
}

// Dry-run is observation-only; touching settings.json would defeat it.
func TestRunClaudeInstallDryRunSkipsHooks(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, scope, "install", true, false)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false under dry-run")
	}

	installed, _, _ := hooks.IsInstalled(scope)
	if installed {
		t.Error("expected hook not registered under dry-run")
	}
}

// The primary guard against uninstall silently doing nothing.
func TestRunClaudeUninstall_MissingManifest(t *testing.T) {
	targetDir := t.TempDir()

	// /dev/null keeps TTY detection from stat-ing a nil file.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devNull.Close()

	_, err = runClaudeUninstall(targetDir, targetDir, devNull)
	if err == nil {
		t.Fatal("expected error when no pre-install manifest, got nil")
	}
	if !strings.Contains(err.Error(), "no pre-install snapshot") {
		t.Errorf("error %q does not mention 'no pre-install snapshot'", err.Error())
	}
}

// Three-way detection must surface user modifications, or uninstall silently
// clobbers post-install edits to settings.json or CLAUDE.md.
func TestRunClaudeUninstall_NeedsMerge(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-install: %v", err)
	}

	// settings.json is not in the bundle, so its embedded SHA is "".
	preInstallContent := []byte(`{"theme":"light"}`)
	preInstallSHA := sha256HexString(preInstallContent)

	if err := os.WriteFile(filepath.Join(preInstallDir, "settings.json"), preInstallContent, 0o644); err != nil {
		t.Fatalf("write pre-install settings.json: %v", err)
	}

	// Differs from both pre-install and embedded: the user modified it.
	onDiskContent := []byte(`{"theme":"dark","fontSize":14}`)
	if err := os.WriteFile(filepath.Join(targetDir, "settings.json"), onDiskContent, 0o644); err != nil {
		t.Fatalf("write on-disk settings.json: %v", err)
	}

	manifestJSON := `{
		"created": "2026-05-24T00:00:00Z",
		"atomic_version": "1.5.1",
		"files": [
			{"path": "settings.json", "sha256": "` + preInstallSHA + `", "existed": true}
		]
	}`
	if err := os.WriteFile(filepath.Join(preInstallDir, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devNull.Close()

	prompt, err := runClaudeUninstall(targetDir, targetDir, devNull)
	if err != nil {
		t.Fatalf("runClaudeUninstall: %v", err)
	}
	if !strings.Contains(prompt, "NEEDS MERGE") {
		t.Errorf("expected 'NEEDS MERGE' in prompt for user-modified file; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "settings.json") {
		t.Errorf("expected 'settings.json' in prompt; got:\n%s", prompt)
	}
}

// runProfile must pass home straight to profileAction: config.ProfilePath
// resolves <home>/.atomic/profile.md, so an extra ".claude" join writes to the
// wrong path. profileAction's own tests inject a tempdir as home, which is why
// they cannot catch this. runProfile calls os.Exit, so it runs in a subprocess
// with HOME redirected.
func TestRunProfile_UsesHomeNotClaudeHome(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_PROFILE_HELPER") == "1" {
		runProfile([]string{"refresh"})
		return
	}

	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestRunProfile_UsesHomeNotClaudeHome")
	// PATH is stripped so no detector runs: a real probe (bazel, say) writes its
	// cache into the temp HOME and races t.TempDir cleanup on CI.
	cmd.Env = append(os.Environ(), "ATOMIC_TEST_RUN_PROFILE_HELPER=1", "HOME="+home, "PATH=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subprocess runProfile failed: %v\n%s", err, out)
	}

	profilePath := filepath.Join(home, ".atomic", "profile.md")
	if _, err := os.Stat(profilePath); err != nil {
		t.Errorf("expected profile.md at %s (home, not home/.claude), stat err = %v", profilePath, err)
	}
	wrongPath := filepath.Join(home, ".claude", ".atomic", "profile.md")
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Errorf("profile.md incorrectly written under home/.claude/.atomic (%s); stat err = %v", wrongPath, err)
	}
}

func TestRunClaudeUninstall_ProducesPrompt(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-install: %v", err)
	}
	manifestJSON := `{
		"created": "2026-05-24T00:00:00Z",
		"atomic_version": "1.5.1",
		"files": [
			{"path": "CLAUDE.md", "sha256": "abc123", "existed": true},
			{"path": "agents/atomic-builder.md", "sha256": "", "existed": false}
		]
	}`
	if err := os.WriteFile(filepath.Join(preInstallDir, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devNull.Close()

	prompt, err := runClaudeUninstall(targetDir, targetDir, devNull)
	if err != nil {
		t.Fatalf("runClaudeUninstall: %v", err)
	}
	if prompt == "" {
		t.Fatal("expected non-empty prompt, got empty string")
	}
	if !strings.Contains(prompt, "## Atomic Claude Uninstall") {
		t.Errorf("prompt missing '## Atomic Claude Uninstall'")
	}
	if !strings.Contains(prompt, "atomic-builder.md") {
		t.Errorf("prompt missing 'atomic-builder.md'")
	}
	if !strings.Contains(prompt, "CLAUDE.md") {
		t.Errorf("prompt missing 'CLAUDE.md'")
	}
}
