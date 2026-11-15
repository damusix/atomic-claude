package validate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var reSummaryLine = regexp.MustCompile(`(\d+) PASS, (\d+) WARN, (\d+) FAIL\.`)

// groupPasses returns the PASS count of each human summary line, in output
// order: spec, config, [bundle,] artifacts.
func groupPasses(t *testing.T, out string) []int {
	t.Helper()
	var passes []int
	for _, m := range reSummaryLine.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[1])
		passes = append(passes, n)
	}
	return passes
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// buildCleanRepo has one file per config-read kind (CLAUDE.md, an agent, a
// command), each of which the artifact corpus also scans.
func buildCleanRepo(t *testing.T) string {
	t.Helper()
	root := buildMinimalRepo(t)
	writeFile(t, root, "context/agents/atomic-helper.md", "---\nname: atomic-helper\n---\n\nHelps.\n")
	writeFile(t, root, "context/commands/do-thing.md", "Dispatch `subagent_type: \"atomic-helper\"`.\n")
	return root
}

// A clean run must say how many files it looked at; "0 PASS" on a clean repo
// reads as "nothing was inspected".
func TestSummary_CleanRepoPassCountsInspectedFiles(t *testing.T) {
	root := buildCleanRepo(t)
	code, out := runFromDir(t, root, nil)
	if code != 0 {
		t.Fatalf("clean repo: exit %d, want 0\n%s", code, out)
	}
	got := groupPasses(t, out)
	want := []int{1, 3, 3} // spec, config, artifacts
	if len(got) != len(want) {
		t.Fatalf("summary lines = %v, want %v\n%s", got, want, out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("group %d PASS = %d, want %d\n%s", i, got[i], want[i], out)
		}
	}
}

// The JSON consumer sees only the aggregate, so it must be the sum of the
// per-group counts the human output shows.
func TestSummary_AggregateIsSumOfGroups(t *testing.T) {
	root := buildCleanRepo(t)
	_, human := runFromDir(t, root, nil)
	sum := 0
	for _, n := range groupPasses(t, human) {
		sum += n
	}

	_, raw := runFromDir(t, root, []string{"--json"})
	var env struct {
		Summary struct {
			Pass int `json:"pass"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, raw)
	}
	if sum == 0 || env.Summary.Pass != sum {
		t.Errorf("JSON summary.pass = %d, human group sum = %d; want equal and > 0", env.Summary.Pass, sum)
	}
}

// A broken @-ref must still fail the run, and the file carrying it must not be
// counted as passing.
func TestSummary_BrokenAtRefFailsAndIsNotPass(t *testing.T) {
	root := buildCleanRepo(t)
	writeFile(t, root, "context/AGENTS.md", "# AGENTS.md\n\n@docs/missing.md\n")
	code, out := runFromDir(t, root, []string{"config"})
	if code != 1 {
		t.Errorf("broken @-ref: exit %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL  C5") {
		t.Errorf("broken @-ref: want FAIL C5\n%s", out)
	}
	if got := groupPasses(t, out); len(got) != 1 || got[0] != 2 {
		t.Errorf("config PASS = %v, want [2] (agent, command; not AGENTS.md)\n%s", got, out)
	}
}

// A WARN is not a pass: an agent that C9 warns on drops out of the count.
func TestSummary_WarnedFileIsNotPass(t *testing.T) {
	root := buildCleanRepo(t)
	writeFile(t, root, "context/agents/NotAtomic.md", "---\nname: not-atomic\n---\n")
	code, out := runFromDir(t, root, []string{"config"})
	if code != 0 {
		t.Errorf("WARN only: exit %d, want 0\n%s", code, out)
	}
	if got := groupPasses(t, out); len(got) != 1 || got[0] != 3 {
		t.Errorf("config PASS = %v, want [3] (4 read, 1 warned)\n%s", got, out)
	}
}

// Bare `atomic validate` is the form CI and pre-ship gates run, so it must
// apply the spec rules to docs/spec/*.md, not only the explicit subcommand.
func TestSummary_BareValidateRunsSpecRules(t *testing.T) {
	root := buildCleanRepo(t)
	writeFile(t, root, "docs/spec/broken.md", "# Broken\n\nNo sections.\n")
	code, out := runFromDir(t, root, nil)
	if code != 1 {
		t.Errorf("malformed spec: exit %d, want 1\n%s", code, out)
	}
	for _, rule := range []string{"FAIL  S5", "FAIL  S6"} {
		if !strings.Contains(out, rule) {
			t.Errorf("malformed spec: want %q\n%s", rule, out)
		}
	}
	if got := groupPasses(t, out); len(got) == 0 || got[0] != 1 {
		t.Errorf("spec PASS = %v, want first entry 1 (good-spec only)\n%s", got, out)
	}
}

// Every manifest entry differs from this fixture, but only five are listed;
// the unlisted ones must not be counted as passing.
func TestSummary_BundleOverflowIsNotPass(t *testing.T) {
	root := buildCleanRepo(t)
	addRepoDevMarker(t, root)
	code, out := runFromDir(t, root, []string{"bundle"})
	if code != 1 {
		t.Fatalf("drifted bundle: exit %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "more diffs not shown") {
		t.Fatalf("fixture must trigger the overflow cap\n%s", out)
	}
	if got := groupPasses(t, out); len(got) != 1 || got[0] != 0 {
		t.Errorf("bundle PASS = %v, want [0]\n%s", got, out)
	}
}

// In this repo the manifest and the working tree largely agree, so a bundle
// run must report matched entries rather than 0 PASS.
func TestSummary_BundleInRepoCountsMatchedEntries(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := findRepoRootForTest(cwd)
	if root == "" {
		t.Skip("not inside a git checkout")
	}
	_, out := runFromDir(t, root, []string{"bundle"})
	if got := groupPasses(t, out); len(got) != 1 || got[0] == 0 {
		t.Errorf("bundle PASS = %v, want one entry > 0\n%s", got, out)
	}
}
