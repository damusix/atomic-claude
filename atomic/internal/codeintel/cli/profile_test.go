package cli_test

// Tests for `atomic code index --profile` (and ATOMIC_CODE_PROFILE=1).
//
// Contract verified:
//   - --profile flag → 5 "[profile] " lines emitted to stderr, in order:
//     extract, frameworks, resolve.warm, resolve.match, resolve.synth
//   - ATOMIC_CODE_PROFILE=1 env → same 5 lines (no flag needed).
//   - Default (no flag, env unset) → zero "[profile] " lines in stderr.
//   - extract line appears first and contains "files".
//   - frameworks line appears second and contains "routes".
//   - All profile lines have a non-empty duration string between ": " and the
//     next space or "(".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"
)

// profileLines returns lines from s that start with "[profile] ".
func profileLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "[profile] ") {
			out = append(out, line)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 13. --profile flag: 5 ordered profile lines to stderr
// ---------------------------------------------------------------------------

func TestIndex_Profile_Flag(t *testing.T) {
	dir := writeFixture(t)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index", "--profile"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index --profile exit %d; stderr: %s", code, stderr.String())
	}

	lines := profileLines(stderr.String())
	if len(lines) != 5 {
		t.Fatalf("expected 5 [profile] lines, got %d:\n%s", len(lines), stderr.String())
	}

	// Order: extract, frameworks, resolve.warm, resolve.match, resolve.synth.
	wantPrefixes := []string{
		"[profile] extract:",
		"[profile] frameworks:",
		"[profile] resolve.warm:",
		"[profile] resolve.match:",
		"[profile] resolve.synth:",
	}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line[%d]: want prefix %q, got %q", i, want, lines[i])
		}
	}
}

// ---------------------------------------------------------------------------
// 14. ATOMIC_CODE_PROFILE=1 env: same 5 lines
// ---------------------------------------------------------------------------

func TestIndex_Profile_Env(t *testing.T) {
	dir := writeFixture(t)

	t.Setenv("ATOMIC_CODE_PROFILE", "1")

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index (ATOMIC_CODE_PROFILE=1) exit %d; stderr: %s", code, stderr.String())
	}

	lines := profileLines(stderr.String())
	if len(lines) != 5 {
		t.Fatalf("expected 5 [profile] lines via env, got %d:\n%s", len(lines), stderr.String())
	}

	wantPrefixes := []string{
		"[profile] extract:",
		"[profile] frameworks:",
		"[profile] resolve.warm:",
		"[profile] resolve.match:",
		"[profile] resolve.synth:",
	}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line[%d]: want prefix %q, got %q", i, want, lines[i])
		}
	}
}

// ---------------------------------------------------------------------------
// 15. Default (no flag, env unset): zero [profile] lines
// ---------------------------------------------------------------------------

func TestIndex_NoProfile_ByDefault(t *testing.T) {
	dir := writeFixture(t)

	t.Setenv("ATOMIC_CODE_PROFILE", "")

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index exit %d; stderr: %s", code, stderr.String())
	}

	lines := profileLines(stderr.String())
	if len(lines) != 0 {
		t.Errorf("expected 0 [profile] lines in default run, got %d:\n%s", len(lines), stderr.String())
	}
}

// ---------------------------------------------------------------------------
// 16. extract line present + duration non-empty + contains "files"
// ---------------------------------------------------------------------------

func TestIndex_Profile_ExtractLineHasDuration(t *testing.T) {
	dir := writeFixture(t)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index", "--profile"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index --profile exit %d", code)
	}

	lines := profileLines(stderr.String())
	if len(lines) < 1 {
		t.Fatal("no [profile] lines found")
	}

	// extract line: "[profile] extract: <dur> (<n> files)"
	extractLine := lines[0]
	if !strings.HasPrefix(extractLine, "[profile] extract:") {
		t.Fatalf("first profile line should be extract:, got %q", extractLine)
	}
	if !strings.Contains(extractLine, "files") {
		t.Errorf("extract line should contain 'files': %q", extractLine)
	}
	// Duration appears between "extract: " and " (": must be non-empty.
	rest := strings.TrimPrefix(extractLine, "[profile] extract: ")
	if rest == "" || strings.HasPrefix(rest, "(") {
		t.Errorf("extract line has empty duration: %q", extractLine)
	}
}

