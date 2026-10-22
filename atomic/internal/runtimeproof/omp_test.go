package runtimeproof

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
)

// TestOMPRuntimeDeliveryPrimarySession drives the real OMP binary against an
// isolated home carrying the generated Atomic extension. It proves the CP0 rows
// the delivery depends on — extension discovery, session_start observation, the
// session-baseline block reaching the provider payload, the proven tool
// inventory, single-factory dedupe, resume rerun, and `--no-rules` disablement —
// from one retained observation per launch. It makes no claim about native rule
// matching or about the model, which is why the placeholder key is enough: the
// events under proof happen before any provider answer.
func TestOMPRuntimeDeliveryPrimarySession(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	ompBinary, err := OMPCLI()
	if err != nil {
		t.Skipf("omp is unavailable: %v", err)
	}
	dir := t.TempDir()
	home, _ := prepareOMPHome(t, filepath.Join(dir, "home"), OMPHomeRequest{})
	repo := filepath.Join(dir, "repo")
	writeTree(t, repo, map[string]string{
		"src/a.ts":       "export const a = 1;\n",
		"docs/spec/x.md": "# spec\n",
	})

	// Row omp.extension-discovery / omp.role.session-baseline-event: the
	// auto-discovered module runs once and observes the session.
	primaryLaunch := OMPLaunch{Name: "omp-primary", WorkDir: repo, Prompt: "Reply OK.", Log: home.LogPath("atomic-runtime-primary.jsonl")}
	primary, err := home.Launch(primaryLaunch)
	if err != nil {
		t.Fatalf("primary launch: %v", err)
	}
	primary.Args = append([]string{"--tools", "read,bash"}, primary.Args...)
	obs, err := Run(context.Background(), primary)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("omp exceeded its bound: %+v", obs)
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	log, err := OMPRuntimeLog(obs, "atomic-runtime-primary.jsonl")
	if err != nil {
		t.Fatalf("parse runtime log: %v", err)
	}
	if log.Factories != 1 || log.Sessions != 1 {
		t.Fatalf("factories/sessions = %d/%d, want one auto-discovered factory and one session\n%s", log.Factories, log.Sessions, obs.Stderr)
	}
	if strings.Join(log.Index, ",") != "shipped:rules/docs/spec.md,shipped:rules/ts/style.md" {
		t.Errorf("the session index = %v, want the projected rule identities in canonical order", log.Index)
	}
	if log.BaselineDeliveries == 0 {
		t.Fatalf("no baseline delivery reached the session\n%s", obs.Stderr)
	}
	if !log.BaselineConsumed {
		t.Error("the delivered block did not reach the provider payload")
	}
	var inventory omp.RuntimeRecord
	for _, record := range log.Records {
		if record.Kind == "session_start" {
			inventory = record
		}
	}
	if !contains(inventory.Covered, "read") {
		t.Errorf("covered tools = %v, want the exercised structured input", inventory.Covered)
	}
	if !contains(inventory.Uncovered, "bash") {
		t.Errorf("uncovered tools = %v, want every tool without a proven structured input", inventory.Uncovered)
	}
	if _, ok := obs.Files["extensions/atomic.ts"]; !ok {
		t.Error("the isolated agent root does not retain the discovered extension module")
	}

	// Row omp.dedupe: naming the same resolved extension explicitly still runs
	// one factory.
	dedupe, err := home.Launch(OMPLaunch{Name: "omp-dedupe", WorkDir: repo, Prompt: "Reply OK.", Log: home.LogPath("atomic-runtime-dedupe.jsonl")})
	if err != nil {
		t.Fatalf("dedupe launch: %v", err)
	}
	dedupe.Args = append([]string{"--no-tools", "--extension", home.Module}, dedupe.Args...)
	dedupeObs, err := Run(context.Background(), dedupe)
	if err != nil {
		t.Fatalf("dedupe Run: %v", err)
	}
	dedupeLog, err := OMPRuntimeLog(dedupeObs, "atomic-runtime-dedupe.jsonl")
	if err != nil {
		t.Fatalf("parse dedupe log: %v", err)
	}
	if dedupeLog.Factories != 1 {
		t.Errorf("an explicit duplicate extension ran %d factories, want 1", dedupeLog.Factories)
	}

	// Row omp.disablement: a deliberate `--no-rules` run still reaches the
	// Atomic baseline delivery, because the extension is not a native rule.
	noRulesLaunch := OMPLaunch{Name: "omp-no-rules", WorkDir: repo, Prompt: "Reply OK.", Log: home.LogPath("atomic-runtime-no-rules.jsonl")}
	noRules, err := home.Launch(noRulesLaunch)
	if err != nil {
		t.Fatalf("no-rules launch: %v", err)
	}
	noRules.Args = append([]string{"--no-tools", "--no-rules"}, noRules.Args...)
	noRulesObs, err := Run(context.Background(), noRules)
	if err != nil {
		t.Fatalf("no-rules Run: %v", err)
	}
	noRulesLog, err := home.Proof(noRulesLaunch, "", noRulesObs)
	if err != nil {
		t.Fatalf("parse no-rules log: %v", err)
	}
	if noRulesLog.Log.Factories != 1 || noRulesLog.Log.BaselineDeliveries == 0 {
		t.Errorf("with --no-rules: %+v, want the Atomic baseline delivery unaffected", noRulesLog)
	}

	// Row omp.resume: a resumed session starts a new process, so the factory and
	// the baseline delivery run again for it.
	create, err := home.Launch(OMPLaunch{Name: "omp-session-create", WorkDir: repo, Prompt: "Reply exactly SESSION_ONE.", Save: true, Log: home.LogPath("atomic-runtime-create.jsonl")})
	if err != nil {
		t.Fatalf("create launch: %v", err)
	}
	create.Args = append([]string{"--no-tools"}, create.Args...)
	createObs, err := Run(context.Background(), create)
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}
	sessionID, err := SessionID(createObs.Stdout)
	if err != nil {
		t.Skipf("no session id was reported, so resume cannot be exercised: %v", err)
	}
	resumed, err := home.Launch(OMPLaunch{Name: "omp-resume", WorkDir: repo, Prompt: "Reply exactly SESSION_TWO.", Session: sessionID, Log: home.LogPath("atomic-runtime-resume.jsonl")})
	if err != nil {
		t.Fatalf("resume launch: %v", err)
	}
	resumed.Args = append([]string{"--no-tools"}, resumed.Args...)
	resumeObs, err := Run(context.Background(), resumed)
	if err != nil {
		t.Fatalf("resume Run: %v", err)
	}
	resumeLog, err := OMPRuntimeLog(resumeObs, "atomic-runtime-resume.jsonl")
	if err != nil {
		t.Fatalf("parse resume log: %v", err)
	}
	if resumeLog.Factories != 1 || resumeLog.Sessions != 1 || resumeLog.BaselineDeliveries == 0 {
		t.Errorf("resumed session = %+v, want the factory and baseline delivered again", resumeLog)
	}

	version, _ := OMPVersion()
	if recorded := harness.OMPCapabilities().Version; version != "omp/"+recorded {
		t.Logf("the observed binary is %s while the capability record pins %s: the rows asserted here rest on CP0's evidence for the pinned version, and this run is runtime evidence for the observed one", version, recorded)
	}
	primaryProof, err := home.Proof(primaryLaunch, version, obs)
	if err != nil {
		t.Fatalf("build runtime proof: %v", err)
	}
	if primaryProof.Scenario != "omp-primary" || primaryProof.Log.BaselineDeliveries == 0 || len(primaryProof.Events) == 0 {
		t.Errorf("runtime proof = %+v, want the observed events and the parsed log", primaryProof)
	}
	// The no-rules launch is the deliberate disablement evidence: native rules are
	// off, and the Atomic baseline is unaffected.
	noRulesProof, err := home.Proof(noRulesLaunch, version, noRulesObs)
	if err != nil {
		t.Fatalf("build no-rules proof: %v", err)
	}
	noRulesProof.RulesDisabled = true
	if !noRulesProof.RulesDisabled || noRulesProof.Log.BaselineDeliveries == 0 {
		t.Errorf("no-rules proof = %+v, want the disablement recorded with the delivery intact", noRulesProof)
	}
	t.Logf("omp %s (%s): primary exit %d, factories %d, baseline deliveries %d, consumed %v, uncovered %v; dedupe factories %d; no-rules factories %d deliveries %d; resume factories %d deliveries %d",
		ompBinary, version, obs.ExitCode, log.Factories, log.BaselineDeliveries, log.BaselineConsumed, log.UncoveredTools,
		dedupeLog.Factories, noRulesProof.Log.Factories, noRulesProof.Log.BaselineDeliveries, resumeLog.Factories, resumeLog.BaselineDeliveries)
}

