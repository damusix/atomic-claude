package workflow

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, content string) *Block {
	t.Helper()
	b, err := ParseBlock(content)
	if err != nil {
		t.Fatalf("ParseBlock: %v", err)
	}
	if b == nil {
		t.Fatal("ParseBlock found no block")
	}
	return b
}

func hasWarning(ws []string, substr string) bool {
	for _, w := range ws {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestParseBlock_LevelQuoting(t *testing.T) {
	for _, open := range []string{
		`<workflow-preferences level='repo'>`,
		`<workflow-preferences level="repo">`,
		`<workflow-preferences level=repo>`,
	} {
		b := mustParse(t, open+"\n- source-control: gitlab\n</workflow-preferences>\n")
		if b.Level != "repo" {
			t.Errorf("%s: Level = %q, want repo", open, b.Level)
		}
		if got := b.Values[KeySourceControl]; got.Value != "gitlab" || got.Soft {
			t.Errorf("%s: source-control = %+v, want explicit gitlab", open, got)
		}
	}
}

func TestParseBlock_NoBlockIsNotAnError(t *testing.T) {
	b, err := ParseBlock("# profile\n\nno block here\n")
	if b != nil || err != nil {
		t.Errorf("ParseBlock = (%v, %v), want (nil, nil)", b, err)
	}
}

func TestParseBlock_AllFiveKeysAndNotes(t *testing.T) {
	b := mustParse(t, `<workflow-preferences level='realm'>
- source-control: gitlab  # our forge
- source-control-host: git.acme.example
- issue-tracker: jira
- issue-tracker-project: ACME-BILLING
- worktrees: trees/wt
</workflow-preferences>
`)
	want := map[Key]string{
		KeySourceControl:       "gitlab",
		KeySourceControlHost:   "git.acme.example",
		KeyIssueTracker:        "jira",
		KeyIssueTrackerProject: "ACME-BILLING",
		KeyWorktrees:           "trees/wt",
	}
	for k, v := range want {
		if got := b.Values[k]; got.Value != v || got.Soft {
			t.Errorf("%s = %+v, want explicit %q", k, got, v)
		}
	}
	if len(b.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", b.Warnings)
	}
}

func TestParseBlock_EmptyValueIsUnset(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='repo'>\n- source-control:\n- issue-tracker:   # nothing\n</workflow-preferences>\n")
	for _, k := range []Key{KeySourceControl, KeyIssueTracker} {
		if got, ok := b.Values[k]; ok && got.Value != "" {
			t.Errorf("%s = %+v, want unset", k, got)
		}
	}
	if len(b.Warnings) != 0 {
		t.Errorf("an empty value is unset, not a warning: %v", b.Warnings)
	}
}

func TestParseBlock_NoneAndFilesystemAreValues(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='repo'>\n- source-control: none\n- issue-tracker: filesystem\n</workflow-preferences>\n")
	if b.Values[KeySourceControl].Value != "none" || b.Values[KeyIssueTracker].Value != "filesystem" {
		t.Errorf("values = %+v, want none and filesystem kept", b.Values)
	}
}

func TestParseBlock_DetectedNoteMakesASoftValue(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='profile'>\n- source-control: gitlab  # detected: glab authed for gitlab.com\n- issue-tracker: linear # kept by hand\n</workflow-preferences>\n")
	if !b.Values[KeySourceControl].Soft {
		t.Error("a # detected line must be soft")
	}
	if b.Values[KeyIssueTracker].Soft {
		t.Error("any other note leaves the line explicit")
	}
}

func TestParseBlock_FencedBlocksAreIgnored(t *testing.T) {
	content := "```\n<workflow-preferences level='repo'>\n- source-control: gitlab\n</workflow-preferences>\n```\n"
	b, err := ParseBlock(content)
	if b != nil || err != nil {
		t.Errorf("fenced block parsed: (%v, %v)", b, err)
	}
}

func TestParseBlock_FencedExampleBesideARealBlock(t *testing.T) {
	content := "```\n<workflow-preferences level='repo'>\n</workflow-preferences>\n```\n\n<workflow-preferences level='repo'>\n- source-control: codeberg\n</workflow-preferences>\n"
	if got := mustParse(t, content).Values[KeySourceControl].Value; got != "codeberg" {
		t.Errorf("source-control = %q, want the unfenced block's value", got)
	}
}

func TestParseBlock_RefusalCases(t *testing.T) {
	open := "<workflow-preferences level='repo'>\n"
	cl := "</workflow-preferences>\n"
	cases := map[string]string{
		"two blocks":     open + cl + open + cl,
		"unpaired open":  open + "- source-control: gitlab\n",
		"unpaired close": "- source-control: gitlab\n" + cl,
		"close first":    cl + open,
	}
	for name, content := range cases {
		b, err := ParseBlock(content)
		if err == nil || b != nil {
			t.Errorf("%s: ParseBlock = (%v, %v), want a refusal error", name, b, err)
		}
	}
}

func TestParseBlock_MissingLevelAttribute(t *testing.T) {
	b := mustParse(t, "<workflow-preferences>\n- source-control: gitlab\n</workflow-preferences>\n")
	if b.Level != "" {
		t.Errorf("Level = %q, want empty for a missing attribute", b.Level)
	}
}

func TestParseBlock_UnknownKeyWarnsAndIsIgnored(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='repo'>\n- frobnicate: yes\n</workflow-preferences>\n")
	if !hasWarning(b.Warnings, "frobnicate") {
		t.Errorf("warnings = %v, want one naming the key", b.Warnings)
	}
	if len(b.Values) != 0 {
		t.Errorf("values = %v, want the unknown key ignored", b.Values)
	}
}

func TestParseBlock_InvalidValuesWarnAndUnset(t *testing.T) {
	cases := []struct{ key, value string }{
		{"source-control", "svn"},
		{"issue-tracker", "bitbucket"},
		{"source-control-host", "not a host"},
		{"worktrees", "../escape"},
		{"worktrees", "a/../b"},
		{"worktrees", "/abs/path"},
	}
	for _, tc := range cases {
		b := mustParse(t, "<workflow-preferences level='repo'>\n- "+tc.key+": "+tc.value+"\n</workflow-preferences>\n")
		if _, ok := b.Values[Key(tc.key)]; ok {
			t.Errorf("%s: %q kept, want unset", tc.key, tc.value)
		}
		if !hasWarning(b.Warnings, tc.key) {
			t.Errorf("%s: %q produced no warning: %v", tc.key, tc.value, b.Warnings)
		}
	}
}

func TestParseBlock_WorktreesAcceptsAutoOffAndRelativePath(t *testing.T) {
	for _, v := range []string{"auto", "off", ".claude/worktrees", "trees"} {
		b := mustParse(t, "<workflow-preferences level='repo'>\n- worktrees: "+v+"\n</workflow-preferences>\n")
		if b.Values[KeyWorktrees].Value != v || len(b.Warnings) != 0 {
			t.Errorf("worktrees %q: %+v, warnings %v", v, b.Values[KeyWorktrees], b.Warnings)
		}
	}
}

func TestParseBlock_RepeatedKeyFirstWins(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='repo'>\n- source-control: gitlab\n- source-control: github\n</workflow-preferences>\n")
	if b.Values[KeySourceControl].Value != "gitlab" {
		t.Errorf("source-control = %q, want the first line", b.Values[KeySourceControl].Value)
	}
	if !hasWarning(b.Warnings, "source-control") {
		t.Errorf("warnings = %v, want a repeated-key warning", b.Warnings)
	}
}

// Detection writes only non-host trackers as soft values, so a hand-written
// soft host tracker is invalid.
func TestParseBlock_SoftHostTrackerIsInvalid(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='profile'>\n- issue-tracker: github  # detected\n</workflow-preferences>\n")
	if _, ok := b.Values[KeyIssueTracker]; ok {
		t.Error("a soft host tracker must be ignored")
	}
	if !hasWarning(b.Warnings, "issue-tracker") {
		t.Errorf("warnings = %v, want one for the soft host tracker", b.Warnings)
	}
}

func TestParseBlock_SoftNonHostTrackerIsKept(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='profile'>\n- issue-tracker: linear  # detected\n</workflow-preferences>\n")
	if got := b.Values[KeyIssueTracker]; got.Value != "linear" || !got.Soft {
		t.Errorf("issue-tracker = %+v, want soft linear", got)
	}
}

func TestParseBlock_CloseTagToleratesTrailingWhitespace(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='repo'>\n- source-control: gitlab\n</workflow-preferences> \t\n")
	if b.Values[KeySourceControl].Value != "gitlab" {
		t.Errorf("values = %v", b.Values)
	}
}

// A repeat of a key whose first line was empty or invalid still counts as a
// repeat: the first line decides, so the key stays unset.
func TestParseBlock_RepeatAfterEmptyOrInvalidFirstLineStaysUnset(t *testing.T) {
	cases := map[string]string{
		"empty first":   "- source-control:\n- source-control: github",
		"invalid first": "- source-control: svn\n- source-control: github",
	}
	for name, lines := range cases {
		b := mustParse(t, "<workflow-preferences level='repo'>\n"+lines+"\n</workflow-preferences>\n")
		if got, ok := b.Values[KeySourceControl]; ok {
			t.Errorf("%s: source-control = %+v, want unset", name, got)
		}
		if !hasWarning(b.Warnings, "repeated") {
			t.Errorf("%s: warnings = %v, want a repeat warning", name, b.Warnings)
		}
	}
}

func TestParseBlock_MalformedLineWarns(t *testing.T) {
	b := mustParse(t, "<workflow-preferences level='repo'>\n\n- source-control gitlab\nsome prose\n</workflow-preferences>\n")
	if len(b.Warnings) != 2 || !hasWarning(b.Warnings, "malformed") {
		t.Errorf("warnings = %v, want one malformed-line warning per bad line and none for the blank", b.Warnings)
	}
}

func TestParseBlock_OnlyDetectedNoteIsSoft(t *testing.T) {
	cases := map[string]bool{
		"# detected":               true,
		"# detected: gh authed":    true,
		"# detectedness is a word": false,
		"# not detected":           false,
	}
	for note, soft := range cases {
		b := mustParse(t, "<workflow-preferences level='repo'>\n- source-control: gitlab  "+note+"\n</workflow-preferences>\n")
		if got := b.Values[KeySourceControl].Soft; got != soft {
			t.Errorf("note %q: Soft = %v, want %v", note, got, soft)
		}
	}
}

func TestParseBlock_FenceClosesOnlyWithItsOwnMarker(t *testing.T) {
	content := "~~~\n```\n<workflow-preferences level='repo'>\n- source-control: gitlab\n</workflow-preferences>\n~~~\n"
	if b, err := ParseBlock(content); b != nil || err != nil {
		t.Errorf("ParseBlock = (%v, %v), want the block to stay fenced", b, err)
	}
}

func TestProfileSection_OnlyTheWorkflowPreferencesSectionCounts(t *testing.T) {
	profile := `# User profile

## Notes

<workflow-preferences level='profile'>
- source-control: gitlab
</workflow-preferences>

## Workflow preferences

<workflow-preferences level='profile'>
- source-control: codeberg
</workflow-preferences>

## Environment

text
`
	b := mustParse(t, ProfileSection(profile))
	if got := b.Values[KeySourceControl].Value; got != "codeberg" {
		t.Errorf("source-control = %q, want the value inside ## Workflow preferences", got)
	}
}

func TestParseBlock_RecordsTagAndKeyLines(t *testing.T) {
	b := mustParse(t, "intro\n<workflow-preferences level='profile'>\n"+
		"- worktrees: off\n- source-control:\n- source-control: github\n"+
		"- issue-tracker: github  # detected\n</workflow-preferences>\n")
	if b.Close != 6 {
		t.Errorf("Close = %d; want 6", b.Close)
	}
	want := map[Key]Line{
		KeyWorktrees:     {N: 2},
		KeySourceControl: {N: 3},
		KeyIssueTracker:  {N: 5, Soft: true},
	}
	for k, w := range want {
		if b.Lines[k] != w {
			t.Errorf("Lines[%s] = %+v; want %+v (soft kept for a dropped value)", k, b.Lines[k], w)
		}
	}
}

func TestProfileSectionStart_MatchesTheHeadingProfileSectionDoes(t *testing.T) {
	const head = "## Workflow preferences\n"
	cases := []struct {
		name, in string
		want     int
	}{
		{"plain", "# P\n\n" + head + "body\n", len("# P\n\n" + head)},
		{"fenced copy skipped", "```\n" + head + "```\n" + head + "x\n",
			len("```\n"+head+"```\n") + len(head)},
		{"longer heading is not it", "## Workflow preferences extra\n", -1},
		{"indented is not a heading", "  ## Workflow preferences\n", -1},
		{"absent", "# P\n## Environment\n", -1},
		{"no trailing newline", "# P\n## Workflow preferences", len("# P\n## Workflow preferences")},
	}
	for _, c := range cases {
		if got := ProfileSectionStart(c.in); got != c.want {
			t.Errorf("%s: ProfileSectionStart = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestProfileSection_MissingSectionYieldsNoBlock(t *testing.T) {
	if got := ProfileSection("# User profile\n\n## Environment\n"); strings.Contains(got, "workflow-preferences") {
		t.Errorf("ProfileSection = %q, want empty", got)
	}
}
