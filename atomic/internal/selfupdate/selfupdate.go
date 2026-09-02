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
	"sync/atomic"
	"time"
)

const (
	defaultBaseURL = "https://api.github.com/repos/damusix/atomic-claude"
	lookupTimeout  = 10 * time.Second

	// defaultStallTimeout aborts a download only when no bytes arrive for this
	// long; a slow-but-moving stream never trips it.
	defaultStallTimeout = 30 * time.Second

	progressEmitBytes = 512 * 1024

	// lookupPerPage is GitHub's maximum page size. The default of 30 let a run
	// of prereleases push the newest stable release off the first page, which
	// would strand the stable channel with "no suitable release found".
	lookupPerPage  = 100
	lookupMaxPages = 5
)

// Release channels. Stable takes only full releases and only ever moves
// forward. Prerelease tracks the tip of the pre-release branch.
const (
	ChannelStable     = "stable"
	ChannelPrerelease = "prerelease"
)

// ValidChannel reports whether s names a release channel.
func ValidChannel(s string) bool {
	return s == ChannelStable || s == ChannelPrerelease
}

// BannerWindow is the minimum interval between repeated update-available
// banners for the same running version. The parent fast path renders the
// banner from state.json alone and never re-notifies within this window.
const BannerWindow = 24 * time.Hour

// displayVersion strips a tag's leading "v" so version strings match
// `atomic --version`, which prints goreleaser's {{.Version}} without one.
func displayVersion(tag string) string { return strings.TrimPrefix(tag, "v") }

// DisplayVersion normalizes a raw release tag for callers outside this package,
// before it reaches state.json or a banner.
func DisplayVersion(tag string) string { return displayVersion(tag) }

// Release is a minimal representation of a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
	// PublishedAt orders the prerelease channel. The list endpoint sorts by
	// created_at, which is the tag's commit date, so it is not a reliable
	// stand-in for when a release actually went out.
	PublishedAt time.Time `json:"published_at"`
}

// Asset is a downloadable artifact attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Client holds injectable dependencies for testability.
type Client struct {
	HTTPClient  *http.Client
	BaseURL     string // default: defaultBaseURL
	DownloadURL string // optional override for asset host base URL (tests only)

	// OnProgress, when set, receives byte counts while a release archive
	// streams down. total is the Content-Length, or equal to received on the
	// final call when the server sent none. Checksum fetches never report.
	OnProgress func(received, total int64)

	// StallTimeout overrides the no-data abort window (default 30s).
	StallTimeout time.Duration
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

// downloadClient has no whole-request Timeout: http.Client.Timeout caps the
// entire body read, which killed archive downloads on slow links at 10s. The
// stall watchdog in download governs instead.
func (c *Client) downloadClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{}
}

// eligible reports whether r is a candidate on channel. The stable channel
// takes only full releases; the prerelease channel takes either, so a stable
// release still graduates a prerelease user.
func eligible(r Release, channel string) bool {
	if r.Draft {
		return false
	}
	return channel == ChannelPrerelease || !r.Prerelease
}

