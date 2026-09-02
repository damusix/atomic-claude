package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// makePagedTestServer serves one slice of releases per page, honouring the
// ?page= query the way GitHub does, so the pagination walk is exercised rather
// than assumed. A page past the end returns an empty list.
func makePagedTestServer(pages [][]Release) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		out := []Release{}
		if page >= 1 && page <= len(pages) {
			out = pages[page-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	return httptest.NewServer(mux)
}

// A prerelease counter passes 9 and keeps ordering. Comparing the identifiers
// as raw strings puts "next.10" below "next.2", which would refuse every
// further prerelease to anyone sitting on next.2 through next.9.
func TestComparePrereleaseOrdersNumericIdentifiersNumerically(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0-next.2", "1.0.0-next.10", -1},
		{"1.0.0-next.10", "1.0.0-next.2", 1},
		{"1.0.0-next.9", "1.0.0-next.89", -1},
		{"1.0.0-next.3", "1.0.0-next.3", 0},
		// A bare prerelease precedes its own numbered successors.
		{"1.0.0-next", "1.0.0-next.1", -1},
		// Numeric identifiers rank below alphanumeric ones (semver §11.4).
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		// A prerelease always ranks below the release of the same core.
		{"1.0.0-next.99", "1.0.0", -1},
	}
	for _, tc := range cases {
		if got := CompareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// A run of prereleases longer than one page must not bury the newest stable
// release: the stable channel would report "no suitable release found" and
// every stable user would silently lose updates and the banner.
func TestLookupPagesPastAFullPageOfPrereleases(t *testing.T) {
	var page1 []Release
	for i := 0; i < lookupPerPage; i++ {
		page1 = append(page1, Release{TagName: fmt.Sprintf("v2.0.0-next.%d", i), Prerelease: true})
	}
	page2 := []Release{{TagName: "v1.9.0"}, {TagName: "v1.8.0"}}

	srv := makePagedTestServer([][]Release{page1, page2})
	defer srv.Close()

	rel, err := testClient(srv).Lookup(context.Background(), ChannelStable, "")
	if err != nil {
		t.Fatalf("stable lookup behind a full page of prereleases: %v", err)
	}
	if rel.TagName != "v1.9.0" {
		t.Errorf("TagName = %q, want v1.9.0", rel.TagName)
	}
}

// The list endpoint orders by commit date, not version, so a hotfix cut from an
// older branch can be listed above a higher release. The stable channel is a
// forward-only ladder, so it must take the highest version, not the first
// listed.
func TestLookupStableTakesHighestVersionNotFirstListed(t *testing.T) {
	releases := []Release{
		{TagName: "v1.6.2"},
		{TagName: "v1.7.0"},
		{TagName: "v1.6.1"},
	}
	srv := makePagedTestServer([][]Release{releases})
	defer srv.Close()

	rel, err := testClient(srv).Lookup(context.Background(), ChannelStable, "")
	if err != nil {
		t.Fatalf("stable lookup: %v", err)
	}
	if rel.TagName != "v1.7.0" {
		t.Errorf("TagName = %q, want v1.7.0", rel.TagName)
	}
}

// The regression that selecting by version would reintroduce: a full release
// outranks every prerelease of its own core, so a version-max prerelease
// channel would pin itself to v1.7.0 and never surface another -next tag,
// however many are published after it. That is exactly the stranding the
// channel exists to avoid, moved one layer up from the install gate.
func TestLookupPrereleaseIsNotStrandedByAStableRelease(t *testing.T) {
	at := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	releases := []Release{
		{TagName: "v1.7.0-next.20", Prerelease: true, PublishedAt: at(20)},
		{TagName: "v1.7.0-next.19", Prerelease: true, PublishedAt: at(19)},
		{TagName: "v1.7.0", PublishedAt: at(10)},
		{TagName: "v1.7.0-next.4", Prerelease: true, PublishedAt: at(4)},
	}
	srv := makePagedTestServer([][]Release{releases})
	defer srv.Close()

	rel, err := testClient(srv).Lookup(context.Background(), ChannelPrerelease, "")
	if err != nil {
		t.Fatalf("prerelease lookup: %v", err)
	}
	if rel.TagName != "v1.7.0-next.20" {
		t.Errorf("TagName = %q, want v1.7.0-next.20", rel.TagName)
	}

	// The same list on stable must still resolve to the full release.
	rel, err = testClient(srv).Lookup(context.Background(), ChannelStable, "")
	if err != nil {
		t.Fatalf("stable lookup: %v", err)
	}
	if rel.TagName != "v1.7.0" {
		t.Errorf("stable TagName = %q, want v1.7.0", rel.TagName)
	}
}

// published_at, not list position: the list is ordered by the tag's commit
// date, so a release cut from an older commit but published later sorts below
// releases it actually supersedes.
func TestLookupPrereleaseTakesMostRecentlyPublished(t *testing.T) {
	at := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	releases := []Release{
		{TagName: "v1.8.0-next.1", Prerelease: true, PublishedAt: at(3)},
		{TagName: "v1.8.0-next.2", Prerelease: true, PublishedAt: at(9)},
	}
	srv := makePagedTestServer([][]Release{releases})
	defer srv.Close()

	rel, err := testClient(srv).Lookup(context.Background(), ChannelPrerelease, "")
	if err != nil {
		t.Fatalf("prerelease lookup: %v", err)
	}
	if rel.TagName != "v1.8.0-next.2" {
		t.Errorf("TagName = %q, want v1.8.0-next.2", rel.TagName)
	}
}

// With no published_at to separate them the list order is the only recency
// signal there is, so the first eligible entry wins.
func TestLookupPrereleaseFallsBackToListOrder(t *testing.T) {
	releases := []Release{
		{TagName: "v1.8.0-next.2", Prerelease: true},
		{TagName: "v1.8.0-next.1", Prerelease: true},
	}
	srv := makePagedTestServer([][]Release{releases})
	defer srv.Close()

	rel, err := testClient(srv).Lookup(context.Background(), ChannelPrerelease, "")
	if err != nil {
		t.Fatalf("prerelease lookup: %v", err)
	}
	if rel.TagName != "v1.8.0-next.2" {
		t.Errorf("TagName = %q, want v1.8.0-next.2", rel.TagName)
	}
}

// The prerelease channel tracks the tip of the pre-release branch rather than
// climbing a version ladder. After 1.7.0 ships stable, the next prerelease cut
// from the same core is 1.7.0-next.N — newer code carrying a lower version. A
// forward-only gate would refuse it and leave the channel dark until the core
// advanced again.
func TestShouldInstallPrereleaseTracksTipAcrossAStableRelease(t *testing.T) {
	cases := []struct {
		name            string
		channel         string
		current, latest string
		want            bool
	}{
		{"prerelease takes a lower tip after stable ships", ChannelPrerelease, "1.7.0", "1.7.0-next.4", true},
		{"prerelease is a no-op on the tag already running", ChannelPrerelease, "1.7.0-next.4", "1.7.0-next.4", false},
		{"prerelease still climbs", ChannelPrerelease, "1.7.0-next.4", "1.7.0-next.5", true},
		{"prerelease graduates onto stable", ChannelPrerelease, "1.7.0-next.4", "1.7.0", true},
		{"stable never moves backwards", ChannelStable, "1.7.0", "1.7.0-next.4", false},
		{"stable climbs", ChannelStable, "1.6.1", "1.7.0", true},
		{"stable graduates a prerelease user", ChannelStable, "1.7.0-next.4", "1.7.0", true},
		{"an empty latest is never installable", ChannelPrerelease, "1.7.0", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldInstall(tc.channel, tc.current, tc.latest); got != tc.want {
				t.Errorf("ShouldInstall(%q, %q, %q) = %v, want %v", tc.channel, tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

// The banner reads the same gate as the installer, so a prerelease machine is
// told about a tip the installer would actually take.
func TestShouldNotifyFollowsTheChannelGate(t *testing.T) {
	now := time.Now()
	if !ShouldNotify(ChannelPrerelease, "1.7.0", "1.7.0-next.4", time.Time{}, now) {
		t.Error("prerelease channel should banner a lower-versioned tip")
	}
	if ShouldNotify(ChannelStable, "1.7.0", "1.7.0-next.4", time.Time{}, now) {
		t.Error("stable channel must never banner a prerelease")
	}
}

func TestValidChannel(t *testing.T) {
	for _, s := range []string{ChannelStable, ChannelPrerelease} {
		if !ValidChannel(s) {
			t.Errorf("ValidChannel(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "beta", "Stable", "pre"} {
		if ValidChannel(s) {
			t.Errorf("ValidChannel(%q) = true, want false", s)
		}
	}
}
