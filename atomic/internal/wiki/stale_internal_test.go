package wiki

// C1: unit tests for resolveSummaryMember, the summary→member resolver used
// by Stale's 2a section. See docs/spec/wiki-stale-summary-resolution.md
// "Approach" for the three-step resolution order these cases exercise.

import "testing"

func TestResolveSummaryMember_FlatClaimed(t *testing.T) {
	classified := []Member{
		{Path: "alpha", Status: "summarized", SummaryPath: "repos/alpha.md"},
	}
	got, ok := resolveSummaryMember("repos/alpha.md", classified)
	if !ok || got != "alpha" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"alpha\", true)", got, ok)
	}
}

func TestResolveSummaryMember_SplitClaimed(t *testing.T) {
	classified := []Member{
		{Path: "beta", Status: "summarized", SummaryPath: "repos/beta/"},
	}
	got, ok := resolveSummaryMember("repos/beta/design.md", classified)
	if !ok || got != "beta" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"beta\", true)", got, ok)
	}
}

// TestResolveSummaryMember_SplitPrefixDoesNotMatchSiblingDirectory pins the
// trailing-slash discriminator in the split-form prefix check: "repos/beta/"
// must not match "repos/beta-extra/auth.md" merely because "repos/beta" is a
// string prefix of it. beta is listed before beta-extra so a comparison that
// dropped the trailing slash would make beta falsely claim beta-extra's
// summary (matching first in iteration order) instead of resolving to the
// correct owner.
func TestResolveSummaryMember_SplitPrefixDoesNotMatchSiblingDirectory(t *testing.T) {
	classified := []Member{
		{Path: "beta", Status: "summarized", SummaryPath: "repos/beta/"},
		{Path: "beta-extra", Status: "summarized", SummaryPath: "repos/beta-extra/"},
	}
	got, ok := resolveSummaryMember("repos/beta-extra/auth.md", classified)
	if !ok || got != "beta-extra" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"beta-extra\", true)", got, ok)
	}
}

func TestResolveSummaryMember_NestedMemberClaimed(t *testing.T) {
	classified := []Member{
		{Path: "packages/gamma", Status: "summarized", SummaryPath: "repos/gamma/"},
	}
	got, ok := resolveSummaryMember("repos/gamma/design.md", classified)
	if !ok || got != "packages/gamma" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"packages/gamma\", true)", got, ok)
	}
}

// TestResolveSummaryMember_BaseNameFallback covers the exact scenario the
// spec's Approach section calls out: classifyMembers rule 2 (indexed)
// outranks rule 3 (summarized), so a graduated member carries an empty
// SummaryPath even though a leftover summary file is still on disk.
func TestResolveSummaryMember_BaseNameFallback(t *testing.T) {
	classified := []Member{
		{Path: "packages/delta", Status: "indexed", SummaryPath: ""},
	}
	got, ok := resolveSummaryMember("repos/delta.md", classified)
	if !ok || got != "packages/delta" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"packages/delta\", true)", got, ok)
	}
}

func TestResolveSummaryMember_ZeroMatchUnresolved(t *testing.T) {
	classified := []Member{
		{Path: "alpha", Status: "summarized", SummaryPath: "repos/alpha.md"},
	}
	got, ok := resolveSummaryMember("repos/orphan.md", classified)
	if ok || got != "" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestResolveSummaryMember_TwoMemberAmbiguityUnresolved(t *testing.T) {
	classified := []Member{
		{Path: "a/tool", Status: "indexed", SummaryPath: ""},
		{Path: "b/tool", Status: "indexed", SummaryPath: ""},
	}
	got, ok := resolveSummaryMember("repos/tool.md", classified)
	if ok || got != "" {
		t.Fatalf("resolveSummaryMember() = (%q, %v), want (\"\", false) for ambiguous base name", got, ok)
	}
}
