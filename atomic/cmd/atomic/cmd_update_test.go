package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// sha256HexString returns the hex-encoded SHA256 of data.
func sha256HexString(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestShouldRunPostUpdateDoctor tests precedence:
// flag (--no-doctor) > config (update.run_doctor=false) > default true.
func TestShouldRunPostUpdateDoctor(t *testing.T) {
	cases := []struct {
		name      string
		noDoctor  bool
		runDoctor bool
		want      bool
	}{
		{"flag suppresses, config true", true, true, false},
		{"flag suppresses, config false", true, false, false},
		{"no flag, config true", false, true, true},
		{"no flag, config false", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunPostUpdateDoctor(tc.noDoctor, tc.runDoctor)
			if got != tc.want {
				t.Errorf("shouldRunPostUpdateDoctor(noDoctor=%v, runDoctor=%v) = %v, want %v",
					tc.noDoctor, tc.runDoctor, got, tc.want)
			}
		})
	}
}

func TestScanNoUpdateCheck(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantFound bool
		wantArgs  []string
	}{
		{
			name:      "flag before subcommand",
			argv:      []string{"atomic", "--no-update-check", "signals", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag after subcommand",
			argv:      []string{"atomic", "signals", "scan", "--no-update-check"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag equals true",
			argv:      []string{"atomic", "--no-update-check=true", "signals", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag equals false strips token but returns false",
			argv:      []string{"atomic", "--no-update-check=false", "signals", "scan"},
			wantFound: false,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag absent",
			argv:      []string{"atomic", "signals", "scan"},
			wantFound: false,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag between subcommand and sub-verb",
			argv:      []string{"atomic", "signals", "--no-update-check", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, cleaned := scanNoUpdateCheck(tc.argv)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if len(cleaned) != len(tc.wantArgs) {
				t.Errorf("cleaned = %v, want %v", cleaned, tc.wantArgs)
				return
			}
			for i, a := range cleaned {
				if a != tc.wantArgs[i] {
					t.Errorf("cleaned[%d] = %q, want %q", i, a, tc.wantArgs[i])
				}
			}
		})
	}
}

// artifactRefreshArgs builds the re-exec argv for the post-swap refresh.
// The hook clause encodes the one policy in this flow: the refresh must
// never be the thing that first registers hooks or overrides an explicit
// --no-hooks install choice — only an existing registration is renewed.
func TestArtifactRefreshArgs(t *testing.T) {
	got := artifactRefreshArgs(true)
	want := []string{"claude", "update", "--no-update-check"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("hooksInstalled=true: args = %v, want %v", got, want)
	}

	got = artifactRefreshArgs(false)
	want = []string{"claude", "update", "--no-update-check", "--no-hooks"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("hooksInstalled=false: args = %v, want %v", got, want)
	}
}

// --- self-update fast path (docs/spec/selfupdate-state.md, parent fast path) ---

func TestStripBackgroundCheckMarker(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantFound bool
		wantArgs  []string
	}{
		{
			name:      "marker present, trailing",
			args:      []string{"--check", backgroundCheckMarker},
			wantFound: true,
			wantArgs:  []string{"--check"},
		},
		{
			name:      "marker present, leading",
			args:      []string{backgroundCheckMarker, "--check"},
			wantFound: true,
			wantArgs:  []string{"--check"},
		},
		{
			name:      "marker absent",
			args:      []string{"--check"},
			wantFound: false,
			wantArgs:  []string{"--check"},
		},
		{
			name:      "no args",
			args:      nil,
			wantFound: false,
			wantArgs:  []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, cleaned := stripBackgroundCheckMarker(tc.args)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if strings.Join(cleaned, " ") != strings.Join(tc.wantArgs, " ") {
				t.Errorf("cleaned = %v, want %v", cleaned, tc.wantArgs)
			}
		})
	}
}

