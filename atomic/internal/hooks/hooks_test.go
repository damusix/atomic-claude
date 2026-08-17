package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// addReminderWithDate backdates a new reminder's frontmatter created date.
func addReminderWithDate(t *testing.T, root, body string, daysAgo int) string {
	t.Helper()
	id, err := reminder.Add(root, body)
	if err != nil {
		t.Fatalf("addReminderWithDate: Add: %v", err)
	}
	if daysAgo == 0 {
		return id
	}
	dir := filepath.Join(root, ".claude", ".scratchpad", "reminders")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("addReminderWithDate: ReadDir: %v", err)
	}
	target := time.Now().UTC().AddDate(0, 0, -daysAgo).Format("2006-01-02")
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, _ := os.ReadFile(p)
		content := string(raw)
		today := time.Now().UTC().Format("2006-01-02")
		patched := strings.Replace(content, "created: "+today, "created: "+target, 1)
		if patched == content {
			continue
		}
		if !strings.Contains(content, "id: "+id) {
			continue
		}
		if err := os.WriteFile(p, []byte(patched), 0o644); err != nil {
			t.Fatalf("addReminderWithDate: WriteFile: %v", err)
		}
		return id
	}
	t.Fatalf("addReminderWithDate: could not find file for id %q", id)
	return ""
}

// stubNoWikiStaleness keeps empty-output assertions off the test machine's real
// <wikis> registry, which may list dirty wikis.
func stubNoWikiStaleness(t *testing.T) {
	t.Helper()
	orig := hooks.WikiCheckStaleness
	hooks.WikiCheckStaleness = func(_ string, _ int, _ func(string, ...string) error, _ func() time.Time) ([]string, error) {
		return nil, nil
	}
	t.Cleanup(func() { hooks.WikiCheckStaleness = orig })
}

// stubNoWherePosition pins the plain no-wiki/no-realm case, keeping empty-output
// assertions off the real registry and the test process's own ancestor tree.
func stubNoWherePosition(t *testing.T) {
	t.Helper()
	orig := hooks.WherePosition
	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = orig })
}

func TestSessionStart_EmptyReminders(t *testing.T) {
	stubNoWikiStaleness(t)
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output with no reminders, got %q", out)
	}
}

func TestSessionStart_OneFreshReminder_JSONEnvelope(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "fix the auth race in middleware", 0)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	if suppress, ok := payload["suppressOutput"].(bool); !ok || !suppress {
		t.Errorf("expected suppressOutput=true, got %v", payload["suppressOutput"])
	}

	hso, ok := payload["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("hookSpecificOutput missing or wrong type: %v", payload["hookSpecificOutput"])
	}
	if hso["hookEventName"] != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", hso["hookEventName"])
	}
	ctx, _ := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "fix the auth race in middleware") {
		t.Errorf("additionalContext missing reminder text: %q", ctx)
	}
	if !strings.Contains(ctx, "Pending reminders (1)") {
		t.Errorf("additionalContext missing header: %q", ctx)
	}
	if !strings.Contains(ctx, "today") {
		t.Errorf("additionalContext missing 'today': %q", ctx)
	}

	if _, has := payload["systemMessage"]; has {
		t.Errorf("unexpected systemMessage for fresh reminder: %v", payload["systemMessage"])
	}
}

func TestSessionStart_TwelveReminders_CappedAtTen(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	for i := range 12 {
		addReminderWithDate(t, root, strings.Repeat("x", i+1)+" reminder body", 0)
	}

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "Pending reminders (12)") {
		t.Errorf("header should show total count 12: %q", ctx)
	}
	if !strings.Contains(ctx, "(and 2 more)") {
		t.Errorf("expected '(and 2 more)' in context: %q", ctx)
	}
	lines := strings.Split(ctx, "\n")
	bulletCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- [") {
			bulletCount++
		}
	}
	if bulletCount != 10 {
		t.Errorf("expected 10 reminder bullets, got %d", bulletCount)
	}
}