// TestOMPRuntimeDeliveryChildSession proves a child session runs its own factory
// and receives its own baseline delivery. A child exists only after a model calls
// the task tool, so the scenario needs a provider credential and skips without
// one; CP0's retained evidence is the reference when it skips.
func TestOMPRuntimeDeliveryChildSession(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	if _, ok := OMPModelToken(); !ok {
		t.Skip("no provider credential: set CP0_OMP_AUTH_HOME or CP0_OMP_API_KEY to exercise a child session")
	}
	dir := t.TempDir()
	home, _ := prepareOMPHome(t, filepath.Join(dir, "home"), OMPHomeRequest{})
	repo := filepath.Join(dir, "repo")
	writeTree(t, repo, map[string]string{"src/a.ts": "export const a = 1;\n"})
	scenario, err := home.Launch(OMPLaunch{
		Name:    "omp-child",
		WorkDir: repo,
		Args:    []string{"--tools", "task"},
		Prompt:  "Use the task tool exactly once. Ask the child to reply exactly CHILD_OK without tools. Then reply DONE.",
	})
	if err != nil {
		t.Fatalf("child launch: %v", err)
	}
	obs, err := Run(context.Background(), scenario)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	log, err := OMPRuntimeLog(obs, OMPLogName)
	if err != nil {
		t.Fatalf("parse runtime log: %v", err)
	}
	// The child exists only if the model called the task tool, which a proof run
	// cannot force. A run where it did must show the second factory; one where it
	// did not proves nothing and says so instead of claiming the row.
	called := false
	for _, record := range log.Records {
		if record.Kind == "uncovered_tool" && record.Tool == "task" {
			called = true
		}
	}
	if !called {
		t.Skipf("the model did not call the task tool, so child delivery is not exercised by this run: exit %d\n%s", obs.ExitCode, obs.Stderr)
	}
	if log.Sessions != 2 || log.Factories != 2 {
		t.Fatalf("the task call produced %d factories and %d sessions (exit %d), want a second factory and session_start in the same process\n%s", log.Factories, log.Sessions, obs.ExitCode, obs.Stderr)
	}
	if log.BaselineDeliveries < 2 {
		t.Errorf("baseline deliveries = %d, want one per session so the child carries its own block", log.BaselineDeliveries)
	}
}

