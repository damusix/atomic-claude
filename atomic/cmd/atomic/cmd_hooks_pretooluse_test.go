package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// A panic in the card lookup must not surface as a nonzero exit: Codex would
// treat the failed hook as a failed edit.
func TestPreToolUseVerbSwallowsAPanic(t *testing.T) {
	defer func(orig func(io.Reader, string, time.Time) string) { preToolUse = orig }(preToolUse)
	preToolUse = func(io.Reader, string, time.Time) string { panic("card lookup blew up") }
	runPreToolUse(strings.NewReader("{}"))
}

// Codex runs the hook before every apply_patch under the session's own
// environment, so a bad state override must not fail the edit either.
func TestPreToolUseVerbIgnoresInvalidStateDirEnv(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	payload, err := json.Marshal(map[string]any{
		"session_id": "s1",
		"cwd":        repo,
		"tool_input": map[string]string{"command": "*** Begin Patch\n*** Update File: main.go\n*** End Patch\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	inPath := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(inPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{config.StateDirEnvVar + "=/", "ATOMIC_TEST_CLI_IN=" + inPath, "PATH=" + os.Getenv("PATH")}

	out, diag, code := runAtomicCLIFull(t, home, env, "root", "hooks", "pre-tool-use")
	if code != 0 || out != "" || strings.Contains(diag, config.StateDirEnvVar) {
		t.Fatalf("exit = %d, stdout %q, diagnostics %q; want a silent exit 0", code, out, diag)
	}
}

func TestSessionStartVerbRefusesInvalidStateDirEnv(t *testing.T) {
	env := []string{config.StateDirEnvVar + "=/"}
	_, diag, code := runAtomicCLIFull(t, t.TempDir(), env, "root", "hooks", "session-start")
	if code == 0 || !strings.Contains(diag, config.StateDirEnvVar) {
		t.Fatalf("exit = %d, diagnostics %q; want a non-zero %s refusal", code, diag, config.StateDirEnvVar)
	}
}
