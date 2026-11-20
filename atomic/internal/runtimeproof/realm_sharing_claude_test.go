package runtimeproof

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestClaudeRealmSharingStartupRequest(t *testing.T) {
	const (
		teamPath         = ".agents/realm.md"
		publisherPrivate = "RUNTIMEPROOF_REALM_PRIVATE_A_43F17C9A"
		joinerPrivate    = "RUNTIMEPROOF_REALM_PRIVATE_B_7E0AC4D2"
		teamMarker       = "RUNTIMEPROOF_REALM_TEAM_9D7E62B1"
		prompt           = "Reply OK to this realm startup probe."
		realmConfig      = "scope = \"realm\"\n"
	)
	ev := ScenarioEvidence{
		Scenario:  "claude/realm-sharing-startup-request",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "after independent startup, both SessionStart responses deliver only the reviewed team marker and each tools-disabled request contains the team marker and only its own private marker",
		Command:   `claude -p --tools "" --include-hook-events --output-format stream-json --verbose <prompt>`,
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	t.Setenv("GIT_AUTHOR_NAME", "runtimeproof")
	t.Setenv("GIT_AUTHOR_EMAIL", "runtimeproof@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "runtimeproof")
	t.Setenv("GIT_COMMITTER_EMAIL", "runtimeproof@example.invalid")

	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real Claude launch is skipped")
	}
	claudeBin, err := ClaudeCLI()
	if err == nil {
		claudeBin, err = exec.LookPath(claudeBin)
	}
	if err != nil {
		recordScenarioSkip(t, ev, "claude unavailable: "+err.Error())
	}
	claudeBin, err = filepath.Abs(claudeBin)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATOMIC_RUNTIMEPROOF_CLAUDE", claudeBin)
	bin := t.TempDir()
	if err := os.Symlink(claudeBin, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	installFiles := map[string]string{"README.md": "# Install fixture\n"}
	publisherCLI := newClaudeRedesign(t, ev, installFiles)
	joinerCLI := newClaudeRedesign(t, ev, installFiles)
	publisher := filepath.Join(publisherCLI.root, "publisher")
	joiner := filepath.Join(joinerCLI.root, "joiner")
	remote := filepath.Join(t.TempDir(), "remote.git")
	ev.Paths = []string{publisherCLI.home, publisherCLI.configDir, publisher, joinerCLI.home, joinerCLI.configDir, joiner, remote}
	t.Logf("Claude CLI: %s", publisherCLI.version)
	ev.Outcome = publisherCLI.version + ": loopback rejects model requests without a model reply"
	defer func() { recordScenario(t, ev) }()

	publisherCLI.mustRun(t, publisherCLI.repo, "install", "--harness", "claude", "--yes")
	joinerCLI.mustRun(t, joinerCLI.repo, "install", "--harness", "claude", "--yes")
	writeTree(t, publisher, map[string]string{
		".atomic/atomic.toml": realmConfig,
		"AGENTS.local.md":     publisherPrivate + "\n",
		teamPath:              teamMarker + "\n",
	})
	writeTree(t, joiner, map[string]string{
		".atomic/atomic.toml": realmConfig,
		"AGENTS.local.md":     joinerPrivate + "\n",
	})
	guidanceGit(t, filepath.Dir(remote), "init", "--bare", "-b", "main", remote)
	reviewDigest := regexp.MustCompile(`(?m)^Review digest: (sha256:[[:xdigit:]]{64})$`)
	approve := func(cli *claudeRedesign, dir string, args ...string) {
		t.Helper()
		preview := cli.mustRun(t, dir, append(args, "--dry-run")...)
		if args[1] == "link" && !strings.Contains(preview, `"`+teamMarker+`"`) {
			t.Fatalf("atomic %v did not preview the exact team marker:\n%s", args, preview)
		}
		digests := reviewDigest.FindAllStringSubmatch(preview, -1)
		if len(digests) != 1 {
			t.Fatalf("expected one review digest in atomic %v output:\n%s", args, preview)
		}
		cli.mustRun(t, dir, append(args, "--yes", "--review", digests[0][1])...)
	}
	approve(publisherCLI, publisher, "realm", "share", teamPath, "--remote", remote, "--branch", "main")
	approve(publisherCLI, publisher, "realm", "link", teamPath)
	joinerCLI.mustRun(t, joiner, "realm", "join", remote)
	approve(joinerCLI, joiner, "realm", "link", teamPath)

	sessions := make(map[string]bool)
	for _, developer := range []struct {
		name, dir, privateMarker, peerMarker string
		cli                                  *claudeRedesign
	}{
		{"publisher", publisher, publisherPrivate, joinerPrivate, publisherCLI},
		{"joiner", joiner, joinerPrivate, publisherPrivate, joinerCLI},
	} {
		t.Run(developer.name, func(t *testing.T) {
			name := "realm-sharing-startup-" + developer.name
			s := developer.cli.scenario(name, developer.dir,
				[]string{"-p", "--tools", "", "--include-hook-events", "--output-format", "stream-json", "--verbose", prompt})
			obs, requests := captureClaudeRealmStartup(t, s)
			ev.Detail += developer.name + " hook stream sha256 " + retain(t, name+".stream.jsonl", obs.Stdout) + "; "

			sessionID, _ := streamInit(t, obs.Stdout)["session_id"].(string)
			if sessionID == "" || sessions[sessionID] {
				t.Fatalf("expected an independent Claude session ID, got %q", sessionID)
			}
			sessions[sessionID] = true
			started := false
			for _, event := range obs.Events {
				started = started || (event.Name == "SessionStart" && event.Source == "startup" &&
					event.SessionID == sessionID && event.Cwd == developer.dir)
			}
			if !started {
				t.Errorf("native SessionStart observer did not record startup in %q for %q", developer.dir, sessionID)
			}

			sharedResponses := 0
			for _, delivered := range sessionStartContexts(t, obs.Stdout) {
				if strings.Contains(delivered, teamMarker) {
					sharedResponses++
					if strings.TrimSpace(delivered) != teamMarker {
						t.Errorf("SessionStart team context = %q, want only %q", delivered, teamMarker)
					}
				}
				if strings.Contains(delivered, developer.privateMarker) || strings.Contains(delivered, developer.peerMarker) {
					t.Error("SessionStart returned private guidance instead of leaving it to the local loader")
				}
			}
			if sharedResponses != 1 {
				t.Errorf("SessionStart returned the exact shared marker in %d responses, want one:\n%s", sharedResponses, obs.Stdout)
			}

			matched := false
			for _, body := range requests {
				var request struct {
					System   json.RawMessage   `json:"system"`
					Tools    []json.RawMessage `json:"tools"`
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.Unmarshal(body, &request); err != nil {
					t.Fatalf("decode Claude Messages request: %v", err)
				}
				team, own, peer := false, false, false
				checkMarkers := func(text string) {
					team = team || strings.Contains(text, teamMarker)
					own = own || strings.Contains(text, developer.privateMarker)
					peer = peer || strings.Contains(text, developer.peerMarker)
				}
				checkMarkers(claudeRealmRequestText(t, request.System))
				foundPrompt := false
				for _, message := range request.Messages {
					content := claudeRealmRequestText(t, message.Content)
					checkMarkers(content)
					foundPrompt = foundPrompt || (message.Role == "user" && strings.Contains(content, prompt))
				}
				if peer {
					t.Errorf("Claude model input contained peer private marker %q", developer.peerMarker)
				}
				if !foundPrompt {
					continue
				}
				if !matched {
					matched = true
					ev.Detail += developer.name + " Messages request sha256 " + retain(t, name+".request.json", string(body)) + "; "
				}
				if len(request.Tools) != 0 {
					t.Errorf("Claude sent %d tools with --tools set to empty", len(request.Tools))
				}
				if !team {
					t.Errorf("Claude model input lacked exact team marker %q", teamMarker)
				}
				if !own {
					t.Errorf("Claude model input lacked exact local private marker %q", developer.privateMarker)
				}
			}
			if !matched {
				t.Fatalf("Claude sent no Messages request with the user probe (%d captured, exit %d)\nstdout: %s\nstderr: %s",
					len(requests), obs.ExitCode, obs.Stdout, obs.Stderr)
			}
		})
	}
	ev.Detail = strings.TrimSuffix(ev.Detail, "; ")
}

func captureClaudeRealmStartup(t *testing.T, s Scenario) (Observation, [][]byte) {
	t.Helper()
	var mu sync.Mutex
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read Claude Messages request: %v", err)
			} else {
				mu.Lock()
				requests = append(requests, body)
				mu.Unlock()
			}
		}
		http.Error(w, "runtimeproof: no model reply", http.StatusBadRequest)
	}))
	s.Env = append(s.Env,
		"ANTHROPIC_BASE_URL="+server.URL,
		"ANTHROPIC_AUTH_TOKEN=runtimeproof-"+s.Name,
		"CLAUDECODE=", "NO_PROXY=127.0.0.1", "no_proxy=127.0.0.1",
	)
	obs, err := Run(t.Context(), s)
	server.Close()
	if err != nil {
		t.Fatalf("Claude startup: %v\nstdout: %s\nstderr: %s", err, obs.Stdout, obs.Stderr)
	}
	return obs, requests
}

func claudeRealmRequestText(t *testing.T, content json.RawMessage) string {
	t.Helper()
	if len(content) == 0 {
		return ""
	}
	if content[0] == '"' {
		var text string
		if err := json.Unmarshal(content, &text); err != nil {
			t.Fatalf("decode Claude request text: %v", err)
		}
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		t.Fatalf("decode Claude request content blocks: %v", err)
	}
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			text.WriteString(block.Text)
			text.WriteByte('\n')
		}
	}
	return text.String()
}
