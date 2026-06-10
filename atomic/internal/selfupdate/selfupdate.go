// Package selfupdate implements foreground and background self-update logic
// for the atomic binary.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.github.com/repos/damusix/atomic-claude"
	lookupTimeout  = 10 * time.Second
	cacheWindow    = time.Hour
	bannerWindow   = 24 * time.Hour
)

// displayVersion strips the leading "v" from a release tag so user-facing
// version strings match `atomic --version` (which prints version.Version
// without a "v", per goreleaser {{.Version}}).
func displayVersion(tag string) string { return strings.TrimPrefix(tag, "v") }

// Release is a minimal representation of a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// Asset is a downloadable artifact attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Result is the outcome of a background update check.
type Result struct {
	Latest string
	Err    error
}

// Client holds injectable dependencies for testability.
type Client struct {
	HTTPClient  *http.Client
	BaseURL     string // default: defaultBaseURL
	DownloadURL string // optional override for asset host base URL (tests only)
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: lookupTimeout}
}

// Lookup fetches the latest release for the given channel from GitHub.
// channel: "stable" (default) or "prerelease".
// token: optional GitHub personal access token (from GITHUB_TOKEN env).
func (c *Client) Lookup(ctx context.Context, channel, token string) (Release, error) {
	url := c.baseURL() + "/releases"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("selfupdate: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("selfupdate: lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return Release{}, fmt.Errorf("selfupdate: lookup: HTTP %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return Release{}, fmt.Errorf("selfupdate: parse releases: %w", err)
	}

	for _, r := range releases {
		if r.Draft {
			continue
		}
		if channel != "prerelease" && r.Prerelease {
			continue
		}
		return r, nil
	}
	return Release{}, fmt.Errorf("selfupdate: no suitable release found for channel %q", channel)
}

// Apply downloads the release asset matching the current OS/arch, verifies the
// SHA256 checksum, and atomically replaces currentBinary.
//
// EXDEV mitigation: the downloaded binary is staged into the same directory as
// currentBinary (not $TMPDIR) so that os.Rename is a same-filesystem operation.
// Downloads still use a system-temp working directory; only the final staged
// binary is placed next to the target.
func (c *Client) Apply(ctx context.Context, rel Release, currentBinary string) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	tag := strings.TrimPrefix(rel.TagName, "v")

	assetName := fmt.Sprintf("atomic_%s_%s_%s", tag, goos, goarch)
	var archiveExt string
	if goos == "windows" {
		archiveExt = ".zip"
	} else {
		archiveExt = ".tar.gz"
	}
	assetName += archiveExt
	checksumName := "checksums.txt"

	archiveURL := c.assetURL(rel, assetName)
	checksumURL := c.assetURL(rel, checksumName)
	if archiveURL == "" {
		return fmt.Errorf("selfupdate: no asset %q in release %s", assetName, rel.TagName)
	}
	if checksumURL == "" {
		return fmt.Errorf("selfupdate: no asset %q in release %s", checksumName, rel.TagName)
	}

	// Temp dir for downloads. Still uses os.TempDir() for the archive files.
	tmpDir, err := os.MkdirTemp(os.TempDir(), "atomic-update-")
	if err != nil {
		return fmt.Errorf("selfupdate: make tempdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := c.download(ctx, archiveURL, archivePath); err != nil {
		return fmt.Errorf("selfupdate: download archive: %w", err)
	}

	checksumPath := filepath.Join(tmpDir, checksumName)
	if err := c.download(ctx, checksumURL, checksumPath); err != nil {
		return fmt.Errorf("selfupdate: download checksums: %w", err)
	}

	// verify SHA256
	if err := verifySHA256(archivePath, checksumPath, assetName); err != nil {
		return err
	}

	// Extract binary into tmpDir first, then stage it next to the target binary.
	// Staging next to the target guarantees a same-filesystem rename (avoids EXDEV
	// when $TMPDIR is on a different mount than the install path).
	extractedBinary, err := extractBinary(archivePath, tmpDir, goos)
	if err != nil {
		return fmt.Errorf("selfupdate: extract: %w", err)
	}

	// Stage in the install directory so os.Rename is same-filesystem.
	stagedBinary := filepath.Join(filepath.Dir(currentBinary), ".atomic.new")
	// Register cleanup BEFORE the copy attempt: if renameCrossFS partially writes
	// and then errors, the staged file must not be left on disk.
	defer os.Remove(stagedBinary) //nolint:errcheck — best-effort cleanup
	if err := renameCrossFS(extractedBinary, stagedBinary); err != nil {
		return fmt.Errorf("selfupdate: stage binary: %w", err)
	}

	// Atomic replace.
	if err := os.Rename(stagedBinary, currentBinary); err != nil {
		return fmt.Errorf(
			"selfupdate: replace binary: %w\nhint: try: sudo install %s %s",
			err, stagedBinary, currentBinary,
		)
	}
	return nil
}

// renameCrossFS moves src to dst, falling back to copy+remove if they are on
// different filesystems (EXDEV). Used to move from tmpDir to the install dir.
func renameCrossFS(src, dst string) (err error) {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback: copy then remove.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()|0o111)
	if err != nil {
		return err
	}
	// Commit close error if nothing else failed.
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func (c *Client) assetURL(rel Release, name string) string {
	for _, a := range rel.Assets {
		if a.Name == name {
			if c.DownloadURL != "" {
				// strip the real host and prepend test server URL
				return c.DownloadURL + "/" + name
			}
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func (c *Client) download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// verifySHA256 checks that the SHA256 of the file at archivePath matches the
// entry in the checksums file (one line per file: "<hex>  <name>").
func verifySHA256(archivePath, checksumPath, assetName string) error {
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("selfupdate: read checksums: %w", err)
	}
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[len(fields)-1]
		// some tools emit "  filename" with leading spaces — strip
		name = strings.TrimLeft(name, "* ")
		if name == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("selfupdate: checksum for %q not found in checksums.txt", assetName)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("selfupdate: open archive for hash: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("selfupdate: hash archive: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("selfupdate: SHA256 mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// extractBinary extracts the "atomic" (or "atomic.exe" on Windows) binary
// from the archive, writes it to destDir, and returns its path.
func extractBinary(archivePath, destDir, goos string) (string, error) {
	binaryName := "atomic"
	if goos == "windows" {
		binaryName = "atomic.exe"
	}

	if strings.HasSuffix(archivePath, ".zip") {
		return extractFromZip(archivePath, destDir, binaryName)
	}
	return extractFromTarGz(archivePath, destDir, binaryName)
}

func extractFromTarGz(archivePath, destDir, binaryName string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar: %w", err)
		}
		name := filepath.Base(hdr.Name)
		if name != binaryName {
			continue
		}
		out := filepath.Join(destDir, binaryName)
		if err := writeFile(out, tr, hdr.FileInfo().Mode()); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("binary %q not found in archive", binaryName)
}

func extractFromZip(archivePath, destDir, binaryName string) (string, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name != binaryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out := filepath.Join(destDir, binaryName)
		if err := writeFile(out, rc, f.Mode()); err != nil {
			rc.Close()
			return "", err
		}
		rc.Close()
		return out, nil
	}
	return "", fmt.Errorf("binary %q not found in zip", binaryName)
}

func writeFile(dst string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o111)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

// Check looks up the latest release and reports whether an update is available.
// Returns (isNewer, latestTag, error).
func (c *Client) Check(ctx context.Context, channel, currentVersion string) (bool, string, error) {
	token := os.Getenv("GITHUB_TOKEN")
	rel, err := c.Lookup(ctx, channel, token)
	if err != nil {
		return false, "", err
	}
	newer, err := newerThan(currentVersion, rel.TagName)
	if err != nil {
		return false, displayVersion(rel.TagName), err
	}
	return newer, displayVersion(rel.TagName), nil
}

// Update performs the full foreground update: lookup + apply.
// currentBinary must be the resolved path to the running executable.
func (c *Client) Update(ctx context.Context, channel, currentVersion, currentBinary string) error {
	token := os.Getenv("GITHUB_TOKEN")
	rel, err := c.Lookup(ctx, channel, token)
	if err != nil {
		return err
	}
	newer, err := newerThan(currentVersion, rel.TagName)
	if err != nil {
		return err
	}
	if !newer {
		fmt.Printf("atomic is up to date (%s)\n", displayVersion(rel.TagName))
		return nil
	}
	if err := c.Apply(ctx, rel, currentBinary); err != nil {
		return err
	}
	fmt.Printf("updated atomic %s → %s.\n", currentVersion, displayVersion(rel.TagName))

	// Note if claude artifact bundle may be stale.
	home, err := os.UserHomeDir()
	if err == nil {
		claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
		if _, err := os.Stat(claudeMD); err == nil {
			fmt.Printf("note: artifact bundle may now be out of sync — run `atomic claude update` when you want to refresh it.\n")
		}
	}
	return nil
}

// ShouldBanner returns true when the banner should be printed:
// latest is newer than current AND notified_at is zero or older than 24h.
func ShouldBanner(entry CacheEntry, currentVersion string) bool {
	return shouldBanner(entry, currentVersion)
}

// MaybeBanner prints an update-available banner to w when ShouldBanner is true
// for the given cache entry and current version. It updates the cache entry's
// NotifiedAt on a successful print. Returns true if the banner was printed.
func MaybeBanner(w io.Writer, cur, latest string, cache CacheEntry, cachePath string, now time.Time) bool {
	if !shouldBanner(cache, cur) {
		return false
	}
	fmt.Fprintf(w, "update available: %s (current: %s). run: atomic update\n", displayVersion(latest), cur)
	cache.NotifiedAt = now.UTC()
	_ = WriteCache(cachePath, cache)
	return true
}

func shouldBanner(entry CacheEntry, currentVersion string) bool {
	newer, err := newerThan(currentVersion, entry.LatestVersion)
	if err != nil || !newer {
		return false
	}
	return entry.NotifiedAt.IsZero() || time.Since(entry.NotifiedAt) >= bannerWindow
}

// BackgroundCheck starts a goroutine that fetches the latest release.
// It returns a channel that will receive exactly one Result when done.
// If the cache is younger than cacheWindow, no HTTP call is made and the
// cached latest version is returned directly.
func (c *Client) BackgroundCheck(ctx context.Context, cachePath, currentVersion, channel string) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		// try cache first
		entry, err := ReadCache(cachePath)
		if err == nil && !entry.CheckedAt.IsZero() && time.Since(entry.CheckedAt) < cacheWindow {
			ch <- Result{Latest: entry.LatestVersion}
			return
		}

		token := os.Getenv("GITHUB_TOKEN")
		rel, err := c.Lookup(ctx, channel, token)
		if err != nil {
			ch <- Result{Err: err}
			return
		}

		// update cache (best-effort)
		newEntry := CacheEntry{
			CheckedAt:      time.Now().UTC(),
			CurrentVersion: currentVersion,
			LatestVersion:  rel.TagName,
			NotifiedAt:     entry.NotifiedAt,
		}
		_ = WriteCache(cachePath, newEntry)

		ch <- Result{Latest: rel.TagName}
	}()
	return ch
}
