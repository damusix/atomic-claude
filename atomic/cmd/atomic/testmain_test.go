package main

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// TestMain sandboxes every test in this package under a temp $HOME. Several
// tests resolve project-keyed state (reports, archive) via
// config.ProjectStateDir, which defaults to the real ~/.atomic — without this
// a `go test ./...` run pollutes the operator's actual home directory with
// one stray var-folders-...-Test<name>-NNN entry per such test.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "atomic-cmd-test-home")
	if err != nil {
		panic(err)
	}
	// The CLI helper child re-runs TestMain with the roots a test injected.
	if os.Getenv("ATOMIC_TEST_CLI_ARGS") == "" {
		for _, name := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "OMP_PROFILE", config.StateDirEnvVar} {
			os.Unsetenv(name)
		}
		for _, name := range harness.MarkerVars {
			os.Unsetenv(name)
		}
	}
	// profile.Refresh reads these via WorkflowProbe.env; no seam reaches the
	// cobra commands, so their tests keep the default tool registry.
	for _, name := range profile.WorkflowEnvVars() {
		os.Unsetenv(name)
	}
	restore := config.SetHomeDirForTest(home)
	// Tests run inside this repository; install must not converge its cards.
	installRepoRoot = func() (string, bool) { return "", false }
	code := m.Run()
	restore()
	os.RemoveAll(home)
	os.Exit(code)
}
