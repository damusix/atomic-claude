package claudeinstall_test

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// TestMain neutralizes the profile-refresh seam for the whole package.
//
// DefaultProfileRefresh runs the real detection registry: ~57 subprocess
// spawns with a 3s timeout each. RenderStub writes its <deterministic> block
// without a lastcheck attribute, so RefreshIfStale finds no parseable date and
// does a full refresh every time — which made an install test cost 1.29s
// instead of 0.01s, for behavior none of these tests are about.
//
// Tests that care about the seam set their own stub and assert on it; the
// real detection path is covered in internal/profile.
func TestMain(m *testing.M) {
	claudeinstall.ProfileRefresh = func(_, _ string, _ int) (bool, error) { return false, nil }
	os.Exit(m.Run())
}
