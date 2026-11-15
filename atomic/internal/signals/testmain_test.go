package signals_test

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// tree.go's skippedPrefixes reads the user's state.dir, and config.ScanGlobs
// reads the selection record; keep both off the real home.
func TestMain(m *testing.M) {
	cleanup := config.IsolateHomeForTest()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
