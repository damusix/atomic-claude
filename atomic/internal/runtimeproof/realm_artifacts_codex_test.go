package runtimeproof

import (
	"bufio"
	"bytes"
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
	"testing"
	"time"
)

func TestCodexRealmArtifactSkillReviewedLink(t *testing.T) {
	const (
		name = "realm-color-proof"
		body = "The fixture color is RUNTIMEPROOF_CODEX_REALM_SKILL_B6CB0C22."
		unit = ".agents/skills/" + name
	)
	ev := ScenarioEvidence{
		Scenario:  "codex/realm-artifact-skill-reviewed-link",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex app-server, loopback provider)",
		Criterion: "An enrolled Codex discovers the reviewed Atomic skill link at the realm root, not in a nested Git member, and sends its body only on explicit selection",
		Command:   "codex app-server: skills/list; thread/start; turn/start with and without an explicit skill item",
	}
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real harness launch is skipped")
	}
	_, err := exec.LookPath("codex")
	if err != nil {
		recordScenarioSkip(t, ev, "codex unavailable: "+err.Error())
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "CHATGPT_") ||
			strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CODEX_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	isolatedEnv := []string{
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TEMPLATE_DIR=", "GIT_ALLOW_PROTOCOL=file",
		"HTTP_PROXY=http://127.0.0.1:0", "HTTPS_PROXY=http://127.0.0.1:0", "ALL_PROXY=http://127.0.0.1:0",
		"http_proxy=http://127.0.0.1:0", "https_proxy=http://127.0.0.1:0", "all_proxy=http://127.0.0.1:0",
		"NO_PROXY=127.0.0.1", "no_proxy=127.0.0.1",
	}
	for _, entry := range isolatedEnv {
		key, value, _ := strings.Cut(entry, "=")
		t.Setenv(key, value)
	}

	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read loopback Responses request: %v", err)
		} else {
			select {
			case requests <- string(payload):
			default:
				t.Error("unexpected extra loopback Responses request")
			}
		}
		http.Error(w, "runtimeproof: no model reply", http.StatusBadRequest)
	}))
	defer server.Close()
	defer func() { recordScenario(t, ev) }()

	consumer := newCodexRedesign(t, ev, map[string]string{"README.md": "# Codex realm artifact consumer\n"}, func(h *redesignHome) {
		t.Setenv("USERPROFILE", h.home)
		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
			value := filepath.Join(h.root, key)
			t.Setenv(key, value)
			isolatedEnv = append(isolatedEnv, key+"="+value)
		}
		writeTree(t, h.home, map[string]string{".atomic/atomic.toml": "[update]\ncheck = false\n"})
		writeTree(t, h.codexHome, map[string]string{"config.toml": fmt.Sprintf(`
cli_auth_credentials_store = "file"
check_for_update_on_startup = false
model = "gpt-5.2"
model_provider = "runtimeproof"

[model_providers.runtimeproof]
name = "runtimeproof"
base_url = %q
wire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
`, server.URL+"/v1")})
	})
	realm := linkRealmRuntimeArtifacts(t, ev, consumer.redesignHome, map[string]string{
		unit + "/SKILL.md": "---\nname: " + name + "\ndescription: A benign color fixture.\n---\n\n" + body + "\n",
	}, []string{unit})
	member := filepath.Join(realm, "repos", "member")
	link := filepath.Join(realm, filepath.FromSlash(unit))
	ev.Paths = []string{consumer.home, consumer.codexHome, realm, member, link}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("reviewed realm skill %q is not a symlink: %v", link, err)
	}
	skillFile, err := filepath.EvalSymlinks(filepath.Join(link, "SKILL.md"))
	if err != nil {
		t.Fatalf("resolve reviewed realm skill: %v", err)
	}
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	guidanceGit(t, member, "init", "-q", "-b", "main")

	env := append(isolatedEnv, consumer.path(), "HOME="+consumer.home, "USERPROFILE="+consumer.home, "CODEX_HOME="+consumer.codexHome)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	t.Log(consumer.version)

	cmd := exec.CommandContext(ctx, consumer.codex, "app-server")
	cmd.Dir, cmd.Env = realm, env
	cmd.WaitDelay = postExitWaitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
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
		cancel()
		_ = cmd.Wait()
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("codex app-server stderr:\n%s", &stderr)
		}
	}()

	encoder := json.NewEncoder(stdin)
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 0, 64<<10), 16<<20)
	id := 0
	rpc := func(method string, params any) json.RawMessage {
		t.Helper()
		id++
		if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			t.Fatalf("send %s: %v", method, err)
		}
		for lines.Scan() {
			var message struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(lines.Bytes(), &message); err != nil {
				t.Fatalf("decode app-server message for %s: %v\n%s", method, err, lines.Bytes())
			}
			if message.ID == nil || *message.ID != id {
				continue
			}
			if len(message.Error) != 0 {
				t.Fatalf("app-server %s: %s", method, message.Error)
			}
			return message.Result
		}
		t.Fatalf("app-server ended before answering %s: scan=%v context=%v", method, lines.Err(), ctx.Err())
		return nil
	}
	rpc("initialize", map[string]any{"clientInfo": map[string]any{"name": "runtimeproof", "version": "0"}})
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		t.Fatal(err)
	}

	type skill struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Scope string `json:"scope"`
	}
	list := func(cwd string) (json.RawMessage, []skill) {
		t.Helper()
		raw := rpc("skills/list", map[string]any{"cwds": []string{cwd}})
		var result struct {
			Data []struct {
				Cwd    string            `json:"cwd"`
				Skills []skill           `json:"skills"`
				Errors []json.RawMessage `json:"errors"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decode skills/list for %s: %v\n%s", cwd, err, raw)
		}
		if len(result.Data) != 1 || result.Data[0].Cwd != cwd || len(result.Data[0].Errors) != 0 {
			t.Fatalf("skills/list did not return a clean result for %s:\n%s", cwd, raw)
		}
		var matches []skill
		for _, entry := range result.Data[0].Skills {
			if entry.Name == name {
				matches = append(matches, entry)
			}
		}
		return raw, matches
	}
	realmList, matches := list(realm)
	if len(matches) != 1 {
		t.Fatalf("realm skills/list returned %d entries for %q, want exactly one:\n%s", len(matches), name, realmList)
	}
	selected := matches[0]
	if path, err := filepath.EvalSymlinks(selected.Path); err != nil || path != skillFile {
		t.Fatalf("skill path %q resolved to %q (%v), want %q from the reviewed link", selected.Path, path, err, skillFile)
	}
	memberList, memberMatches := list(member)
	t.Logf("realm skill scope=%q path=%q; nested Git member matches=%v", selected.Scope, selected.Path, memberMatches)
	ev.Detail = fmt.Sprintf("realm scope %q; nested Git member matches %d (list only); realm skills/list sha256 %s; member skills/list sha256 %s",
		selected.Scope, len(memberMatches),
		retain(t, "realm-artifacts-codex.realm.skills-list.json", string(realmList)),
		retain(t, "realm-artifacts-codex.member.skills-list.json", string(memberList)))
	if len(memberMatches) != 0 {
		t.Fatalf("nested Git member unexpectedly listed the realm skill %q:\n%s", name, memberList)
	}

	turn := func(input []map[string]any) string {
		t.Helper()
		start := rpc("thread/start", map[string]any{"cwd": realm, "ephemeral": true, "sandbox": "read-only", "approvalPolicy": "never"})
		var thread struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(start, &thread); err != nil || thread.Thread.ID == "" {
			t.Fatalf("thread/start did not return an id (%v): %s", err, start)
		}
		rpc("turn/start", map[string]any{"threadId": thread.Thread.ID, "input": input})
		select {
		case request := <-requests:
			return request
		case <-ctx.Done():
			t.Fatalf("Codex sent no loopback Responses request: %v", ctx.Err())
			return ""
		}
	}
	prompt := map[string]any{"type": "text", "text": DefaultPrompt, "textElements": []any{}}
	for _, sample := range []struct {
		label    string
		input    []map[string]any
		wantBody bool
	}{
		{"unselected", []map[string]any{prompt}, false},
		{"selected", []map[string]any{prompt, {"type": "skill", "name": selected.Name, "path": selected.Path}}, true},
	} {
		payload := turn(sample.input)
		ev.Detail += "; " + sample.label + " Responses request sha256 " + retain(t, "realm-artifacts-codex."+sample.label+".request.json", payload)
		var request struct {
			Input []struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.Unmarshal([]byte(payload), &request); err != nil || len(request.Input) == 0 {
			t.Fatalf("%s Responses request lacks valid input: %v", sample.label, err)
		}
		var values []string
		for _, item := range request.Input {
			for _, content := range item.Content {
				if content.Type == "input_text" {
					values = append(values, content.Text)
				}
			}
		}
		if !anyContains(values, DefaultPrompt) || anyContains(values, body) != sample.wantBody {
			t.Errorf("%s Responses input: want prompt and skill body present=%t; got %q", sample.label, sample.wantBody, values)
		}
	}
	if !t.Failed() {
		ev.Outcome = consumer.version + ": reviewed canonical skill listed at realm root, absent in nested Git member, body captured only on explicit selection; no model reply or command/agent evidence"
	}
}
