package runtimeproof

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
)

type ompRedesign struct {
	*redesignHome
	omp     OMPHome
	version string
}

func newOMPRedesign(t *testing.T, ev ScenarioEvidence, repoFiles map[string]string, setup func(h *redesignHome)) *ompRedesign {
	t.Helper()
	h := newRedesignHome(t, ev, repoFiles)
	binary, err := OMPCLI()
	if err != nil {
		recordScenarioSkip(t, ev, "omp unavailable: "+err.Error())
	}
	if setup != nil {
		setup(h)
	}
	h.mustRun(t, h.repo, "install", "--harness", "omp", "--yes")
	agentRoot := filepath.Join(h.home, filepath.FromSlash(omp.DefaultAgentDir))
	return &ompRedesign{
		redesignHome: h,
		version:      h.version(t, binary),
		omp: OMPHome{
			Home:      h.home,
			AgentRoot: agentRoot,
			Module:    filepath.Join(agentRoot, "extensions", "atomic.ts"),
			Log:       filepath.Join(agentRoot, OMPLogName),
		},
	}
}

// launch runs one print-mode OMP session with the payload probe, which records
// each provider request OMP builds before the placeholder key is rejected.
func (o *ompRedesign) launch(t *testing.T, name, dir, prompt string) (Observation, string) {
	t.Helper()
	probe, err := filepath.Abs(filepath.Join("testdata", "omp-payload-probe.ts"))
	if err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(o.root, name+".payload.jsonl")
	s, err := o.omp.Launch(OMPLaunch{Name: name, WorkDir: dir, Prompt: prompt, Args: []string{"--extension", probe}})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	s.Env = append(append(s.Env, o.launchOverrides()...), "ATOMIC_OMP_PAYLOAD_PROBE="+payload)
	obs, err := Run(context.Background(), s)
	if err != nil {
		t.Fatalf("%s: %v\nstderr: %s", name, err, obs.Stderr)
	}
	data, err := os.ReadFile(payload)
	if err != nil {
		t.Fatalf("%s: no provider request was captured: %v\nstdout: %s\nstderr: %s", name, err, obs.Stdout, obs.Stderr)
	}
	return obs, string(data)
}

// Migration links the cards into .omp/rules, and OMP lists each linked card in
// its <domain-rules> block under the link filename with the card's globs.
func TestOMPMigratedRepositoryResolvesAtomicAndListsItsLinkedCards(t *testing.T) {
	migrated := ScenarioEvidence{
		Scenario:  "omp/migration",
		Group:     "omp",
		Engine:    "atomic state migrate (built binary)",
		Criterion: "legacy .claude state moves to .atomic under an OMP-enrolled home and the OMP card copy is published",
		Command:   "atomic install --harness omp --yes; atomic where --json; atomic state migrate; atomic where --json",
	}
	card := "---\npaths:\n  - \"src/**\"\n---\n\nRUNTIMEPROOF_WIKI_CARD source domain.\n"
	o := newOMPRedesign(t, migrated, map[string]string{
		"src/a.ts":                        "export const a = 1;\n",
		".claude/.scratchpad/s1/BRIEF.md": "# brief\n",
		".claude/project/followups/f1.md": "# followup\n",
		".claude/atomic.toml":             "[comments]\nmax_lines = 2\n",
		".claude/rules/wiki/src.md":       card,
	}, nil)
	before := o.mustRun(t, o.repo, "where", "--json")
	migrateOut := o.mustRun(t, o.repo, "state", "migrate")
	after := o.mustRun(t, o.repo, "where", "--json")

	var where struct {
		StateRoot string `json:"state_root"`
	}
	if err := json.Unmarshal([]byte(after), &where); err != nil {
		t.Fatalf("where --json: %v\n%s", err, after)
	}
	if want := filepath.Join(o.repo, ".atomic"); where.StateRoot != want {
		t.Errorf("state_root after migrate = %q, want %q", where.StateRoot, want)
	}
	for _, moved := range []string{".scratchpad/s1/BRIEF.md", "project/followups/f1.md", "atomic.toml", "rules/wiki/src.md"} {
		if !fileExists(filepath.Join(o.repo, ".atomic", moved)) {
			t.Errorf("migrate did not move %s into .atomic", moved)
		}
	}
	published := filepath.Join(o.repo, filepath.FromSlash(omp.ProjectRulesDir), "atomic-wiki-src.md")
	if got, err := os.Readlink(published); err != nil || got != "../../.atomic/rules/wiki/src.md" {
		t.Errorf("%s = %q, %v; want a relative link to the canonical card", published, got, err)
	}
	if !strings.Contains(readFileString(t, published), "RUNTIMEPROOF_WIKI_CARD") {
		t.Errorf("migrate did not publish the card to %s", published)
	}
	migrated.Paths = []string{o.home, o.repo}
	migrated.Outcome = o.version + ": where resolved .atomic; scratchpad, followups, atomic.toml, and the card moved; the card was linked at .omp/rules/atomic-wiki-src.md"
	migrated.Detail = "level files; sha256 " + retain(t, "migration.txt", before+"\n--- state migrate\n"+migrateOut+"\n--- where after\n"+after)
	recordScenario(t, migrated)

	listing := ScenarioEvidence{
		Scenario:  "omp/wiki-card-listing",
		Group:     "omp",
		Engine:    "runtimeproof (real OMP)",
		Criterion: "OMP's <domain-rules> listing in the opening request carries the linked card under the link filename, with its globs and description, beside a hand-written control rule",
		Command:   "omp -p --mode json --cwd <repo> --extension omp-payload-probe.ts 'Summarize @src/a.ts'",
		Paths:     []string{o.home, o.repo},
	}
	// A hand-written described rule shows the rule surface is live, so a missing
	// card line is the card's shape and not a disabled surface.
	writeTree(t, o.repo, map[string]string{
		".omp/rules/runtimeproof-control.md": "---\ndescription: RUNTIMEPROOF_CONTROL_RULE\nglobs:\n  - \"src/**\"\n---\n\nRUNTIMEPROOF_CONTROL_BODY\n",
	})
	_, payload := o.launch(t, "wiki-card-listing", o.repo, "Summarize @src/a.ts")
	text := jsonStrings(t, payload)
	if !anyContains(text, "- runtimeproof-control (src/**): RUNTIMEPROOF_CONTROL_RULE") {
		t.Fatalf("the control rule is not listed; OMP's rule surface did not load:\n%s", payload)
	}
	if !anyContains(text, "- atomic-wiki-src (src/**): RUNTIMEPROOF_WIKI_CARD source domain.") {
		t.Errorf("OMP did not list the published card:\n%s", payload)
	}
	listing.Outcome = o.version + ": control rule listed as `- runtimeproof-control (src/**): RUNTIMEPROOF_CONTROL_RULE` and the card as `- atomic-wiki-src (src/**): RUNTIMEPROOF_WIKI_CARD source domain.`; OMP lists a described rule and the model loads its body through rule://, which needs a model turn and was not reached"
	listing.Detail = "level startup; payload sha256 " + retain(t, "wiki-card-listing.payload.jsonl", payload)
	recordScenario(t, listing)
}

