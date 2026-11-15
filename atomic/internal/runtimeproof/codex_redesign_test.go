package runtimeproof

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
)

type codexRedesign struct {
	*redesignHome
	codex, version string
}

func newCodexRedesign(t *testing.T, ev ScenarioEvidence, repoFiles map[string]string, setup func(h *redesignHome)) *codexRedesign {
	t.Helper()
	h := newRedesignHome(t, ev, repoFiles)
	binary, err := exec.LookPath("codex")
	if err != nil {
		recordScenarioSkip(t, ev, "codex unavailable: "+err.Error())
	}
	if setup != nil {
		setup(h)
	}
	h.mustRun(t, h.repo, "install", "--harness", "codex", "--yes")
	return &codexRedesign{redesignHome: h, codex: binary, version: h.version(t, binary)}
}

func (c *codexRedesign) scenario(name string, args []string) Scenario {
	return Scenario{
		Name: name, Harness: c.codex, Args: args,
		Home: c.home, ConfigDir: c.codexHome, ConfigEnv: "CODEX_HOME",
		WorkDir: c.repo, EventLog: filepath.Join(c.root, name+".events.jsonl"),
		Timeout: 2 * time.Minute, Env: c.launchOverrides(),
		Prepare: func(string) error { return nil },
	}
}

type codexHook struct {
	Key         string `json:"key"`
	EventName   string `json:"eventName"`
	Command     string `json:"command"`
	Source      string `json:"source"`
	CurrentHash string `json:"currentHash"`
	TrustStatus string `json:"trustStatus"`
}

