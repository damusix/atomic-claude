package bus

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// Session identity follows harness detection, so the session running the
// tests must not leak its markers into them.
func TestMain(m *testing.M) {
	for _, name := range harness.MarkerVars {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}
