package doctor_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func TestRegistryCount(t *testing.T) {
	cats := doctor.Categories()
	if len(cats) != 13 {
		t.Fatalf("registry len = %d, want 13", len(cats))
	}

	for i, c := range cats {
		want := i + 1
		if c.Index != want {
			t.Errorf("cats[%d].Index = %d, want %d", i, c.Index, want)
		}
	}
}

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
		"config",
		"profile",
		"code-index",
		"migrate",
		"repo-config",
	}
	cats := doctor.Categories()
	for i, want := range wantNames {
		if cats[i].Name != want {
			t.Errorf("cats[%d].Name = %q, want %q", i, cats[i].Name, want)
		}
	}
}

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
		doctor.WARN, // 9 config
		doctor.WARN, // 10 profile
		doctor.WARN, // 11 code-index
		doctor.WARN, // 12 migrate
		doctor.WARN, // 13 repo-config
	}
	cats := doctor.Categories()
	for i, want := range wantSeverities {
		if cats[i].Severity != want {
			t.Errorf("cats[%d] (%s) Severity = %q, want %q", i, cats[i].Name, cats[i].Severity, want)
		}
	}
}

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

func TestRunFiltersBySkip(t *testing.T) {
	opts := doctor.Opts{Skip: []int{2, 4, 6, 8}, StaleDays: 7}
	results, err := doctor.Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 9 {
		t.Fatalf("Run returned %d results, want 9", len(results))
	}
	wantIndices := []int{1, 3, 5, 7, 9, 10, 11, 12, 13}
	for i, want := range wantIndices {
		if results[i].Index != want {
			t.Errorf("results[%d].Index = %d, want %d", i, results[i].Index, want)
		}
	}
}

func TestStubsReturnSkip(t *testing.T) {
	// Retained as a no-op: every check is implemented, no stubs remain.
}

// Check 8 (binary) is excluded to keep this test off the network; it is
// covered in isolation via RunCheckBinaryWith.
func TestRunReturnsAllResults(t *testing.T) {
	opts := doctor.Opts{Only: []int{1, 2, 3, 4, 5, 6, 7}, StaleDays: 7}
	results, err := doctor.Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 7 {
		t.Fatalf("Run returned %d results, want 7", len(results))
	}
	for i, r := range results {
		want := i + 1
		if r.Index != want {
			t.Errorf("results[%d].Index = %d, want %d", i, r.Index, want)
		}
	}
}

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

func TestFlagParsingRejectsNegativeStaleDays(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--stale-days", "-1"})
	if err == nil {
		t.Fatal("expected error for --stale-days -1, got nil")
	}
}

func TestFlagParsingRejectsZeroStaleDays(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--stale-days", "0"})
	if err == nil {
		t.Fatal("expected error for --stale-days 0, got nil")
	}
}

func TestFlagParsingMutualExclusionFixAndJSON(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--fix", "--json"})
	if err == nil {
		t.Fatal("expected error for --fix + --json, got nil")
	}
}

func TestFlagParsingOnlyByIndex(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--only", "1,3"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if len(opts.Only) != 2 {
		t.Fatalf("Only len = %d, want 2", len(opts.Only))
	}
}

func TestFlagParsingOnlyByName(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--only", "install,signals"})
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

func TestFlagParsingRejectsUnknownCategory(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--only", "notacategory"})
	if err == nil {
		t.Fatal("expected error for unknown category name, got nil")
	}
	if !strings.Contains(err.Error(), "notacategory") {
		t.Errorf("error %q does not mention the unknown name", err.Error())
	}
}

func TestFlagParsingRejectsOutOfRangeIndex(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"--only", "14"})
	if err == nil {
		t.Fatal("expected error for out-of-range index 14, got nil")
	}

	_, err = doctor.ParseFlags([]string{"--only", "0"})
	if err == nil {
		t.Fatal("expected error for out-of-range index 0, got nil")
	}
}

func TestFlagParsingSkipByName(t *testing.T) {
	opts, err := doctor.ParseFlags([]string{"--skip", "binary"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if len(opts.Skip) != 1 || opts.Skip[0] != 8 {
		t.Errorf("Skip = %v, want [8]", opts.Skip)
	}
}