func TestSessionStart_OldReminder_SystemMessage(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "revisit error handling in ingest", 15)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	sm, ok := payload["systemMessage"].(string)
	if !ok || sm == "" {
		t.Fatalf("expected systemMessage for old reminder, got %v", payload["systemMessage"])
	}
	if !strings.Contains(sm, "1 reminders pending") && !strings.Contains(sm, "1 reminder pending") {
		t.Errorf("systemMessage should mention count: %q", sm)
	}
	if !strings.Contains(sm, "days old") {
		t.Errorf("systemMessage should mention days old: %q", sm)
	}
}

func TestSessionStart_FormatText(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "benchmark the new query plan", 2)

	out, err := hooks.SessionStartText(root, now)
	if err != nil {
		t.Fatalf("SessionStartText: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty text output")
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("text format should not be JSON: %q", out)
	}
	if !strings.Contains(out, "benchmark the new query plan") {
		t.Errorf("text missing reminder: %q", out)
	}
}

func TestSessionStart_FormatText_EmptyReminders(t *testing.T) {
	stubNoWikiStaleness(t)
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	out, err := hooks.SessionStartText(root, now)
	if err != nil {
		t.Fatalf("SessionStartText: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty text output with no reminders, got %q", out)
	}
}

func TestSessionStart_BodyTruncated(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	longBody := strings.Repeat("a", 100)
	addReminderWithDate(t, root, longBody, 0)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	json.Unmarshal([]byte(out), &payload)
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "…") {
		t.Errorf("expected ellipsis in truncated body: %q", ctx)
	}
	if strings.Contains(ctx, longBody) {
		t.Errorf("expected body to be truncated, found full text: %q", ctx)
	}
}

func TestSessionStart_AgoBuckets(t *testing.T) {
	stubNoWherePosition(t)
	cases := []struct {
		days int
		want string
	}{
		{0, "today"},
		{1, "yesterday"},
		{2, "2 days ago"},
		{6, "6 days ago"},
		{7, "1 week ago"},
		{13, "1 week ago"},
		{14, "2 weeks ago"},
		{29, "4 weeks ago"},
		{30, "1 month ago"},
		{89, "2 months ago"},
		{100, "3 months ago"},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.want, " ", "_"), func(t *testing.T) {
			root := t.TempDir()
			now := time.Now().UTC()
			addReminderWithDate(t, root, "test reminder body", c.days)
			out, err := hooks.SessionStart(root, now)
			if err != nil {
				t.Fatalf("SessionStart: %v", err)
			}
			if out == "" {
				t.Fatal("expected non-empty output")
			}
			var payload map[string]any
			json.Unmarshal([]byte(out), &payload)
			hso := payload["hookSpecificOutput"].(map[string]any)
			ctx := hso["additionalContext"].(string)
			if !strings.Contains(ctx, c.want) {
				t.Errorf("days=%d: expected %q in context, got: %q", c.days, c.want, ctx)
			}
		})
	}
}

// addReminderWithDue offsets due from now; daysOffset may be negative.
func addReminderWithDue(t *testing.T, root, body string, daysOffset int) string {
	t.Helper()
	due := time.Now().UTC().AddDate(0, 0, daysOffset).Format(time.RFC3339)
	id, err := reminder.Add(root, body, reminder.WithDue(due))
	if err != nil {
		t.Fatalf("addReminderWithDue: Add: %v", err)
	}
	return id
}

// patchDueField rewrites a reminder's due: line, malformed values included.
func patchDueField(t *testing.T, root, id, dueValue string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", ".scratchpad", "reminders")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("patchDueField: ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, _ := os.ReadFile(p)
		content := string(raw)
		if !strings.Contains(content, "id: "+id) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "due: ") {
				lines[i] = "due: " + dueValue
			}
		}
		os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
		return
	}
	t.Fatalf("patchDueField: could not find file for id %q", id)
}