// writeTestUpdateConfig writes a minimal config.toml under home with the
// given [update] table body, so tests can exercise config.Update.Check
// without going through the full Set/WritePersist machinery.
func writeTestUpdateConfig(t *testing.T, home, updateTableBody string) {
	t.Helper()
	dir := config.Dir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[update]\n" + updateTableBody
	if err := os.WriteFile(config.TOMLPath(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSelfupdateFastPath_SpawnGates covers success criterion: "Spawn fires
// only when ALL gates hold ... last_check stamped and persisted BEFORE the
// spawn". Each subtest flips exactly one gate false against an otherwise
// spawn-eligible baseline. The injected spawn func means no subtest ever
// forks a real process.
func TestSelfupdateFastPath_SpawnGates(t *testing.T) {
	baseNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseNow }

	t.Run("all gates hold: spawns exactly once, last_check persisted before spawn fires", func(t *testing.T) {
		home := t.TempDir()
		var calls int
		spawn := func(exe string) error {
			calls++
			// The stamp-before-spawn ordering: by the time spawn runs,
			// last_check must already be on disk.
			st := selfupdate.LoadState(config.StatePath(home))
			if st.Update.LastCheck.IsZero() {
				t.Error("last_check was not persisted before spawn was invoked")
			}
			return nil
		}
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 1 {
			t.Errorf("expected exactly 1 spawn, got %d", calls)
		}
	})

	t.Run("child's own update invocation never re-spawns a grandchild", func(t *testing.T) {
		home := t.TempDir()
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "update", "1.0.0", false, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("verb=update must never spawn, got %d spawns", calls)
		}
	})

	t.Run("--no-update-check present: never spawns", func(t *testing.T) {
		home := t.TempDir()
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", true, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("--no-update-check must suppress spawn, got %d spawns", calls)
		}
	})

	t.Run("config update.check=false: never spawns", func(t *testing.T) {
		home := t.TempDir()
		writeTestUpdateConfig(t, home, "check = false\n")
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("update.check=false must suppress spawn, got %d spawns", calls)
		}
	})

	t.Run("last_check within the hour: never spawns", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LastCheck = baseNow.Add(-30 * time.Minute)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("fresh last_check must suppress spawn, got %d spawns", calls)
		}
	})

	t.Run("last_check exactly 1h ago: spawns (inclusive boundary)", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LastCheck = baseNow.Add(-time.Hour)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 1 {
			t.Errorf("expected 1 spawn at the 1h boundary, got %d", calls)
		}
	})
}

// TestSelfupdateFastPath_Banner covers success criterion: "Banner prints
// from state only (no network), at most once per 24h, stamps last_notified."
// Every subtest sets noUpdateCheck=true and verb="update" so the spawn gate
// never fires — isolating banner behavior from the spawn decision above.
func TestSelfupdateFastPath_Banner(t *testing.T) {
	baseNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseNow }
	noopSpawn := func(exe string) error { return nil }

	t.Run("newer version, never notified: prints and stamps last_notified", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "999.0.0"
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)

		if !strings.Contains(buf.String(), "999.0.0") {
			t.Errorf("expected banner mentioning 999.0.0, got %q", buf.String())
		}
		got := selfupdate.LoadState(config.StatePath(home))
		if !got.Update.LastNotified.Equal(baseNow) {
			t.Errorf("last_notified = %v, want %v", got.Update.LastNotified, baseNow)
		}
	})

	t.Run("notified 1h ago: suppressed within the 24h window", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "999.0.0"
		state.Update.LastNotified = baseNow.Add(-time.Hour)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if buf.Len() != 0 {
			t.Errorf("expected no banner within the 24h window, got %q", buf.String())
		}
	})

	t.Run("notified 25h ago: banners again past the window", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "999.0.0"
		state.Update.LastNotified = baseNow.Add(-25 * time.Hour)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if !strings.Contains(buf.String(), "999.0.0") {
			t.Errorf("expected a banner past the 24h window, got %q", buf.String())
		}
	})

	t.Run("no latest_version recorded yet: never banners", func(t *testing.T) {
		home := t.TempDir()
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if buf.Len() != 0 {
			t.Errorf("expected no banner with empty state, got %q", buf.String())
		}
	})

	// F-1: the banner must never print a "v"-prefixed version, regardless of
	// what is already on disk in state.json — defense-in-depth alongside the
	// check-branch write site normalizing before it ever writes latest_version.
	t.Run("v-prefixed latest_version on disk: banner strips the v", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "v9.9.9"
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if strings.Contains(buf.String(), "v9.9.9") {
			t.Errorf("banner must never print a v-prefixed version, got %q", buf.String())
		}
		if !strings.Contains(buf.String(), "9.9.9") {
			t.Errorf("expected normalized version 9.9.9 in banner, got %q", buf.String())
		}
	})
}

