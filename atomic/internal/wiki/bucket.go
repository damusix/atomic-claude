package wiki

// bucket.go — CP1 bucket manifest core.
//
// A bucket is a named, content-tracked folder under a wiki.  The manifest
// directory wiki/.buckets/<name>/ holds three files that record the history of
// what the folder contained:
//
//   current   — written by every BucketDiff and PromoteBucket call; the
//               SHA-256 walk of the folder at the time of that call.
//               Debugging artifact only — never read back as state.
//   baseline  — written by PromoteBucket; the snapshot that Diff compares
//               against.  On the first promote: fresh walk → baseline.
//   previous  — written by PromoteBucket on the second and subsequent
//               promotes: the old baseline slides into previous.
//
// Exported API (for CP2 CLI wiring):
//
//   WalkBucket(dir string) ([]string, error)
//   RegisterBucket(wikiDir, name string) error
//   BucketDiff(wikiDir, name, dir string) (BucketDiffResult, error)
//   PromoteBucket(wikiDir, name, dir string) error

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

// osJunk is the set of file base names that are always excluded from a bucket
// walk regardless of directory position.
var osJunk = map[string]bool{
	".DS_Store": true,
	"Thumbs.db": true,
}

// BucketDiffResult holds the three disjoint change sets returned by BucketDiff.
// Each slice contains paths relative to the bucket root, sorted.
type BucketDiffResult struct {
	Added   []string // present in current, absent in baseline
	Changed []string // present in both but hash differs
	Removed []string // present in baseline, absent in current
}

// WalkBucket computes a sorted content fingerprint for dir.
//
// Each returned entry is formatted as "<relpath>\t<sha256hex>" where relpath
// uses forward slashes and is relative to dir.
//
// Exclusions:
//   - The bucket-root index.md (relpath == "index.md").
//   - Files whose base name is in osJunk (.DS_Store, Thumbs.db).
//   - Entire subtrees whose directory base name is in skipDirs (the same set
//     used by the wiki discovery walk).
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
		// Normalise to forward slashes for stable cross-platform entries.
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

		// File exclusions.
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

// sha256File returns the lowercase hex SHA-256 of the file at path.
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

// manifestDir returns the path to wiki/.buckets/<name>/.
func manifestDir(wikiDir, name string) string {
	return filepath.Join(wikiDir, ".buckets", name)
}

// RegisterBucket creates the manifest directory wiki/.buckets/<name>/ and
// validates the registration constraints:
//
//   - name "wiki" is refused (reserved).
//   - Re-registering an already-registered bucket is refused.
func RegisterBucket(wikiDir, name string) error {
	if name == "wiki" {
		return fmt.Errorf("bucket: name %q is reserved", name)
	}

	mdir := manifestDir(wikiDir, name)

	if _, err := os.Lstat(mdir); err == nil {
		// Manifest dir already exists → double-register.
		return fmt.Errorf("bucket: %q is already registered (manifest dir %s exists)", name, mdir)
	}

	if err := os.MkdirAll(mdir, 0o755); err != nil {
		return fmt.Errorf("bucket: create manifest dir %s: %w", mdir, err)
	}
	return nil
}

// BucketDiff walks dir, writes the result as wiki/.buckets/<name>/current, and
// returns the three-way diff against the current baseline.
//
// If no baseline exists (first call after RegisterBucket), all walked files are
// reported as Added.
//
// The bucket must already be registered (manifest dir must exist); if it is
// not, BucketDiff returns an error.
func BucketDiff(wikiDir, name, dir string) (BucketDiffResult, error) {
	mdir := manifestDir(wikiDir, name)
	if _, err := os.Lstat(mdir); err != nil {
		return BucketDiffResult{}, fmt.Errorf("bucket: %q is not registered (manifest dir absent)", name)
	}

	// Walk the bucket folder.
	current, err := WalkBucket(dir)
	if err != nil {
		return BucketDiffResult{}, err
	}

	// Write current manifest.
	currentPath := filepath.Join(mdir, "current")
	if err := os.WriteFile(currentPath, []byte(strings.Join(current, "\n")+"\n"), 0o644); err != nil {
		return BucketDiffResult{}, fmt.Errorf("bucket: write current: %w", err)
	}

	// Read baseline (may not exist on first run).
	baseline, err := readManifest(filepath.Join(mdir, "baseline"))
	if err != nil {
		return BucketDiffResult{}, err
	}

	return computeDiff(baseline, current), nil
}

// bucketDiffReadOnly computes the diff between the live directory state and the
// stored baseline WITHOUT writing the current manifest file.  It is the
// read-only counterpart of BucketDiff: identical semantics except the
// wiki/.buckets/<name>/current file is never created or modified.
// Used by list, which is a status verb and must have no side effects.
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

// PromoteBucket recomputes the manifest walk for dir live, writes it to
// wiki/.buckets/<name>/current, then rotates the manifests:
//
//   - First promote: fresh walk → baseline; previous is NOT written.
//   - Subsequent promotes: baseline → previous, fresh walk → baseline.
//
// The bucket must be registered; if it is not, PromoteBucket returns an error.
// Unlike BucketDiff, PromoteBucket never reads a previously written current
// file — it always recomputes from the live directory state.
func PromoteBucket(wikiDir, name, dir string) error {
	mdir := manifestDir(wikiDir, name)
	if _, err := os.Lstat(mdir); err != nil {
		return fmt.Errorf("bucket: %q is not registered", name)
	}

	// Recompute the walk live — never read the previously written current.
	fresh, err := WalkBucket(dir)
	if err != nil {
		return err
	}

	currentPath := filepath.Join(mdir, "current")
	baselinePath := filepath.Join(mdir, "baseline")
	previousPath := filepath.Join(mdir, "previous")

	freshData := []byte(strings.Join(fresh, "\n") + "\n")

	// Write fresh manifest to current (debugging artifact).
	if err := os.WriteFile(currentPath, freshData, 0o644); err != nil {
		return fmt.Errorf("bucket: promote: write current: %w", err)
	}

	// If baseline already exists, slide it into previous.
	if existingBaseline, err := os.ReadFile(baselinePath); err == nil {
		if err := os.WriteFile(previousPath, existingBaseline, 0o644); err != nil {
			return fmt.Errorf("bucket: promote: write previous: %w", err)
		}
	}

	// Promote fresh manifest → baseline.
	if err := os.WriteFile(baselinePath, freshData, 0o644); err != nil {
		return fmt.Errorf("bucket: promote: write baseline: %w", err)
	}

	return nil
}

// readManifest reads a manifest file and returns its lines (one per entry).
// Returns nil (not an error) when the file does not exist — treated as empty
// baseline.
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

// computeDiff compares baseline entries against current entries and returns the
// three-way diff.  Both slices must be sorted "<relpath>\t<sha256hex>" lines.
func computeDiff(baseline, current []string) BucketDiffResult {
	// Build hash maps for O(1) lookup.
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

// parseManifest converts sorted "<relpath>\t<sha256hex>" lines into a map.
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