func TestSessionStart_FutureDue_Silent(t *testing.T) {
	stubNoWikiStaleness(t)
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDue(t, root, "future reminder should be silent", +1)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for future-due reminder, got %q", out)
	}
}

func TestSessionStart_PastDue_InOutput(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDue(t, root, "past due reminder", -1)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output for past-due reminder")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "past due reminder") {
		t.Errorf("past-due reminder missing from context: %q", ctx)
	}
	if !strings.Contains(ctx, "should-remind-user: true") {
		t.Errorf("should-remind-user marker missing from context: %q", ctx)
	}
}

func TestSessionStart_LegacyNoDue_InOutput(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "legacy reminder no due field", 0)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output for legacy reminder (no due field)")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "legacy reminder no due field") {
		t.Errorf("legacy reminder missing from context: %q", ctx)
	}
	if !strings.Contains(ctx, "should-remind-user: true") {
		t.Errorf("should-remind-user marker missing from context for legacy reminder: %q", ctx)
	}
}

func TestSessionStart_MalformedDue_InOutput(t *testing.T) {
	// Corrupting a real due: field exercises filterPastDue's parse-error branch
	// rather than its legacy Due=="" branch.
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	id := addReminderWithDue(t, root, "malformed due reminder", -1)
	patchDueField(t, root, id, "not-a-valid-iso")

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output for malformed-due reminder")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "malformed due reminder") {
		t.Errorf("malformed-due reminder missing from context: %q", ctx)
	}
	if !strings.Contains(ctx, "should-remind-user: true") {
		t.Errorf("should-remind-user marker missing for malformed-due reminder: %q", ctx)
	}
}

// systemMessage counts surfaced reminders, not everything stored.
func TestSessionStart_OldReminder_SystemMessage_CountsOnlySurfaced(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "old past due reminder", 15)
	for i := range 5 {
		addReminderWithDue(t, root, strings.Repeat("f", i+1)+" future silent", +1)
	}

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	sm, ok := payload["systemMessage"].(string)
	if !ok || sm == "" {
		t.Fatalf("expected systemMessage for old reminder, got %v", payload["systemMessage"])
	}
	if strings.Contains(sm, "6 reminders") {
		t.Errorf("systemMessage over-counts (includes future reminders): %q", sm)
	}
	if !strings.Contains(sm, "1 reminders pending") && !strings.Contains(sm, "1 reminder pending") {
		t.Errorf("systemMessage should report 1 surfaced reminder, got: %q", sm)
	}
}

func TestSessionStart_MixedDue_OnlyPastDue(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDue(t, root, "past due visible", -2)
	addReminderWithDue(t, root, "future silent", +2)
	addReminderWithDate(t, root, "legacy visible", 0)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "past due visible") {
		t.Errorf("past-due reminder missing: %q", ctx)
	}
	if !strings.Contains(ctx, "legacy visible") {
		t.Errorf("legacy reminder missing: %q", ctx)
	}
	if strings.Contains(ctx, "future silent") {
		t.Errorf("future reminder must be silent but appeared: %q", ctx)
	}
}

func TestSessionStart_CapAppliedToPastDueSet(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	for i := range 12 {
		addReminderWithDue(t, root, strings.Repeat("p", i+1)+" past due", -1)
	}
	for i := range 5 {
		addReminderWithDue(t, root, strings.Repeat("f", i+1)+" future", +1)
	}

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "Pending reminders (12)") {
		t.Errorf("header should show past-due count 12, not total 17: %q", ctx)
	}
	if !strings.Contains(ctx, "(and 2 more)") {
		t.Errorf("expected '(and 2 more)' overflow line: %q", ctx)
	}
	lines := strings.Split(ctx, "\n")
	bulletCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- [") {
			bulletCount++
		}
	}
	if bulletCount != 10 {
		t.Errorf("expected 10 reminder bullets, got %d", bulletCount)
	}
	if strings.Contains(ctx, "future") {
		t.Errorf("future reminders must not appear in output: %q", ctx)
	}
}

