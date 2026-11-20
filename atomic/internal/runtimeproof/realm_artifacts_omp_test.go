package runtimeproof

import (
	"bytes"
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
)

func TestOMPRealmArtifactCandidates(t *testing.T) {
	const (
		modelID     = "runtimeproof-realm-artifacts"
		skillName   = "realm-omp-fixture-skill"
		skillDesc   = "Benign reviewed realm skill OMP_REALM_SKILL_DESC_F09ACD65."
		listPrompt  = "This is the OMP_REALM_SKILL_LIST_3BFCE52D listing probe."
		command     = "realm-omp-fixture-workflow"
		skillBody   = "Fixture skill line one: OMP_REALM_SKILL_4BE17DF9.\nFixture skill line two: OMP_REALM_SKILL_END_D09C4A81."
		workflow    = "Fixture workflow: OMP_REALM_WORKFLOW_3F906C07 arguments=["
		skillArgs   = "OMP_REALM_SKILL_ARGS_074612A0"
		commandArgs = "OMP_REALM_WORKFLOW_ARGS_5071A12D second"
	)
	base := ScenarioEvidence{
		Scenario:  "omp/realm-artifact-reviewed-links",
		Group:     "omp",
		Engine:    "runtimeproof (real OMP)",
		Criterion: "native project discovery and explicit invocation of individually reviewed Atomic realm links",
		Command:   "omp -p --mode json --offline --no-session",
	}
	t.Run("agent-unverified", func(t *testing.T) {
		ev := base
		ev.Scenario = "omp/realm-artifact-agent"
		ev.Criterion = "explicit native named agent selection and body delivery"
		ev.Command = ""
		recordScenarioSkip(t, ev, "unverified: neither .agents/agents nor .omp/agents was exercised; no agent discovery, selection or delegation claim")
	})
	if testing.Short() {
		recordScenarioSkip(t, base, "short mode: the real OMP launch is skipped")
	}
	binary, err := OMPCLI()
	if err == nil {
		binary, err = exec.LookPath(binary)
	}
	if err != nil {
		recordScenarioSkip(t, base, "omp unavailable: "+err.Error())
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var captured [][]byte
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.IsAbs() || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "runtimeproof: only the local model endpoint is available", http.StatusForbidden)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil || !json.Valid(data) {
			http.Error(w, "runtimeproof: invalid request body", http.StatusBadRequest)
			return
		}
		mu.Lock()
		captured = append(captured, data)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":{"message":"runtimeproof: no model reply","type":"invalid_request_error"}}`)
	}))
	defer provider.Close()

	sandbox := isolatedHome(t)
	bin := filepath.Join(sandbox, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, filepath.Join(bin, "omp")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ATOMIC_RUNTIMEPROOF_OMP", binary)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(sandbox, key))
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, provider.URL)
	}
	t.Setenv("NO_PROXY", "127.0.0.1,::1")
	t.Setenv("no_proxy", "127.0.0.1,::1")

	o := newOMPRedesign(t, base, map[string]string{"README.md": "# Install fixture\n"}, func(h *redesignHome) {
		writeTree(t, h.home, map[string]string{
			".atomic/config.toml": "[update]\ncheck = false\n",
			".atomic/profile.md":  "# Runtimeproof profile\n\n<deterministic lastcheck=" + time.Now().UTC().Format(time.DateOnly) + ">\n</deterministic>\n",
		})
	})
	const (
		skillPath    = ".agents/skills/" + skillName
		workflowPath = ".agents/commands/" + command + ".md"
	)
	realm := linkRealmRuntimeArtifacts(t, base, o.redesignHome, map[string]string{
		skillPath + "/SKILL.md": "---\nname: " + skillName + "\ndescription: " + skillDesc + "\n---\n\n" + skillBody + "\n",
		workflowPath:            "---\ndescription: Benign reviewed realm workflow.\n---\n\n" + workflow + "$ARGUMENTS]\n",
	}, []string{skillPath, workflowPath})
	member := filepath.Join(realm, "repos", "member")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	guidanceGit(t, member, "init", "--template=", "-b", "main")

	config := o.omp.AgentRoot
	skillLink := filepath.Join(realm, skillPath)
	workflowLink := filepath.Join(realm, workflowPath)
	targets := make(map[string]string, 2)
	for _, link := range []string{skillLink, workflowLink} {
		if _, err := os.Readlink(link); err != nil {
			t.Fatalf("expected a reviewed Atomic realm link at %s: %v", link, err)
		}
		target, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("resolve reviewed Atomic realm link at %s: %v", link, err)
		}
		targets[link] = target
	}
	fixture := Scenario{
		Harness: binary, Home: o.home, ConfigEnv: NoConfigEnv,
		Env: append(o.launchOverrides(), "PI_CODING_AGENT_DIR="+config, "OMP_NO_UPDATE_CHECK=1"),
	}
	t.Logf("OMP CLI: %s", o.version)
	writeTree(t, config, map[string]string{"models.yml": fmt.Sprintf(`providers:
  runtimeproof:
    baseUrl: %s/v1
    api: openai-completions
    apiKey: %s
    models:
      - id: %s
        name: Realm artifact probe
        contextWindow: 128000
        maxTokens: 2048
`, provider.URL, ompPlaceholderKey, modelID)})
	probe, err := filepath.Abs(filepath.Join("testdata", "omp-payload-probe.ts"))
	if err != nil {
		t.Fatal(err)
	}

	skillPrompt := "/skill:" + skillName + " " + skillArgs
	workflowPrompt := "/" + command + " " + commandArgs
	workflowBody := workflow + commandArgs + "]"
	for _, tc := range []struct {
		name, dir, link, prompt, requestMarker, body, userText, outcome string
		discoverSkill, readTool                                         bool
	}{
		{"skill/realm-root-discovery", realm, skillLink, listPrompt, listPrompt, skillBody, listPrompt,
			"native request listed the skill name and description without its body, with Read available but not executed", true, true},
		{"skill/realm-root", realm, skillLink, skillPrompt, skillArgs, skillBody, skillPrompt,
			"print mode forwarded literal /skill: input without expansion; explicit native skill invocation is unverified", false, true},
		{"skill/member-root", member, skillLink, skillPrompt, skillArgs, skillBody, skillPrompt,
			"nested Git root did not list the realm skill and forwarded the literal slash input", false, true},
		{"workflow/realm-root", realm, workflowLink, workflowPrompt, commandArgs, workflowBody, workflowBody,
			"named workflow slash input delivered the exact authored body and arguments", false, false},
		{"workflow/member-root", member, workflowLink, workflowPrompt, commandArgs, workflowBody, workflowPrompt,
			"nested Git root forwarded the literal slash input without the realm workflow body", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := base
			ev.Scenario = "omp/realm-artifact-" + strings.ReplaceAll(tc.name, "/", "-")
			ev.Paths = []string{tc.dir, tc.link, targets[tc.link]}
			ev.Outcome = o.version + ": reviewed Atomic realm link; no model reply or permission proof"
			defer func() { recordScenario(t, ev) }()

			probeOutput := filepath.Join(t.TempDir(), "provider.payload.jsonl")
			launch := fixture
			launch.Name, launch.WorkDir, launch.ConfigDir = tc.name, tc.dir, config
			launch.EventLog = filepath.Join(o.root, "events.jsonl")
			launch.Prepare = func(string) error { return nil }
			launch.Args = []string{"-p", "--mode", "json", "--cwd", tc.dir, "--offline", "--no-session", "--no-title",
				"--max-time", "20", "--model", "runtimeproof/" + modelID, "--api-key", ompPlaceholderKey, "--extension", probe}
			if tc.readTool {
				launch.Args = append(launch.Args, "--tools", "read")
				ev.Command += " --tools read"
			} else {
				launch.Args = append(launch.Args, "--no-tools")
				ev.Command += " --no-tools"
			}
			ev.Command += ": " + tc.prompt
			launch.Args = append(launch.Args, tc.prompt)
			launch.Env = append(append([]string(nil), fixture.Env...), "ATOMIC_OMP_PAYLOAD_PROBE="+probeOutput)
			obs, runErr := Run(t.Context(), launch)
			mu.Lock()
			requests := captured
			captured = nil
			mu.Unlock()
			probed, probeErr := os.ReadFile(probeOutput)
			if runErr != nil || probeErr != nil {
				t.Fatalf("OMP launch: %v; payload probe: %v (exit %d)\nstdout: %s\nstderr: %s", runErr, probeErr, obs.ExitCode, obs.Stdout, obs.Stderr)
			}
			var wire, hook bytes.Buffer
			for _, capture := range []struct {
				name string
				data [][]byte
				out  *bytes.Buffer
			}{
				{"loopback", requests, &wire},
				{"provider hook", bytes.Split(bytes.TrimSpace(probed), []byte{'\n'}), &hook},
			} {
				for _, raw := range capture.data {
					if !bytes.Contains(raw, []byte(tc.requestMarker)) {
						continue
					}
					if err := json.Compact(capture.out, raw); err != nil {
						t.Fatal(err)
					}
					capture.out.WriteByte('\n')
				}
				if capture.out.Len() == 0 {
					t.Errorf("no matching %s request for %q (%d captured; exit %d)\nstdout: %s\nstderr: %s",
						capture.name, tc.prompt, len(capture.data), obs.ExitCode, obs.Stdout, obs.Stderr)
				}
			}
			if hook.Len() != 0 {
				ev.Detail = "provider hook sha256 " + retain(t, safeEvidenceName(ev.Scenario)+".hook.jsonl", hook.String())
			}
			if wire.Len() != 0 {
				ev.Detail += "; loopback request sha256 " + retain(t, safeEvidenceName(ev.Scenario)+".payload.jsonl", wire.String())
				for _, request := range bytes.Split(bytes.TrimSpace(wire.Bytes()), []byte{'\n'}) {
					assertOMPRealmArtifactRequest(t, request, modelID, tc.readTool, tc.body, tc.userText)
					text := jsonStrings(t, string(request))
					if tc.discoverSkill {
						if !anyContains(text, skillName) || !anyContains(text, skillDesc) {
							t.Errorf("native request did not list %q with description %q", skillName, skillDesc)
						}
					} else if tc.readTool && tc.dir == member && anyContains(text, skillDesc) {
						t.Errorf("nested Git member request listed the realm skill description %q", skillDesc)
					}
				}
			}
			if !t.Failed() {
				ev.Outcome += "; " + tc.outcome
			}
		})
	}
}

