// CP8A session scenarios: real Claude and OMP launches over isolated homes for
// the primary and resumed rows, with the model-gated child and deny rows
// recorded as loud skips rather than silent passes.
package runtimeproof

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// TestCP8AClaudeSessionRows proves the Claude primary and resumed rows over an
// isolated adapter-generated home: the startup session loads the global steering
// once, and a resumed session rediscovers the same guidance with a resume start.
//
// Criterion: Milestone A passes Claude runtime scenarios for resumed sessions
// with no duplicate steering application.
func TestCP8AClaudeSessionRows(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/session/claude-primary-resumed",
		Group:     "session",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "a Claude primary session loads global steering once and a resumed session rediscovers it without duplication",
		Command:   "claude -p (isolated CLAUDE_CONFIG_DIR) → claude -p --resume",
	}
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real Claude launch is skipped")
		return
	}
	binary, err := ClaudeCLI()
	if err != nil {
		recordScenarioSkip(t, ev, "claude unavailable: "+err.Error())
		return
	}

	home, configDir, repo := claudeGuidanceHome(t)
	globalPath := filepath.Join(configDir, "CLAUDE.md")
	ev.Paths = []string{home, configDir, repo}

	primary, err := Run(context.Background(), claudeLaunch(t, binary, home, configDir, repo, "cp8a-claude-primary"))
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("claude exceeded its bound: %+v", primary)
	}
	if err != nil {
		t.Fatalf("primary run: %v", err)
	}
	primaryCounts := claudeGuidanceCounts(primary)
	if primaryCounts[globalPath] != 1 {
		t.Errorf("primary session loaded global steering %d times, want 1: %v", primaryCounts[globalPath], sortedCounts(primaryCounts))
	}
	started := false
	for _, e := range primary.Events {
		if e.Name == "SessionStart" {
			started = true
		}
	}
	if !started {
		t.Errorf("the primary launch reported no SessionStart event")
	}

	sessionID := claudeSessionID(primary.Stdout)
	if sessionID == "" {
		recordScenarioSkip(t, ev, "the launch reported no session id, so the resume row cannot be exercised")
		return
	}
	resumeScenario := claudeLaunch(t, binary, home, configDir, repo, "cp8a-claude-resume")
	resumeScenario.Args = []string{"-p", "--output-format", "json", "--resume", sessionID, DefaultPrompt}
	resumed, err := Run(context.Background(), resumeScenario)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("claude exceeded its bound: %+v", resumed)
	}
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	resumeCounts := claudeGuidanceCounts(resumed)
	if resumeCounts[globalPath] != 1 {
		t.Errorf("resumed session loaded global steering %d times, want 1: %v", resumeCounts[globalPath], sortedCounts(resumeCounts))
	}
	if !observedResume(resumed) {
		t.Errorf("the resume launch reported no SessionStart source=resume")
	}

	ev.Outcome = "primary loaded global steering once; resumed session rediscovered it once with a resume start"
	recordScenario(t, ev)
}

// TestCP8AClaudeChildSessionUncovered records the Claude child-session row as
// uncovered: a child exists only after a model Task call, which the
// startup-only runner cannot issue.
//
// Criterion: child-session behavior remains unsupported and is reported as
// uncovered, never claimed.
func TestCP8AClaudeChildSessionUncovered(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/session/claude-child-uncovered",
		Group:     "session",
		Engine:    "capability record",
		Criterion: "Claude child-session delivery stays unsupported and uncovered",
		Command:   "none — the startup-only runner cannot issue a model Task call",
	}
	recordScenarioSkip(t, ev, "CP0 lists Claude child-session delivery unsupported: a child needs a model Task call, which the startup-only runner cannot issue")
}

// TestCP8AOMPPrimarySession proves a real OMP launch against an isolated home
// carrying the generated Atomic extension runs one factory and one session.
//
// Criterion: Milestone A passes OMP runtime scenarios for the primary session.
func TestCP8AOMPPrimarySession(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/session/omp-primary",
		Group:     "session",
		Engine:    "runtimeproof (real OMP)",
		Criterion: "an OMP primary session runs exactly one auto-discovered Atomic factory and one session",
		Command:   "omp (isolated HOME) with the generated extension",
	}
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real OMP launch is skipped")
		return
	}
	if _, err := OMPCLI(); err != nil {
		recordScenarioSkip(t, ev, "omp unavailable: "+err.Error())
		return
	}
	dir := t.TempDir()
	home, err := NewOMPHome(filepath.Join(dir, "home"), runtimeModule(t, nil))
	if err != nil {
		t.Fatalf("prepare home: %v", err)
	}
	repo := filepath.Join(dir, "repo")
	writeTree(t, repo, map[string]string{"src/a.ts": "export const a = 1;\n"})
	ev.Paths = []string{home.AgentRoot, repo}

	scenario, err := home.Launch(OMPLaunch{Name: "cp8a-omp-primary", WorkDir: repo, Prompt: "Reply OK.", Log: home.LogPath("atomic-runtime-primary.jsonl")})
	if err != nil {
		t.Fatalf("build launch: %v", err)
	}
	scenario.Args = append([]string{"--tools", "read,bash"}, scenario.Args...)
	obs, err := Run(context.Background(), scenario)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("omp exceeded its bound: %+v", obs)
	}
	if err != nil {
		t.Fatalf("run omp: %v", err)
	}
	log, err := OMPRuntimeLog(obs, "atomic-runtime-primary.jsonl")
	if err != nil {
		t.Fatalf("parse runtime log: %v", err)
	}
	if log.Factories != 1 || log.Sessions != 1 {
		t.Fatalf("factories/sessions = %d/%d, want one each", log.Factories, log.Sessions)
	}
	if _, ok := obs.Files["extensions/atomic.ts"]; !ok {
		t.Errorf("the isolated agent root does not retain the discovered extension module")
	}

	ev.Outcome = "one auto-discovered factory and one session observed"
	recordScenario(t, ev)
}

// TestCP8AOMPChildAndDenyUncovered records the model-gated OMP rows — a
// credentialed child session and the exact-predicate deny — as uncovered: this
// matrix is startup-only and issues no model request, so the rows stay uncovered
// even when a provider credential exists. The credentialed runs live in the
// CP4B2 runtime scenarios (omp_test.go); claiming them here would assert
// coverage this suite does not perform.
//
// Criterion: the OMP child-session and exact-predicate deny rows are uncovered
// in this matrix and defer to CP4B2's credentialed evidence.
func TestCP8AOMPChildAndDenyUncovered(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/session/omp-child-deny-uncovered",
		Group:     "session",
		Engine:    "capability record",
		Criterion: "the OMP child-session and exact-predicate deny rows are uncovered in this startup-only matrix",
		Command:   "none — the credentialed child/deny probes live in the CP4B2 runtime scenarios",
	}
	reason := "this matrix issues no model request; the credentialed child/deny rows are exercised by the CP4B2 runtime scenarios (omp_test.go)"
	if _, ok := OMPModelToken(); ok {
		reason = "a provider credential is configured, but this matrix issues no model request; the credentialed child/deny rows are exercised by the CP4B2 runtime scenarios (omp_test.go)"
	}
	recordScenarioSkip(t, ev, reason)
}