func TestSessionStartText_PastDueFilter(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDue(t, root, "past due text check", -1)
	addReminderWithDue(t, root, "future text silent", +1)

	out, err := hooks.SessionStartText(root, now)
	if err != nil {
		t.Fatalf("SessionStartText: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty text output")
	}
	if !strings.Contains(out, "past due text check") {
		t.Errorf("past-due reminder missing from text: %q", out)
	}
	if strings.Contains(out, "future text silent") {
		t.Errorf("future reminder must be silent in text: %q", out)
	}
	if !strings.Contains(out, "should-remind-user: true") {
		t.Errorf("should-remind-user marker missing from text output: %q", out)
	}
}

// systemMessage says "reminder" for one and "reminders" for more.
func TestSessionStart_SystemMessage_Pluralization(t *testing.T) {
	stubNoWherePosition(t)
	t.Run("singular", func(t *testing.T) {
		root := t.TempDir()
		now := time.Now().UTC()
		addReminderWithDate(t, root, "one old reminder", 15)

		out, err := hooks.SessionStart(root, now)
		if err != nil {
			t.Fatalf("SessionStart: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("not valid JSON: %v", err)
		}
		sm, ok := payload["systemMessage"].(string)
		if !ok || sm == "" {
			t.Fatalf("expected systemMessage, got %v", payload["systemMessage"])
		}
		if strings.Contains(sm, "1 reminders") {
			t.Errorf("grammar bug: got %q, want '1 reminder pending'", sm)
		}
		if !strings.Contains(sm, "1 reminder pending") {
			t.Errorf("expected '1 reminder pending' in systemMessage, got: %q", sm)
		}
	})

	t.Run("plural", func(t *testing.T) {
		root := t.TempDir()
		now := time.Now().UTC()
		addReminderWithDate(t, root, "old reminder one", 15)
		addReminderWithDate(t, root, "old reminder two", 15)

		out, err := hooks.SessionStart(root, now)
		if err != nil {
			t.Fatalf("SessionStart: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("not valid JSON: %v", err)
		}
		sm, ok := payload["systemMessage"].(string)
		if !ok || sm == "" {
			t.Fatalf("expected systemMessage, got %v", payload["systemMessage"])
		}
		if !strings.Contains(sm, "2 reminders pending") {
			t.Errorf("expected '2 reminders pending' in systemMessage, got: %q", sm)
		}
	})
}

func TestInstall_EmptyDir_RegistersInlineCommand(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// No wrapper script is written — the command is inlined into settings.json.
	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	if _, err := os.Stat(scriptPath); err == nil {
		t.Errorf("wrapper script should not be created, but %s exists", scriptPath)
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not found: %v", err)
	}
	if cmd := sessionStartCommandIn(t, raw); cmd != "atomic hooks session-start" {
		t.Errorf("registered command = %q, want %q", cmd, "atomic hooks session-start")
	}
}

// sessionStartCommandIn fails the test when the expected structure is absent.
func sessionStartCommandIn(t *testing.T, raw []byte) string {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json invalid JSON: %v", err)
	}
	hooks_, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks key missing: %v", settings)
	}
	ss, ok := hooks_["SessionStart"].([]any)
	if !ok || len(ss) == 0 {
		t.Fatalf("SessionStart missing: %v", hooks_)
	}
	entry, _ := ss[0].(map[string]any)
	inner, _ := entry["hooks"].([]any)
	if len(inner) == 0 {
		t.Fatalf("inner hooks missing: %v", entry)
	}
	h, _ := inner[0].(map[string]any)
	cmd, _ := h["command"].(string)
	return cmd
}

