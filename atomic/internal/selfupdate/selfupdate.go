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
)

// BannerWindow is the minimum interval between repeated update-available
// banners for the same running version. The parent fast path renders the
// banner from state.json alone and never re-notifies within this window.
const BannerWindow = 24 * time.Hour

// displayVersion strips the leading "v" from a release tag so user-facing
// version strings match `atomic --version` (which prints version.Version
// without a "v", per goreleaser {{.Version}}).
func displayVersion(tag string) string { return strings.TrimPrefix(tag, "v") }

// DisplayVersion is the exported form of displayVersion, for callers outside
// this package that must normalize a raw release tag (no leading "v") before
// it reaches state.json or a banner — e.g. the check-branch state write and
// the parent fast path's banner render.
func DisplayVersion(tag string) string { return displayVersion(tag) }

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

// ApplyStaged swaps currentBinary using an already-downloaded archive at
// stagedPath — never re-downloading the archive itself — after
// re-verifying its SHA256 against a freshly fetched checksums.txt for rel.
// The staged archive may have been downloaded against an EARLIER release
// cut (the release can be re-published since staging), so this never
// trusts the staged record's own recorded checksum: the expected hash is
// re-derived from rel here. Returns an error, without touching
// currentBinary, on a missing/unreadable staged file or any checksum
// mismatch — callers fall back to Apply's full download flow in that case.
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
	if err := c.download(ctx, checksumURL, checksumPath); err != nil {
		return fmt.Errorf("selfupdate: download checksums: %w", err)
	}

	if err := verifySHA256(stagedPath, checksumPath, assetName); err != nil {
		return err
	}

	extractedBinary, err := extractBinary(stagedPath, tmpDir, goos)
	if err != nil {
		return fmt.Errorf("selfupdate: extract: %w", err)
	}

	// Stage in the install directory so os.Rename is same-filesystem —
	// mirrors Apply's own EXDEV mitigation.
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

// StageDir returns the directory where background staging downloads land:
// ~/.cache/atomic/staged/. home must be the caller's already-resolved home
// directory — this never calls os.UserHomeDir() itself — so tests (and any
// future caller) can point staging at a temp directory without mutating the
// process environment.
func StageDir(home string) string {
	return filepath.Join(home, ".cache", "atomic", "staged")
}

// Stage downloads the release archive for the current OS/arch, verifies its
// SHA256 against checksums.txt, and moves the checksum-verified archive into
// stageDir — never swapping the running binary. The archive (not the
// extracted binary) is what gets staged: its recorded SHA256 is the same
// value carried in the release's checksums.txt, so a later swap can
// re-verify the staged file against a fresh checksums.txt without any
// format translation, and extract only once it decides to use it.
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
	if err := c.download(ctx, archiveURL, archivePath); err != nil {
		return StagedInfo{}, fmt.Errorf("selfupdate: download archive: %w", err)
	}

	checksumPath := filepath.Join(tmpDir, checksumName)
	if err := c.download(ctx, checksumURL, checksumPath); err != nil {
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

	got, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("selfupdate: hash archive: %w", err)
	}
	if got != expected {
		return fmt.Errorf("selfupdate: SHA256 mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// sha256File returns the lowercase hex SHA256 digest of the file at path.
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

// IsNewer reports whether latest is a newer semver than current, wrapping
// the internal newerThan. A malformed or empty latest reports false — the
// parent's state-only banner decision must never error, only skip.
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

// ShouldNotify reports whether the parent fast path should render the
// update-available banner: latest is newer than current, AND lastNotified is
// zero or older than BannerWindow.
func ShouldNotify(current, latest string, lastNotified, now time.Time) bool {
	if !IsNewer(current, latest) {
		return false
	}
	return lastNotified.IsZero() || now.Sub(lastNotified) >= BannerWindow
}

// AcquireLock attempts to acquire the update lock on s — the simple
// acquire-when-free primitive used by background staging (CP4), setting
// Updating=true and UpdateStartedAt=now. Returns the updated state and
// whether the lock was acquired — false, with s returned unmodified, when
// s.Update.Updating is already true. The foreground apply path (CP5) uses
// AcquireOrTakeoverLock instead, which adds stale-lock takeover, refusal
// messaging naming the lock's age, and --force.
func AcquireLock(s State, now time.Time) (State, bool) {
	if s.Update.Updating {
		return s, false
	}
	s.Update.Updating = true
	s.Update.UpdateStartedAt = now
	return s, true
}

// ReleaseLock clears the update lock fields on s only if s's own
// UpdateStartedAt still equals ownedSince — the fencing token the caller
// recorded when it acquired the lock. Callers must pass a freshly-loaded s
// (LoadState called immediately before release) so the comparison reflects
// the current on-disk owner rather than the caller's own stale in-memory
// copy — otherwise the mismatch this guards against can never be observed.
// A mismatch means a newer holder has since taken over (stale-lock takeover
// or --force); s's lock fields are then left untouched so that holder's
// active lock survives, while any other field the caller already set on s
// (e.g. staged, last_result, stage_attempted_for) is still returned as
// given.
func ReleaseLock(s State, ownedSince time.Time) State {
	if !s.Update.UpdateStartedAt.Equal(ownedSince) {
		return s
	}
	s.Update.Updating = false
	s.Update.UpdateStartedAt = time.Time{}
	return s
}

// StaleLockAfter is the age at which a held update lock is considered
// abandoned by a crashed or killed updater and may be taken over by a later
// `atomic update` invocation.
const StaleLockAfter = 10 * time.Minute

// AcquireOrTakeoverLock implements the foreground apply lock policy (spec
// Flow "staged fast-path swap", step 2, and Flow "--force"): a free lock is
// always acquired; a held lock younger than StaleLockAfter is refused with
// an error naming its age; a held lock at or past that age is considered
// abandoned and taken over. force bypasses this check entirely and
// (re)acquires unconditionally, regardless of the current lock's age or
// presence — it never weakens the swap's own checksum verification, which
// happens downstream of locking. On any acquisition (free, takeover, or
// forced) Updating is set true and UpdateStartedAt is stamped to now; on
// refusal s is returned unmodified.
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