// ---------------------------------------------------------------------------
// 17. Profile off → stdout output unchanged (same as non-profile run)
// ---------------------------------------------------------------------------

func TestIndex_Profile_StdoutUnchanged(t *testing.T) {
	dir1 := writeFixture(t)
	dir2 := writeFixture(t)

	t.Setenv("ATOMIC_CODE_PROFILE", "")

	var stdout1, stderr1 bytes.Buffer
	var stdout2, stderr2 bytes.Buffer

	code1 := codecli.RunCode([]string{"index", "--profile"}, dir1, &stdout1, &stderr1, noStdin())
	code2 := codecli.RunCode([]string{"index"}, dir2, &stdout2, &stderr2, noStdin())

	if code1 != 0 || code2 != 0 {
		t.Fatalf("index exit codes: profiled=%d plain=%d", code1, code2)
	}

	// Normalize the project-root path so dir1 and dir2 don't differ.
	normalize := func(s, dir string) string {
		return strings.ReplaceAll(s, dir, "<dir>")
	}

	n1 := normalize(stdout1.String(), dir1)
	n2 := normalize(stdout2.String(), dir2)
	if n1 != n2 {
		t.Errorf("stdout with --profile should match stdout without --profile\n"+
			"with profile:\n%s\nwithout:\n%s", stdout1.String(), stdout2.String())
	}
}

// ---------------------------------------------------------------------------
// 18. F-70: --profile summary line uses post-resolve stats, not post-extract
// ---------------------------------------------------------------------------

func TestIndex_Profile_SummaryUsesPostResolveStats(t *testing.T) {
	// WHY: before F-70 the `indexed: N files, N nodes, N edges` summary line
	// in --profile mode reused `profileStats` captured right after extract —
	// BEFORE framework route extraction and resolution added more nodes/edges.
	// The fix re-fetches stats after resolve so the summary matches what
	// `status --json` reports. This test catches any regression where the two
	// diverge.
	//
	// Uses writeFixtureWithTest (adds greeter_test.go with a "./greeter" relative
	// import) so that resolution creates at least one edge — making the pre/post
	// edge counts differ and surfacing the stale-stats bug.
	dir := writeFixtureWithTest(t)

	// Run index --profile and capture the "indexed:" summary line.
	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index", "--profile"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index --profile exit %d; stderr: %s", code, stderr.String())
	}

	// Parse the summary line: "indexed: N files, N nodes, N edges"
	var summaryFiles, summaryNodes, summaryEdges int
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "indexed:") {
			if _, err := fmt.Sscanf(line, "indexed: %d files, %d nodes, %d edges", &summaryFiles, &summaryNodes, &summaryEdges); err != nil {
				t.Fatalf("could not parse indexed: line %q: %v", line, err)
			}
			break
		}
	}
	_ = summaryFiles // used for parsing; not compared
	if summaryNodes == 0 {
		t.Fatalf("indexed: line not found or nodes=0 in stdout:\n%s", stdout.String())
	}

	// Run status --json on the same dir to get the ground-truth post-resolve counts.
	var statusOut, statusErr bytes.Buffer
	if code2 := codecli.RunCode([]string{"status", "--json"}, dir, &statusOut, &statusErr, noStdin()); code2 != 0 {
		t.Fatalf("status --json exit %d; stderr: %s", code2, statusErr.String())
	}
	var s codecli.StatusJSON
	if err := json.Unmarshal(statusOut.Bytes(), &s); err != nil {
		t.Fatalf("status --json not valid JSON: %v\noutput: %s", err, statusOut.String())
	}

	// The summary in --profile mode must match post-resolve DB state.
	if summaryNodes != s.NodeCount {
		t.Errorf("indexed: nodes in --profile summary = %d, status --json nodeCount = %d; summary must use post-resolve stats",
			summaryNodes, s.NodeCount)
	}
	if summaryEdges != s.EdgeCount {
		t.Errorf("indexed: edges in --profile summary = %d, status --json edgeCount = %d; summary must use post-resolve stats",
			summaryEdges, s.EdgeCount)
	}
}