// A stored override reaches the OMP agent file under OMP's own keys, and OMP's
// task tool lists the agent.
func TestOMPAgentOverrideIsProjectedAndTheAgentListed(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "omp/agent-override",
		Group:     "omp",
		Engine:    "runtimeproof (real OMP)",
		Criterion: "the override lands in agents/atomic-investigator.md as model and thinking, and OMP's opening request lists atomic-investigator",
		Command:   "[omp.agents.atomic-investigator] model=openai/gpt-5.1 effort=low in ~/.atomic/config.toml; atomic install --harness omp --yes; omp -p --mode json 'Reply OK.'",
	}
	o := newOMPRedesign(t, ev, map[string]string{"README.md": "# probe\n"}, func(h *redesignHome) {
		writeTree(t, h.home, map[string]string{
			".atomic/config.toml": "[omp.agents.atomic-investigator]\nmodel = \"openai/gpt-5.1\"\neffort = \"low\"\n",
		})
	})
	ev.Paths = []string{o.home, o.repo}
	investigator := readFileString(t, filepath.Join(o.omp.AgentRoot, "agents", "atomic-investigator.md"))
	front := frontmatter(investigator)
	if !strings.Contains(front, "\nmodel: openai/gpt-5.1\n") || !strings.Contains(front, "\nthinking: low\n") {
		t.Errorf("atomic-investigator frontmatter lacks model: openai/gpt-5.1 and thinking: low:\n%s", front)
	}
	if reviewer := frontmatter(readFileString(t, filepath.Join(o.omp.AgentRoot, "agents", "atomic-reviewer.md"))); strings.Contains(reviewer, "\nmodel:") {
		t.Errorf("an agent with no override carries a model pin:\n%s", reviewer)
	}

	_, payload := o.launch(t, "agent-override", o.repo, DefaultPrompt)
	if !anyContains(jsonStrings(t, payload), "- `atomic-investigator`: Read-only code locator.") {
		t.Errorf("the opening request does not list atomic-investigator")
	}
	ev.Outcome = o.version + ": frontmatter carries model: openai/gpt-5.1 and thinking: low; atomic-reviewer carries no model; the task tool lists atomic-investigator"
	ev.Detail = "levels files + startup; a child run on the override needs a model turn and was not reached; agent file sha256 " +
		retain(t, "agent-override.atomic-investigator.md", investigator) + "; payload sha256 " + retain(t, "agent-override.payload.jsonl", payload)
	recordScenario(t, ev)
}