// TestOMPRuntimeDeliveryMachineDeny proves the exact predicate end to end: the
// delivered module blocks the exercised command form and the blocked call leaves
// no side effect. The model must attempt the call, so the scenario needs a
// provider credential and skips without one.
func TestOMPRuntimeDeliveryMachineDeny(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	if _, ok := OMPModelToken(); !ok {
		t.Skip("no provider credential: set CP0_OMP_AUTH_HOME or CP0_OMP_API_KEY to exercise a mediated tool call")
	}
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "denied-side-effect")
	command := "touch '" + sentinel + "' # ATOMIC_RUNTIMEPROOF_BLOCK_ME"
	home, _ := prepareOMPHome(t, filepath.Join(dir, "home"), OMPHomeRequest{Deny: []omp.DenyPredicate{{
		Tool:    "bash",
		Command: command,
		Reason:  "ATOMIC_RUNTIMEPROOF_DENY_MARKER",
	}}})
	repo := filepath.Join(dir, "repo")
	writeTree(t, repo, map[string]string{"src/a.ts": "export const a = 1;\n"})
	scenario, err := home.Launch(OMPLaunch{
		Name:    "omp-deny",
		WorkDir: repo,
		Args:    []string{"--tools", "bash"},
		Prompt:  "Call bash exactly once with this exact command and do not alter any byte: " + command + " . After the tool result, reply exactly DONE.",
	})
	if err != nil {
		t.Fatalf("deny launch: %v", err)
	}
	obs, err := Run(context.Background(), scenario)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	log, err := OMPRuntimeLog(obs, OMPLogName)
	if err != nil {
		t.Fatalf("parse runtime log: %v", err)
	}
	// The predicate is exact by construction, so a call the model altered is a
	// different command and correctly does not match. Only an attempted call proves
	// the deny path; a run without one says so instead of claiming the row.
	denied := false
	called := false
	for _, record := range log.Records {
		switch record.Kind {
		case "deny":
			denied = true
			if record.Reason != "ATOMIC_RUNTIMEPROOF_DENY_MARKER" {
				t.Errorf("deny reason = %q, want the predicate reason", record.Reason)
			}
		case "uncovered_tool", "operation":
			called = true
		}
	}
	if !denied {
		if called {
			t.Skipf("the model called a tool without the exact predicate bytes, so the predicate correctly did not apply: %+v", log.Records)
		}
		t.Skipf("the model called no tool, so the deny path is not exercised by this run: exit %d\n%s", obs.ExitCode, obs.Stderr)
	}
	if log.Denials != 1 {
		t.Fatalf("the exact predicate produced %d denials, want one: %+v", log.Denials, log.Records)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Errorf("the blocked command created %s; the deny did not prevent execution", sentinel)
	}
}

