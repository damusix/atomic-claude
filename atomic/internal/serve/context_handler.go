// context_handler.go — shared page resolution helpers for the /api/page
// (api_handlers.go) and /api/rail (rail_handler.go) JSON endpoints.
//
// Both endpoints resolve a relpath against the realm root the same way
// (index-file resolution, directory listing, path-traversal guard) so link
// resolution stays single-sourced between the page body and the rail.
package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// normRelPath converts a URL path segment to the forward-slash form stored in
// the graph (filepath.ToSlash + filepath.Clean). It strips a leading slash and
// cleans the path so that segments like "." and ".." are resolved before the
// graph lookup, preventing spurious 404s on requests like /context/./b.md.
func normRelPath(p string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "/")))
}

// readFile reads the file at absPath.
func readFile(absPath string) ([]byte, error) {
	return os.ReadFile(absPath) //nolint:gosec // caller must validate path before calling
}

// baseName returns the base filename from a path string.
func baseName(p string) string {
	return filepath.Base(p)
}

// dirEntry is one entry (file or subfolder) in a directory listing, used by
// the /api/page JSON handler (api_handlers.go).
type dirEntry struct {
	Name    string `json:"name"`    // display name (subfolder name, or file name with .md stripped)
	RelPath string `json:"relpath"` // realm-root-relative target: "<dir>/<name>/" for a folder, "<dir>/<file>" for a file
	Folder  bool   `json:"folder"`
}

// listDirEntries reads the immediate markdown files and subfolders of
// dirRel (realm-root-relative), sorted, hidden files and skip-dirs omitted.
// ok is false when dirRel cannot be resolved or read.
func listDirEntries(root, dirRel string) (entries []dirEntry, ok bool) {
	abs, resolveOK := safeResolve(root, dirRel)
	if !resolveOK {
		return nil, false
	}
	rawEntries, err := os.ReadDir(abs)
	if err != nil {
		return nil, false
	}

	var dirs, files []string
	for _, e := range rawEntries {
		name := e.Name()
		if e.IsDir() {
			if shouldSkipDir(name) {
				continue
			}
			dirs = append(dirs, name)
		} else if strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".") {
			files = append(files, name)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)

	for _, d := range dirs {
		entries = append(entries, dirEntry{
			Name:    d,
			RelPath: filepath.ToSlash(filepath.Join(dirRel, d)) + "/",
			Folder:  true,
		})
	}
	for _, f := range files {
		entries = append(entries, dirEntry{
			Name:    stripMDExt(f),
			RelPath: filepath.ToSlash(filepath.Join(dirRel, f)),
			Folder:  false,
		})
	}
	return entries, true
}

// breadcrumbSeg is one segment of a page's breadcrumb, used by the /api/page
// JSON handler (api_handlers.go) — the client renders these into the
// breadcrumb bar.
type breadcrumbSeg struct {
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"` // cumulative ancestor prefix; omitted for the final (current-page) segment
	Folder bool   `json:"folder,omitempty"`
}

// breadcrumbSegmentsData builds the ancestor-then-current segment sequence
// for a page's breadcrumb, as structured data for JSON responses.
func breadcrumbSegmentsData(relPath string) []breadcrumbSeg {
	clean := filepath.ToSlash(strings.TrimPrefix(relPath, "/"))
	parts := strings.Split(clean, "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return nil
	}

	segs := make([]breadcrumbSeg, 0, len(parts))
	prefix := ""
	for i, p := range parts[:len(parts)-1] {
		if i > 0 {
			prefix += "/"
		}
		prefix += p
		segs = append(segs, breadcrumbSeg{Label: p, Path: prefix, Folder: true})
	}
	segs = append(segs, breadcrumbSeg{Label: parts[len(parts)-1]})
	return segs
}

// isNilGraphProvider reports whether g represents "no graph": a bare nil
// interface, or a typed-nil value (nil *Graph, nil *snapshotStore, or any
// other nilable-kind implementor) boxed into the graphProvider interface. A
// boxed typed-nil value carries a non-nil type descriptor, so a plain
// `g == nil` comparison is always false for it — the interface only equals
// nil when both its type and value are unset — even though the underlying
// value has nothing to read. The Kind switch covers every nilable reflect
// kind (Ptr, Map, Slice, Chan, Func, Interface) so any typed-nil provider
// degrades, not just pointer implementors; IsNil panics on a non-nilable
// kind, which is why the switch guards it.
func isNilGraphProvider(g graphProvider) bool {
	if g == nil {
		return true
	}
	v := reflect.ValueOf(g)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}
