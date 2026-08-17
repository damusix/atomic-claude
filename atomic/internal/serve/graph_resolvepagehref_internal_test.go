package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func hrefFile(t *testing.T, root, relPath string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte("# content\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func hrefDir(t *testing.T, root, relPath string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
}

func TestResolvePageHref_LeadingSlashBundleRelative(t *testing.T) {
	root := t.TempDir()

	// resolvePageHref probes the filesystem, so every link target must exist.
	hrefFile(t, root, "repos/alpha-service.md")
	hrefFile(t, root, "concerns/perf.md")
	hrefFile(t, root, "src/main.go") // non-.md, routes to /file/
	hrefDir(t, root, "knowledge/")
	hrefFile(t, root, "knowledge/observability.md")

	// README makes the dir-index resolution fire.
	hrefDir(t, root, "wiki/")
	hrefFile(t, root, "wiki/README.md")

	cases := []struct {
		name         string
		pageRelPath  string // page holding the link
		raw          string // href as written in source
		wantHref     string
		wantHTMX     bool
		wantExternal bool
	}{
		{
			name:         "leading-slash md page that exists",
			pageRelPath:  "index.md",
			raw:          "/repos/alpha-service.md",
			wantHref:     "/page/repos/alpha-service.md",
			wantHTMX:     true,
			wantExternal: false,
		},
		{
			name:         "leading-slash md page from a sub-page (same result regardless of caller location)",
			pageRelPath:  "wiki/README.md",
			raw:          "/concerns/perf.md",
			wantHref:     "/page/concerns/perf.md",
			wantHTMX:     true,
			wantExternal: false,
		},
		{
			name:         "leading-slash to a non-.md source file",
			pageRelPath:  "index.md",
			raw:          "/src/main.go",
			wantHref:     "/file/src/main.go",
			wantHTMX:     false,
			wantExternal: false,
		},
		{
			name:         "leading-slash to a directory that has a README index",
			pageRelPath:  "index.md",
			raw:          "/wiki/",
			wantHTMX:     true,
			wantExternal: false,
			// Index resolution happens later, in the page handler.
			wantHref: "/page/wiki/",
		},
		{
			name:         "leading-slash to non-existent path stays raw",
			pageRelPath:  "index.md",
			raw:          "/does-not-exist/missing.md",
			wantHref:     "/page/does-not-exist/missing.md", // in-shell 404, not raw
			wantHTMX:     true,
			wantExternal: false,
		},
		{
			name:         "leading-slash traversal attempt stays raw",
			pageRelPath:  "index.md",
			raw:          "/../../../etc/passwd",
			wantHref:     "/../../../etc/passwd",
			wantHTMX:     false,
			wantExternal: false,
		},
		{
			name:         "relative link to sibling md page",
			pageRelPath:  "index.md",
			raw:          "repos/alpha-service.md",
			wantHref:     "/page/repos/alpha-service.md",
			wantHTMX:     true,
			wantExternal: false,
		},
		{
			name:         "relative link from sub-page to peer",
			pageRelPath:  "repos/alpha-service.md",
			raw:          "../concerns/perf.md",
			wantHref:     "/page/concerns/perf.md",
			wantHTMX:     true,
			wantExternal: false,
		},
		{
			name:         "https URL is external",
			pageRelPath:  "index.md",
			raw:          "https://example.com",
			wantHref:     "https://example.com",
			wantHTMX:     false,
			wantExternal: true,
		},
		{
			name:         "http URL is external",
			pageRelPath:  "index.md",
			raw:          "http://example.com/page",
			wantHref:     "http://example.com/page",
			wantHTMX:     false,
			wantExternal: true,
		},
		{
			name:         "anchor-only is left verbatim",
			pageRelPath:  "index.md",
			raw:          "#section",
			wantHref:     "#section",
			wantHTMX:     false,
			wantExternal: false,
		},
		{
			name:         "empty raw is left verbatim",
			pageRelPath:  "index.md",
			raw:          "",
			wantHref:     "",
			wantHTMX:     false,
			wantExternal: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			href, htmx, ext := resolvePageHref(root, tc.pageRelPath, tc.raw)
			if href != tc.wantHref {
				t.Errorf("href: got %q, want %q", href, tc.wantHref)
			}
			if htmx != tc.wantHTMX {
				t.Errorf("htmxPage: got %v, want %v", htmx, tc.wantHTMX)
			}
			if ext != tc.wantExternal {
				t.Errorf("external: got %v, want %v", ext, tc.wantExternal)
			}
		})
	}
}