// The extension runs the session-start verb at session_start, refreshes a stale
// profile through it, and hands the provider request the verb's exact text.
func TestOMPSessionStartDeliversTheResolvedWorkflowBlock(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "omp/session-context",
		Group:     "omp",
		Engine:    "runtimeproof (real OMP)",
		Criterion: "the extension ran `atomic hooks session-start` at session_start, refreshed the profile, and the opening request carries the verb's text with <workflow-preferences level='resolved'>",
		Command:   "omp -p --mode json 'Reply OK.'; atomic hooks session-start --format text --harness omp --cwd <repo>",
	}
	o := newOMPRedesign(t, ev, map[string]string{
		".atomic/workflow-preferences.md": "<workflow-preferences level='repo'>\n- issue-tracker: linear\n- issue-tracker-project: RUNTIMEPROOF\n</workflow-preferences>\n",
	}, nil)
	ev.Paths = []string{o.home, o.repo}
	profile := filepath.Join(o.home, ".atomic", "profile.md")
	writeTree(t, o.home, map[string]string{".atomic/profile.md": "# stale profile, no lastcheck\n"})

	obs, payload := o.launch(t, "session-context", o.repo, DefaultPrompt)
	log, err := OMPRuntimeLog(obs, OMPLogName)
	if err != nil {
		t.Fatal(err)
	}
	loaded := false
	for _, r := range log.Records {
		loaded = loaded || r.Kind == "session_context"
	}
	if !loaded {
		t.Fatalf("the extension recorded no session_context load:\n%s", obs.Files[OMPLogName])
	}
	if !strings.Contains(readFileString(t, profile), "lastcheck=") {
		t.Errorf("the session-start verb did not refresh the stale profile")
	}
	verb := strings.TrimSpace(o.mustRun(t, o.repo, "hooks", "session-start", "--format", "text", "--harness", "omp", "--cwd", o.repo))
	if !strings.Contains(verb, "<workflow-preferences level='resolved'>") || !strings.Contains(verb, "- issue-tracker: linear (repo)") {
		t.Fatalf("the verb's text lacks the resolved repo preferences:\n%s", verb)
	}
	block := "<atomic-session-context marker=\"atomic-omp-session-context\">\n" + verb + "\n</atomic-session-context>"
	if !anyContains(jsonStrings(t, payload), block) {
		t.Errorf("the opening request does not carry the verb's text as the session-context block")
	}
	ev.Outcome = o.version + ": session_context loaded; profile refreshed; the opening request carries the verb's text with issue-tracker: linear (repo)"
	ev.Detail = "level startup (placeholder key, so no model turn); payload sha256 " + retain(t, "session-context.payload.jsonl", payload) +
		"; verb sha256 " + retain(t, "session-context.verb.txt", verb) + "; runtime log sha256 " + retain(t, "session-context.runtime.jsonl", obs.Files[OMPLogName])
	recordScenario(t, ev)
}