// TestSelfupdateFastPath_ExecutableFailureReportsToWriter covers F-2:
// os.Executable() failing in the spawn path must report to w, matching the
// sibling failure branches (write-state, spawn) immediately above and below
// it, instead of returning silently.
func TestSelfupdateFastPath_ExecutableFailureReportsToWriter(t *testing.T) {
	home := t.TempDir()
	orig := executableFn
	executableFn = func() (string, error) { return "", errors.New("boom") }
	defer func() { executableFn = orig }()

	baseNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseNow }
	spawnCalled := false
	spawn := func(exe string) error { spawnCalled = true; return nil }

	var buf bytes.Buffer
	selfupdateFastPath(home, "signals", "1.0.0", false, &buf, now, spawn)

	if spawnCalled {
		t.Error("spawn must not be invoked when executableFn fails")
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("expected the executable-resolution failure reported to w, got %q", buf.String())
	}
}

// TestDefaultUpdateSpawn_StartsDetachedWithoutWaiting proves the real spawn
// path starts a process and returns without waiting on it — using /bin/echo
// rather than a built atomic binary, since defaultUpdateSpawn only cares
// that the target process starts and is released, not what it does.
func TestDefaultUpdateSpawn_StartsDetachedWithoutWaiting(t *testing.T) {
	if _, err := exec.LookPath("/bin/echo"); err != nil {
		t.Skip("/bin/echo not available")
	}
	if err := defaultUpdateSpawn("/bin/echo"); err != nil {
		t.Fatalf("defaultUpdateSpawn: %v", err)
	}
}

// --- runUpdateCheck (detached-child check branch + once-only staging) ---

// fakeReleaseServer wires a full fake GitHub release backend behind one
// httptest server: /releases (hit by both Check and the staging Lookup),
// the release archive, and checksums.txt (hit by Stage). Every hit against
// /releases increments releaseHits so tests can assert exactly how many
// lookups occurred (the once-only staging gate's whole point is to avoid a
// second download attempt, not a second lookup, but counting lookups also
// proves the gate short-circuits before any network call on a repeat).
type fakeReleaseServer struct {
	srv         *httptest.Server
	client      *selfupdate.Client
	releaseHits int
	archiveHits int
}

func newFakeReleaseServer(t *testing.T, tag, archiveContent string) *fakeReleaseServer {
	t.Helper()
	buildDir := t.TempDir()
	assetName := fmt.Sprintf("atomic_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	archivePath := filepath.Join(buildDir, assetName)
	if err := os.WriteFile(archivePath, []byte(archiveContent), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256HexString([]byte(archiveContent))
	checksumPath := filepath.Join(buildDir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%s  %s\n", sum, assetName)), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeReleaseServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		f.releaseHits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: tag,
			Assets: []selfupdate.Asset{
				{Name: assetName},
				{Name: "checksums.txt"},
			},
		}})
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		f.archiveHits++
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	f.client = &selfupdate.Client{
		BaseURL:     f.srv.URL,
		DownloadURL: f.srv.URL,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}
	return f
}

