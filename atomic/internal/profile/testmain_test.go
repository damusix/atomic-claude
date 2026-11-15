package profile

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// WorkflowProbe.env, DetectWorkflow's default envSet, and resolveHome read the
// process environment; a zero-probe Refresh must see no real login.
func TestMain(m *testing.M) {
	cleanup := config.IsolateHomeForTest()
	for _, name := range WorkflowEnvVars() {
		os.Unsetenv(name)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
