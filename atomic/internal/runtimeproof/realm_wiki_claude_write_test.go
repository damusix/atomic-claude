package runtimeproof

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const claudeWikiWriteToolID = "toolu_runtimeproof_wiki_write"

func TestClaudeRealmWikiSavedWrite(t *testing.T) {
	const (
		filename = "claude-write-proof.md"
		content  = "# Claude saved write proof\n"
		mode     = "acceptEdits"
		prompt   = "Create the Claude saved write proof in the linked wiki using Write."
		finished = "Claude Write result received."
	)
	ev := ScenarioEvidence{
		Scenario: "claude/realm-wiki-saved-write",
		Group:    "claude",
		Engine:   "runtimeproof (real Claude)",
		Criterion: "S28: fresh Claude accepts a native Write through the wiki symlink with saved local additionalDirectories, " +
			"but rejects the same Write without that grant",
	}
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real Claude launch is skipped")
	}
	binary, err := ClaudeCLI()
	if err == nil {
		binary, err = exec.LookPath(binary)
	}
	if err != nil {
		recordScenarioSkip(t, ev, "claude unavailable: "+err.Error())
	}
	binary, err = filepath.Abs(binary)
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
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	consumer := newRedesignHome(t, ev, map[string]string{"README.md": "# Consumer realm\n"})
	defer func() { recordScenario(t, ev) }()
	realm := linkRealmRuntimeArtifacts(t, ev, consumer, map[string]string{"wiki/index.md": "# Shared wiki\n"}, []string{"wiki"})
	wiki := filepath.Join(realm, "wiki")
	if _, err := os.Readlink(wiki); err != nil {
		t.Fatalf("expected a linked wiki directory: %v", err)
	}
	physicalWiki, err := filepath.EvalSymlinks(wiki)
	if err != nil {
		t.Fatal(err)
	}
	git := exec.CommandContext(t.Context(), "git", "-C", physicalWiki, "rev-parse", "--show-toplevel")
	git.Env = consumer.env()
	out, err := git.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve linked wiki checkout: %v\n%s", err, out)
	}
	clone, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(consumer.home, clone); err != nil || !filepath.IsLocal(rel) || rel == "." {
		t.Fatalf("linked wiki checkout %q is not inside the disposable consumer home %q", clone, consumer.home)
	}
	logical, physical := filepath.Join(wiki, filename), filepath.Join(physicalWiki, filename)
	settings := filepath.Join(realm, ".claude", "settings.local.json")
	absent := func(t *testing.T, paths ...string) {
		t.Helper()
		for _, path := range paths {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("expected %q to be absent, lstat: %v", path, err)
			}
		}
	}
	absent(t, settings, filepath.Join(consumer.home, ".claude", "settings.json"))
	assertNoLoader := func(t *testing.T) {
		t.Helper()
		for _, path := range []string{"CLAUDE.md", "CLAUDE.local.md", ".claude/CLAUDE.md", ".claude/settings.json"} {
			absent(t, filepath.Join(realm, path))
		}
	}
	assertNoLoader(t)

	args := []string{
		"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", mode,
		"--setting-sources", "project,local", "--tools", "Write", "--no-session-persistence",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--", prompt,
	}
	ev.Command = fmt.Sprintf("%q %q", binary, args)
	ev.Paths = []string{consumer.home, realm, clone, settings, logical, physical}
	sessions := make(map[string]bool, 2)
	for _, tc := range []struct {
		name  string
		grant bool
	}{{"no-grant", false}, {"saved-grant", true}} {
		t.Run(tc.name, func(t *testing.T) {
			absent(t, logical, physical)
			policy := "{}\n"
			if tc.grant {
				dir, err := json.Marshal(clone)
				if err != nil {
					t.Fatal(err)
				}
				policy = fmt.Sprintf("{\"permissions\":{\"additionalDirectories\":[%s]}}\n", dir)
			}
			writeTree(t, realm, map[string]string{".claude/settings.local.json": policy})
			server, result := claudeRealmWikiWriteAPI(t, prompt, logical, content, finished)
			ctx, cancel := context.WithTimeout(t.Context(), DefaultTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Dir = realm
			cmd.WaitDelay = postExitWaitDelay
			cmd.Env = []string{
				"HOME=" + consumer.home, "CLAUDE_CONFIG_DIR=" + filepath.Join(consumer.home, ".claude"),
				"PATH=" + os.Getenv("PATH"), "TMPDIR=" + consumer.root,
				"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_AUTH_TOKEN=runtimeproof-local-only",
				"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
				"HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "ALL_PROXY=http://127.0.0.1:1",
				"NO_PROXY=127.0.0.1",
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			server.Close()
			digest := retain(t, "realm-wiki-claude-write-"+tc.name+".stream.jsonl", stdout.String())
			ev.Detail += fmt.Sprintf("%s: policy=%s; stream sha256 %s\n", tc.name, strings.TrimSpace(policy), digest)
			if err != nil {
				t.Fatalf("Claude launch: %v\nstdout: %s\nstderr: %s", err, &stdout, &stderr)
			}

			var init, final map[string]any
			for _, message := range streamMessages(t, stdout.String()) {
				switch message["type"] {
				case "system":
					switch message["subtype"] {
					case "init":
						init = message
					case "hook_started", "hook_progress", "hook_response":
						t.Fatalf("unexpected Claude hook event: %v", message)
					}
				case "result":
					final = message
				}
			}
			version, _ := init["claude_code_version"].(string)
			session, _ := init["session_id"].(string)
			if version == "" || init["cwd"] != realm || init["permissionMode"] != mode || session == "" || sessions[session] {
				t.Fatalf("expected a fresh Claude session at %q in %s, got %v", realm, mode, init)
			}
			sessions[session] = true
			if final["subtype"] != "success" || final["result"] != finished || !result.received || result.isError == tc.grant {
				t.Fatalf("grant=%t: expected one Write result (is_error=%t) and loopback completion; got %+v, final %v\nstdout: %s\nstderr: %s",
					tc.grant, !tc.grant, *result, final, &stdout, &stderr)
			}
			denials, ok := final["permission_denials"].([]any)
			if !ok {
				t.Fatalf("grant=%t: expected permission_denials array in native result, got %v", tc.grant, final)
			}
			writeDenied := slices.ContainsFunc(denials, func(entry any) bool {
				denial, _ := entry.(map[string]any)
				return denial["tool_use_id"] == claudeWikiWriteToolID && denial["tool_name"] == "Write"
			})
			if writeDenied != !tc.grant {
				t.Fatalf("grant=%t: expected native Write permission denial for %q=%t, got %v",
					tc.grant, claudeWikiWriteToolID, !tc.grant, denials)
			}
			if tc.grant {
				for _, path := range []string{logical, physical} {
					info, err := os.Lstat(path)
					if err != nil || !info.Mode().IsRegular() {
						t.Fatalf("expected regular file at %q, lstat: %v, %v", path, info, err)
					}
					if got, err := os.ReadFile(path); err != nil || string(got) != content {
						t.Fatalf("read %q = %q, %v; want %q", path, got, err, content)
					}
				}
			} else {
				absent(t, logical, physical)
			}
			assertNoLoader(t)
			if got, err := os.ReadFile(settings); err != nil || string(got) != policy {
				t.Fatalf("saved local policy changed: got %q, %v; want %q", got, err, policy)
			}
			line := fmt.Sprintf("Claude Code %s (%s), cwd=%s, mode=%s, clone=%s, grant=%t, Write is_error=%t",
				version, binary, realm, mode, clone, tc.grant, result.isError)
			ev.Outcome += line + "\n"
			t.Log(line)
		})
	}
}

type claudeWikiWriteResult struct {
	received bool
	isError  bool
}

func claudeRealmWikiWriteAPI(t *testing.T, prompt, path, content, finished string) (*httptest.Server, *claudeWikiWriteResult) {
	t.Helper()
	input, err := json.Marshal(map[string]string{"file_path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	sent := false
	result := &claudeWikiWriteResult{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model    string
			Stream   bool
			Tools    []struct{ Name string }
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Stream || req.Model == "" || len(req.Messages) == 0 {
			http.Error(w, "expected a streaming Messages request", http.StatusBadRequest)
			return
		}
		// Claude appends its environment as a system message after the conversation.
		end := len(req.Messages)
		for end > 0 && req.Messages[end-1].Role == "system" {
			end--
		}
		if end == 0 || req.Messages[end-1].Role != "user" {
			http.Error(w, fmt.Sprintf("expected a final user message before %d trailing system messages", len(req.Messages)-end), http.StatusBadRequest)
			return
		}
		last := req.Messages[end-1]
		var text string
		var blocks []struct {
			Type      string
			Text      string
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if err := json.Unmarshal(last.Content, &text); err != nil {
			if err := json.Unmarshal(last.Content, &blocks); err != nil {
				http.Error(w, "expected user content as text or content blocks", http.StatusBadRequest)
				return
			}
		}
		hasPrompt := strings.Contains(text, prompt)
		for _, block := range blocks {
			if block.Type == "text" && strings.Contains(block.Text, prompt) {
				hasPrompt = true
				break
			}
		}
		var block, delta map[string]any
		mu.Lock()
		defer mu.Unlock()
		id, stop := "msg_runtimeproof_write", "tool_use"
		switch {
		case !sent && hasPrompt:
			if !slices.ContainsFunc(req.Tools, func(tool struct{ Name string }) bool { return tool.Name == "Write" }) {
				http.Error(w, "native Write was not offered", http.StatusBadRequest)
				return
			}
			sent = true
			block = map[string]any{"type": "tool_use", "id": claudeWikiWriteToolID, "name": "Write", "input": map[string]any{}}
			delta = map[string]any{"type": "input_json_delta", "partial_json": string(input)}
		case sent && !result.received:
			for _, candidate := range blocks {
				if candidate.Type == "tool_result" && candidate.ToolUseID == claudeWikiWriteToolID {
					*result = claudeWikiWriteResult{received: true, isError: candidate.IsError}
					break
				}
			}
			if result.received {
				id, stop = "msg_runtimeproof_finish", "end_turn"
				block = map[string]any{"type": "text", "text": ""}
				delta = map[string]any{"type": "text_delta", "text": finished}
			}
		}
		if block == nil {
			http.Error(w, fmt.Sprintf("expected the fixture prompt or its native Write result (messages=%d, trailing system=%d, user blocks=%d, prompt matched=%t, Write sent=%t, result received=%t)",
				len(req.Messages), len(req.Messages)-end, len(blocks), hasPrompt, sent, result.received), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(kind string, data map[string]any) {
			data["type"] = kind
			fmt.Fprintf(w, "event: %s\ndata: ", kind)
			if err := json.NewEncoder(w).Encode(data); err != nil {
				t.Errorf("encode Messages %s event: %v", kind, err)
			}
			fmt.Fprintln(w)
		}
		event("message_start", map[string]any{"message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": req.Model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]int{"input_tokens": 1, "output_tokens": 0},
		}})
		event("content_block_start", map[string]any{"index": 0, "content_block": block})
		event("content_block_delta", map[string]any{"index": 0, "delta": delta})
		event("content_block_stop", map[string]any{"index": 0})
		event("message_delta", map[string]any{
			"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1},
		})
		event("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server, result
}
