package doctor_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// hasIndex reports whether results contain a check with the given category index.
func hasIndex(results []doctor.Result, idx int) bool {
	for _, r := range results {
		if r.Index == idx {
			return true
		}
	}
	return false
}

// TestRunWith_NotRepoDev_OmitsManifest proves that outside the atomic-claude
// repo, the manifest check (category 5, RepoDevOnly) is omitted entirely — no
// result row, not even a SKIP. End users never see repo-dev noise.
func TestRunWith_NotRepoDev_OmitsManifest(t *testing.T) {
	results, err := doctor.RunWith(doctor.Opts{}, false)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if hasIndex(results, 5) {
		t.Errorf("manifest check (index 5) must be omitted outside atomic-claude repo, but it is present")
	}
}

// TestRunWith_RepoDev_IncludesManifest proves the manifest check IS present when
// running inside the atomic-claude repo.
func TestRunWith_RepoDev_IncludesManifest(t *testing.T) {
	results, err := doctor.RunWith(doctor.Opts{}, true)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !hasIndex(results, 5) {
		t.Errorf("manifest check (index 5) must be present inside atomic-claude repo, but it is omitted")
	}
}

// TestRunWith_NotRepoDev_ExplicitOnlyRunsManifest proves an explicit
// `--only 5` request runs the manifest check even outside the repo (it then
// self-reports SKIP). Explicit intent overrides the auto-omit.
func TestRunWith_NotRepoDev_ExplicitOnlyRunsManifest(t *testing.T) {
	results, err := doctor.RunWith(doctor.Opts{Only: []int{5}}, false)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !hasIndex(results, 5) {
		t.Errorf("explicit --only 5 must run manifest check even outside the repo")
	}
}
