package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// shippedRules loads the authored corpus and reads each record's source bytes,
// the exact pair a projection consumes.
func shippedRules(t *testing.T) []RuleSource {
	t.Helper()
	records, err := rules.LoadShipped(filepath.Join(repoRoot, "context", "rules"))
	if err != nil {
		t.Fatalf("rules.LoadShipped: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("the shipped rule corpus is empty")
	}
	out := make([]RuleSource, 0, len(records))
	for _, r := range records {
		data, err := os.ReadFile(filepath.Join(repoRoot, "context", filepath.FromSlash(r.Source)))
		if err != nil {
			t.Fatalf("read %s: %v", r.Source, err)
		}
		out = append(out, RuleSource{Record: r, Bytes: data})
	}
	return out
}

func wikiRules(t *testing.T, projectKey string) []RuleSource {
	t.Helper()
	rulesDir := filepath.Join("testdata", "rule", "wiki", "rules")
	records, err := rules.LoadWiki(rulesDir, projectKey)
	if err != nil {
		t.Fatalf("rules.LoadWiki: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("the wiki fixture carries no pointer card")
	}
	out := make([]RuleSource, 0, len(records))
	for _, r := range records {
		data, err := os.ReadFile(filepath.Join(rulesDir, filepath.FromSlash(strings.TrimPrefix(r.Source, "rules/"))))
		if err != nil {
			t.Fatalf("read %s: %v", r.Source, err)
		}
		out = append(out, RuleSource{Record: r, Bytes: data})
	}
	return out
}

func checkRuleGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "rule", "claude", filepath.FromSlash(name))
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", p, err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v (run go test ./internal/harness -update)", p, err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// Every shipped rule projects to its Claude-native path with the authored bytes
// unchanged, so the `paths:` metadata and body survive byte-for-byte.
func TestProjectClaudeRules_ShippedCorpusByteForByte(t *testing.T) {
	sources := shippedRules(t)
	report, err := ProjectClaudeRules(sources, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectClaudeRules: %v", err)
	}
	if report.Target != artifacts.TargetClaude {
		t.Errorf("target = %q", report.Target)
	}
	if report.Ordering != RuleOrderingRecordID {
		t.Errorf("ordering = %q, want %q", report.Ordering, RuleOrderingRecordID)
	}
	if len(report.Rules) != len(sources) {
		t.Fatalf("rules = %d, want %d", len(report.Rules), len(sources))
	}

	wantPath := map[string]string{
		"shipped:rules/python/style.md":        "rules/atomic/python/style.md",
		"shipped:rules/specs/spec-currency.md": "rules/atomic/specs/spec-currency.md",
		"shipped:rules/typescript/style.md":    "rules/atomic/typescript/style.md",
	}
	byID := map[string]RuleSource{}
	for _, s := range sources {
		byID[s.Record.ID] = s
	}

	for i, got := range report.Rules {
		src, ok := byID[got.RecordID]
		if !ok {
			t.Fatalf("projection names unknown record %q", got.RecordID)
		}
		if got.NativeResource != wantPath[got.RecordID] {
			t.Errorf("%s native resource = %q, want %q", got.RecordID, got.NativeResource, wantPath[got.RecordID])
		}
		if string(got.Bytes) != string(src.Bytes) {
			t.Errorf("%s is not the authored bytes byte-for-byte", got.RecordID)
		}
		if got.SourceDigest != src.Record.SourceDigest {
			t.Errorf("%s source digest = %q, want %q", got.RecordID, got.SourceDigest, src.Record.SourceDigest)
		}
		if got.Digest != artifacts.ProjectionDigest(src.Bytes) {
			t.Errorf("%s projection digest is not the digest of its bytes", got.RecordID)
		}
		if got.Class != rules.ClassShipped {
			t.Errorf("%s class = %q", got.RecordID, got.Class)
		}
		if got.Tier != artifacts.EnforcementUnsupported || got.Delivery != rules.RuntimeNone || len(got.Scope) != 0 {
			t.Errorf("%s tier/delivery/scope = %q/%q/%+v, want unsupported/none/empty", got.RecordID, got.Tier, got.Delivery, got.Scope)
		}
		if got.Order != i {
			t.Errorf("%s order = %d, want %d", got.RecordID, got.Order, i)
		}
		if i > 0 && report.Rules[i-1].RecordID >= got.RecordID {
			t.Errorf("generation is not in ascending identity order at %d", i)
		}
		checkRuleGolden(t, got.NativeResource, got.Bytes)
	}
}

// A repository wiki pointer card keeps its distinguished atomic-wiki/ native
// name, so its projected path never shares a file with a shipped rule's
// (rules/atomic-wiki/... vs rules/atomic/...). The names still collide for a
// shipped source under context/rules/atomic-wiki/ of the same domain
// (TestProjectClaudeRules_RejectsNativeCollision).
func TestProjectClaudeRules_WikiPointerCard(t *testing.T) {
	report, err := ProjectClaudeRules(wikiRules(t, "example-key"), ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectClaudeRules: %v", err)
	}
	if len(report.Rules) != 1 {
		t.Fatalf("rules = %+v, want one card", report.Rules)
	}
	got := report.Rules[0]
	if got.RecordID != "wiki:example-key:typescript" {
		t.Errorf("record ID = %q", got.RecordID)
	}
	if got.NativeResource != "rules/atomic-wiki/typescript.md" {
		t.Errorf("native resource = %q, want rules/atomic-wiki/typescript.md", got.NativeResource)
	}
	if got.Class != rules.ClassWikiPointer {
		t.Errorf("class = %q, want wiki-pointer", got.Class)
	}
	if !strings.Contains(string(got.Bytes), "paths:") {
		t.Error("projected card lost its paths: metadata")
	}
	checkRuleGolden(t, got.NativeResource, got.Bytes)
}

// The tier is exactly what the Claude CP0 record proves: every rule-delivery
// role is unsupported, so no record claims a native surface, a hook path, or a
// deny predicate Claude 2.1.273 never demonstrated.
func TestProjectClaudeRules_TierMatchesClaudeCapabilities(t *testing.T) {
	caps := ClaudeCapabilities()
	if caps.Harness != KindClaude {
		t.Fatalf("capability harness = %q", caps.Harness)
	}
	for _, role := range ruleRoles {
		if caps.Supports(role) {
			t.Errorf("Claude capability record claims %q", role)
		}
	}

	sources := append(shippedRules(t), wikiRules(t, "example-key")...)
	byID := map[string]rules.RuleRecord{}
	for _, s := range sources {
		byID[s.Record.ID] = s.Record
	}

	report, err := ProjectClaudeRules(sources, caps)
	if err != nil {
		t.Fatalf("ProjectClaudeRules: %v", err)
	}
	ev := ruleEvidence(caps)
	for _, got := range report.Rules {
		rec, ok := byID[got.RecordID]
		if !ok {
			t.Fatalf("projection names unknown record %q", got.RecordID)
		}
		if got.Tier != artifacts.EnforcementUnsupported {
			t.Errorf("%s tier = %q, want unsupported", got.RecordID, got.Tier)
		}
		if got.Tier == artifacts.EnforcementNativeScope || got.Tier == artifacts.EnforcementHookRequired || got.Tier == artifacts.EnforcementDenyEnforced {
			t.Errorf("%s claims an unproven Claude tier", got.RecordID)
		}
		if got.Delivery != rules.RuntimeNone {
			t.Errorf("%s delivery = %q, want none", got.RecordID, got.Delivery)
		}
		sel := rules.Select(rec, artifacts.TargetClaude, ev)
		if sel.Tier != got.Tier || sel.Delivery != got.Delivery {
			t.Errorf("%s projection disagrees with the shared selector", got.RecordID)
		}
	}

	gaps := report.Gaps
	if len(gaps) != len(ruleRoles) {
		t.Fatalf("gaps = %d, want %d: %+v", len(gaps), len(ruleRoles), gaps)
	}
	for i, g := range gaps {
		if g.Role != ruleRoles[i] || g.Status == StatusSupported {
			t.Errorf("gap %d = %+v, want role %q unproven", i, g, ruleRoles[i])
		}
		if g.Limitation == "" {
			t.Errorf("gap %q carries no limitation", g.Role)
		}
	}
}

// The same corpus yields the same order and digests every projection.
func TestProjectClaudeRules_Deterministic(t *testing.T) {
	sources := append(shippedRules(t), wikiRules(t, "example-key")...)
	first, err := ProjectClaudeRules(sources, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectClaudeRules: %v", err)
	}
	second, err := ProjectClaudeRules(sources, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectClaudeRules (second): %v", err)
	}
	if len(first.Rules) != len(second.Rules) {
		t.Fatalf("rule counts differ: %d vs %d", len(first.Rules), len(second.Rules))
	}
	for i := range first.Rules {
		a, b := first.Rules[i], second.Rules[i]
		if a.RecordID != b.RecordID || a.Order != b.Order || a.Digest != b.Digest || a.NativeResource != b.NativeResource {
			t.Errorf("projection %d is not stable: %+v vs %+v", i, a, b)
		}
	}
}

// Source bytes that do not match the parsed record are refused, so a projection
// can never write bytes its record does not describe.
func TestProjectClaudeRules_RefusesSourceDigestMismatch(t *testing.T) {
	sources := shippedRules(t)
	sources[0].Bytes = append([]byte{}, sources[0].Bytes...)
	sources[0].Bytes = append(sources[0].Bytes, '\n')

	_, err := ProjectClaudeRules(sources, ClaudeCapabilities())
	if err == nil {
		t.Fatal("projection accepted source bytes that do not match the record digest")
	}
	if !strings.Contains(err.Error(), sources[0].Record.ID) || !strings.Contains(err.Error(), sources[0].Record.SourceDigest) {
		t.Errorf("error = %q, want the record identity and digest", err)
	}
}

// Two records whose native names collide fail before any target is touched:
// a shipped source under context/rules/atomic-wiki/ and a wiki card of the
// same domain share NativeName even though their projected paths differ
// (rules/atomic/… vs rules/atomic-wiki/…).
func TestProjectClaudeRules_RejectsNativeCollision(t *testing.T) {
	shippedBytes := []byte("---\npaths:\n  - \"**/*.md\"\n---\nshipped\n")
	shipped, err := rules.ParseShipped("rules/atomic-wiki/card.md", shippedBytes)
	if err != nil {
		t.Fatalf("ParseShipped: %v", err)
	}
	wikiBytes := []byte("---\npaths:\n  - \"**/*.md\"\n---\nwiki\n")
	wiki, err := rules.ParseWiki("proj", "rules/wiki/card.md", wikiBytes)
	if err != nil {
		t.Fatalf("ParseWiki: %v", err)
	}

	_, err = ProjectClaudeRules([]RuleSource{
		{Record: shipped, Bytes: shippedBytes},
		{Record: wiki, Bytes: wikiBytes},
	}, ClaudeCapabilities())
	if err == nil || !strings.Contains(err.Error(), "native-name collision") {
		t.Fatalf("error = %v, want a native-name collision", err)
	}
}

// A non-Claude capability record is refused rather than projected with another
// harness's tiers.
func TestProjectClaudeRules_RejectsWrongHarness(t *testing.T) {
	_, err := ProjectClaudeRules(shippedRules(t), OMPCapabilities())
	if err == nil || !strings.Contains(err.Error(), `got "omp"`) {
		t.Fatalf("error = %v, want a Claude capability refusal", err)
	}
}

// An empty set projects an empty, gapped generation rather than an error.
func TestProjectClaudeRules_EmptySet(t *testing.T) {
	report, err := ProjectClaudeRules(nil, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectClaudeRules: %v", err)
	}
	if len(report.Rules) != 0 || report.Ordering != RuleOrderingRecordID {
		t.Errorf("report = %+v", report)
	}
	if len(report.Gaps) != len(ruleRoles) {
		t.Errorf("gaps = %+v, want every unproven role", report.Gaps)
	}
}
