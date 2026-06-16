// external.go — CP5 external-link registry.
//
// BuildExternalRegistry walks every *.md file under root, calls
// mdlink.ExtractLinks on each, and aggregates all outbound http/https URLs into
// a registry: one ExternalEntry per unique URL with the list of source pages
// (realm-root-relative) that cite it and the earliest first-seen date across
// those source files.
//
// First-seen date is determined by the injected FileDateFn seam:
//   - Production: GitOrMtimeDateFn — runs `git log --diff-filter=A` to get the
//     file's add-date from git history; falls back to mtime on any failure
//     (git absent, non-zero exit, untracked file, parse error).
//   - Tests: a deterministic stub that returns known dates without disk I/O.
//
// NewExternalHandler returns an http.Handler for /external that renders the
// registry as a sorted table (URL · source pages · first-seen). Consistent with
// other routes: full page for direct navigation, fragment for HX-Request.
package serve

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// FileDateFn is the injectable seam for determining the "date" of a file.
// It receives the absolute path to the file and returns the relevant timestamp.
// The production default is MtimeDateFn. Tests inject a stub.
type FileDateFn func(absPath string) time.Time

// MtimeDateFn is a FileDateFn that returns the file's modification time.
// Falls back to the zero time on stat failure so the caller always gets a value.
func MtimeDateFn(absPath string) time.Time {
	info, err := os.Stat(absPath) //nolint:gosec // absPath is validated by the walk
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// GitOrMtimeDateFn is the production FileDateFn: it queries git for the
// file's first-commit date (the date git first added the file to history)
// and falls back to MtimeDateFn on any failure:
//   - git binary not on PATH
//   - non-zero exit (file untracked or repo not initialised)
//   - empty output (untracked file with no commits referencing it)
//   - RFC3339 parse error
//
// The git call is read-only (`git log`). It is run with the file's directory
// as cwd so relative-path resolution is consistent across platforms.
func GitOrMtimeDateFn(absPath string) time.Time {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	// Run: git log --diff-filter=A --format=%aI -1 -- <basename>
	// --diff-filter=A: only the commit that Added the file.
	// %aI: author date in strict ISO 8601 / RFC3339 format.
	// -1: one result (the earliest add commit).
	cmd := exec.Command("git", "log", "--diff-filter=A", "--format=%aI", "-1", "--", base) //nolint:gosec
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		// git not found, not a repo, or non-zero exit — fall back silently.
		return MtimeDateFn(absPath)
	}

	raw := strings.TrimSpace(out.String())
	if raw == "" {
		// File exists on disk but is untracked — fall back to mtime.
		return MtimeDateFn(absPath)
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// Unexpected format — fall back to mtime.
		return MtimeDateFn(absPath)
	}
	return t
}

// ExternalEntry is one entry in the external-link registry.
type ExternalEntry struct {
	// URL is the absolute http/https URL.
	URL string

	// Sources is the sorted list of realm-root-relative paths that cite this URL.
	Sources []string

	// FirstSeen is the earliest date across the source files (determined by FileDateFn).
	// Zero time when no source file yields a valid date.
	FirstSeen time.Time
}

// BuildExternalRegistry walks *.md files under root, extracts external URLs via
// mdlink.ExtractLinks, and returns a sorted slice of ExternalEntry (sorted by URL).
// Internal links (relative paths, wikilinks) and code-block-embedded URLs are
// excluded by ExtractLinks' fence-aware logic.
func BuildExternalRegistry(root string, dateFn FileDateFn) []ExternalEntry {
	if dateFn == nil {
		dateFn = MtimeDateFn
	}

	// url → {sources set, first-seen}
	type accumulator struct {
		sources   map[string]bool // rel paths that cite the URL
		firstSeen time.Time
	}
	acc := make(map[string]*accumulator)

	// Walk every .md file under root (same walk as BuildLinkGraph).
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
			// Keep only absolute http/https URLs in markdown links.
			// Wikilinks never have http:// targets; markdown links might be internal.
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

			// Track earliest date across source files.
			if entry.firstSeen.IsZero() || (!fileDate.IsZero() && fileDate.Before(entry.firstSeen)) {
				entry.firstSeen = fileDate
			}
		}
		return nil
	})

	// Build sorted result slice.
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

// ─── templates ───────────────────────────────────────────────────────────────

const externalPageTmplStr = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>External links registry</title>
<link rel="stylesheet" href="/static/app.css">
<script src="/static/vendor/htmx.min.js"></script>
</head>
<body>
<div id="page-content" class="md-content">
{{template "external-body" .}}
</div>
</body>
</html>`

const externalFragmentTmplStr = `<div id="page-content" class="md-content">
{{template "external-body" .}}
</div>`

const externalBodyTmplStr = `{{define "external-body"}}
<h1>External links registry</h1>
<p>All outbound <code>http(s)</code> URLs across the realm, with source pages and first-seen date.</p>
{{if .}}
<table class="external-registry">
<thead>
<tr><th>URL</th><th>Source pages</th><th>First seen</th></tr>
</thead>
<tbody>
{{range .}}
<tr>
  <td><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{.URL}}</a></td>
  <td>{{range .Sources}}<a class="nav-item" hx-get="/page/{{.}}" hx-target="#main-pane" hx-push-url="true" href="/page/{{.}}">{{.}}</a> {{end}}</td>
  <td>{{if .FirstSeen.IsZero}}&mdash;{{else}}{{.FirstSeen.Format "2006-01-02"}}{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
{{else}}
<p>No external links found in this realm.</p>
{{end}}
{{end}}`

var (
	externalBodyTmpl     = template.Must(template.New("external-parts").Parse(externalBodyTmplStr))
	externalPageTmpl     = template.Must(template.Must(externalBodyTmpl.Clone()).Parse(externalPageTmplStr))
	externalFragmentTmpl = template.Must(template.Must(externalBodyTmpl.Clone()).Parse(externalFragmentTmplStr))
)

// NewExternalHandler returns an http.Handler for /external that builds the
// external-link registry on each request and renders it.
//
// Full page for direct navigation; htmx fragment (no DOCTYPE) when HX-Request
// header is present — consistent with NewPageHandler.
func NewExternalHandler(root string, dateFn FileDateFn) http.Handler {
	if dateFn == nil {
		dateFn = MtimeDateFn
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reg := BuildExternalRegistry(root, dateFn)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		var err error
		if r.Header.Get("HX-Request") != "" {
			err = externalFragmentTmpl.ExecuteTemplate(w, "external-body", reg)
		} else {
			err = externalPageTmpl.ExecuteTemplate(w, "external-body", reg)
		}
		if err != nil {
			// Headers already sent — log only; can't change status.
			fmt.Fprintf(os.Stderr, "atomic serve /external: template error: %v\n", err)
		}
	})
}