// brokenReleaseClient returns a Client whose lookups always fail (points at
// a closed listener), for exercising runUpdateCheck's lookup-failure path
// without any real network access.
func brokenReleaseClient(t *testing.T) *selfupdate.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens here — connection refused on every request
	return &selfupdate.Client{BaseURL: "http://" + addr, HTTPClient: &http.Client{Timeout: 2 * time.Second}}
}

func TestRunUpdateCheck_ManualCheckWritesStateNeverStages(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v9.9.9", "payload")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	newer, tag, err := runUpdateCheck(context.Background(), home, false, f.client, "stable", "1.0.0", now, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer || tag != "9.9.9" {
		t.Fatalf("newer=%v tag=%q, want newer=true tag=9.9.9", newer, tag)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.LatestVersion != "9.9.9" {
		t.Errorf("LatestVersion = %q, want %q (no leading v — F-1)", got.Update.LatestVersion, "9.9.9")
	}
	if got.Update.LastResult != "" {
		t.Errorf("LastResult = %q, want empty on success", got.Update.LastResult)
	}
	if got.Update.StageAttemptedFor != "" {
		t.Errorf("manual --check must never stage: StageAttemptedFor = %q, want empty", got.Update.StageAttemptedFor)
	}
	if got.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("manual --check must never stage: Staged = %+v, want zero value", got.Update.Staged)
	}
	if f.archiveHits != 0 {
		t.Errorf("manual --check downloaded the archive %d times, want 0", f.archiveHits)
	}
}

func TestRunUpdateCheck_LookupFailureRecordsLastResultLeavesLatestVersionUnchanged(t *testing.T) {
	home := t.TempDir()
	// Seed a prior good value: a failed lookup must not clobber it.
	seed := selfupdate.State{}
	seed.Update.LatestVersion = "5.0.0"
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	c := brokenReleaseClient(t)
	now := func() time.Time { return time.Now() }

	_, _, err := runUpdateCheck(context.Background(), home, false, c, "stable", "1.0.0", now, io.Discard)
	if err == nil {
		t.Fatal("expected a lookup error, got nil")
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.LatestVersion != "5.0.0" {
		t.Errorf("LatestVersion = %q, want unchanged %q on lookup failure", got.Update.LatestVersion, "5.0.0")
	}
	if got.Update.LastResult == "" {
		t.Error("LastResult was not recorded on lookup failure")
	}
}

func TestRunUpdateCheck_BackgroundStagesWhenNewerAndEnabled(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	newer, tag, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer || tag != "2.0.0" {
		t.Fatalf("newer=%v tag=%q, want newer=true tag=2.0.0", newer, tag)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "2.0.0" {
		t.Errorf("StageAttemptedFor = %q, want %q", got.Update.StageAttemptedFor, "2.0.0")
	}
	if got.Update.Staged.Version != "2.0.0" {
		t.Errorf("Staged.Version = %q, want %q", got.Update.Staged.Version, "2.0.0")
	}
	if got.Update.Staged.SHA256 == "" {
		t.Error("Staged.SHA256 empty, want a recorded checksum")
	}
	wantDir := selfupdate.StageDir(home)
	if !strings.HasPrefix(got.Update.Staged.Path, wantDir) {
		t.Errorf("Staged.Path %q not under %q", got.Update.Staged.Path, wantDir)
	}
	if _, statErr := os.Stat(got.Update.Staged.Path); statErr != nil {
		t.Errorf("staged file missing on disk: %v", statErr)
	}
	if got.Update.Updating {
		t.Error("lock must be released after staging completes")
	}
	if !got.Update.UpdateStartedAt.IsZero() {
		t.Error("update_started_at must be cleared after staging completes")
	}
}

func TestRunUpdateCheck_OnceOnlyGateHoldsAcrossRepeatedChecks(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("first check: %v", err)
	}
	if f.archiveHits != 1 {
		t.Fatalf("archiveHits after first check = %d, want 1", f.archiveHits)
	}

	// Repeat the exact same cycle: same version, still background, still
	// newer, still enabled. The once-only budget for this version is
	// already spent — no second download.
	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("second check: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archiveHits after second check = %d, want still 1 (once-only budget already spent)", f.archiveHits)
	}
}

