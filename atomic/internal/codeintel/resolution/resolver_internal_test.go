package resolution

// In-package so npmPackageName can be tested directly, not only through
// ResolveImport.

import "testing"

// Pins the identity rule: a scoped package keeps its scope segment, a "node:"
// builtin stays whole, a URL specifier yields no package, everything else
// truncates to its first path
// segment.
func TestNpmPackageName_NormalizerTable(t *testing.T) {
	cases := []struct {
		name      string
		specifier string
		want      string
	}{
		{"scoped deep import", "@scope/pkg/deep/path.js", "@scope/pkg"},
		{"bare package subpath", "pkg/sub", "pkg"},
		{"node builtin subpath", "node:fs/promises", "node:fs/promises"},
		{"bare package no subpath", "vitest", "vitest"},
		{"URL-scheme specifier", "https://cdn.jsdelivr.net/npm/zod@3.23.8/+esm", ""},
		{"scope with trailing slash, no package segment", "@scope/", "@scope"},
		{"bare scope, no slash at all", "@scope", "@scope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := npmPackageName(tc.specifier)
			if got != tc.want {
				t.Errorf("npmPackageName(%q) = %q, want %q", tc.specifier, got, tc.want)
			}
		})
	}
}