// hooksList asks `codex app-server` for the hooks Codex discovers in the
// repository, with the trust status Codex itself computes. No model is called.
func (c *codexRedesign) hooksList(t *testing.T) ([]codexHook, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.codex, "app-server")
	cmd.Dir = c.repo
	cmd.Env = launchEnv(c.scenario("hooks-list", nil))
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
		t.Fatalf("start codex app-server: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 0, 1<<20), 16<<20)
	await := func(id float64) json.RawMessage {
		for lines.Scan() {
			var m struct {
				ID     *float64        `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(lines.Bytes(), &m) == nil && m.ID != nil && *m.ID == id {
				if m.Error != nil {
					t.Fatalf("app-server request %v failed: %s", id, m.Error)
				}
				return m.Result
			}
		}
		t.Fatalf("app-server ended before answering request %v", id)
		return nil
	}
	send := func(line string) {
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	send(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"runtimeproof","title":null,"version":"0"},"capabilities":null}}`)
	await(1)
	send(`{"method":"initialized"}`)
	send(fmt.Sprintf(`{"id":2,"method":"hooks/list","params":{"cwds":[%q]}}`, c.repo))
	raw := await(2)
	var resp struct {
		Data []struct {
			Hooks []codexHook `json:"hooks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("hooks/list result: %v\n%s", err, raw)
	}
	var hooks []codexHook
	for _, entry := range resp.Data {
		hooks = append(hooks, entry.Hooks...)
	}
	return hooks, string(raw)
}

func (c *codexRedesign) sessionHook(t *testing.T) (codexHook, string) {
	t.Helper()
	hooks, raw := c.hooksList(t)
	for _, h := range hooks {
		if h.EventName == "sessionStart" && h.Command == "atomic hooks session-start --harness codex" {
			return h, raw
		}
	}
	t.Fatalf("hooks/list does not report Atomic's SessionStart member:\n%s", raw)
	return codexHook{}, raw
}

// setHookState replaces config.toml's [hooks.state] entries with state, keeping
// the registration tables install wrote.
func (c *codexRedesign) setHookState(t *testing.T, state string) {
	t.Helper()
	path := codex.ConfigPath(c.codexHome)
	data := readFileString(t, path)
	if i := strings.Index(data, "\n[hooks.state."); i >= 0 {
		data = data[:i+1]
	}
	if err := os.WriteFile(path, []byte(data+state), 0o644); err != nil {
		t.Fatal(err)
	}
}

// exec points Codex at a stand-in provider that records the request and answers
// 400, so the recorded body is Codex's assembled model input with no credential.
func (c *codexRedesign) exec(t *testing.T, name, model string) (Observation, string) {
	t.Helper()
	var mu sync.Mutex
	var bodies strings.Builder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies.Write(body)
		bodies.WriteByte('\n')
		mu.Unlock()
		http.Error(w, "runtimeproof", http.StatusBadRequest)
	}))
	defer server.Close()
	args := []string{"exec", "--json",
		"-c", `model_providers.runtimeproof.name="runtimeproof"`,
		"-c", fmt.Sprintf(`model_providers.runtimeproof.base_url=%q`, server.URL+"/v1"),
		"-c", `model_providers.runtimeproof.wire_api="responses"`,
		"-c", `model_provider="runtimeproof"`,
		"-c", fmt.Sprintf("model=%q", model),
		DefaultPrompt,
	}
	obs, err := Run(context.Background(), c.scenario(name, args))
	if err != nil {
		t.Fatalf("%s: %v\n%s", name, err, obs.Stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if bodies.Len() == 0 {
		t.Fatalf("%s: codex sent no request\nstdout: %s\nstderr: %s", name, obs.Stdout, obs.Stderr)
	}
	return obs, bodies.String()
}

// Install registers the plugin, writes the global block, the agents, and the
// SessionStart member, and Codex discovers each of them.
func TestCodexInstallIsDiscoveredByCodex(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "codex/install",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex)",
		Criterion: "install writes config.toml registration, the global AGENTS.md block, hooks.json, and agents; plugin list, prompt-input, and hooks/list report them",
		Command:   "atomic install --harness codex --yes; codex plugin list --json; codex debug prompt-input 'Reply OK.'; codex app-server hooks/list",
	}
	c := newCodexRedesign(t, ev, map[string]string{"README.md": "# probe\n"}, nil)
	ev.Paths = []string{c.home, c.codexHome, c.repo}

	config := readFileString(t, codex.ConfigPath(c.codexHome))
	hooksJSON := readFileString(t, filepath.Join(c.codexHome, "hooks.json"))
	global := readFileString(t, codex.SteeringPath(c.codexHome))
	if !strings.Contains(config, `[plugins."atomic@atomic-claude"]`) || !strings.Contains(global, "<atomic>") ||
		!strings.Contains(hooksJSON, "atomic hooks session-start --harness codex") ||
		!fileExists(filepath.Join(c.codexHome, "agents", "atomic-investigator.toml")) {
		t.Fatalf("install left the Codex home incomplete:\n%s\n%s", config, hooksJSON)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	list := exec.CommandContext(ctx, c.codex, "plugin", "list", "--json")
	list.Dir, list.Env = c.repo, c.env()
	plugins, err := list.CombinedOutput()
	if err != nil || !strings.Contains(string(plugins), "atomic@atomic-claude") {
		t.Errorf("codex plugin list does not report atomic@atomic-claude: %v\n%s", err, plugins)
	}
	prompt := exec.CommandContext(ctx, c.codex, "debug", "prompt-input", DefaultPrompt)
	prompt.Dir, prompt.Env = c.repo, c.env()
	input, err := prompt.CombinedOutput()
	if err != nil {
		t.Fatalf("codex debug prompt-input: %v\n%s", err, input)
	}
	if !strings.Contains(string(input), "atomic:atomic-") || !strings.Contains(string(input), "Walk this ladder before writing anything") {
		t.Errorf("prompt-input lacks the plugin's skills or the global block")
	}
	hook, raw := c.sessionHook(t)
	if hook.Source != "user" || hook.TrustStatus != "untrusted" {
		t.Errorf("hooks/list reports the member as %s/%s, want user/untrusted before review", hook.Source, hook.TrustStatus)
	}

	ev.Outcome = c.version + ": plugin listed; prompt-input carries atomic:<skill> listings and the global block; hooks/list reports the SessionStart member, source user, untrusted"
	ev.Detail = "levels files + startup; files sha256 " + retain(t, "install.files.txt", "--- config.toml\n"+config+"--- hooks.json\n"+hooksJSON) +
		"; plugin list sha256 " + retain(t, "install.plugin-list.json", string(plugins)) +
		"; prompt-input sha256 " + retain(t, "install.prompt-input.json", string(input)) +
		"; hooks/list sha256 " + retain(t, "install.hooks-list.json", raw)
	recordScenario(t, ev)
}

// Codex keys hook trust by the resolved hooks.json path, the event, and the
// member's position, with a sha256 identity hash; Atomic's read must agree.
func TestCodexHookTrustKeyIsTheOneAtomicReads(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "codex/hook-trust",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex app-server)",
		Criterion: "for each config.toml [hooks.state] shape, Codex's hooks/list trustStatus and Atomic's ObserveSessionHookTrust agree",
		Command:   "edit [hooks.state.\"<key>\"] in $CODEX_HOME/config.toml; codex app-server hooks/list; codex.ObserveSessionHookTrust",
	}
	c := newCodexRedesign(t, ev, map[string]string{"README.md": "# probe\n"}, nil)
	ev.Paths = []string{c.home, c.codexHome, c.repo}
	hook, _ := c.sessionHook(t)
	if want := filepath.Join(c.codexHome, "hooks.json") + ":session_start:0:0"; hook.Key != want {
		t.Errorf("Codex keys the member %q, want %q", hook.Key, want)
	}

	var table strings.Builder
	for _, tc := range []struct{ name, state, codex string }{
		{"no [hooks.state]", "", "untrusted"},
		{"member key, current hash", fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", hook.Key, hook.CurrentHash), "trusted"},
		{"member key, other hash", fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", hook.Key, "sha256:"+strings.Repeat("0", 64)), "modified"},
		{"member key, no hash", fmt.Sprintf("[hooks.state.%q]\nenabled = true\n", hook.Key), "untrusted"},
		{"other key only", fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", hook.Key+"9", hook.CurrentHash), "untrusted"},
	} {
		c.setHookState(t, tc.state)
		seen, _ := c.sessionHook(t)
		atomic, why := codex.ObserveSessionHookTrust(c.home, c.codexHome)
		fmt.Fprintf(&table, "%s | codex %s | atomic %s (%s)\n", tc.name, seen.TrustStatus, atomic, why)
		if seen.TrustStatus != tc.codex {
			t.Errorf("%s: Codex reports %s, want %s", tc.name, seen.TrustStatus, tc.codex)
		}
		if string(atomic) != seen.TrustStatus {
			t.Errorf("%s: Atomic reads %s (%s), Codex reports %s", tc.name, atomic, why, seen.TrustStatus)
		}
	}
	ev.Outcome = c.version + ": key <resolved hooks.json>:session_start:0:0; Codex's currentHash equals Atomic's identity hash; Atomic's read agrees with Codex's trustStatus on every shape"
	ev.Detail = "level startup (app-server hooks/list, no model call); table sha256 " + retain(t, "hook-trust.txt", "key "+hook.Key+"\ncurrentHash "+hook.CurrentHash+"\n"+table.String())
	recordScenario(t, ev)
}

// A stored override reaches the projected agent TOML, and Codex names the
// override in the spawn_agent role it builds from that file.
func TestCodexAgentOverrideIsProjectedAndTheRoleListed(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "codex/agent-override",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex, local stand-in provider)",
		Criterion: "the override lands in agents/atomic-investigator.toml and Codex's spawn_agent roles list atomic-investigator with that model and effort",
		Command:   "[codex.agents.atomic-investigator] model=gpt-5.1-codex-mini effort=low; atomic install --harness codex --yes; codex exec --json -c model_provider=<stand-in> -c model=\"gpt-5.1-codex\" 'Reply OK.'",
	}
	c := newCodexRedesign(t, ev, map[string]string{"README.md": "# probe\n"}, func(h *redesignHome) {
		writeTree(t, h.home, map[string]string{
			".atomic/config.toml": "[codex.agents.atomic-investigator]\nmodel = \"gpt-5.1-codex-mini\"\neffort = \"low\"\n",
		})
	})
	ev.Paths = []string{c.home, c.codexHome, c.repo}
	agent := readFileString(t, filepath.Join(c.codexHome, "agents", "atomic-investigator.toml"))
	if !strings.Contains(agent, "\nmodel = 'gpt-5.1-codex-mini'\n") || !strings.Contains(agent, "\nmodel_reasoning_effort = 'low'\n") {
		t.Errorf("atomic-investigator.toml lacks the override keys")
	}
	if reviewer := readFileString(t, filepath.Join(c.codexHome, "agents", "atomic-reviewer.toml")); strings.Contains(reviewer, "\nmodel = ") {
		t.Errorf("an agent with no override carries a model pin")
	}
	// gpt-5.1-codex exposes spawn_agent directly; newer catalog models defer it
	// behind tool_search, where the roles are not in the opening request.
	_, request := c.exec(t, "agent-override", "gpt-5.1-codex")
	if !anyContains(jsonStrings(t, request), "atomic-investigator: {\nRead-only code locator.") {
		t.Errorf("the spawn_agent roles do not list atomic-investigator")
	}
	if !anyContains(jsonStrings(t, request), "This role's model is set to `gpt-5.1-codex-mini` and its reasoning effort is set to `low`.") {
		t.Errorf("Codex did not read the override into the atomic-investigator role")
	}
	ev.Outcome = c.version + ": TOML carries model and model_reasoning_effort; spawn_agent lists atomic-investigator with \"This role's model is set to `gpt-5.1-codex-mini` and its reasoning effort is set to `low`\""
	ev.Detail = "levels files + startup; the spawned child running on the override needs a model turn and was not reached; agent sha256 " +
		retain(t, "agent-override.atomic-investigator.toml", agent) + "; request sha256 " + retain(t, "agent-override.request.jsonl", request)
	recordScenario(t, ev)
}

// Codex runs the SessionStart member only once it is trusted, and then the
// request carries the verb's additionalContext as a developer message.
func TestCodexSessionStartHookDeliversTheResolvedWorkflowBlockOnceTrusted(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "codex/session-context",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex, local stand-in provider)",
		Criterion: "untrusted, the request carries no hook context; trusted, it carries the verb's additionalContext with <workflow-preferences level='resolved'>",
		Command:   "codex exec --json (untrusted); trust via [hooks.state] with hooks/list currentHash; codex exec --json; atomic hooks session-start --harness codex --cwd <repo>",
	}
	c := newCodexRedesign(t, ev, map[string]string{
		".atomic/workflow-preferences.md": "<workflow-preferences level='repo'>\n- issue-tracker: linear\n- issue-tracker-project: RUNTIMEPROOF\n</workflow-preferences>\n",
	}, nil)
	ev.Paths = []string{c.home, c.codexHome, c.repo}
	direct := c.mustRun(t, c.repo, "hooks", "session-start", "--harness", "codex", "--cwd", c.repo)
	want := hookAdditionalContext(t, direct)
	if !strings.Contains(want, "<workflow-preferences level='resolved'>") || !strings.Contains(want, "- issue-tracker: linear (repo)") {
		t.Fatalf("the verb's context lacks the resolved repo preferences:\n%s", want)
	}

	_, untrusted := c.exec(t, "session-context-untrusted", "gpt-5.2")
	if anyContains(jsonStrings(t, untrusted), "- issue-tracker: linear (repo)") {
		t.Errorf("an untrusted hook delivered context")
	}
	hook, _ := c.sessionHook(t)
	c.setHookState(t, fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", hook.Key, hook.CurrentHash))
	writeTree(t, c.home, map[string]string{".atomic/profile.md": "# stale profile, no lastcheck\n"})
	_, trusted := c.exec(t, "session-context-trusted", "gpt-5.2")
	if !anyContains(jsonStrings(t, trusted), want) {
		t.Errorf("the trusted run's request does not carry the verb's additionalContext")
	}
	if !strings.Contains(readFileString(t, filepath.Join(c.home, ".atomic", "profile.md")), "lastcheck=") {
		t.Errorf("the hook did not refresh the stale profile")
	}
	ev.Outcome = c.version + ": untrusted, no hook context; trusted, the request carries the verb's additionalContext (issue-tracker: linear (repo)) and the profile was refreshed"
	ev.Detail = "level startup (stand-in provider, no model turn); untrusted sha256 " + retain(t, "session-context.untrusted.jsonl", untrusted) +
		"; trusted sha256 " + retain(t, "session-context.trusted.jsonl", trusted) + "; verb sha256 " + retain(t, "session-context.verb.json", direct)
	recordScenario(t, ev)
}