func TestInstall_ExistingUnrelatedKeys_Preserved(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"theme": "dark", "hooks": {"PreToolUse": [{"matcher": ".*", "hooks": [{"type": "command", "command": "echo hi"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)

	if settings["theme"] != "dark" {
		t.Errorf("theme key not preserved: %v", settings["theme"])
	}
	hooks_, _ := settings["hooks"].(map[string]any)
	if _, ok := hooks_["PreToolUse"]; !ok {
		t.Errorf("PreToolUse not preserved: %v", hooks_)
	}
	if _, ok := hooks_["SessionStart"]; !ok {
		t.Errorf("SessionStart not added: %v", hooks_)
	}
}

func TestInstall_Idempotent(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Errorf("expected 1 SessionStart entry after idempotent install, got %d", len(ss))
	}
}

func TestInstall_ExistingSessionStartElsewhere_Appends(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	initial := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "/other/hook.sh"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 2 {
		t.Errorf("expected 2 SessionStart entries after append, got %d", len(ss))
	}
}

func TestInstall_MalformedSettings_Refuses(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ not valid json "), 0o644)

	err := hooks.Install(repoRoot, scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}

	raw, _ := os.ReadFile(settingsPath)
	if string(raw) != "{ not valid json " {
		t.Errorf("malformed settings.json was modified: %q", string(raw))
	}
}

func TestInstall_ScopeProject_WritesUnderClaudeDir(t *testing.T) {
	projectRoot := t.TempDir()
	if err := hooks.Install(projectRoot, projectRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not found under project root: %v", err)
	}
	if cmd := sessionStartCommandIn(t, raw); cmd != "atomic hooks session-start" {
		t.Errorf("registered command = %q, want inline command", cmd)
	}
}

// A full install+uninstall cycle must not corrupt JWCC settings.json.
func TestInstall_JWCCSettingsPreservesCommentsAndTrailingCommas(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	jwcc := `{
  // user preference
  "theme": "dark",
  "model": "claude-opus-4-6", // pinned
}
`
	os.WriteFile(settingsPath, []byte(jwcc), 0o644)

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install on JWCC settings: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after install: %v", err)
	}

	if !strings.Contains(string(raw), "// user preference") {
		t.Errorf("install stripped JWCC comment from settings.json:\n%s", raw)
	}
	// The trailing comma after the last original key must survive (JWCC feature).
	if !strings.Contains(string(raw), `"claude-opus-4-6",`) {
		t.Errorf("install stripped trailing comma from JWCC settings.json:\n%s", raw)
	}

	if !strings.Contains(string(raw), "SessionStart") {
		t.Errorf("install did not add SessionStart to JWCC settings:\n%s", raw)
	}

	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall on JWCC settings: %v", err)
	}

	raw2, _ := os.ReadFile(settingsPath)
	if !strings.Contains(string(raw2), "// user preference") {
		t.Errorf("uninstall stripped JWCC comment from settings.json:\n%s", raw2)
	}
	if strings.Contains(string(raw2), "SessionStart") {
		t.Errorf("SessionStart should be removed after uninstall:\n%s", raw2)
	}
}

func TestUninstall_RemovesRegistration(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)

	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	installed, _, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("hook still registered after uninstall")
	}
}

// Uninstall removes the legacy wrapper script but not its siblings.
func TestUninstall_RemovesLegacyScriptFile(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)

	hooksDir := filepath.Join(scopeRoot, ".claude", "hooks")
	os.MkdirAll(hooksDir, 0o755)
	legacyPath := filepath.Join(hooksDir, "session-start-reminders.sh")
	os.WriteFile(legacyPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)
	siblingPath := filepath.Join(hooksDir, "other.sh")
	os.WriteFile(siblingPath, []byte("#!/bin/bash\necho other\n"), 0o755)

	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(legacyPath); err == nil {
		t.Error("legacy wrapper script still exists after uninstall")
	}
	if _, err := os.Stat(siblingPath); err != nil {
		t.Errorf("sibling hook removed: %v", err)
	}
}