// Lookup fetches the GitHub release each channel considers current. token is an
// optional GitHub personal access token.
//
// The two channels select differently because they mean different things.
// Stable is a forward-only ladder, so it takes the highest version: the list
// endpoint orders by commit date rather than version, and an interleaved hotfix
// published after a higher release would otherwise win. Prerelease tracks the
// tip of the pre-release branch, so it takes the most recently published
// release — selecting by version there would pin it to the newest full release
// forever, since a full release outranks every prerelease of the same core, and
// that is the stranding the channel exists to avoid.
//
// Paging continues past a page with no eligible entry so a run of prereleases
// can never bury the newest stable release out of reach of the stable channel.
func (c *Client) Lookup(ctx context.Context, channel, token string) (Release, error) {
	var (
		best        Release
		bestVer     semver
		found       bool
		unparseable int
	)
	tracking := channel == ChannelPrerelease
	for page := 1; page <= lookupMaxPages; page++ {
		releases, err := c.fetchReleasePage(ctx, page, token)
		if err != nil {
			return Release{}, err
		}
		for _, r := range releases {
			if !eligible(r, channel) {
				continue
			}
			if tracking {
				// Strictly-after keeps the earliest-listed release on ties,
				// which is GitHub's own recency order when published_at is
				// absent or equal.
				if !found || r.PublishedAt.After(best.PublishedAt) {
					best, found = r, true
				}
				continue
			}
			v, err := parseSemver(r.TagName)
			if err != nil {
				unparseable++
				continue
			}
			if !found || v.compare(bestVer) > 0 {
				best, bestVer, found = r, v, true
			}
		}
		if found || len(releases) < lookupPerPage {
			break
		}
	}
	if !found {
		if unparseable > 0 {
			return Release{}, fmt.Errorf("selfupdate: no suitable release found for channel %q (%d eligible release(s) skipped: unparseable tag)", channel, unparseable)
		}
		return Release{}, fmt.Errorf("selfupdate: no suitable release found for channel %q", channel)
	}
	return best, nil
}

func (c *Client) fetchReleasePage(ctx context.Context, page int, token string) ([]Release, error) {
	url := fmt.Sprintf("%s/releases?per_page=%d&page=%d", c.baseURL(), lookupPerPage, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("selfupdate: lookup: HTTP %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("selfupdate: parse releases: %w", err)
	}
	return releases, nil
}

// Apply downloads the release asset for the current OS/arch, verifies its
// SHA256, and atomically replaces currentBinary.
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

	tmpDir, err := os.MkdirTemp(os.TempDir(), "atomic-update-")
	if err != nil {
		return fmt.Errorf("selfupdate: make tempdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := c.download(ctx, archiveURL, archivePath, c.OnProgress); err != nil {
		return fmt.Errorf("selfupdate: download archive: %w", err)
	}

	checksumPath := filepath.Join(tmpDir, checksumName)
	if err := c.download(ctx, checksumURL, checksumPath, nil); err != nil {
		return fmt.Errorf("selfupdate: download checksums: %w", err)
	}

	if err := verifySHA256(archivePath, checksumPath, assetName); err != nil {
		return err
	}

	extractedBinary, err := extractBinary(archivePath, tmpDir, goos)
	if err != nil {
		return fmt.Errorf("selfupdate: extract: %w", err)
	}

	// Stage beside the target, not in $TMPDIR, so the final rename is
	// same-filesystem and cannot fail with EXDEV.
	stagedBinary := filepath.Join(filepath.Dir(currentBinary), ".atomic.new")
	// Registered before the copy: a partial write must not survive an error.
	defer os.Remove(stagedBinary) //nolint:errcheck — best-effort cleanup
	if err := renameCrossFS(extractedBinary, stagedBinary); err != nil {
		return fmt.Errorf("selfupdate: stage binary: %w", err)
	}

	if err := os.Rename(stagedBinary, currentBinary); err != nil {
		return fmt.Errorf(
			"selfupdate: replace binary: %w\nhint: try: sudo install %s %s",
			err, stagedBinary, currentBinary,
		)
	}
	return nil
}

// ApplyStaged swaps currentBinary from an already-downloaded archive without
// re-downloading it. The staged record's own checksum is never trusted — a
// release can be re-published after staging — so the expected hash is re-derived
// from a freshly fetched checksums.txt. Any mismatch errors with currentBinary
// untouched, and callers fall back to Apply's full download.
func (c *Client) ApplyStaged(ctx context.Context, rel Release, stagedPath, currentBinary string) error {
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

	checksumURL := c.assetURL(rel, checksumName)
	if checksumURL == "" {
		return fmt.Errorf("selfupdate: no asset %q in release %s", checksumName, rel.TagName)
	}

	if _, err := os.Stat(stagedPath); err != nil {
		return fmt.Errorf("selfupdate: staged archive unreadable: %w", err)
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "atomic-swap-")
	if err != nil {
		return fmt.Errorf("selfupdate: make tempdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	checksumPath := filepath.Join(tmpDir, checksumName)
	if err := c.download(ctx, checksumURL, checksumPath, nil); err != nil {
		return fmt.Errorf("selfupdate: download checksums: %w", err)
	}

	if err := verifySHA256(stagedPath, checksumPath, assetName); err != nil {
		return err
	}

	extractedBinary, err := extractBinary(stagedPath, tmpDir, goos)
	if err != nil {
		return fmt.Errorf("selfupdate: extract: %w", err)
	}

	// Same EXDEV mitigation as Apply.
	stagedBinary := filepath.Join(filepath.Dir(currentBinary), ".atomic.new")
	defer os.Remove(stagedBinary) //nolint:errcheck — best-effort cleanup
	if err := renameCrossFS(extractedBinary, stagedBinary); err != nil {
		return fmt.Errorf("selfupdate: stage binary: %w", err)
	}

	if err := os.Rename(stagedBinary, currentBinary); err != nil {
		return fmt.Errorf(
			"selfupdate: replace binary: %w\nhint: try: sudo install %s %s",
			err, stagedBinary, currentBinary,
		)
	}
	return nil
}

// renameCrossFS moves src to dst, falling back to a copy when they sit on
// different filesystems (EXDEV).
func renameCrossFS(src, dst string) (err error) {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
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
	// A close error still fails the copy if nothing else did.
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

// StageDir returns ~/.cache/atomic/staged/. home must already be resolved by
// the caller so tests can redirect staging without mutating the environment.
func StageDir(home string) string {
	return filepath.Join(home, ".cache", "atomic", "staged")
}

// Stage downloads and checksum-verifies the release archive into stageDir,
// never swapping the running binary. The archive is staged rather than the
// extracted binary so a later swap can re-verify it against a fresh
// checksums.txt with no format translation.
func (c *Client) Stage(ctx context.Context, rel Release, stageDir string) (StagedInfo, error) {
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
		return StagedInfo{}, fmt.Errorf("selfupdate: no asset %q in release %s", assetName, rel.TagName)
	}
	if checksumURL == "" {
		return StagedInfo{}, fmt.Errorf("selfupdate: no asset %q in release %s", checksumName, rel.TagName)
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "atomic-stage-")
	if err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: make tempdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := c.download(ctx, archiveURL, archivePath, c.OnProgress); err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: download archive: %w", err)
	}

	checksumPath := filepath.Join(tmpDir, checksumName)
	if err := c.download(ctx, checksumURL, checksumPath, nil); err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: download checksums: %w", err)
	}

	if err := verifySHA256(archivePath, checksumPath, assetName); err != nil {
		return StagedInfo{}, err
	}

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: mkdir stage dir: %w", err)
	}
	stagedPath := filepath.Join(stageDir, assetName)
	if err := renameCrossFS(archivePath, stagedPath); err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: move staged archive: %w", err)
	}

	sha, err := sha256File(stagedPath)
	if err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: hash staged archive: %w", err)
	}

	return StagedInfo{Version: tag, Path: stagedPath, SHA256: sha}, nil
}

