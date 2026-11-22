package bus

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// Do not let the calling session leak its identity into tests.
func TestMain(m *testing.M) {
	for _, name := range harness.MarkerVars {
		os.Unsetenv(name)
	}
	os.Unsetenv(ompSessionEnvVar)
	os.Exit(m.Run())
}
