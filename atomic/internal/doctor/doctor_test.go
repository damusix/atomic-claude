package doctor_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestRegistryCount verifies exactly 8 entries with stable indices 1–8.
func TestRegistryCount(t *testing.T) {
	cats := doctor.Categories()
	if len(cats) != 8 {
		t.Fatalf("registry len = %d, want 8", len(cats))
	}

	// Indices must be 1..8 with no gaps.
	for i, c := range cats {
		want := i + 1
		if c.Index != want {
			t.Errorf("cats[%d].Index = %d, want %d", i, c.Index, want)
		}
	}
}

// TestRegistryCategoryNames verifies the canonical names match the spec exactly.
func TestRegistryCategoryNames(t *testing.T) {
	wantNames := []string{
		"install",
		"hooks",
		"signals",
		"refs",
		"manifest",
		"followups",
		"memory",
		"binary",
	}
	cats := doctor.Categories()
	for i, want := range wantNames {
		if cats[i].Name != want {
			t.Errorf("cats[%d].Name = %q, want %q", i, cats[i].Name, want)
		}
	}
}

// TestRegistryCategorySeverities verifies each category's default severity matches the spec table.
// Spec: 4=refs→FAIL, 5=manifest→FAIL; all others→WARN.
func TestRegistryCategorySeverities(t *testing.T) {
	wantSeverities := []doctor.Severity{
		doctor.WARN, // 1 install
		doctor.WARN, // 2 hooks
		doctor.WARN, // 3 signals
		doctor.FAIL, // 4 refs
		doctor.FAIL, // 5 manifest
		doctor.WARN, // 6 followups
		doctor.WARN, // 7 memory
		doctor.WARN, // 8 binary
	}
	cats := doctor.Categories()
	for i, want := range wantSeverities {
		if cats[i].Severity != want {
			t.Errorf("cats[%d] (%s) Severity = %q, want %q", i, cats[i].Name, cats[i].Severity, want)
		}
	}
}

// TestRunFiltersByOnly verifies Run with Only=[1,3] returns exactly those two results in registry order.
func TestRunFiltersByOnly(t *testing.T) {
	opts := doctor.Opts{Only: []int{1, 3}, StaleDays: 7}
	results, err := doctor.Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	if results[0].Index != 1 {
		t.Errorf("results[0].Index = %d, want 1", results[0].Index)
	}
	if results[1].Index != 3 {
		t.Errorf("results[1].Index = %d, want 3", results[1].Index)
	}
}

// TestRunFiltersBySkip verifies Run with Skip=[2,4,6,8] returns indices [1,3,5,7].
func TestRunFiltersBySkip(t *testing.T) {
	opts := doctor.Opts{Skip: []int{2, 4, 6, 8}, StaleDays: 7}
	results, err := doctor.Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	wantIndices := []int{1, 3, 5, 7}
	for i, want := range wantIndices {
		if results[i].Index != want {
			t.Errorf("results[%d].Index = %d, want %d", i, results[i].Index, want)
		}
	}
}

// TestStubsReturnSkip verifies all check funcs are stubs returning SKIP.
func TestStubsReturnSkip(t *testing.T) {
	cats := doctor.Categories()
	opts := doctor.Opts{StaleDays: 7}
	for _, c := range cats {
		r := c.Run(opts)
		if r.Severity != doctor.SKIP {
			t.Errorf("category %q stub returned severity %v, want SKIP", c.Name, r.Severity)
		}
		if r.Detail == "" {
			t.Errorf("category %q stub returned empty Detail", c.Name)
		}
	}
}

// TestRunReturnsAllResults verifies Run returns exactly 8 results in index order.
func TestRunReturnsAllResults(t *testing.T) {
	opts := doctor.Opts{StaleDays: 7}
	results, err := doctor.Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 8 {
		t.Fatalf("Run returned %d results, want 8", len(results))
	}
	for i, r := range results {
		want := i + 1
		if r.Index != want {
			t.Errorf("results[%d].Index = %d, want %d", i, r.Index, want)
		}
	}
}

// TestFlagParsingHappyPath verifies ParseFlags accepts valid input.
func TestFlagParsingHappyPath(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--stale-days", "14", "--verbose"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if opts.StaleDays != 14 {
		t.Errorf("StaleDays = %d, want 14", opts.StaleDays)
	}
	if !opts.Verbose {
		t.Error("Verbose = false, want true")
	}
}

// TestFlagParsingRejectsNegativeStaleDays verifies negative threshold is rejected.
func TestFlagParsingRejectsNegativeStaleDays(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--stale-days", "-1"})
	if err == nil {
		t.Fatal("expected error for --stale-days -1, got nil")
	}
}

// TestFlagParsingRejectsZeroStaleDays verifies zero threshold is rejected.
func TestFlagParsingRejectsZeroStaleDays(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--stale-days", "0"})
	if err == nil {
		t.Fatal("expected error for --stale-days 0, got nil")
	}
}

// TestFlagParsingMutualExclusionFixAndJSON verifies --fix + --json is rejected.
func TestFlagParsingMutualExclusionFixAndJSON(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--fix", "--json"})
	if err == nil {
		t.Fatal("expected error for --fix + --json, got nil")
	}
}

// TestFlagParsingOnlyByIndex verifies --only accepts comma-separated indices.
func TestFlagParsingOnlyByIndex(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--only", "1,3"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if len(opts.Only) != 2 {
		t.Fatalf("Only len = %d, want 2", len(opts.Only))
	}
}

// TestFlagParsingOnlyByName verifies --only accepts canonical names.
func TestFlagParsingOnlyByName(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--only", "install,signals"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	// Resolved to indices 1 and 3.
	wantIndices := map[int]bool{1: true, 3: true}
	if len(opts.Only) != 2 {
		t.Fatalf("Only len = %d, want 2", len(opts.Only))
	}
	for _, idx := range opts.Only {
		if !wantIndices[idx] {
			t.Errorf("unexpected index %d in Only", idx)
		}
	}
}

// TestFlagParsingOnlyMixed verifies --only accepts mixed indices+names.
func TestFlagParsingOnlyMixed(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--only", "1,signals"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	wantIndices := map[int]bool{1: true, 3: true}
	if len(opts.Only) != 2 {
		t.Fatalf("Only len = %d, want 2", len(opts.Only))
	}
	for _, idx := range opts.Only {
		if !wantIndices[idx] {
			t.Errorf("unexpected index %d in Only", idx)
		}
	}
}

// TestFlagParsingRejectsUnknownCategory verifies --only rejects unknown names/indices.
func TestFlagParsingRejectsUnknownCategory(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--only", "notacategory"})
	if err == nil {
		t.Fatal("expected error for unknown category name, got nil")
	}
	if !strings.Contains(err.Error(), "notacategory") {
		t.Errorf("error %q does not mention the unknown name", err.Error())
	}
}

// TestFlagParsingRejectsOutOfRangeIndex verifies --only rejects index 0 and >8.
func TestFlagParsingRejectsOutOfRangeIndex(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--only", "9"})
	if err == nil {
		t.Fatal("expected error for out-of-range index 9, got nil")
	}

	_, err = doctor.ParseFlags([]string{"--only", "0"})
	if err == nil {
		t.Fatal("expected error for out-of-range index 0, got nil")
	}
}

// TestFlagParsingSkipByName verifies --skip accepts canonical names.
func TestFlagParsingSkipByName(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--skip", "binary"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if len(opts.Skip) != 1 || opts.Skip[0] != 8 {
		t.Errorf("Skip = %v, want [8]", opts.Skip)
	}
}