func (c *Client) assetURL(rel Release, name string) string {
	for _, a := range rel.Assets {
		if a.Name == name {
			if c.DownloadURL != "" {
				return c.DownloadURL + "/" + name
			}
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func (c *Client) download(ctx context.Context, url, dst string, onProgress func(received, total int64)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.downloadClient().Do(req)
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

	stall := c.StallTimeout
	if stall <= 0 {
		stall = defaultStallTimeout
	}
	var stalled atomic.Bool
	watchdog := time.AfterFunc(stall, func() {
		stalled.Store(true)
		cancel()
	})
	defer watchdog.Stop()

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	var received, sinceEmit int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(stall)
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			received += int64(n)
			sinceEmit += int64(n)
			if onProgress != nil && sinceEmit >= progressEmitBytes {
				sinceEmit = 0
				onProgress(received, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if stalled.Load() {
				return fmt.Errorf("download stalled: no data for %s", stall)
			}
			return rerr
		}
	}
	if onProgress != nil {
		if total <= 0 {
			total = received
		}
		onProgress(received, total)
	}
	return nil
}

// verifySHA256 checks archivePath against its entry in a checksums file, whose
// lines are "<hex>  <name>".
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
		// Some tools prefix the name with "*" or spaces.
		name = strings.TrimLeft(name, "* ")
		if name == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("selfupdate: checksum for %q not found in checksums.txt", assetName)
	}

	got, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("selfupdate: hash archive: %w", err)
	}
	if got != expected {
		return fmt.Errorf("selfupdate: SHA256 mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("selfupdate: open for hash: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("selfupdate: hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary writes the atomic binary from the archive into destDir.
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

// Check reports whether an update is available on channel, with the latest tag.
func (c *Client) Check(ctx context.Context, channel, currentVersion string) (bool, string, error) {
	token := os.Getenv("GITHUB_TOKEN")
	rel, err := c.Lookup(ctx, channel, token)
	if err != nil {
		return false, "", err
	}
	return ShouldInstall(channel, currentVersion, rel.TagName), displayVersion(rel.TagName), nil
}

// IsNewer reports whether latest is a newer semver than current. A malformed or
// empty latest reports false: the parent's banner decision must skip, not error.
func IsNewer(current, latest string) bool {
	if latest == "" {
		return false
	}
	newer, err := newerThan(current, latest)
	if err != nil {
		return false
	}
	return newer
}

// ShouldInstall reports whether latest warrants replacing current on channel.
//
// Stable only ever moves forward. Prerelease tracks the tip of the pre-release
// branch, so it also accepts a tag that is semver-lower than what is running: a
// prerelease cut after a stable release of the same core carries newer code
// with a lower version, and a forward-only gate would leave the channel dark
// until the core advanced again.
func ShouldInstall(channel, current, latest string) bool {
	if latest == "" {
		return false
	}
	if channel == ChannelPrerelease {
		return displayVersion(latest) != displayVersion(current)
	}
	return IsNewer(current, latest)
}

// ShouldNotify reports whether to render the update-available banner: latest is
// installable on channel, and lastNotified is zero or older than BannerWindow.
func ShouldNotify(channel, current, latest string, lastNotified, now time.Time) bool {
	if !ShouldInstall(channel, current, latest) {
		return false
	}
	return lastNotified.IsZero() || now.Sub(lastNotified) >= BannerWindow
}

// AcquireLock takes the update lock only when it is free; background staging
// uses it. The foreground apply path needs AcquireOrTakeoverLock instead, which
// adds stale-lock takeover and --force.
func AcquireLock(s State, now time.Time) (State, bool) {
	if s.Update.Updating {
		return s, false
	}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = now
	return s, true
}

// ReleaseLock clears the lock only if UpdateStartedAt still equals ownedSince,
// the fencing token from acquisition. Callers must pass a freshly-loaded s, or
// the comparison reads their own stale copy and the guard never fires. On
// mismatch a newer holder has taken over, so its lock survives untouched while
// every other field the caller set on s is still returned.
func ReleaseLock(s State, ownedSince time.Time) State {
	if !s.Update.UpdateStartedAt.Equal(ownedSince) {
		return s
	}
	s.Update.Updating = false
	s.Update.UpdateStartedAt = time.Time{}
	return s
}

// StaleLockAfter is the age at which a held lock is treated as abandoned by a
// crashed updater and may be taken over.
const StaleLockAfter = 10 * time.Minute

// AcquireOrTakeoverLock is the foreground apply lock policy: acquire when free,
// refuse a lock younger than StaleLockAfter with an error naming its age, take
// over one at or past that age. force skips the check entirely; it never
// weakens the swap's checksum verification, which happens downstream.
func AcquireOrTakeoverLock(s State, now time.Time, force bool) (State, error) {
	if !force && s.Update.Updating {
		age := now.Sub(s.Update.UpdateStartedAt)
		if age < StaleLockAfter {
			return s, fmt.Errorf("an update is already in progress (started %s ago); use --force to override", age.Round(time.Second))
		}
	}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = now
	return s, nil
}
