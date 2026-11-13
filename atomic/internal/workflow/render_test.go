package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleResolved() Resolved {
	return Resolved{
		Values: []Value{
			{KeySourceControl, "gitlab", LevelRealm},
			{KeySourceControlHost, "git.acme.example", LevelRealm},
			{KeyIssueTracker, "jira", LevelRealm},
			{KeyIssueTrackerProject, "ACME-BILLING", LevelRepo},
			{KeyWorktrees, ".claude/worktrees", LevelDefault},
		},
		Read: []string{
			"references/source-control/gitlab.md",
			"references/issue-tracking/jira.md",
			"references/worktrees.md",
		},
	}
}

func TestRenderText_MatchesTheSpecShape(t *testing.T) {
	want := `<workflow-preferences level='resolved'>
- source-control: gitlab (realm)
- source-control-host: git.acme.example (realm)
- issue-tracker: jira (realm)
- issue-tracker-project: ACME-BILLING (repo)
- worktrees: .claude/worktrees (default)
warning: none
Read: references/source-control/gitlab.md, references/issue-tracking/jira.md, references/worktrees.md
</workflow-preferences>
`
	if got := RenderText(sampleResolved()); got != want {
		t.Errorf("RenderText =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderText_OneWarningLinePerWarning(t *testing.T) {
	r := sampleResolved()
	r.Warnings = []string{"first", "second"}
	got := RenderText(r)
	if want := "warning: first\nwarning: second\nRead:"; !strings.Contains(got, want) {
		t.Errorf("RenderText = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "warning: none") {
		t.Errorf("RenderText = %q, must not say none beside real warnings", got)
	}
}

func TestRenderJSON_CarriesTheSameFacts(t *testing.T) {
	r := sampleResolved()
	r.Values = r.Values[:1]
	r.Warnings = []string{"w1"}

	raw, err := RenderJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Values   []struct{ Key, Value, Level string }
		Warnings []string
		Read     []string
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	if len(got.Values) != 1 || got.Values[0].Key != "source-control" || got.Values[0].Value != "gitlab" || got.Values[0].Level != "realm" {
		t.Errorf("values = %+v", got.Values)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "w1" || len(got.Read) != 3 {
		t.Errorf("warnings %v read %v", got.Warnings, got.Read)
	}
}

func TestRenderJSON_EmptyListsAreArraysNotNull(t *testing.T) {
	raw, err := RenderJSON(Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"values":[]`, `"warnings":[]`, `"read":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("RenderJSON = %s, want %s", raw, want)
		}
	}
}
