// The external-link registry behind /api/external: every outbound http(s) URL
// in the realm's markdown, with the pages citing it and the earliest date any
// of them was added. The date comes from the FileDateFn seam, so tests can
// supply known dates without touching disk or git.
package serve

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// FileDateFn dates one file. Tests inject a stub.
type FileDateFn func(absPath string) time.Time

// MtimeDateFn returns the file's mtime, or the zero time, so the caller always
// gets a value.
func MtimeDateFn(absPath string) time.Time {
	info, err := os.Stat(absPath) //nolint:gosec // absPath is validated by the walk
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// GitOrMtimeDateFn is the production FileDateFn: the date git added the file,
// falling back to mtime whenever git cannot answer — absent binary, untracked
// file, no repo, unparseable output. The call runs in the file's own directory
// so relative-path resolution is consistent.
func GitOrMtimeDateFn(absPath string) time.Time {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	// --diff-filter=A restricts to the commit that added the file; %aI is
	// RFC3339.
	cmd := exec.Command("git", "log", "--diff-filter=A", "--format=%aI", "-1", "--", base) //nolint:gosec
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return MtimeDateFn(absPath)
	}

	raw := strings.TrimSpace(out.String())
	if raw == "" {
		return MtimeDateFn(absPath)
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return MtimeDateFn(absPath)
	}
	return t
}

// ExternalEntry is one URL in the registry.
type ExternalEntry struct {
	URL string

	// Sources are the realm-relative paths citing this URL, sorted.
	Sources []string

	// FirstSeen is the earliest date across the sources, zero when none yields
	// a valid date.
	FirstSeen time.Time
}

// BuildExternalRegistry returns the registry sorted by URL. ExtractLinks is
// fence-aware, so URLs inside code blocks are already excluded.
func BuildExternalRegistry(root string, dateFn FileDateFn) []ExternalEntry {
	if dateFn == nil {
		dateFn = MtimeDateFn
	}

	type accumulator struct {
		sources   map[string]bool
		firstSeen time.Time
	}
	acc := make(map[string]*accumulator)

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic serve /external: walk error at %s: %v\n", path, err)
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || hiddenFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		data, readErr := os.ReadFile(path) //nolint:gosec // path comes from WalkDir under root
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "atomic serve /external: read error at %s: %v\n", path, readErr)
			return nil
		}

		links := mdlink.ExtractLinks(string(data))
		fileDate := dateFn(path)

		for _, l := range links {
			if l.Kind != mdlink.MarkdownLink {
				continue
			}
			target := l.Target
			if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
				continue
			}

			entry, exists := acc[target]
			if !exists {
				entry = &accumulator{sources: make(map[string]bool)}
				acc[target] = entry
			}
			entry.sources[rel] = true

			if entry.firstSeen.IsZero() || (!fileDate.IsZero() && fileDate.Before(entry.firstSeen)) {
				entry.firstSeen = fileDate
			}
		}
		return nil
	})

	result := make([]ExternalEntry, 0, len(acc))
	for url, a := range acc {
		sources := make([]string, 0, len(a.sources))
		for s := range a.sources {
			sources = append(sources, s)
		}
		sort.Strings(sources)
		result = append(result, ExternalEntry{
			URL:       url,
			Sources:   sources,
			FirstSeen: a.firstSeen,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].URL < result[j].URL
	})
	return result
}

// apiExternalEntry carries FirstSeen as null when no source yields a date.
type apiExternalEntry struct {
	URL       string   `json:"url"`
	Sources   []string `json:"sources"`
	FirstSeen *string  `json:"firstSeen"`
}

type apiExternalResponse struct {
	Entries []apiExternalEntry `json:"entries"`
}

// NewAPIExternalHandler serves GET /api/external.
func NewAPIExternalHandler(root string, dateFn FileDateFn, store *snapshotStore) http.Handler {
	if dateFn == nil {
		dateFn = MtimeDateFn
	}
	// The registry walk runs git once per file, seconds on a large realm, so
	// memoize it against the snapshot fingerprint.
	var mu sync.Mutex
	var cachedFP string
	var cached []ExternalEntry
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reg []ExternalEntry
		fp := ""
		if store != nil {
			if snap, _ := store.ensureFresh(); snap != nil {
				fp = snap.fp
			}
		}
		mu.Lock()
		if fp != "" && fp == cachedFP && cached != nil {
			reg = cached
			mu.Unlock()
		} else {
			mu.Unlock()
			reg = BuildExternalRegistry(root, dateFn)
			mu.Lock()
			cachedFP, cached = fp, reg
			mu.Unlock()
		}

		entries := make([]apiExternalEntry, len(reg))
		for i, e := range reg {
			var firstSeen *string
			if !e.FirstSeen.IsZero() {
				s := e.FirstSeen.Format("2006-01-02")
				firstSeen = &s
			}
			entries[i] = apiExternalEntry{URL: e.URL, Sources: e.Sources, FirstSeen: firstSeen}
		}

		writeAPIJSON(w, apiExternalResponse{Entries: entries})
	})
}