func TestRunUpdateCheck_NewVersionAllowsNewAttemptAfterBudgetSpent(t *testing.T) {
	home := t.TempDir()
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	f1 := newFakeReleaseServer(t, "v1.1.0", "payload-1.1.0")
	if _, _, err := runUpdateCheck(context.Background(), home, true, f1.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("first check: %v", err)
	}
	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "1.1.0" {
		t.Fatalf("StageAttemptedFor = %q, want %q", got.Update.StageAttemptedFor, "1.1.0")
	}

	// A new release appears: same home, different version — a fresh
	// once-only budget for 1.2.0 must be available.
	f2 := newFakeReleaseServer(t, "v1.2.0", "payload-1.2.0")
	if _, _, err := runUpdateCheck(context.Background(), home, true, f2.client, "stable", "1.1.0", now, io.Discard); err != nil {
		t.Fatalf("second check: %v", err)
	}
	got = selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "1.2.0" {
		t.Errorf("StageAttemptedFor = %q, want %q (new version allows a new attempt)", got.Update.StageAttemptedFor, "1.2.0")
	}
	if f2.archiveHits != 1 {
		t.Errorf("archiveHits for the new version = %d, want 1", f2.archiveHits)
	}
}

func TestRunUpdateCheck_LockContentionSkipsStagingWithoutStamping(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Simulate a concurrent updater already holding the lock.
	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = now()
	if err := selfupdate.WriteState(config.StatePath(home), held); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "" {
		t.Errorf("StageAttemptedFor = %q, want empty (lock contention must not spend the once-only budget)", got.Update.StageAttemptedFor)
	}
	if got.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("Staged = %+v, want zero value under lock contention", got.Update.Staged)
	}
	if f.archiveHits != 0 {
		t.Errorf("archiveHits = %d, want 0 under lock contention", f.archiveHits)
	}
	// The base state write (latest_version/last_result) still happens —
	// only staging is gated by the lock.
	if got.Update.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q even under lock contention", got.Update.LatestVersion, "2.0.0")
	}
}

func TestRunUpdateCheck_FailedStageRecordsLastResultStaysStampedNeverRetried(t *testing.T) {
	home := t.TempDir()
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	// A release whose advertised assets never match the archive name Stage
	// computes ("atomic_3.0.0_<goos>_<goarch>.tar.gz") — Stage fails
	// deterministically at asset lookup, before any download, no network
	// flakiness required to produce the failure.
	var releaseHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		releaseHits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: "v3.0.0",
			Assets:  []selfupdate.Asset{{Name: "unrelated.tar.gz"}, {Name: "checksums.txt"}},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &selfupdate.Client{BaseURL: srv.URL, DownloadURL: srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}

	newer, tag, err := runUpdateCheck(context.Background(), home, true, c, "stable", "1.0.0", now, io.Discard)
	if err != nil {
		t.Fatalf("Check itself must still succeed: %v", err)
	}
	if !newer || tag != "3.0.0" {
		t.Fatalf("newer=%v tag=%q, want newer=true tag=3.0.0", newer, tag)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "3.0.0" {
		t.Errorf("StageAttemptedFor = %q, want %q (stays stamped despite failure)", got.Update.StageAttemptedFor, "3.0.0")
	}
	if got.Update.LastResult == "" {
		t.Error("expected a recorded staging failure in LastResult")
	}
	if got.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("Staged = %+v, want zero value on failure", got.Update.Staged)
	}
	if got.Update.Updating {
		t.Error("lock must be released even after a staging failure")
	}

	// Repeat: same version, same failure mode — must never retry.
	if _, _, err := runUpdateCheck(context.Background(), home, true, c, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("second check: %v", err)
	}
	if releaseHits != 3 {
		// 1 (first Check) + 1 (first staging Lookup) + 1 (second Check) — the
		// second staging Lookup must never fire because the gate short-circuits.
		t.Errorf("releaseHits = %d, want 3 (no retry of the failed stage attempt)", releaseHits)
	}
}