func TestUninstall_DropsHooksKeyWhenEmpty(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)
	hooks.Uninstall(repoRoot, scopeRoot)

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)

	if _, has := settings["hooks"]; has {
		if m, ok := settings["hooks"].(map[string]any); ok && len(m) == 0 {
			t.Error("empty hooks object should be dropped from settings.json")
		}
	}
}

func TestUninstall_NoScript_NoError(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall without prior install: %v", err)
	}
}

func TestUninstall_PreservesOtherRegistrations(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	initial := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "/other/hook.sh"}]}, {"matcher": ".*", "hooks": [{"type": "command", "command": "` + scriptPath + `"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)
	os.MkdirAll(filepath.Dir(scriptPath), 0o755)
	os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)

	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Errorf("expected 1 remaining SessionStart entry, got %d", len(ss))
	}
	entry, _ := ss[0].(map[string]any)
	innerHooks, _ := entry["hooks"].([]any)
	innerHook, _ := innerHooks[0].(map[string]any)
	if innerHook["command"] != "/other/hook.sh" {
		t.Errorf("wrong remaining hook: %v", innerHook["command"])
	}
}

func TestIsInstalled_AfterInstall_InstalledNotDrifted(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("installed = false, want true")
	}
	if drifted {
		t.Error("drifted = true, want false")
	}
}

func TestIsInstalled_NoSettingsFile_NotInstalled(t *testing.T) {
	scopeRoot := t.TempDir()
	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("installed = true, want false")
	}
	if drifted {
		t.Error("drifted = true, want false")
	}
}

func TestIsInstalled_NoHookEntry_NotInstalled(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(settingsPath, []byte(`{"theme":"dark"}`), 0o644)

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("installed = true, want false")
	}
	if drifted {
		t.Error("drifted = true, want false")
	}
}

// seedLegacyRegistration registers the old wrapper-script path as the command.
func seedLegacyRegistration(t *testing.T, scopeRoot string) {
	t.Helper()
	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyCmd := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	content := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "` + legacyCmd + `"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsInstalled_LegacyRegistration_InstalledAndDrifted(t *testing.T) {
	scopeRoot := t.TempDir()
	seedLegacyRegistration(t, scopeRoot)

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("installed = false, want true (legacy registration still fires)")
	}
	if !drifted {
		t.Error("drifted = false, want true (legacy wrapper-script form)")
	}
}

func TestIsInstalled_MalformedSettings_Error(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ bad json"), 0o644)

	_, _, err := hooks.IsInstalled(scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}
}

// Install over a wrapper-script install must leave exactly one entry, so the
// hook cannot double-fire.
func TestInstall_MigratesLegacyRegistration(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	seedLegacyRegistration(t, scopeRoot)
	legacyPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	os.MkdirAll(filepath.Dir(legacyPath), 0o755)
	os.WriteFile(legacyPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(legacyPath); err == nil {
		t.Error("legacy wrapper script not removed by migration")
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Fatalf("expected 1 SessionStart entry after migration, got %d", len(ss))
	}
	if cmd := sessionStartCommandIn(t, raw); cmd != "atomic hooks session-start" {
		t.Errorf("post-migration command = %q, want inline command", cmd)
	}

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed || drifted {
		t.Errorf("post-migration IsInstalled = (installed=%v, drifted=%v), want (true, false)", installed, drifted)
	}
}

// The seam receives home itself, not <home>/.claude — config.ProfilePath expects
// the former. HOME is injected so the assertion never reads the real one.
func TestSessionStart_ProfileRefreshCalled(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	var gotHome, gotToday string
	var gotDays int
	hooks.ProfileRefresh = func(home, today string, days int) (bool, error) {
		gotHome = home
		gotToday = today
		gotDays = days
		return false, nil
	}
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })

	_, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	if gotDays != profile.DefaultRefreshDays {
		t.Errorf("profileRefresh called with days=%d, want profile.DefaultRefreshDays=%d", gotDays, profile.DefaultRefreshDays)
	}
	wantToday := now.Format("2006-01-02")
	if gotToday != wantToday {
		t.Errorf("profileRefresh called with today=%q, want %q", gotToday, wantToday)
	}
	if gotHome != home {
		t.Errorf("profileRefresh called with home=%q, want %q (not <home>/.claude)", gotHome, home)
	}
}

