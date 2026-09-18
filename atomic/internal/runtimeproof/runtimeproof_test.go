package runtimeproof

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scenario builds an isolated launch wired to the Claude fixtures: the home
// template carries global steering plus an unconditional user rule, and the repo
// template carries an unconditional and a path-scoped project rule.
func scenario(t *testing.T) Scenario {
	t.Helper()
	root := t.TempDir()
	return Scenario{
		Name:         "fixture",
		Home:         filepath.Join(root, "home"),
		ConfigDir:    filepath.Join(root, "home", ".claude"),
		WorkDir:      filepath.Join(root, "repo"),
		EventLog:     filepath.Join(root, "events.jsonl"),
		Template:     filepath.Join("testdata", "claude-home"),
		WorkTemplate: filepath.Join("testdata", "claude-repo"),
		Timeout:      10 * time.Second,
	}
}

func stubHarness(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stub-harness")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub harness: %v", err)
	}
	return path
}

// A launch that reaches startup yields the events and the files it touched, and
// the runner installs the observer hooks without a model response.
func TestRunCollectsStartupEventsAndFiles(t *testing.T) {
	s := scenario(t)
	s.Harness = stubHarness(t, `#!/bin/sh
{
  printf '%s\n' '{"hook_event_name":"SessionStart","source":"startup","session_id":"stub"}'
  printf '{"hook_event_name":"InstructionsLoaded","file_path":"%s/rules/user.md","memory_type":"User","load_reason":"session_start","session_id":"stub"}\n' "$CLAUDE_CONFIG_DIR"
} >> "$RUNTIMEPROOF_EVENT_LOG"
exit 0
`)

	obs, err := Run(context.Background(), s)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if obs.ExitCode != 0 || obs.TimedOut {
		t.Fatalf("observation = %+v", obs)
	}
	if len(obs.Events) != 2 {
		t.Fatalf("events = %+v, want two", obs.Events)
	}
	if obs.Events[0].Name != "SessionStart" || obs.Events[0].Source != "startup" {
		t.Errorf("first event = %+v", obs.Events[0])
	}
	if got := obs.Events[1]; got.Name != "InstructionsLoaded" || got.MemoryType != "User" || got.LoadReason != "session_start" {
		t.Errorf("second event = %+v", got)
	}
	if !strings.HasSuffix(obs.Events[1].FilePath, "/rules/user.md") {
		t.Errorf("event file path = %q", obs.Events[1].FilePath)
	}

	if got := obs.Files["CLAUDE.md"]; !strings.Contains(got, "RUNTIMEPROOF_GLOBAL_MARKER") {
		t.Errorf("global steering not observed: %q", got)
	}
	if got := obs.Files["rules/user.md"]; !strings.Contains(got, "RUNTIMEPROOF_USER_RULE_MARKER") {
		t.Errorf("user rule not observed: %q", got)
	}
	settings, ok := obs.Files["settings.json"]
	if !ok {
		t.Fatalf("observer settings were not installed: %v", keys(obs.Files))
	}
	for _, event := range []string{"SessionStart", "InstructionsLoaded"} {
		if !strings.Contains(settings, event) {
			t.Errorf("settings.json does not observe %s: %s", event, settings)
		}
	}
	if got := obs.WorkFiles[".claude/rules/scoped.md"]; !strings.Contains(got, "paths:") {
		t.Errorf("path-scoped project rule not observed: %q", got)
	}
	if got := obs.WorkFiles["nested/target.ts"]; !strings.Contains(got, "runtimeproof") {
		t.Errorf("nested target not observed: %q", got)
	}
}

// A nonzero exit status is evidence, not a launch failure: the startup
// observation is still returned.
func TestRunReportsNonzeroExitAsEvidence(t *testing.T) {
	s := scenario(t)
	s.Harness = stubHarness(t, "#!/bin/sh\nexit 3\n")

	obs, err := Run(context.Background(), s)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if obs.ExitCode != 3 || obs.TimedOut {
		t.Errorf("observation = %+v, want exit 3", obs)
	}
	if len(obs.Events) != 0 {
		t.Errorf("events = %+v, want none", obs.Events)
	}
	if _, ok := obs.Files["settings.json"]; !ok {
		t.Error("observer settings were not installed")
	}
}