func TestRunUpdateCheck_StageDisabledByConfigNeverStages(t *testing.T) {
	home := t.TempDir()
	writeTestUpdateConfig(t, home, "stage = false\n")
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "" {
		t.Errorf("StageAttemptedFor = %q, want empty when update.stage=false", got.Update.StageAttemptedFor)
	}
	if f.archiveHits != 0 {
		t.Errorf("archiveHits = %d, want 0 when update.stage=false", f.archiveHits)
	}
}

// TestRunUpdateCheck_StagerCompletionDoesNotClobberForegroundTakeover pins
// the owner-checked release fix: the background stager acquires the lock,
// then — while its archive download is in flight — a foreground `atomic
// update` takes over the (now stale, or --force-stamped) lock, recording a
// newer update_started_at. When the stager's own download finishes and it
// writes its completion record, the foreground's active lock must survive:
// a blind ReleaseLock would clear Updating/UpdateStartedAt out from under
// the still-in-progress foreground swap, opening a window for a third
// `atomic update` to race a concurrent os.Rename on the same binary.
func TestRunUpdateCheck_StagerCompletionDoesNotClobberForegroundTakeover(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)

	stagerAcquiredAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return stagerAcquiredAt }
	foregroundStartedAt := stagerAcquiredAt.Add(5 * time.Minute)

	buildDir := t.TempDir()
	assetName := fmt.Sprintf("atomic_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archivePath := filepath.Join(buildDir, assetName)
	content := "release-payload-v2"
	if err := os.WriteFile(archivePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	checksumPath := filepath.Join(buildDir, "checksums.txt")
	sum := sha256HexString([]byte(content))
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%s  %s\n", sum, assetName)), 0o644); err != nil {
		t.Fatal(err)
	}

	var takeoverDone bool
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: "v2.0.0",
			Assets: []selfupdate.Asset{
				{Name: assetName},
				{Name: "checksums.txt"},
			},
		}})
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		// Simulate the foreground process's takeover write landing on disk
		// mid-download — before this stager's own completion write below.
		if !takeoverDone {
			takeoverDone = true
			fg := selfupdate.State{}
			fg.Update.Updating = true
			fg.Update.UpdateStartedAt = foregroundStartedAt
			if err := selfupdate.WriteState(statePath, fg); err != nil {
				t.Fatal(err)
			}
		}
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &selfupdate.Client{BaseURL: srv.URL, DownloadURL: srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}

	if _, _, err := runUpdateCheck(context.Background(), home, true, c, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := selfupdate.LoadState(statePath)
	if !got.Update.Updating || !got.Update.UpdateStartedAt.Equal(foregroundStartedAt) {
		t.Fatalf("foreground lock clobbered by stager completion: Updating=%v UpdateStartedAt=%v, want Updating=true UpdateStartedAt=%v",
			got.Update.Updating, got.Update.UpdateStartedAt, foregroundStartedAt)
	}
	// The stager's own non-lock fields must still land even though it no
	// longer owns the lock at completion time.
	if got.Update.StageAttemptedFor != "2.0.0" {
		t.Errorf("StageAttemptedFor = %q, want %q even without lock ownership", got.Update.StageAttemptedFor, "2.0.0")
	}
	if got.Update.Staged.Version != "2.0.0" {
		t.Errorf("Staged.Version = %q, want %q", got.Update.Staged.Version, "2.0.0")
	}
}

// --- runUpdateApply (lock + staged fast-path swap in the apply branch) ---

// buildRealArchiveTarGz builds a genuine gzip-compressed tar archive
// containing one file, "atomic", with content, at dir/assetName — unlike
// fakeReleaseServer's raw-bytes fixture (checksum-only; staging never
// extracts what it downloads), swap flow actually extracts and
// renames the binary, so its tests need a real, extractable archive.
func buildRealArchiveTarGz(t *testing.T, dir, assetName, content string) (archivePath, sha string) {
	t.Helper()
	archivePath = filepath.Join(dir, assetName)
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "atomic", Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	return archivePath, sha256HexString(data)
}