func TestSessionStartText_ProfileRefreshCalled(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	called := false
	hooks.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		called = true
		return false, nil
	}
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })

	_, err := hooks.SessionStartText(root, now)
	if err != nil {
		t.Fatalf("SessionStartText: %v", err)
	}
	if !called {
		t.Error("profileRefresh seam was not called by SessionStartText")
	}
}

// The refresh is best-effort: a disk failure must not break reminder injection.
func TestSessionStart_ProfileRefreshError_NeverBlocks(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "must still surface despite refresh error", 0)

	hooks.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		return false, fmt.Errorf("simulated refresh failure")
	}
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart returned error despite best-effort refresh: %v", err)
	}
	if out == "" {
		t.Fatal("expected reminder output even when refresh fails")
	}
	if !strings.Contains(out, "must still surface despite refresh error") {
		t.Errorf("reminder text missing from output: %q", out)
	}
}

// Nudges from the seam must reach additionalContext.
func TestSessionStart_WikiNudgesInjected(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()

	addReminderWithDate(t, root, "existing reminder", 0)

	hooks.WikiCheckStaleness = func(claudeHome string, thresholdDays int, runner func(string, ...string) error, clock func() time.Time) ([]string, error) {
		return []string{"wiki /some/path/wiki/index.md is stale: not refreshed in 45 days — run /refresh-wiki"}, nil
	}
	t.Cleanup(func() { hooks.WikiCheckStaleness = hooks.DefaultWikiCheckStaleness })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "wiki /some/path/wiki/index.md is stale") {
		t.Errorf("wiki nudge missing from additionalContext: %q", ctx)
	}
}

func TestSessionStart_WikiNudgesOnly_NoReminders(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WikiCheckStaleness = func(claudeHome string, thresholdDays int, runner func(string, ...string) error, clock func() time.Time) ([]string, error) {
		return []string{"wiki /home/user/wiki/index.md is stale: uncommitted changes since last refresh (.dirty) — run /refresh-wiki"}, nil
	}
	t.Cleanup(func() { hooks.WikiCheckStaleness = hooks.DefaultWikiCheckStaleness })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when only wiki nudges are present")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "wiki /home/user/wiki/index.md is stale") {
		t.Errorf("wiki nudge missing from additionalContext: %q", ctx)
	}
}

// Wiki staleness is best-effort: a read failure must not break the session.
func TestSessionStart_WikiError_NeverBlocks(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "must still surface despite wiki error", 0)

	hooks.WikiCheckStaleness = func(claudeHome string, thresholdDays int, runner func(string, ...string) error, clock func() time.Time) ([]string, error) {
		return nil, fmt.Errorf("simulated wiki staleness failure")
	}
	t.Cleanup(func() { hooks.WikiCheckStaleness = hooks.DefaultWikiCheckStaleness })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart returned error despite best-effort wiki check: %v", err)
	}
	if out == "" {
		t.Fatal("expected reminder output even when wiki check fails")
	}
	if !strings.Contains(out, "must still surface despite wiki error") {
		t.Errorf("reminder text missing from output: %q", out)
	}
}

