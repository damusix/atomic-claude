package serve

import (
	"reflect"
	"testing"
)

// A status line ends in its transition, not its path, so keying by the last
// field would badge a member named "pending→indexed" instead of the real one.
func TestParseStaleLines_KeysEveryDriftLineByPath(t *testing.T) {
	got := parseStaleLines("DRIFT added beta\nDRIFT status packages/alpha pending→indexed\nSTALE summary wiki/repos/gamma.md\n")

	want := map[string]bool{"beta": true, "packages/alpha": true}
	if !reflect.DeepEqual(got.Members, want) {
		t.Errorf("Members = %v, want %v", got.Members, want)
	}
}

// /api/status lists drifted members by path; a nested member listed twice
// would read as two repos drifting.
func TestProductionWikiStale_ReportsNestedMemberOnce(t *testing.T) {
	old := navStalenessCache
	navStalenessCache = &stalenessCache{walk: func(string) staleSets {
		return parseStaleLines("DRIFT status packages/alpha pending→indexed\nDRIFT added beta\n")
	}}
	t.Cleanup(func() { navStalenessCache = old })

	got := productionWikiStale("/realm").StaleRepos

	want := []string{"beta", "packages/alpha"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StaleRepos = %v, want %v", got, want)
	}
}