// fakeSwapServer serves a real, extractable release (archive + checksums.txt
// + /releases lookup) for one tag — used by tests that exercise actual
// extraction/swap, as opposed to fakeReleaseServer's checksum-only fixture.
type fakeSwapServer struct {
	srv         *httptest.Server
	client      *selfupdate.Client
	assetName   string
	archivePath string
	sha256      string
	archiveHits int
}

func newFakeSwapServer(t *testing.T, tag, binaryContent string) *fakeSwapServer {
	t.Helper()
	dir := t.TempDir()
	assetName := fmt.Sprintf("atomic_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	archivePath, sha := buildRealArchiveTarGz(t, dir, assetName, binaryContent)
	checksumPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%s  %s\n", sha, assetName)), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeSwapServer{assetName: assetName, archivePath: archivePath, sha256: sha}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: tag,
			Assets: []selfupdate.Asset{
				{Name: assetName},
				{Name: "checksums.txt"},
			},
		}})
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		f.archiveHits++
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	f.client = &selfupdate.Client{BaseURL: f.srv.URL, DownloadURL: f.srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}
	return f
}

func TestRunUpdateApply_StagedFastPathSwapsWithoutDownloadingArchive(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v2-content"
	f := newFakeSwapServer(t, "v2.0.0", binaryContent)

	// Place a byte-identical copy of the release archive in the staged
	// directory — mirrors what a prior background Stage() call would
	// have left behind.
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	data, err := os.ReadFile(f.archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "2.0.0", Path: stagedPath, SHA256: f.sha256}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}

	if f.archiveHits != 0 {
		t.Errorf("archive endpoint hit %d times, want 0 on the staged fast path", f.archiveHits)
	}
	got, err := os.ReadFile(currentBin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared after a successful swap")
	}
	if state.Update.UpdatedAt.IsZero() {
		t.Error("updated_at must be stamped on success")
	}
	if state.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("staged record must be cleared after swap, got %+v", state.Update.Staged)
	}
	if _, statErr := os.Stat(stagedPath); statErr == nil {
		t.Error("staged file should be removed best-effort after a successful swap")
	}
}

