package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"
)

func profileLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "[profile] ") {
			out = append(out, line)
		}
	}
	return out
}

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

	extractLine := lines[0]
	if !strings.HasPrefix(extractLine, "[profile] extract:") {
		t.Fatalf("first profile line should be extract:, got %q", extractLine)
	}
	if !strings.Contains(extractLine, "files") {
		t.Errorf("extract line should contain 'files': %q", extractLine)
	}
	rest := strings.TrimPrefix(extractLine, "[profile] extract: ")
	if rest == "" || strings.HasPrefix(rest, "(") {
		t.Errorf("extract line has empty duration: %q", extractLine)
	}
}

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

func TestIndex_Profile_SummaryUsesPostResolveStats(t *testing.T) {
	// The summary must reflect post-resolve state, not stats captured right
	// after extract. The fixture carries a relative import so resolution adds
	// an edge, making the two counts differ if they ever diverge again.
	dir := writeFixtureWithTest(t)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"index", "--profile"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("index --profile exit %d; stderr: %s", code, stderr.String())
	}

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

	var statusOut, statusErr bytes.Buffer
	if code2 := codecli.RunCode([]string{"status", "--json"}, dir, &statusOut, &statusErr, noStdin()); code2 != 0 {
		t.Fatalf("status --json exit %d; stderr: %s", code2, statusErr.String())
	}
	var s codecli.StatusJSON
	if err := json.Unmarshal(statusOut.Bytes(), &s); err != nil {
		t.Fatalf("status --json not valid JSON: %v\noutput: %s", err, statusOut.String())
	}

	if summaryNodes != s.NodeCount {
		t.Errorf("indexed: nodes in --profile summary = %d, status --json nodeCount = %d; summary must use post-resolve stats",
			summaryNodes, s.NodeCount)
	}
	if summaryEdges != s.EdgeCount {
		t.Errorf("indexed: edges in --profile summary = %d, status --json edgeCount = %d; summary must use post-resolve stats",
			summaryEdges, s.EdgeCount)
	}
}