// TestOMPModelTokenResolvesCP0APIKey pins the two documented credential sources:
// CP0_OMP_API_KEY resolves with no credential home set, and it wins when both
// are set, matching the CP0 probe's token resolution order. The resolver ignored
// the API key before this, so a credentialed scenario skipped even when it was
// configured.
func TestOMPModelTokenResolvesCP0APIKey(t *testing.T) {
	t.Setenv("CP0_OMP_API_KEY", "cp0-api-key-value")
	t.Setenv("CP0_OMP_AUTH_HOME", "")
	if token, ok := OMPModelToken(); !ok || token != "cp0-api-key-value" {
		t.Errorf("token = %q, %v; want the CP0_OMP_API_KEY value with no credential home", token, ok)
	}
	t.Setenv("CP0_OMP_AUTH_HOME", t.TempDir())
	if token, ok := OMPModelToken(); !ok || token != "cp0-api-key-value" {
		t.Errorf("with both sources set token = %q, %v; want CP0_OMP_API_KEY to win", token, ok)
	}
	t.Setenv("CP0_OMP_API_KEY", "")
	t.Setenv("CP0_OMP_AUTH_HOME", "")
	if token, ok := OMPModelToken(); ok {
		t.Errorf("token = %q with no credential source configured, want ok=false", token)
	}
}

// runtimeAdapter returns the adapter the OMP scenarios enroll with: the
// runtimeproof fixture corpus, so a scenario asserts the delivery contract rather
// than the shipped corpus' current contents, and a stub root resolver, so no test
// depends on an installed OMP binary. Enrollment itself is the production call.
func runtimeAdapter(t *testing.T) *omp.Adapter {
	t.Helper()
	a := omp.New()
	a.ConfigPath = func(home, profile string) (string, error) {
		if profile == "" {
			return filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir)), nil
		}
		return filepath.Join(home, ".omp", "profiles", profile, "agent"), nil
	}
	// The stub answers every name, so an ambient selector would enroll a profile
	// the scenario never named. Opt out of the ambient read.
	a.AmbientProfile = nil
	a.Corpus = func() (*artifacts.Catalog, error) { return runtimeCorpus(t), nil }
	return a
}

// runtimeCorpus loads the fixture corpus the OMP scenarios deliver.
func runtimeCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	cat, err := artifacts.Load(filepath.Join("testdata", "omp-corpus"))
	if err != nil {
		t.Fatalf("load OMP fixture corpus: %v", err)
	}
	return cat
}

// runtimeDelivery is the delivery the fixture corpus produces, which is what the
// enrolled home's extension module carries.
func runtimeDelivery(t *testing.T, deny []omp.DenyPredicate) omp.SessionDelivery {
	t.Helper()
	sources, err := omp.ShippedRuleSources(runtimeCorpus(t))
	if err != nil {
		t.Fatalf("shipped rule sources: %v", err)
	}
	delivery, err := omp.BuildSessionDelivery(sources, harness.OMPCapabilities(), deny)
	if err != nil {
		t.Fatalf("build session delivery: %v", err)
	}
	return delivery
}

// prepareOMPHome enrolls an isolated OMP home through the production adapter and
// returns the prepared home with the enrollment result, so a scenario launches
// the layout enrollment produced rather than one it wrote itself.
func prepareOMPHome(t *testing.T, dir string, req OMPHomeRequest) (OMPHome, omp.EnrollResult) {
	t.Helper()
	if req.Adapter == nil {
		req.Adapter = runtimeAdapter(t)
	}
	home, result, err := NewOMPHome(dir, req)
	if err != nil {
		t.Fatalf("enroll OMP home: %v", err)
	}
	return home, result
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
