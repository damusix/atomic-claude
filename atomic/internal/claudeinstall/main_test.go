package claudeinstall_test

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// TestMain stubs the profile-refresh seam package-wide: the real path spawns
// ~57 detection subprocesses on every install, and the stub has no lastcheck so
// it never self-gates. Detection itself is covered in internal/profile.
func TestMain(m *testing.M) {
	claudeinstall.ProfileRefresh = func(_, _ string, _ int) (bool, error) { return false, nil }
	os.Exit(m.Run())
}