// A scenario that overruns its bound is killed and reported, and the bound is
// actually respected.
func TestRunBoundsAndReportsTimeout(t *testing.T) {
	s := scenario(t)
	s.Harness = stubHarness(t, "#!/bin/sh\nsleep 10\n")
	s.Timeout = 300 * time.Millisecond

	start := time.Now()
	obs, err := Run(context.Background(), s)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
	if !obs.TimedOut {
		t.Errorf("observation = %+v, want TimedOut", obs)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout was not respected: ran %s", elapsed)
	}
}

// The launch environment cannot carry a model credential and cannot point at
// another native root, so the observation is a startup observation.
func TestRunIsolatesCredentialsAndRoot(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("ANTHROPIC_BASE_URL", "https://example.invalid")
	t.Setenv("CLAUDE_CONFIG_DIR", "/elsewhere")

	s := scenario(t)
	s.Harness = stubHarness(t, `#!/bin/sh
printf '%s|%s|%s\n' "${ANTHROPIC_API_KEY-unset}" "${ANTHROPIC_BASE_URL-unset}" "$CLAUDE_CONFIG_DIR"
printf '%s\n' "$HOME"
`)

	obs, err := Run(context.Background(), s)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(obs.Stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q", obs.Stdout)
	}
	want := "unset|unset|" + s.ConfigDir
	if lines[0] != want {
		t.Errorf("launch env = %q, want %q", lines[0], want)
	}
	if lines[1] != s.Home {
		t.Errorf("launch HOME = %q, want %q", lines[1], s.Home)
	}
	if _, err := os.Stat(s.EventLog); err != nil {
		t.Errorf("event log missing: %v", err)
	}
}

func TestRunRejectsIncompleteScenario(t *testing.T) {
	base := scenario(t)
	for name, mutate := range map[string]func(*Scenario){
		"Home":      func(s *Scenario) { s.Home = "" },
		"ConfigDir": func(s *Scenario) { s.ConfigDir = "" },
		"WorkDir":   func(s *Scenario) { s.WorkDir = "" },
		"EventLog":  func(s *Scenario) { s.EventLog = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := base
			mutate(&s)
			if _, err := Run(context.Background(), s); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error = %v, want a %s refusal", err, name)
			}
		})
	}
}

// TestRunRealClaudeStartup drives the real Claude binary against an isolated
// home. It proves the seam launches, reaches startup, and collects rule-discovery
// evidence without a model response; it makes no claim about Claude's native rule
// matching, which CP8A judges against the capability matrix.
func TestRunRealClaudeStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	harness, err := ClaudeCLI()
	if err != nil {
		t.Skipf("claude not available: %v", err)
	}

	s := scenario(t)
	s.Name = "claude-startup"
	s.Harness = harness
	s.Timeout = 60 * time.Second

	obs, err := Run(context.Background(), s)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("claude exceeded its bound: %+v", obs)
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sessions, loaded int
	var ruleFiles []string
	for _, e := range obs.Events {
		switch e.Name {
		case "SessionStart":
			sessions++
		case "InstructionsLoaded":
			loaded++
			ruleFiles = append(ruleFiles, e.FilePath)
		}
	}
	if sessions == 0 {
		t.Fatalf("no SessionStart event reached the observer: %+v", obs)
	}
	if loaded == 0 {
		t.Fatalf("no InstructionsLoaded event reached the observer: %+v", obs)
	}
	if !strings.Contains(obs.Files["settings.json"], "SessionStart") {
		t.Error("observer settings were not installed in the isolated root")
	}

	// The tier this checkpoint projects rests on CP0's observation that an
	// unconditional user rule loaded at startup while a path-scoped project rule
	// did not. If a newer Claude contradicts that, the capability record is stale
	// and CP0 must be re-run before the tier changes.
	var userRule, scopedRule bool
	for _, file := range ruleFiles {
		switch {
		case strings.HasSuffix(file, "/rules/user.md"):
			userRule = true
		case strings.HasSuffix(file, "/rules/scoped.md"):
			scopedRule = true
		}
	}
	if !userRule {
		t.Errorf("the unconditional user rule did not load at startup: %v", ruleFiles)
	}
	if scopedRule {
		t.Errorf("a path-scoped rule loaded at startup, contradicting the CP0 record; re-run CP0 before changing the Claude rule tier: %v", ruleFiles)
	}
	t.Logf("claude %s exited %d after %s; %d instruction files loaded: %v", harness, obs.ExitCode, obs.Duration, loaded, ruleFiles)
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