func assertOMPRealmArtifactRequest(t *testing.T, request json.RawMessage, model string, allowRead bool, body, wantUserText string) {
	t.Helper()
	var decoded struct {
		Model string `json:"model"`
		Tools []struct {
			Function struct{ Name string } `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role      string            `json:"role"`
			Content   json.RawMessage   `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(request, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Model != model {
		t.Errorf("expected model %q, got %q", model, decoded.Model)
	}
	if allowRead && len(decoded.Tools) != 1 {
		t.Errorf("expected exactly one exposed Read tool, got %d tools", len(decoded.Tools))
	}
	for _, tool := range decoded.Tools {
		if !allowRead || tool.Function.Name != "read" {
			t.Errorf("unexpected tool exposed by OMP request: %q", tool.Function.Name)
		}
	}
	if wantUserText != body {
		allText := jsonStrings(t, string(request))
		for _, line := range strings.Split(body, "\n") {
			marker, _, _ := strings.Cut(line, " arguments=")
			if anyContains(allText, marker) {
				t.Errorf("request contained unexpected realm artifact marker %q", marker)
			}
		}
	}
	var userText []string
	for _, message := range decoded.Messages {
		if message.Role == "tool" || len(message.ToolCalls) != 0 {
			t.Error("OMP request contained a tool call or result")
		}
		if message.Role != "user" {
			continue
		}
		userText = append(userText, jsonStrings(t, `{"content":`+string(message.Content)+`}`)...)
	}
	for _, text := range userText {
		if strings.TrimSpace(text) == wantUserText {
			return
		}
	}
	t.Errorf("loopback user text = %q, want %q", userText, wantUserText)
}
