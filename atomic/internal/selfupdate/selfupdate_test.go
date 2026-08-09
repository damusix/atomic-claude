package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------- semver tests ----------

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		major   int
		minor   int
		patch   int
		pre     string
	}{
		{"v0.1.0", false, 0, 1, 0, ""},
		{"0.1.0", false, 0, 1, 0, ""},
		{"v0.1.0-rc.1", false, 0, 1, 0, "rc.1"},
		{"1.2.3+build.1", false, 1, 2, 3, ""},
		{"bad", true, 0, 0, 0, ""},
		{"1.2", true, 0, 0, 0, ""},
	}
	for _, tc := range cases {
		sv, err := parseSemver(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSemver(%q): expected error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSemver(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if sv.major != tc.major || sv.minor != tc.minor || sv.patch != tc.patch || sv.prerelease != tc.pre {
			t.Errorf("parseSemver(%q) = {%d %d %d %q}, want {%d %d %d %q}",
				tc.in, sv.major, sv.minor, sv.patch, sv.prerelease,
				tc.major, tc.minor, tc.patch, tc.pre)
		}
	}
}

func TestSemverOrdering(t *testing.T) {
	// prerelease < release
	a, _ := parseSemver("v0.1.0-rc.1")
	b, _ := parseSemver("v0.1.0")
	if a.compare(b) >= 0 {
		t.Errorf("expected rc < release, got %d", a.compare(b))
	}
	// build metadata ignored
	c, _ := parseSemver("1.2.3+build.1")
	d, _ := parseSemver("1.2.3")
	if c.compare(d) != 0 {
		t.Errorf("build metadata should be ignored")
	}
	// ordering
	v010, _ := parseSemver("0.1.0")
	v011, _ := parseSemver("0.1.1")
	if v010.compare(v011) >= 0 {
		t.Errorf("0.1.0 should be < 0.1.1")
	}
	if v011.compare(v010) <= 0 {
		t.Errorf("0.1.1 should be > 0.1.0")
	}
}

// ---------- helpers ----------

func makeTestServer(releases []Release) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(releases)
	})
	return httptest.NewServer(mux)
}

func testClient(srv *httptest.Server) *Client {
	return &Client{
		BaseURL:    srv.URL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// ---------- Lookup tests ----------

func TestLookupStableChannel(t *testing.T) {
	releases := []Release{
		{TagName: "v0.2.0", Prerelease: true, Draft: false, Assets: nil},
		{TagName: "v0.1.1", Prerelease: false, Draft: false, Assets: nil},
		{TagName: "v0.1.0", Prerelease: false, Draft: false, Assets: nil},
	}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	rel, err := c.Lookup(context.Background(), "stable", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.TagName != "v0.1.1" {
		t.Errorf("expected v0.1.1, got %s", rel.TagName)
	}
}

func TestLookupPrereleaseChannel(t *testing.T) {
	releases := []Release{
		{TagName: "v0.2.0-rc.1", Prerelease: true, Draft: false},
		{TagName: "v0.1.1", Prerelease: false, Draft: false},
	}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	rel, err := c.Lookup(context.Background(), "prerelease", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.TagName != "v0.2.0-rc.1" {
		t.Errorf("expected v0.2.0-rc.1, got %s", rel.TagName)
	}
}

func TestLookupDraftSkipped(t *testing.T) {
	releases := []Release{
		{TagName: "v0.2.0", Prerelease: false, Draft: true},
		{TagName: "v0.1.0", Prerelease: false, Draft: false},
	}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	rel, err := c.Lookup(context.Background(), "stable", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.TagName != "v0.1.0" {
		t.Errorf("expected draft to be skipped, got %s", rel.TagName)
	}
}

func TestLookupAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]Release{{TagName: "v0.1.0"}})
	}))
	defer srv.Close()

	c := testClient(srv)
	_, _ = c.Lookup(context.Background(), "stable", "mytoken")
	if gotAuth != "Bearer mytoken" {
		t.Errorf("expected Authorization header 'Bearer mytoken', got %q", gotAuth)
	}
}

func TestLookup4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := testClient(srv)
	_, err := c.Lookup(context.Background(), "stable", "")
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}

func TestLookupBodyParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	c := testClient(srv)
	_, err := c.Lookup(context.Background(), "stable", "")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// ---------- Cache tests ----------

func TestCacheXDGOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	path, err := DefaultCachePath()
	if err != nil {
		t.Fatalf("DefaultCachePath: %v", err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("expected path under %s, got %s", dir, path)
	}
}

