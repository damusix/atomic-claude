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
	"sync/atomic"
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

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.json")

	now := time.Now().UTC().Truncate(time.Second)
	e := CacheEntry{
		CheckedAt:      now,
		CurrentVersion: "0.1.0",
		LatestVersion:  "0.1.1",
		NotifiedAt:     now.Add(-time.Hour),
	}
	if err := WriteCache(path, e); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}
	got, err := ReadCache(path)
	if err != nil {
		t.Fatalf("ReadCache: %v", err)
	}
	if got.CurrentVersion != "0.1.0" || got.LatestVersion != "0.1.1" {
		t.Errorf("unexpected cache contents: %+v", got)
	}
	if !got.CheckedAt.Equal(now) {
		t.Errorf("CheckedAt round-trip: got %v, want %v", got.CheckedAt, now)
	}
}

func TestCacheMissingFile(t *testing.T) {
	dir := t.TempDir()
	e, err := ReadCache(filepath.Join(dir, "noexist.json"))
	if err != nil {
		t.Fatalf("expected zero value for missing file, got error: %v", err)
	}
	if !e.CheckedAt.IsZero() {
		t.Errorf("expected zero CheckedAt for missing file")
	}
}

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

	if err := c.Apply(rel, currentBin); err != nil {
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

	err = c.Apply(rel, currentBin)
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
	if tag != "v0.1.1" {
		t.Errorf("expected tag v0.1.1, got %s", tag)
	}
}

// ---------- BackgroundCheck tests ----------

func TestBackgroundCheckCompletesWithLatest(t *testing.T) {
	releases := []Release{{TagName: "v0.2.0"}}
	srv := makeTestServer(releases)
	defer srv.Close()

	c := testClient(srv)
	cachePath := filepath.Join(t.TempDir(), "update.json")

	ctx := context.Background()
	ch := c.BackgroundCheck(ctx, cachePath, "v0.1.0", "stable")

	select {
	case res := <-ch:
		if res.Err != nil {
			t.Fatalf("unexpected error: %v", res.Err)
		}
		if res.Latest != "v0.2.0" {
			t.Errorf("expected v0.2.0, got %s", res.Latest)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BackgroundCheck did not complete within timeout")
	}
}

func TestBackgroundCheckUsesCache(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode([]Release{{TagName: "v0.3.0"}})
	}))
	defer srv.Close()

	c := testClient(srv)
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update.json")

	// write a fresh cache entry (< 1h old)
	entry := CacheEntry{
		CheckedAt:     time.Now().UTC(),
		LatestVersion: "v0.2.0",
	}
	if err := WriteCache(cachePath, entry); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	ctx := context.Background()
	ch := c.BackgroundCheck(ctx, cachePath, "v0.1.0", "stable")
	res := <-ch

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Latest != "v0.2.0" {
		t.Errorf("expected cached v0.2.0, got %s", res.Latest)
	}
	if callCount.Load() != 0 {
		t.Errorf("expected 0 HTTP calls (cache hit), got %d", callCount.Load())
	}
}

func TestBackgroundCheckStaleCache(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode([]Release{{TagName: "v0.3.0"}})
	}))
	defer srv.Close()

	c := testClient(srv)
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update.json")

	// write a stale cache entry (> 1h old)
	entry := CacheEntry{
		CheckedAt:     time.Now().UTC().Add(-2 * time.Hour),
		LatestVersion: "v0.2.0",
	}
	if err := WriteCache(cachePath, entry); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	ctx := context.Background()
	ch := c.BackgroundCheck(ctx, cachePath, "v0.1.0", "stable")
	res := <-ch

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Latest != "v0.3.0" {
		t.Errorf("expected fresh v0.3.0, got %s", res.Latest)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 HTTP call (stale cache), got %d", callCount.Load())
	}
}

// ---------- MaybeBanner tests ----------

func TestMaybeBannerSuppressedWithin24h(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update.json")

	entry := CacheEntry{
		NotifiedAt:    time.Now().UTC().Add(-23 * time.Hour),
		LatestVersion: "v0.2.0",
	}
	var buf strings.Builder
	printed := MaybeBanner(&buf, "v0.1.0", "v0.2.0", entry, cachePath, time.Now())

	if printed {
		t.Error("expected MaybeBanner to return false within 24h window")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output to writer, got: %q", buf.String())
	}
}

func TestMaybeBannerPrintedFirstTime(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update.json")

	entry := CacheEntry{
		NotifiedAt:    time.Time{}, // zero = never notified
		LatestVersion: "v0.2.0",
	}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var buf strings.Builder
	printed := MaybeBanner(&buf, "v0.1.0", "v0.2.0", entry, cachePath, now)

	if !printed {
		t.Error("expected MaybeBanner to return true on first notification")
	}
	want := "update available: v0.2.0 (current: v0.1.0). run: atomic update\n"
	if buf.String() != want {
		t.Errorf("banner text mismatch:\ngot:  %q\nwant: %q", buf.String(), want)
	}

	// notified_at must be persisted to cache
	saved, err := ReadCache(cachePath)
	if err != nil {
		t.Fatalf("ReadCache after MaybeBanner: %v", err)
	}
	if saved.NotifiedAt.IsZero() {
		t.Error("expected notified_at to be updated in cache after banner print")
	}
	if !saved.NotifiedAt.Equal(now.UTC()) {
		t.Errorf("notified_at mismatch: got %v, want %v", saved.NotifiedAt, now.UTC())
	}
}

func TestMaybeBannerNotPrintedWhenUpToDate(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update.json")

	entry := CacheEntry{
		NotifiedAt:    time.Time{},
		LatestVersion: "v0.1.0",
	}
	var buf strings.Builder
	printed := MaybeBanner(&buf, "v0.1.0", "v0.1.0", entry, cachePath, time.Now())

	if printed {
		t.Error("expected no banner when up to date")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got: %q", buf.String())
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
