package wiki

// A bucket is a named, content-tracked folder under a wiki. Its manifest
// directory wiki/.buckets/<name>/ holds three SHA-256 walks:
//
//	current   — the walk at the last diff or promote. A debugging artifact
//	            only: nothing ever reads it back as state.
//	baseline  — what diffs compare against, written by promote.
//	previous  — the baseline promote displaced, from the second promote on.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// osJunk base names are excluded from a bucket walk at any depth.
var osJunk = map[string]bool{
	".DS_Store": true,
	"Thumbs.db": true,
}

// BucketDiffResult holds three disjoint sets of sorted bucket-relative paths.
type BucketDiffResult struct {
	Added   []string // present in current, absent in baseline
	Changed []string // present in both but hash differs
	Removed []string // present in baseline, absent in current
}

// WalkBucket fingerprints dir as sorted "<relpath>\t<sha256hex>" lines, with
// forward-slash paths. It skips the bucket-root index.md (which the tool
// generates), osJunk base names, and skipDirs subtrees.
func WalkBucket(dir string) ([]string, error) {
	var entries []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if rel == "." {
				return nil
			}
			base := d.Name()
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}

		base := d.Name()
		if osJunk[base] {
			return nil
		}
		if rel == "index.md" {
			return nil
		}

		hash, err := sha256File(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}

		entries = append(entries, rel+"\t"+hash)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk bucket %s: %w", dir, err)
	}

	sort.Strings(entries)
	return entries, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func manifestDir(wikiDir, name string) string {
	return filepath.Join(wikiDir, ".buckets", name)
}

// validateBucketName requires a safe single path segment: the name becomes a
// directory component in three places. It backs up the CLI scanner's own
// dash-token rejection and covers programmatic callers that bypass the CLI.
func validateBucketName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("bucket: name must not be empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("bucket: name %q must not begin with %q", name, "-")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("bucket: name %q must not contain a path separator", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("bucket: name %q is not a valid path segment", name)
	}
	if name == "wiki" {
		return fmt.Errorf("bucket: name %q is reserved", name)
	}
	return nil
}

// RegisterBucket creates the manifest directory, refusing an unsafe name or a
// double-register. Callers rely on it to gate their own mutations.
func RegisterBucket(wikiDir, name string) error {
	if err := validateBucketName(name); err != nil {
		return err
	}

	mdir := manifestDir(wikiDir, name)

	if _, err := os.Lstat(mdir); err == nil {
		return fmt.Errorf("bucket: %q is already registered (manifest dir %s exists)", name, mdir)
	}

	if err := os.MkdirAll(mdir, 0o755); err != nil {
		return fmt.Errorf("bucket: create manifest dir %s: %w", mdir, err)
	}
	return nil
}

// BucketDiff diffs dir against the baseline and records the walk as `current`.
// With no baseline yet, every walked file reports as Added. The bucket must
// already be registered.
func BucketDiff(wikiDir, name, dir string) (BucketDiffResult, error) {
	mdir := manifestDir(wikiDir, name)
	if _, err := os.Lstat(mdir); err != nil {
		return BucketDiffResult{}, fmt.Errorf("bucket: %q is not registered (manifest dir absent)", name)
	}

	current, err := WalkBucket(dir)
	if err != nil {
		return BucketDiffResult{}, err
	}

	currentPath := filepath.Join(mdir, "current")
	if err := os.WriteFile(currentPath, []byte(strings.Join(current, "\n")+"\n"), 0o644); err != nil {
		return BucketDiffResult{}, fmt.Errorf("bucket: write current: %w", err)
	}

	baseline, err := readManifest(filepath.Join(mdir, "baseline"))
	if err != nil {
		return BucketDiffResult{}, err
	}

	return computeDiff(baseline, current), nil
}

// BucketDiffReadOnly is BucketDiff without the `current` write, for status
// consumers such as atomic serve's nav-tree badges.
func BucketDiffReadOnly(wikiDir, name, dir string) (BucketDiffResult, error) {
	return bucketDiffReadOnly(wikiDir, name, dir)
}

func bucketDiffReadOnly(wikiDir, name, dir string) (BucketDiffResult, error) {
	mdir := manifestDir(wikiDir, name)
	if _, err := os.Lstat(mdir); err != nil {
		return BucketDiffResult{}, fmt.Errorf("bucket: %q is not registered (manifest dir absent)", name)
	}

	current, err := WalkBucket(dir)
	if err != nil {
		return BucketDiffResult{}, err
	}

	baseline, err := readManifest(filepath.Join(mdir, "baseline"))
	if err != nil {
		return BucketDiffResult{}, err
	}

	return computeDiff(baseline, current), nil
}

// PromoteBucket rotates baseline into previous and a fresh walk into baseline;
// the first promote writes no previous. It always re-walks the live directory
// rather than trusting the stored `current`.
func PromoteBucket(wikiDir, name, dir string) error {
	mdir := manifestDir(wikiDir, name)
	if _, err := os.Lstat(mdir); err != nil {
		return fmt.Errorf("bucket: %q is not registered", name)
	}

	fresh, err := WalkBucket(dir)
	if err != nil {
		return err
	}

	currentPath := filepath.Join(mdir, "current")
	baselinePath := filepath.Join(mdir, "baseline")
	previousPath := filepath.Join(mdir, "previous")

	freshData := []byte(strings.Join(fresh, "\n") + "\n")

	if err := os.WriteFile(currentPath, freshData, 0o644); err != nil {
		return fmt.Errorf("bucket: promote: write current: %w", err)
	}

	if existingBaseline, err := os.ReadFile(baselinePath); err == nil {
		if err := os.WriteFile(previousPath, existingBaseline, 0o644); err != nil {
			return fmt.Errorf("bucket: promote: write previous: %w", err)
		}
	}

	if err := os.WriteFile(baselinePath, freshData, 0o644); err != nil {
		return fmt.Errorf("bucket: promote: write baseline: %w", err)
	}

	return nil
}

// readManifest returns nil, not an error, for a missing file: an absent
// baseline is an empty one.
func readManifest(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("bucket: read manifest %s: %w", path, err)
	}

	raw := strings.TrimRight(string(data), "\n")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// computeDiff takes WalkBucket-shaped lines on both sides.
func computeDiff(baseline, current []string) BucketDiffResult {
	baseMap := parseManifest(baseline)
	currMap := parseManifest(current)

	var added, changed, removed []string

	for path, hash := range currMap {
		if baseHash, ok := baseMap[path]; !ok {
			added = append(added, path)
		} else if baseHash != hash {
			changed = append(changed, path)
		}
	}
	for path := range baseMap {
		if _, ok := currMap[path]; !ok {
			removed = append(removed, path)
		}
	}

	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)

	return BucketDiffResult{
		Added:   added,
		Changed: changed,
		Removed: removed,
	}
}

// parseManifest indexes manifest lines by path, dropping malformed ones.
func parseManifest(lines []string) map[string]string {
	m := make(map[string]string, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