// ---------- Apply tests ----------

// buildTarGz creates a tar.gz archive with a single file named "atomic"
// containing content, and returns (archivePath, sha256hex).
// The archive name uses the current runtime OS and arch so Apply can find it.
func buildTarGz(dir, content string) (archivePath string, sha256sum string, err error) {
	assetBase := fmt.Sprintf("atomic_0.1.1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archivePath = filepath.Join(dir, assetBase)
	f, err := os.Create(archivePath)
	if err != nil {
		return "", "", err
	}

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name: "atomic",
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		f.Close()
		return "", "", err
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		f.Close()
		return "", "", err
	}
	tw.Close()
	gz.Close()
	f.Close()

	// hash the completed archive file
	af, err := os.Open(archivePath)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, af); err != nil {
		af.Close()
		return "", "", err
	}
	af.Close()
	sha256sum = hex.EncodeToString(h.Sum(nil))
	return archivePath, sha256sum, nil
}

func buildChecksums(dir, assetName, sha256sum string) string {
	path := filepath.Join(dir, "checksums.txt")
	content := fmt.Sprintf("%s  %s\n", sha256sum, assetName)
	os.WriteFile(path, []byte(content), 0o644)
	return path
}

func TestApplyReplacesBinary(t *testing.T) {
	buildDir := t.TempDir()
	const binaryContent = "fake-atomic-binary-v0.1.1"

	archivePath, sha256sum, err := buildTarGz(buildDir, binaryContent)
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	checksumPath := buildChecksums(buildDir, assetName, sha256sum)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	os.WriteFile(currentBin, []byte("old-binary"), 0o755)

	rel := Release{
		TagName: "v0.1.1",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	c := &Client{
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		DownloadURL: srv.URL,
	}

	if err := c.Apply(context.Background(), rel, currentBin); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(currentBin)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(got) != binaryContent {
		t.Errorf("binary content mismatch: got %q, want %q", got, binaryContent)
	}
}

func TestApplySHAMismatch(t *testing.T) {
	buildDir := t.TempDir()
	archivePath, _, err := buildTarGz(buildDir, "content")
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	// write wrong checksum
	checksumPath := buildChecksums(buildDir, assetName, strings.Repeat("0", 64))

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	os.WriteFile(currentBin, []byte("original"), 0o755)

	rel := Release{
		TagName: "v0.1.1",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	c := &Client{
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		DownloadURL: srv.URL,
	}

	err = c.Apply(context.Background(), rel, currentBin)
	if err == nil {
		t.Fatal("expected SHA mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected 'SHA256 mismatch' in error, got: %v", err)
	}

	// original binary must be untouched
	got, _ := os.ReadFile(currentBin)
	if string(got) != "original" {
		t.Errorf("binary was replaced despite SHA mismatch")
	}
}

// TestApplyContextCancellation: cancelling ctx mid-Apply causes Apply to return
// an error from the context rather than completing the download.
func TestApplyContextCancellation(t *testing.T) {
	buildDir := t.TempDir()
	const binaryContent = "fake-atomic-binary-v0.1.1"

	archivePath, sha256sum, err := buildTarGz(buildDir, binaryContent)
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	checksumPath := buildChecksums(buildDir, assetName, sha256sum)

	// Slow handler — waits long enough for cancellation to fire first.
	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			http.ServeFile(w, r, archivePath)
		}
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	os.WriteFile(currentBin, []byte("old-binary"), 0o755)

	rel := Release{
		TagName: "v0.1.1",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	c := &Client{
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		DownloadURL: srv.URL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = c.Apply(ctx, rel, currentBin)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// Must have returned with the context error, not a success.
	got, _ := os.ReadFile(currentBin)
	if string(got) != "old-binary" {
		t.Errorf("binary was replaced despite cancellation")
	}
}

// TestApply_StagingInInstallDir: Apply stages the new binary next to the target
// binary before renaming, ensuring same-filesystem rename (EXDEV avoidance).
// This is a behavioral test: we verify that Apply completes successfully even
// when the binary's dir is not the same as os.TempDir(). The staging file
// (.atomic.new) must be cleaned up on success.
func TestApply_StagingInInstallDir(t *testing.T) {
	buildDir := t.TempDir()
	const binaryContent = "fake-atomic-binary-v0.1.1-staged"

	archivePath, sha256sum, err := buildTarGz(buildDir, binaryContent)
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	checksumPath := buildChecksums(buildDir, assetName, sha256sum)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	os.WriteFile(currentBin, []byte("old-binary"), 0o755)

	rel := Release{
		TagName: "v0.1.1",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	c := &Client{
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		DownloadURL: srv.URL,
	}

	if err := c.Apply(context.Background(), rel, currentBin); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Binary must be replaced.
	got, err := os.ReadFile(currentBin)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(got) != binaryContent {
		t.Errorf("binary content mismatch: got %q, want %q", got, binaryContent)
	}

	// Staging file must be gone after successful apply.
	staged := filepath.Join(binDir, ".atomic.new")
	if _, err := os.Stat(staged); err == nil {
		t.Errorf(".atomic.new staging file was not cleaned up after Apply")
	}
}

// ---------- Check tests ----------

func TestCheckUpToDate(t *testing.T) {
	releases := []Release{{TagName: "v0.1.0"}}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	newer, tag, err := c.Check(context.Background(), "stable", "v0.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newer {
		t.Errorf("expected up-to-date, got newer=true (tag=%s)", tag)
	}
}

func TestCheckNewerAvailable(t *testing.T) {
	releases := []Release{{TagName: "v0.1.1"}}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	newer, tag, err := c.Check(context.Background(), "stable", "v0.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer {
		t.Errorf("expected newer=true, got false (tag=%s)", tag)
	}
	if tag != "0.1.1" {
		t.Errorf("expected tag 0.1.1 (no leading v), got %s", tag)
	}
}

// ---------- IsNewer / ShouldNotify tests (parent state-only banner decision) ----------

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"latest newer", "1.0.0", "1.1.0", true},
		{"equal", "1.0.0", "1.0.0", false},
		{"latest older", "1.1.0", "1.0.0", false},
		{"empty latest (never checked)", "1.0.0", "", false},
		{"malformed latest never errors, reports false", "1.0.0", "not-a-version", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNewer(tc.current, tc.latest); got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestShouldNotify(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		current      string
		latest       string
		lastNotified time.Time
		want         bool
	}{
		{"never notified, newer available", "1.0.0", "1.1.0", time.Time{}, true},
		{"notified 23h ago, within window", "1.0.0", "1.1.0", now.Add(-23 * time.Hour), false},
		{"notified exactly 24h ago, window boundary", "1.0.0", "1.1.0", now.Add(-24 * time.Hour), true},
		{"notified 25h ago, past window", "1.0.0", "1.1.0", now.Add(-25 * time.Hour), true},
		{"up to date, never notify regardless of window", "1.0.0", "1.0.0", time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldNotify(tc.current, tc.latest, tc.lastNotified, now); got != tc.want {
				t.Errorf("ShouldNotify(%q, %q, %v, now) = %v, want %v", tc.current, tc.latest, tc.lastNotified, got, tc.want)
			}
		})
	}
}

// ---------- displayVersion tests ----------

func TestDisplayVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"v1.2.0", "1.2.0"},
		{"1.2.0", "1.2.0"}, // idempotent — no leading v
		{"", ""},
		{"v0.0.1-rc.1", "0.0.1-rc.1"},
	}
	for _, tc := range cases {
		got := displayVersion(tc.in)
		if got != tc.want {
			t.Errorf("displayVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCheckReturnsNoVPrefix: Check with a server whose release tag is "v1.2.0"
// must return "1.2.0" (no leading v) in the tag return value.
func TestCheckReturnsNoVPrefix(t *testing.T) {
	releases := []Release{{TagName: "v1.2.0"}}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	newer, tag, err := c.Check(context.Background(), "stable", "1.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer {
		t.Errorf("expected newer=true, got false")
	}
	if tag != "1.2.0" {
		t.Errorf("Check tag: got %q, want %q (no leading v)", tag, "1.2.0")
	}
}

// TestCheckUpToDateReturnsNoVPrefix: the up-to-date path must also return no-v.
func TestCheckUpToDateReturnsNoVPrefix(t *testing.T) {
	releases := []Release{{TagName: "v1.2.0"}}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	newer, tag, err := c.Check(context.Background(), "stable", "1.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newer {
		t.Errorf("expected up-to-date, got newer=true")
	}
	if tag != "1.2.0" {
		t.Errorf("Check tag (up-to-date): got %q, want %q (no leading v)", tag, "1.2.0")
	}
}

// ---------- Lookup context cancellation test ----------

func TestLookupContextCancelled(t *testing.T) {
	// Slow handler — delays long enough for the cancelled context to fire first.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			// request cancelled by client
		case <-time.After(5 * time.Second):
			json.NewEncoder(w).Encode([]Release{{TagName: "v0.1.0"}})
		}
	}))
	defer srv.Close()

	c := testClient(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Lookup(ctx, "stable", "")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// ---------- DisplayVersion (exported wrapper, F-1) ----------

// TestDisplayVersion_ExportedWrapper pins the exported wrapper against the
// same table as the unexported displayVersion — callers outside this
// package (the check-branch write site, the banner) need this to normalize
// a raw tag before it ever reaches state.json or stdout.
func TestDisplayVersion_ExportedWrapper(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.2.0", "1.2.0"},
		{"1.2.0", "1.2.0"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := DisplayVersion(tc.in); got != tc.want {
			t.Errorf("DisplayVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------- StageDir ----------

// TestStageDir_NoHardcodedHome proves the staging directory is derived
// purely from the home argument (never os.UserHomeDir()), so tests and a
// future real caller land in ~/.cache/atomic/staged/ under whatever home is
// passed in — never a hardcoded path.
func TestStageDir_NoHardcodedHome(t *testing.T) {
	home := "/tmp/fake-home-for-test"
	want := filepath.Join(home, ".cache", "atomic", "staged")
	if got := StageDir(home); got != want {
		t.Errorf("StageDir(%q) = %q, want %q", home, got, want)
	}
}

// ---------- Stage (background staging helper) ----------

// stageTestServer wires a release archive + checksums.txt behind an
// httptest server and returns (srv, rel, sha256sum) — rel points its asset
// URLs at the server via DownloadURL like the existing Apply tests.
func stageTestServer(t *testing.T, tag, content string) (*httptest.Server, Release, string) {
	t.Helper()
	buildDir := t.TempDir()
	archivePath, sha256sum, err := buildTarGz(buildDir, content)
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	checksumPath := buildChecksums(buildDir, assetName, sha256sum)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rel := Release{
		TagName: tag,
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}
	return srv, rel, sha256sum
}

func TestStage_DownloadsAndRecordsChecksumVerifiedArchive(t *testing.T) {
	srv, rel, wantSHA := stageTestServer(t, "v0.1.1", "fake-atomic-archive")
	_ = srv

	c := &Client{HTTPClient: &http.Client{Timeout: 5 * time.Second}, DownloadURL: srv.URL}
	stageDir := t.TempDir()

	got, err := c.Stage(context.Background(), rel, stageDir)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if got.Version != "0.1.1" {
		t.Errorf("Version = %q, want %q (no leading v)", got.Version, "0.1.1")
	}
	if got.SHA256 != wantSHA {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, wantSHA)
	}
	if !strings.HasPrefix(got.Path, stageDir) {
		t.Errorf("Path %q not under stageDir %q", got.Path, stageDir)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Errorf("staged file missing on disk: %v", err)
	}
	// The recorded SHA256 must be independently re-verifiable straight off
	// disk — this is what CP5's swap-time re-check relies on.
	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != wantSHA {
		t.Errorf("staged file content does not hash to the recorded SHA256")
	}
}

func TestStage_SHAMismatchFailsWithoutStaging(t *testing.T) {
	buildDir := t.TempDir()
	archivePath, _, err := buildTarGz(buildDir, "content")
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	checksumPath := buildChecksums(buildDir, assetName, strings.Repeat("0", 64))

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := Release{
		TagName: "v0.1.1",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}
	c := &Client{HTTPClient: &http.Client{Timeout: 5 * time.Second}, DownloadURL: srv.URL}
	stageDir := t.TempDir()

	_, err = c.Stage(context.Background(), rel, stageDir)
	if err == nil {
		t.Fatal("expected SHA mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected 'SHA256 mismatch' in error, got: %v", err)
	}
	entries, _ := os.ReadDir(stageDir)
	if len(entries) != 0 {
		t.Errorf("expected no staged file after SHA mismatch, found %v", entries)
	}
}

func TestStage_NeverTouchesCurrentBinary(t *testing.T) {
	// Stage takes no currentBinary argument at all — this test documents
	// that contract by construction (a compile-time guarantee), and
	// additionally proves stageDir is left as the sole write target.
	srv, rel, _ := stageTestServer(t, "v0.1.1", "content")
	c := &Client{HTTPClient: &http.Client{Timeout: 5 * time.Second}, DownloadURL: srv.URL}
	stageDir := t.TempDir()

	if _, err := c.Stage(context.Background(), rel, stageDir); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly 1 staged file, got %d", len(entries))
	}
}

// ---------- AcquireLock / ReleaseLock (CP4 primitive; CP5 extends) ----------

func TestAcquireLock_FreeLockAcquires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	got, ok := AcquireLock(s, now)
	if !ok {
		t.Fatal("expected acquisition on a free lock")
	}
	if !got.Update.Updating {
		t.Error("Updating not set to true")
	}
	if !got.Update.UpdateStartedAt.Equal(now) {
		t.Errorf("UpdateStartedAt = %v, want %v", got.Update.UpdateStartedAt, now)
	}
}

func TestAcquireLock_HeldLockRefuses(t *testing.T) {
	held := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = held

	now := held.Add(time.Minute)
	got, ok := AcquireLock(s, now)
	if ok {
		t.Fatal("expected refusal when lock already held")
	}
	// State must be returned unmodified on refusal.
	if !got.Update.UpdateStartedAt.Equal(held) {
		t.Errorf("UpdateStartedAt mutated on refusal: got %v, want unchanged %v", got.Update.UpdateStartedAt, held)
	}
}

func TestReleaseLock_ClearsLockFields(t *testing.T) {
	ownedSince := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = ownedSince

	got := ReleaseLock(s, ownedSince)
	if got.Update.Updating {
		t.Error("Updating still true after ReleaseLock")
	}
	if !got.Update.UpdateStartedAt.IsZero() {
		t.Errorf("UpdateStartedAt not cleared: %v", got.Update.UpdateStartedAt)
	}
}

// TestReleaseLock_OwnerMismatchLeavesLockFieldsUntouched pins the fencing
// guard: when s's on-disk UpdateStartedAt no longer equals the caller's
// recorded ownedSince token, a newer holder has taken over (or --force
// re-stamped) since — the stale caller's release must not clear that
// holder's active lock, only its own already-set non-lock fields.
func TestReleaseLock_OwnerMismatchLeavesLockFieldsUntouched(t *testing.T) {
	ownedSince := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newerHolderSince := ownedSince.Add(5 * time.Minute)

	s := State{}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = newerHolderSince
	s.Update.LastResult = "staging failed"

	got := ReleaseLock(s, ownedSince)
	if !got.Update.Updating || !got.Update.UpdateStartedAt.Equal(newerHolderSince) {
		t.Errorf("lock fields mutated on owner mismatch: Updating=%v UpdateStartedAt=%v, want Updating=true UpdateStartedAt=%v",
			got.Update.Updating, got.Update.UpdateStartedAt, newerHolderSince)
	}
	if got.Update.LastResult != "staging failed" {
		t.Errorf("LastResult = %q, want preserved even without lock ownership", got.Update.LastResult)
	}
}

// ---------- AcquireOrTakeoverLock (CP5: foreground apply lock policy) ----------

func TestAcquireOrTakeoverLock_FreeLockAcquires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	got, err := AcquireOrTakeoverLock(s, now, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Update.Updating {
		t.Error("Updating not set to true")
	}
	if !got.Update.UpdateStartedAt.Equal(now) {
		t.Errorf("UpdateStartedAt = %v, want %v", got.Update.UpdateStartedAt, now)
	}
}

func TestAcquireOrTakeoverLock_FreshLockRefusesNamingAge(t *testing.T) {
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = started
	now := started.Add(3 * time.Minute)

	got, err := AcquireOrTakeoverLock(s, now, false)
	if err == nil {
		t.Fatal("expected refusal for a lock younger than the stale threshold")
	}
	if !strings.Contains(err.Error(), "3m0s") {
		t.Errorf("expected the lock's age (3m0s) named in the error, got: %v", err)
	}
	// State must be returned unmodified on refusal.
	if !got.Update.UpdateStartedAt.Equal(started) {
		t.Errorf("UpdateStartedAt mutated on refusal: got %v, want unchanged %v", got.Update.UpdateStartedAt, started)
	}
}

func TestAcquireOrTakeoverLock_StaleLockTakenOver(t *testing.T) {
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = started
	now := started.Add(11 * time.Minute) // past the 10-minute stale threshold

	got, err := AcquireOrTakeoverLock(s, now, false)
	if err != nil {
		t.Fatalf("expected takeover of an abandoned lock, got refusal: %v", err)
	}
	if !got.Update.Updating {
		t.Error("Updating not set to true after takeover")
	}
	if !got.Update.UpdateStartedAt.Equal(now) {
		t.Errorf("UpdateStartedAt not refreshed on takeover: got %v, want %v", got.Update.UpdateStartedAt, now)
	}
}

func TestAcquireOrTakeoverLock_ForceBypassesFreshLock(t *testing.T) {
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = started
	now := started.Add(30 * time.Second) // well within the stale window

	got, err := AcquireOrTakeoverLock(s, now, true)
	if err != nil {
		t.Fatalf("--force must bypass a fresh lock: %v", err)
	}
	if !got.Update.UpdateStartedAt.Equal(now) {
		t.Errorf("force must stamp unconditionally: got %v, want %v", got.Update.UpdateStartedAt, now)
	}
}

// ---------- ApplyStaged (CP5: swap-from-staged sibling of Apply) ----------

// buildStagedArchive builds a genuine, extractable gzip-compressed tar
// archive named assetName, containing a single "atomic" file with content,
// under dir. Unlike stageTestServer's checksum-only fixtures (Stage never
// extracts what it downloads), ApplyStaged actually extracts and swaps, so
// its tests need a real archive.
func buildStagedArchive(t *testing.T, dir, assetName, content string) (archivePath, sha string) {
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
	sum := sha256.Sum256(data)
	return archivePath, hex.EncodeToString(sum[:])
}

func TestApplyStaged_SwapsWithoutDownloadingArchive(t *testing.T) {
	tag := "v0.1.1"
	assetName := fmt.Sprintf("atomic_0.1.1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	const binaryContent = "fake-atomic-binary-staged-swap"

	buildDir := t.TempDir()
	_, sha := buildStagedArchive(t, buildDir, assetName, binaryContent)
	checksumPath := buildChecksums(buildDir, assetName, sha)

	// Staged file lives in a directory the archive endpoint never serves —
	// ApplyStaged must read it straight off disk.
	stageDir := t.TempDir()
	stagedPath := filepath.Join(stageDir, assetName)
	if err := os.Rename(filepath.Join(buildDir, assetName), stagedPath); err != nil {
		t.Fatal(err)
	}

	var archiveHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		archiveHits++
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := Release{
		TagName: tag,
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}
	c := &Client{HTTPClient: &http.Client{Timeout: 5 * time.Second}, DownloadURL: srv.URL}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := c.ApplyStaged(context.Background(), rel, stagedPath, currentBin); err != nil {
		t.Fatalf("ApplyStaged: %v", err)
	}
	if archiveHits != 0 {
		t.Errorf("archive endpoint hit %d times, want 0 (ApplyStaged must never re-download the archive)", archiveHits)
	}
	got, err := os.ReadFile(currentBin)
	if err != nil {
		t.Fatalf("read swapped binary: %v", err)
	}
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

func TestApplyStaged_ChecksumMismatchLeavesBinaryUntouched(t *testing.T) {
	tag := "v0.1.1"
	assetName := fmt.Sprintf("atomic_0.1.1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	buildDir := t.TempDir()
	// Fresh checksums.txt records a value that does not match the staged
	// archive's actual content — simulates a release re-cut since staging.
	checksumPath := buildChecksums(buildDir, assetName, strings.Repeat("0", 64))

	stageDir := t.TempDir()
	stagedPath, _ := buildStagedArchive(t, stageDir, assetName, "stale-or-corrupted-content")

	mux := http.NewServeMux()
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := Release{
		TagName: tag,
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}
	c := &Client{HTTPClient: &http.Client{Timeout: 5 * time.Second}, DownloadURL: srv.URL}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := c.ApplyStaged(context.Background(), rel, stagedPath, currentBin)
	if err == nil {
		t.Fatal("expected a checksum-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected 'SHA256 mismatch' in error, got: %v", err)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "original" {
		t.Errorf("binary was replaced despite a checksum mismatch")
	}
}

func TestApplyStaged_MissingStagedFileErrorsWithoutTouchingBinary(t *testing.T) {
	tag := "v0.1.1"
	assetName := fmt.Sprintf("atomic_0.1.1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	rel := Release{
		TagName: tag,
		Assets: []Asset{
			{Name: "checksums.txt", BrowserDownloadURL: "http://unused.invalid/checksums.txt"},
		},
	}
	c := &Client{HTTPClient: &http.Client{Timeout: 2 * time.Second}}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(t.TempDir(), assetName)
	err := c.ApplyStaged(context.Background(), rel, missing, currentBin)
	if err == nil {
		t.Fatal("expected an error for a missing staged file, got nil")
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "original" {
		t.Error("binary was replaced despite a missing staged file")
	}
}
