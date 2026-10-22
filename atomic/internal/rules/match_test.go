package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func overlapMatcher(t *testing.T, base string) *Matcher {
	t.Helper()
	records, err := LoadShipped("testdata/overlap/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	instances, err := BindAll(records, "proj", base)
	if err != nil {
		t.Fatalf("BindAll: %v", err)
	}
	m, err := NewMatcher(instances)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	return m
}

// Every overlapping record matches, in stable identity order.
func TestMatch_ReturnsAllOverlappingInIdentityOrder(t *testing.T) {
	m := overlapMatcher(t, t.TempDir())

	matches, err := m.Match("pkg/tool.go")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match.Record.ID)
	}
	want := "shipped:rules/a/first.md,shipped:rules/b/second.md"
	if strings.Join(got, ",") != want {
		t.Errorf("match order = %v, want %v", got, want)
	}
}

func TestMatch_NonmatchReturnsNothing(t *testing.T) {
	m := overlapMatcher(t, t.TempDir())

	matches, err := m.Match("pkg/component.tsx")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("matches = %+v, want none", matches)
	}
}

// A record with several include patterns reports the first that matched.
func TestMatch_ReportsFirstMatchingGlob(t *testing.T) {
	m := overlapMatcher(t, t.TempDir())

	matches, err := m.Match("cmd/atomic/main.go")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	var second Match
	for _, match := range matches {
		if match.Record.ID == "shipped:rules/b/second.md" {
			second = match
		}
	}
	if second.Glob != "**/*.go" {
		t.Errorf("glob = %q, want the first matching pattern", second.Glob)
	}
}

func TestMatch_RejectsNonRelativeCandidates(t *testing.T) {
	m := overlapMatcher(t, t.TempDir())
	for _, candidate := range []string{"", "/abs/x.go", "~/x.go", "C:/x.go", "../x.go", "a/../../b.go"} {
		if _, err := m.Match(candidate); err == nil {
			t.Errorf("Match(%q) accepted a non-relative candidate", candidate)
		}
	}
}

// A candidate that traverses a symlink out of the base is not contained, so it
// does not match even though its lexical path sits under the base.
func TestMatch_SymlinkEscapeIsNotContained(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := overlapMatcher(t, base)

	matches, err := m.Match("link/secret.go")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("escaped candidate matched %+v", matches)
	}

	inside, err := m.Match("pkg/tool.go")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(inside) != 2 {
		t.Errorf("contained candidate matched %d records, want 2", len(inside))
	}
}

// A candidate whose leaf and every parent below the base are absent still
// resolves the deepest existing ancestor, so a symlink several levels above the
// leaf is caught. Resolving only the immediate parent would fall back to a
// lexical verdict and match a path that leaves the base.
func TestMatch_DeepSymlinkEscapeIsNotContained(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := overlapMatcher(t, base)

	matches, err := m.Match("link/sub/deep.go")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("escaped candidate matched %+v", matches)
	}
}

// A base reached through a symlink still matches its own contents.
func TestMatch_SymlinkedBaseMatches(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := overlapMatcher(t, link)

	matches, err := m.Match("pkg/tool.go")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("match count = %d, want 2", len(matches))
	}
	if matches[0].Base != resolvedDir(t, real) {
		t.Errorf("match base = %q, want resolved base", matches[0].Base)
	}
}

// Instance input order never changes match order.
func TestMatch_StableAcrossInstanceOrder(t *testing.T) {
	base := t.TempDir()
	records, err := LoadShipped("testdata/overlap/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	instances, err := BindAll(records, "proj", base)
	if err != nil {
		t.Fatalf("BindAll: %v", err)
	}
	forward, err := NewMatcher(instances)
	if err != nil {
		t.Fatal(err)
	}
	reversed := []RuleInstance{instances[len(instances)-1], instances[0]}
	backward, err := NewMatcher(reversed)
	if err != nil {
		t.Fatal(err)
	}

	a, err := forward.Match("pkg/tool.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := backward.Match("pkg/tool.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("match counts differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Record.ID != b[i].Record.ID {
			t.Errorf("position %d: %q vs %q", i, a[i].Record.ID, b[i].Record.ID)
		}
	}
}

func TestNewMatcher_RejectsRelativeBase(t *testing.T) {
	rec := loadOne(t)
	_, err := NewMatcher([]RuleInstance{{Record: rec, Base: "relative"}})
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("error = %v, want a relative-base rejection", err)
	}
}
