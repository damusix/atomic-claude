package runtimeproof

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodexRealmWikiSavedWrite(t *testing.T) {
	const (
		wikiFile = "wiki/index.md"
		original = "# Realm wiki\n"
		updated  = "# Realm wiki\n\nWritten by the native Codex tool.\n"
		model    = "gpt-5.2"
	)
	ev := ScenarioEvidence{
		Scenario:  "codex/realm-wiki-saved-write",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex exec, loopback Responses provider)",
		Criterion: "A fresh trusted Codex session writes through the reviewed realm wiki link only with the saved writable root",
		Command:   "codex exec --ephemeral --json --sandbox workspace-write (no --add-dir or Atomic hook)",
	}
	if testing.Short() || (runtime.GOOS != "darwin" && runtime.GOOS != "linux") {
		recordScenarioSkip(t, ev, "native sandbox proof requires a non-short run on macOS or Linux")
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		recordScenarioSkip(t, ev, "codex unavailable: "+err.Error())
	}
	codex, err = filepath.Abs(codex)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TEMPLATE_DIR", "")

	h := newRedesignHome(t, ev, map[string]string{"README.md": "# Realm consumer\n"})
	defer func() { recordScenario(t, ev) }()
	realm := linkRealmRuntimeArtifacts(t, ev, h, map[string]string{wikiFile: original}, []string{"wiki"})
	wiki := filepath.Join(realm, "wiki")
	if _, err := os.Readlink(wiki); err != nil {
		t.Fatalf("reviewed wiki directory must be a symlink: %v", err)
	}
	physicalWiki, err := filepath.EvalSymlinks(wiki)
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Dir(physicalWiki)
	if want := filepath.Join(h.home, ".atomic", "realms", filepath.Base(realm), "wiki"); physicalWiki != want {
		t.Fatalf("reviewed wiki resolves to %q, want %q", physicalWiki, want)
	}
	logical, physical := filepath.Join(realm, wikiFile), filepath.Join(physicalWiki, "index.md")
	if info, err := os.Lstat(physical); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("shared wiki file %q is not regular: %v", physical, err)
	}
	assertFiles := func(want string) {
		t.Helper()
		for _, path := range []string{logical, physical} {
			if got := readFileString(t, path); got != want {
				t.Fatalf("%q contains %q, want %q", path, got, want)
			}
		}
	}
	assertNoHook := func() {
		t.Helper()
		for _, path := range []string{
			filepath.Join(h.codexHome, "hooks.json"), filepath.Join(realm, ".codex", "hooks.json"),
			filepath.Join(h.codexHome, "AGENTS.md"), filepath.Join(realm, "AGENTS.md"),
		} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("unexpected native hook or instructions path %q: %v", path, err)
			}
		}
	}
	env := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + h.home, "USERPROFILE=" + h.home, "CODEX_HOME=" + h.codexHome,
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"HTTP_PROXY=http://127.0.0.1:0", "HTTPS_PROXY=http://127.0.0.1:0", "ALL_PROXY=http://127.0.0.1:0",
		"NO_PROXY=127.0.0.1", "no_proxy=127.0.0.1",
	}
	versionCmd := exec.CommandContext(t.Context(), codex, "--version")
	versionCmd.Dir, versionCmd.Env = realm, env
	versionOut, err := versionCmd.CombinedOutput()
	version := strings.TrimSpace(string(versionOut))
	if err != nil || !strings.HasPrefix(version, "codex ") && !strings.HasPrefix(version, "codex-cli ") {
		t.Fatalf("%s --version: %v\n%s", codex, err, versionOut)
	}
	ev.Paths = []string{codex, h.home, h.codexHome, realm, clone, logical, physical}
	ev.Detail = fmt.Sprintf("%s; GOOS=%s; trusted project .codex/config.toml; workspace-write, approval never, networking disabled, TMPDIR and /tmp excluded; writable_roots=[] then [%q]", version, runtime.GOOS, clone)
	t.Log(ev.Detail)
	assertNoHook()
	assertFiles(original)

	for _, sample := range []struct {
		name  string
		roots string
		want  string
	}{
		{"no-grant", "[]", original},
		{"saved-grant", fmt.Sprintf("[%q]", clone), updated},
	} {
		var mu sync.Mutex
		var requests []json.RawMessage
		var toolOutput string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "" {
				t.Error("unexpected loopback route or authentication header")
				http.Error(w, "expected unauthenticated Responses request", http.StatusBadRequest)
				return
			}
			var raw json.RawMessage
			var request struct {
				Model string `json:"model"`
				Tools []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"tools"`
				Input []struct {
					Type   string          `json:"type"`
					CallID string          `json:"call_id"`
					Output json.RawMessage `json:"output"`
				} `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Errorf("decode loopback request: %v", err)
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			requests = append(requests, raw)
			if err := json.Unmarshal(raw, &request); err != nil || request.Model != model {
				t.Errorf("unexpected Responses request model %q: %v", request.Model, err)
				http.Error(w, "unexpected model request", http.StatusBadRequest)
				return
			}
			const callID = "call_realm_wiki_write"
			switch len(requests) {
			case 1:
				for _, tool := range request.Tools {
					if tool.Type == "function" && tool.Name == "exec_command" {
						args, _ := json.Marshal(map[string]any{
							"cmd":   "printf '%s\\n' '# Realm wiki' '' 'Written by the native Codex tool.' > wiki/index.md",
							"login": false,
							"shell": "/bin/sh",
						})
						writeCodexWikiResponse(w, "resp_realm_write", map[string]any{
							"type": "function_call", "id": "fc_realm_write", "call_id": callID,
							"name": "exec_command", "arguments": string(args), "status": "completed",
						})
						return
					}
				}
				t.Error("Codex did not advertise the native exec_command tool")
			case 2:
				for _, item := range request.Input {
					if item.Type == "function_call_output" && item.CallID == callID && len(item.Output) > 0 {
						toolOutput = string(item.Output)
						writeCodexWikiResponse(w, "resp_realm_result", map[string]any{
							"type": "message", "id": "msg_realm_result", "role": "assistant", "status": "completed",
							"content": []map[string]any{{"type": "output_text", "text": "Fixture tool returned.", "annotations": []any{}}},
						})
						return
					}
				}
				t.Error("Codex did not send the native write tool result")
			default:
				t.Error("unexpected extra Responses request")
			}
			http.Error(w, "unexpected Responses exchange", http.StatusBadRequest)
		}))
		defer server.Close()

		projectConfig := "[sandbox_workspace_write]\nwritable_roots = " + sample.roots + "\nnetwork_access = false\nexclude_tmpdir_env_var = true\nexclude_slash_tmp = true\n"
		writeTree(t, realm, map[string]string{".codex/config.toml": projectConfig})
		userConfig := fmt.Sprintf(`
cli_auth_credentials_store = "file"
check_for_update_on_startup = false
approval_policy = "never"
model = %q
model_provider = "runtimeproof"

[projects.%q]
trust_level = "trusted"

[model_providers.runtimeproof]
name = "runtimeproof"
base_url = %q
wire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
`, model, realm, server.URL+"/v1")
		writeTree(t, h.codexHome, map[string]string{"config.toml": userConfig})
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		cmd := exec.CommandContext(ctx, codex, "exec", "--ephemeral", "--json", "--sandbox", "workspace-write", "Run the single fixture write.")
		cmd.Dir, cmd.Env, cmd.WaitDelay = realm, env, postExitWaitDelay
		out, err := cmd.CombinedOutput()
		cancel()
		server.Close()
		mu.Lock()
		requestLog, marshalErr := json.Marshal(requests)
		result := toolOutput
		mu.Unlock()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		prefix := "realm-wiki-codex." + sample.name
		ev.Detail += fmt.Sprintf("; %s project policy sha256 %s; trusted user config sha256 %s; Responses exchange sha256 %s; CLI output sha256 %s", sample.name,
			retain(t, prefix+".config.toml", projectConfig),
			retain(t, prefix+".user-config.toml", userConfig),
			retain(t, prefix+".requests.json", string(requestLog)),
			retain(t, prefix+".cli.jsonl", string(out)))
		if err != nil || result == "" || !strings.Contains(string(out), "Fixture tool returned.") {
			t.Fatalf("%s: Codex did not finish a native write turn (%v), tool result %s:\n%s", sample.name, err, result, out)
		}
		denied := strings.Contains(result, "Operation not permitted") || strings.Contains(result, "Permission denied") || strings.Contains(result, "Read-only file system")
		if denied != (sample.want == original) {
			t.Fatalf("%s: unexpected native tool result: %s", sample.name, result)
		}
		assertFiles(sample.want)
		assertNoHook()
	}
	ev.Outcome = "No-grant session was denied; saved-grant session wrote exact bytes through the logical wiki symlink and into the managed clone."
}

func writeCodexWikiResponse(w http.ResponseWriter, id string, item map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	response := map[string]any{"id": id, "object": "response"}
	emit := func(kind string, fields map[string]any) {
		fields["type"] = kind
		data, _ := json.Marshal(fields)
		_, _ = io.WriteString(w, "event: "+kind+"\ndata: "+string(data)+"\n\n")
	}
	emit("response.created", map[string]any{"response": response})
	emit("response.output_item.done", map[string]any{"output_index": 0, "item": item})
	response["status"] = "completed"
	response["output"] = []map[string]any{item}
	response["usage"] = map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}
	emit("response.completed", map[string]any{"response": response})
}
