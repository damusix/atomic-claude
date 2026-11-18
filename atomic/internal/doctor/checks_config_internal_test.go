package doctor

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// A warning whose harness doctor cannot name still has to reach the user, under
// the hint that lets them pick the harness.
func TestAgentWarningDetailsKeepsAWarningWithoutAKnownHarness(t *testing.T) {
	details := agentWarningDetails([]config.Warning{
		{Message: "omp issue", Harness: "omp"},
		{Message: "no harness"},
		{Message: "pi issue", Harness: "pi"},
	})
	want := []string{
		"omp issue — run `atomic config agents --harness omp` to edit them",
		"no harness; pi issue — run `atomic config agents` to edit them",
	}
	if strings.Join(details, "\n") != strings.Join(want, "\n") {
		t.Errorf("details = %q\nwant %q", details, want)
	}
}
