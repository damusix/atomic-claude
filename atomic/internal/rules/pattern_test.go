package rules

import (
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

// equivalenceGlobs are the canonical corpus globs plus the dialect corners the
// runtime matcher is allowed to evaluate.
var equivalenceGlobs = []string{
	"**/*.{ts,tsx}",
	"**/*.py",
	"docs/spec/**/*.md",
	"docs/design/**/*.md",
	"*.md",
	"a/**",
	"a/**/b",
	"**/x",
	"x/**/y/*.ts",
	"**",
	"file?.md",
	"[ab]*.md",
	"[^a]*.md",
	"src/*/test?.{js,[m]js}",
}

// equivalenceCandidates is a matrix of cleaned repository-relative paths,
// chosen so every glob above has both matches and near misses.
var equivalenceCandidates = []string{
	"x.ts", "a/x.ts", "a/b/x.tsx", "x.tsx", "docs/spec/a.md", "docs/spec/x/a.md",
	"docs/spec", "docs/design/x/y.md", "a", "a/b", "a/x/b", "a/x/y/b", "x", "a/x",
	"x/y/a.ts", "x/q/y/a.ts", "f.md", "ab.md", "zb.md", "file1.md", "file12.md",
	"src/b/test1.js", "src/b/test1.mjs", "src/b/test1.ts", "src/deep/test1.js",
	"a/x.ts.backup", "nested/target.ts", "deep/nested/target.ts",
}

// The runtime matcher evaluates a compiled regex where the canonical matcher
// evaluates the glob. Any divergence would deliver Atomic guidance for a path
// the canonical matcher does not match, or miss one it does.
func TestCompileGlobMatchesCanonicalMatcher(t *testing.T) {
	for _, glob := range equivalenceGlobs {
		t.Run(glob, func(t *testing.T) {
			compiled, err := CompileGlob(glob)
			if err != nil {
				t.Fatalf("CompileGlob(%q): %v", glob, err)
			}
			re, err := regexp.Compile(compiled)
			if err != nil {
				t.Fatalf("compiled source %q is not a valid expression: %v", compiled, err)
			}
			for _, candidate := range equivalenceCandidates {
				cleaned := path.Clean(candidate)
				want := doublestar.MatchUnvalidated(glob, cleaned)
				if got := re.MatchString(cleaned); got != want {
					t.Errorf("glob %q candidate %q: compiled source says %v, canonical matcher says %v (%s)", glob, candidate, got, want, compiled)
				}
			}
		})
	}
}

// A pattern outside the translated dialect is refused rather than approximated:
// an inexact translation would silently disagree with canonical matching at
// runtime, which is worse than reporting the rule as undeliverable.
func TestCompileGlobRefusesUnsupportedDialect(t *testing.T) {
	for _, glob := range []string{
		"", "/abs/*.md", "~/x", "../x", "a//b", "a**b", "**x", "a/[unterminated",
		"a/{b", "a/{b}", "a/{,b}", `a/\d.md`, "a/[]",
	} {
		if compiled, err := CompileGlob(glob); err == nil {
			t.Errorf("CompileGlob(%q) = %q, want a refusal", glob, compiled)
		}
	}
}

// The emitted source uses only constructs whose Go and JavaScript semantics
// agree, so the same string can be handed to `new RegExp` in the runtime
// delivery module.
func TestCompileGlobEmitsPortableSource(t *testing.T) {
	compiled, err := CompileGlob("**/*.{ts,tsx}")
	if err != nil {
		t.Fatal(err)
	}
	if compiled != `^(?:[^/]+/)*[^/]*\.(?:ts|tsx)$` {
		t.Errorf("compiled source = %q", compiled)
	}
	for _, forbidden := range []string{"(?<", `(?=`, `(?!`, `\p{`, `\d`, `\w`, "(?P<", "(?i)"} {
		if strings.Contains(compiled, forbidden) {
			t.Errorf("compiled source %q carries the non-portable construct %q", compiled, forbidden)
		}
	}
}
