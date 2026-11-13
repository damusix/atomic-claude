package hooks_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

var preToolUseNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cardRepo is a repository holding one wiki card per domain, each scoped to the
// glob it is given.
func cardRepo(t *testing.T, cards map[string]string) (repo, home string) {
	t.Helper()
	repo = t.TempDir()
	home = t.TempDir()
	for domain, glob := range cards {
		writeTestFile(t, filepath.Join(repo, ".atomic", "rules", "wiki", domain+".md"),
			"---\npaths:\n  - \""+glob+"\"\n---\nDomain: "+domain+". Read docs/wiki/"+domain+".md first.\n")
	}
	return repo, home
}

func patch(headers ...string) string {
	return "*** Begin Patch\n" + strings.Join(headers, "\n") + "\n*** End Patch\n"
}

func payload(t *testing.T, session, cwd, command string) *strings.Reader {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"session_id": session, "cwd": cwd, "hook_event_name": "PreToolUse", "tool_name": "apply_patch",
		"tool_input": map[string]any{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(string(data))
}

func cardContext(t *testing.T, out string) string {
	t.Helper()
	var got struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not the hook JSON: %v\n%s", err, out)
	}
	if got.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", got.HookSpecificOutput.HookEventName)
	}
	return got.HookSpecificOutput.AdditionalContext
}