func TestRunUpdateApply_StagedVersionMismatchFallsBackToDownload(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v3-content"
	f := newFakeSwapServer(t, "v3.0.0", binaryContent)

	// Staged record names an OLDER version than the fresh lookup returns.
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleName := fmt.Sprintf("atomic_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	stalePath := filepath.Join(stageDir, staleName)
	if err := os.WriteFile(stalePath, []byte("stale-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "2.0.0", Path: stalePath, SHA256: sha256HexString([]byte("stale-content"))}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (version mismatch must fall back to a real download)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("stale staged record must be discarded, got %+v", state.Update.Staged)
	}
}

func TestRunUpdateApply_StagedChecksumMismatchFallsBackToDownload(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v4-content"
	f := newFakeSwapServer(t, "v4.0.0", binaryContent)

	// Same version, same asset name, but different bytes than the fresh
	// release now serves — simulates a re-cut release since staging.
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	if err := os.WriteFile(stagedPath, []byte("corrupted-or-stale-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "4.0.0", Path: stagedPath, SHA256: sha256HexString([]byte("corrupted-or-stale-bytes"))}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (checksum mismatch must fall back to a real download)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

func TestRunUpdateApply_StagedFileMissingFallsBackToDownload(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v5-content"
	f := newFakeSwapServer(t, "v5.0.0", binaryContent)

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{
		Version: "5.0.0",
		Path:    filepath.Join(t.TempDir(), "gone.tar.gz"),
		SHA256:  "deadbeef",
	}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (missing staged file must fall back to a real download)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

func TestRunUpdateApply_UpToDateReportsAndClearsLock(t *testing.T) {
	home := t.TempDir()
	f := newFakeSwapServer(t, "v1.0.0", "same-version-content")

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("current-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if !strings.Contains(buf.String(), "up to date") {
		t.Errorf("expected an up-to-date report, got %q", buf.String())
	}
	if f.archiveHits != 0 {
		t.Errorf("archive endpoint hit %d times, want 0 when already up to date", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "current-binary" {
		t.Error("binary must not be replaced when already up to date")
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared when already up to date")
	}
}

func TestRunUpdateApply_FreshLockRefusesNamingAge(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = started
	if err := selfupdate.WriteState(statePath, held); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return started.Add(3 * time.Minute) }
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Never contacted — refusal happens before any lookup.
	c := brokenReleaseClient(t)
	var buf bytes.Buffer
	err := runUpdateApply(context.Background(), home, c, "stable", "1.0.0", currentBin, false, now, &buf)
	if err == nil {
		t.Fatal("expected refusal for a fresh lock")
	}
	if !strings.Contains(err.Error(), "3m0s") {
		t.Errorf("expected the lock's age in the error, got: %v", err)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "old-binary" {
		t.Error("binary must be untouched on refusal")
	}
}

func TestRunUpdateApply_StaleLockTakenOverSwaps(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = started
	if err := selfupdate.WriteState(statePath, held); err != nil {
		t.Fatal(err)
	}

	const binaryContent = "new-binary-v6-content"
	f := newFakeSwapServer(t, "v6.0.0", binaryContent)

	now := func() time.Time { return started.Add(11 * time.Minute) } // past the 10-minute stale threshold
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("expected takeover of the abandoned lock to succeed: %v", err)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

// TestRunUpdateApply_ForceBypassesLockButNotChecksum covers the success
// criterion: --force bypasses lock contention only. A corrupted staged
// archive under --force must still fail its checksum re-verify and fall
// back to a real download rather than swapping the corrupted bytes in.
func TestRunUpdateApply_ForceBypassesLockButNotChecksum(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	const binaryContent = "new-binary-v7-content"
	f := newFakeSwapServer(t, "v7.0.0", binaryContent)

	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	if err := os.WriteFile(stagedPath, []byte("corrupted-payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = started
	held.Update.Staged = selfupdate.StagedInfo{Version: "7.0.0", Path: stagedPath, SHA256: sha256HexString([]byte("corrupted-payload"))}
	if err := selfupdate.WriteState(statePath, held); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return started.Add(30 * time.Second) } // well within the stale window — force must still bypass it
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, true, now, &buf); err != nil {
		t.Fatalf("--force with a fallback download available should still succeed: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (corrupted staged archive must fall back to download, not swap directly)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q (must be the freshly downloaded content, not the corrupted staged bytes)", got, binaryContent)
	}
}

func TestRunUpdateApply_LockClearedOnLookupFailure(t *testing.T) {
	home := t.TempDir()
	c := brokenReleaseClient(t)
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := runUpdateApply(context.Background(), home, c, "stable", "1.0.0", currentBin, false, now, &buf)
	if err == nil {
		t.Fatal("expected a lookup error")
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared best-effort after a lookup failure")
	}
}

func TestRunUpdateApply_LockClearedOnApplyFailure(t *testing.T) {
	home := t.TempDir()
	// A release advertising no matching archive asset — Apply fails
	// deterministically at asset lookup, before any network flakiness.
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: "v9.0.0",
			Assets:  []selfupdate.Asset{{Name: "unrelated.tar.gz"}},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &selfupdate.Client{BaseURL: srv.URL, DownloadURL: srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := runUpdateApply(context.Background(), home, c, "stable", "1.0.0", currentBin, false, now, &buf)
	if err == nil {
		t.Fatal("expected an Apply failure")
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared best-effort after an apply failure")
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "old-binary" {
		t.Error("binary must be untouched on apply failure")
	}
}

// --- atomic prompt dispatch ---