// The seam gets the 30-day deterministic floor.
func TestSessionStart_WikiSeamReceivesThreshold30(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()

	var gotThreshold int
	called := false
	hooks.WikiCheckStaleness = func(claudeHome string, thresholdDays int, runner func(string, ...string) error, clock func() time.Time) ([]string, error) {
		called = true
		gotThreshold = thresholdDays
		return nil, nil
	}
	t.Cleanup(func() { hooks.WikiCheckStaleness = hooks.DefaultWikiCheckStaleness })

	_, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	if !called {
		t.Fatal("WikiCheckStaleness seam was never invoked")
	}
	if gotThreshold != 30 {
		t.Errorf("WikiCheckStaleness called with thresholdDays=%d, want 30", gotThreshold)
	}
}

// A plain position emits nothing — the hook stays silent unless relevant.
func TestSessionStart_WhereSuppressed_PlainPosition(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{}, nil // plain: RepoScope.Found=false, RealmScope.Position=RealmNone
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for plain position, got %q", out)
	}
}

// A repo-scope wiki surfaces one orientation line on its own.
func TestSessionStart_WhereSurfaced_RepoScopeFound(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RepoScope: where.RepoScopeReport{Found: true, Path: "/some/repo/docs/wiki/index.md"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "/some/repo/docs/wiki/index.md") {
		t.Errorf("orientation nudge missing repo-scope path: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}

	lines := strings.Split(ctx, "\n")
	bulletCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			bulletCount++
		}
	}
	if bulletCount != 1 {
		t.Errorf("expected exactly 1 orientation bullet, got %d: %q", bulletCount, ctx)
	}
}

// Realm-only position: the sibling test covers RepoScope.Found alone, leaving
// isPlainPosition's RealmScope clause and whereNudgeLine's switch unreached.
func TestSessionStart_WhereSurfaced_RealmMember(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RealmScope: where.RealmScopeReport{Position: where.RealmMember, RealmRoot: "/some/realm/root"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when realm position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "realm member of /some/realm/root") {
		t.Errorf("orientation nudge missing realm-member content: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}
}

// Covers whereNudgeLine's RealmRoot case.
func TestSessionStart_WhereSurfaced_RealmRoot(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RealmScope: where.RealmScopeReport{Position: where.RealmRoot, RealmRoot: "/some/realm/root"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when realm position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "realm root (/some/realm/root)") {
		t.Errorf("orientation nudge missing realm-root content: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}
}

// Covers whereNudgeLine's RealmOrphaned case.
func TestSessionStart_WhereSurfaced_RealmOrphaned(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RealmScope: where.RealmScopeReport{Position: where.RealmOrphaned, RealmRoot: "/some/realm/root"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when realm position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "orphaned under realm root /some/realm/root (not a registered member)") {
		t.Errorf("orientation nudge missing realm-orphaned content: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}
}

// The seam gets repoRoot as cwd, not the hook process's own working directory.
func TestSessionStart_WhereSeamReceivesRepoRoot(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()

	var gotCwd string
	hooks.WherePosition = func(cwd, _ string) (where.Report, error) {
		gotCwd = cwd
		return where.Report{}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	if _, err := hooks.SessionStart(root, now); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if gotCwd != root {
		t.Errorf("WherePosition called with cwd=%q, want repoRoot=%q", gotCwd, root)
	}
}

// Orientation is best-effort: a seam error must not block the session.
func TestSessionStart_WhereError_NeverBlocks(t *testing.T) {
	stubNoWikiStaleness(t)
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "must still surface despite where error", 0)

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{}, fmt.Errorf("simulated where resolution failure")
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart returned error despite best-effort where check: %v", err)
	}
	if out == "" {
		t.Fatal("expected reminder output even when where resolution fails")
	}
	if !strings.Contains(out, "must still surface despite where error") {
		t.Errorf("reminder text missing from output: %q", out)
	}
}

func TestUninstall_MalformedSettings_Refuses(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ broken"), 0o644)

	err := hooks.Uninstall(repoRoot, scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}
	raw, _ := os.ReadFile(settingsPath)
	if string(raw) != "{ broken" {
		t.Errorf("malformed settings.json was modified")
	}
}
