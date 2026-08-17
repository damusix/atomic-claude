package doctor_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func hasIndex(results []doctor.Result, idx int) bool {
	for _, r := range results {
		if r.Index == idx {
			return true
		}
	}
	return false
}

// Outside the repo the manifest check is omitted entirely, not even as a SKIP
// row, so end users never see repo-dev noise.
func TestRunWith_NotRepoDev_OmitsManifest(t *testing.T) {
	results, err := doctor.RunWith(doctor.Opts{}, false)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if hasIndex(results, 5) {
		t.Errorf("manifest check (index 5) must be omitted outside atomic-claude repo, but it is present")
	}
}

func TestRunWith_RepoDev_IncludesManifest(t *testing.T) {
	results, err := doctor.RunWith(doctor.Opts{}, true)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !hasIndex(results, 5) {
		t.Errorf("manifest check (index 5) must be present inside atomic-claude repo, but it is omitted")
	}
}

// Explicit intent overrides the auto-omit; the check then self-reports SKIP.
func TestRunWith_NotRepoDev_ExplicitOnlyRunsManifest(t *testing.T) {
	results, err := doctor.RunWith(doctor.Opts{Only: []int{5}}, false)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !hasIndex(results, 5) {
		t.Errorf("explicit --only 5 must run manifest check even outside the repo")
	}
}