// new_session fires session_switch into the same extension instance; /move
// fires no event, so the extension reloads at the next before_agent_start,
// whose ctx.cwd is the new directory.
func TestOMPMoveAndNewSessionBothReloadContext(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "omp/session-switch",
		Group:     "omp",
		Engine:    "runtimeproof (real OMP, rpc mode)",
		Criterion: "after /move to a second repository the next turn carries the second repository's context, and after new_session it still does",
		Command:   "omp --mode rpc: prompt '/move <repo2>'; prompt 'Reply OK.'; new_session; prompt 'Reply OK.'",
	}
	o := newOMPRedesign(t, ev, map[string]string{
		".atomic/workflow-preferences.md": "<workflow-preferences level='repo'>\n- issue-tracker: linear\n</workflow-preferences>\n",
	}, nil)
	second := filepath.Join(o.root, "repo2")
	writeTree(t, second, map[string]string{
		".atomic/workflow-preferences.md": "<workflow-preferences level='repo'>\n- issue-tracker: jira\n</workflow-preferences>\n",
	})
	guidanceGit(t, second, "init", "-q", "-b", "main")
	ev.Paths = []string{o.home, o.repo, second}

	payloadProbe, err := filepath.Abs(filepath.Join("testdata", "omp-payload-probe.ts"))
	if err != nil {
		t.Fatal(err)
	}
	eventProbe, err := filepath.Abs(filepath.Join("testdata", "omp-event-probe.ts"))
	if err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(o.root, "switch.payload.jsonl")
	events := filepath.Join(o.root, "switch.events.jsonl")
	s, err := o.omp.Launch(OMPLaunch{Name: "session-switch", WorkDir: o.repo, Save: true})
	if err != nil {
		t.Fatal(err)
	}
	// Launch always builds one-shot print args (`-p --mode json ... <prompt>`), and
	// this run needs a long-lived rpc session, so only its env and paths are kept.
	s.Args = []string{"--mode", "rpc", "--cwd", o.repo, "--model", ompModel(), "--api-key", ompPlaceholderKey,
		"--extension", payloadProbe, "--extension", eventProbe}
	s.Env = append(append(s.Env, o.launchOverrides()...), "ATOMIC_OMP_PAYLOAD_PROBE="+payload, "ATOMIC_OMP_EVENT_PROBE="+events)
	transcript := ompRPC(t, s, []string{
		`{"id":"1","type":"prompt","message":"/move ` + second + `"}`,
		`{"id":"2","type":"prompt","message":"Reply OK."}`,
		`{"id":"3","type":"new_session"}`,
		`{"id":"4","type":"prompt","message":"Reply OK."}`,
	})

	captured, err := os.ReadFile(payload)
	if err != nil {
		t.Fatalf("no provider request was captured: %v\n%s", err, transcript)
	}
	requests := strings.Split(strings.TrimSpace(string(captured)), "\n")
	if len(requests) != 2 {
		t.Fatalf("captured %d provider requests, want 2\n%s", len(requests), transcript)
	}
	afterMove := jsonStrings(t, requests[0])
	if !anyContains(afterMove, "- issue-tracker: jira (repo)") || anyContains(afterMove, "- issue-tracker: linear (repo)") {
		t.Errorf("the turn after /move does not carry only the moved-to repository's context")
	}
	if !anyContains(jsonStrings(t, requests[1]), "- issue-tracker: jira (repo)") {
		t.Errorf("the turn after new_session does not carry the switched repository's context")
	}
	observed := readFileString(t, events)
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(observed), "\n") {
		var e struct{ Event, Reason, Cwd string }
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, e.Event+":"+e.Reason+":"+filepath.Base(e.Cwd))
	}
	want := "session_start::repo,before_agent_start:prompt:repo2,session_switch:new:repo2,before_agent_start:prompt:repo2"
	if got := strings.Join(kinds, ","); got != want {
		t.Errorf("extension events = %s, want %s", got, want)
	}
	ev.Outcome = o.version + ": /move fired no extension event; the next before_agent_start carried ctx.cwd repo2 and the request carried repo2's context (issue-tracker: jira) with none of repo's; new_session fired session_switch reason new with ctx.cwd repo2 and the next request carried repo2's context"
	ev.Detail = "level startup (placeholder key); payload sha256 " + retain(t, "session-switch.payload.jsonl", string(captured)) +
		"; events sha256 " + retain(t, "session-switch.events.jsonl", observed) + "; rpc sha256 " + retain(t, "session-switch.rpc.jsonl", transcript)
	recordScenario(t, ev)
}

// ompRPC drives `omp --mode rpc` one command at a time: each waits for its
// response and, when it started an agent turn, for that turn's prompt_result.
func ompRPC(t *testing.T, s Scenario, commands []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Harness, s.Args...)
	cmd.Dir = s.WorkDir
	cmd.Env = launchEnv(s)
	cmd.WaitDelay = postExitWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start omp rpc: %v", err)
	}
	var transcript strings.Builder
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 0, 1<<20), 64<<20)
	next := func(done func(map[string]any) bool) {
		for lines.Scan() {
			transcript.WriteString(lines.Text() + "\n")
			var m map[string]any
			if json.Unmarshal(lines.Bytes(), &m) == nil && done(m) {
				return
			}
		}
		t.Fatalf("omp rpc ended before the awaited line:\n%s", transcript.String())
	}
	for _, line := range commands {
		var sent struct{ ID, Type string }
		if err := json.Unmarshal([]byte(line), &sent); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			t.Fatal(err)
		}
		// A prompt's response arrives before its turn ends; only a slash command
		// that ran no turn says so with agentInvoked false.
		awaitTurn, settled := sent.Type == "prompt", false
		isResult := func(m map[string]any) bool { return m["type"] == "prompt_result" && m["id"] == sent.ID }
		next(func(m map[string]any) bool {
			settled = settled || isResult(m)
			if m["type"] != "response" || m["id"] != sent.ID {
				return false
			}
			if m["success"] != true {
				t.Fatalf("rpc %s failed: %v", line, m)
			}
			if data, ok := m["data"].(map[string]any); ok && data["agentInvoked"] == false {
				awaitTurn = false
			}
			return true
		})
		if awaitTurn && !settled {
			next(isResult)
		}
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	return transcript.String()
}