func TestPreToolUseDeliversTheCardsMatchingAnEditsPaths(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**", "docs": "docs/**"})
	out := hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: src/a.go", "*** Add File: src/b.go")), home, preToolUseNow)
	got := cardContext(t, out)
	if !strings.Contains(got, "Domain: src.") || strings.Contains(got, "Domain: docs.") {
		t.Errorf("context = %q, want only the src card", got)
	}
	if strings.Count(got, "Domain: src.") != 1 {
		t.Errorf("two matching paths delivered the card more than once:\n%s", got)
	}
}

func TestPreToolUseAddFileHeaderMatchesToo(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"docs": "docs/**"})
	out := hooks.PreToolUse(payload(t, "s1", repo, patch("*** Add File: docs/new.md")), home, preToolUseNow)
	if !strings.Contains(cardContext(t, out), "Domain: docs.") {
		t.Errorf("an Add File header delivered no card:\n%s", out)
	}
}

func TestPreToolUseResolvesPatchPathsAgainstTheSessionCwd(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**"})
	sub := filepath.Join(repo, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	out := hooks.PreToolUse(payload(t, "s1", sub, patch("*** Update File: a.go")), home, preToolUseNow)
	if !strings.Contains(cardContext(t, out), "Domain: src.") {
		t.Errorf("a path relative to a subdirectory cwd matched no card:\n%q", out)
	}
}

func TestPreToolUseStaysSilentWhenNoCardMatches(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**"})
	if out := hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: README.md")), home, preToolUseNow); out != "" {
		t.Errorf("output = %q, want none", out)
	}
}

// The model has already read a card once; resending it on every edit spends
// context for nothing.
func TestPreToolUseSendsEachCardOncePerSession(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**"})
	edit := patch("*** Update File: src/a.go")
	if out := hooks.PreToolUse(payload(t, "s1", repo, edit), home, preToolUseNow); out == "" {
		t.Fatal("the first edit delivered nothing")
	}
	if out := hooks.PreToolUse(payload(t, "s1", repo, edit), home, preToolUseNow); out != "" {
		t.Errorf("a second matching edit in the session resent the card: %q", out)
	}
	if out := hooks.PreToolUse(payload(t, "s2", repo, edit), home, preToolUseNow); out == "" {
		t.Error("another session got no card")
	}
}

func TestPreToolUseSendsOnlyTheCardsNotYetSent(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**", "docs": "docs/**"})
	hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: src/a.go")), home, preToolUseNow)
	got := cardContext(t, hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: src/a.go", "*** Update File: docs/x.md")), home, preToolUseNow))
	if strings.Contains(got, "Domain: src.") || !strings.Contains(got, "Domain: docs.") {
		t.Errorf("context = %q, want only the docs card", got)
	}
}

// The state file would otherwise grow by one entry per session forever.
func TestPreToolUseWriteDropsSessionsLastSeenOverSevenDaysAgo(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**"})
	stamp := func(age time.Duration) string { return preToolUseNow.Add(-age).Format(time.RFC3339) }
	writeTestFile(t, config.CodexCardsSentPath(home), `{"sessions":{`+
		`"stale":{"last_seen":"`+stamp(7*24*time.Hour+time.Minute)+`","cards":["x"]},`+
		`"recent":{"last_seen":"`+stamp(7*24*time.Hour-time.Minute)+`","cards":["y"]}}}`)

	hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: src/a.go")), home, preToolUseNow)

	data, err := os.ReadFile(config.CodexCardsSentPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Sessions map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Sessions["stale"]; ok {
		t.Error("a session last seen over 7 days ago survived the write")
	}
	if _, ok := state.Sessions["recent"]; !ok {
		t.Error("a session last seen within 7 days was dropped")
	}
	if _, ok := state.Sessions["s1"]; !ok {
		t.Error("the current session was not recorded")
	}
}

// A hook that errors would fail the model's edit; the cards are only a hint.
func TestPreToolUseFailsOpenWithNoOutput(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**"})
	edit := patch("*** Update File: src/a.go")

	broken, brokenHome := cardRepo(t, nil)
	writeTestFile(t, filepath.Join(broken, ".atomic", "rules", "wiki", "bad.md"), "---\npaths: [\n---\nbody\n")

	for name, got := range map[string]string{
		"not json":        hooks.PreToolUse(strings.NewReader("{"), home, preToolUseNow),
		"empty":           hooks.PreToolUse(strings.NewReader(""), home, preToolUseNow),
		"no session":      hooks.PreToolUse(payload(t, "", repo, edit), home, preToolUseNow),
		"missing cwd":     hooks.PreToolUse(payload(t, "s1", filepath.Join(repo, "gone"), edit), home, preToolUseNow),
		"unreadable card": hooks.PreToolUse(payload(t, "s1", broken, edit), brokenHome, preToolUseNow),
	} {
		if got != "" {
			t.Errorf("%s: output = %q, want none", name, got)
		}
	}
}

func TestPreToolUseMatchesTheDestinationOfAMove(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"docs": "docs/**"})
	out := hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: README.md", "*** Move to: docs/README.md")), home, preToolUseNow)
	if !strings.Contains(cardContext(t, out), "Domain: docs.") {
		t.Errorf("a Move to destination matched no card:\n%q", out)
	}
}

// The canonical card carries description and globs beside paths; the fail-silent
// hook must still deliver its body.
func TestPreToolUseDeliversACanonicalCardWithDescriptionAndGlobs(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(repo, ".atomic", "rules", "wiki", "src.md"),
		"---\ndescription: \"Domain src: read the map.\"\nglobs:\n  - \"src/**\"\npaths:\n  - \"src/**\"\n---\nDomain: src. Read docs/wiki/src.md first.\n")
	out := hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: src/a.go")), home, preToolUseNow)
	if !strings.Contains(cardContext(t, out), "Domain: src.") {
		t.Errorf("a canonical card delivered nothing:\n%q", out)
	}
}

// Codex may name the repository through a symlink while Git reports the real path.
func TestPreToolUseResolvesAnAbsolutePatchPathThroughASymlink(t *testing.T) {
	repo, home := cardRepo(t, map[string]string{"src": "src/**"})
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	out := hooks.PreToolUse(payload(t, "s1", repo, patch("*** Update File: "+filepath.Join(link, "src", "a.go"))), home, preToolUseNow)
	if !strings.Contains(cardContext(t, out), "Domain: src.") {
		t.Errorf("an absolute path through a symlinked root matched no card:\n%q", out)
	}
}
