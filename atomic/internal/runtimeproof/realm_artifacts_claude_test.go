package runtimeproof

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestClaudeRealmArtifactsReviewedLink(t *testing.T) {
	const primaryPrompt = "Accept this primary agent probe."
	const memberPrompt = "Accept this repository scope probe."
	fixture := ScenarioEvidence{
		Scenario:  "claude/realm-artifacts-reviewed-link",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "an isolated enrolled consumer links individually reviewed Claude realm artifacts",
		Command:   "atomic realm link <skill> <workflow> <agent> --yes --review <digest>",
	}
	if testing.Short() {
		recordScenarioSkip(t, fixture, "short mode: the real Claude launch is skipped")
	}
	binary, cliErr := ClaudeCLI()
	if cliErr == nil {
		binary, cliErr = exec.LookPath(binary)
	}
	if cliErr == nil {
		binary, cliErr = filepath.Abs(binary)
	}
	if cliErr != nil {
		recordScenarioSkip(t, fixture, "claude unavailable: "+cliErr.Error())
	}
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_COUNT", "0")

	artifacts := []struct {
		kind, name, marker, inventory string
	}{
		{"skill", "runtimeproof-realm-skill", "RUNTIMEPROOF_CLAUDE_REALM_SKILL_E6B15A42", "slash_commands"},
		{"command", "runtimeproof-realm-workflow", "RUNTIMEPROOF_CLAUDE_REALM_COMMAND_7C029E3B", "slash_commands"},
		{"agent", "runtimeproof-realm-agent", "RUNTIMEPROOF_CLAUDE_REALM_PRIMARY_9A724D6F", "agents"},
	}
	authored := make(map[string]string, len(artifacts))
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		unit := artifact.name + ".md"
		if artifact.kind == "skill" {
			unit = artifact.name
		}
		path := filepath.Join(".agents", artifact.kind+"s", unit)
		paths = append(paths, path)
		file := path
		if artifact.kind == "skill" {
			file = filepath.Join(path, "SKILL.md")
		}
		metadata := "description: Benign runtime proof fixture\n"
		if artifact.kind != "command" {
			metadata += "name: " + artifact.name + "\n"
		}
		authored[file] = "---\n" + metadata + "---\n\n" + artifact.marker + "\n"
	}
	consumer := newRedesignHome(t, fixture, map[string]string{"README.md": "# Install fixture\n"})
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(consumer.root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(consumer.root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(consumer.root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(consumer.root, "cache"))
	consumer.mustRun(t, consumer.repo, "--no-update-check", "install", "--harness", "claude", "--yes")
	realm := linkRealmRuntimeArtifacts(t, fixture, consumer, authored, paths)
	member := filepath.Join(realm, "repos", "member")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	guidanceGit(t, member, "init", "-q", "--template=", "-b", "main")
	home := consumer.home
	configDir := filepath.Join(home, ".claude")

	for i, artifact := range artifacts {
		t.Run(artifact.kind, func(t *testing.T) {
			path := paths[i]
			native := filepath.Join(realm, ".claude", artifact.kind+"s", filepath.Base(path))
			logical := filepath.Join(realm, path)
			if _, err := os.Readlink(native); err != nil {
				t.Fatalf("read Atomic native link %q: %v", native, err)
			}
			target, err := filepath.EvalSymlinks(native)
			if err != nil {
				t.Fatal(err)
			}
			source, err := filepath.EvalSymlinks(logical)
			if err != nil {
				t.Fatal(err)
			}
			if target != source {
				t.Fatalf("native link %q resolves to %q, want reviewed source %q", native, target, source)
			}
			invoke := []string{"--", "/" + artifact.name}
			if artifact.kind == "agent" {
				invoke = []string{"--agent", artifact.name, "--", primaryPrompt}
			}
			for _, scope := range []string{"realm-root", "member-repo"} {
				t.Run(scope, func(t *testing.T) {
					visible := scope == "realm-root"
					name := "realm-artifacts-reviewed-link-" + artifact.kind + "-" + scope
					promptArgs := invoke
					if !visible {
						promptArgs = []string{"--", memberPrompt}
					}
					args := append([]string{
						"-p", "--tools", "", "--output-format", "stream-json", "--verbose",
						"--setting-sources", "project", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
					}, promptArgs...)
					ev := ScenarioEvidence{
						Scenario: "claude/" + name,
						Group:    "claude",
						Engine:   "runtimeproof (real Claude)",
						Command:  "claude " + fmt.Sprintf("%q", args),
					}
					if visible {
						ev.Criterion = "native inventory names the Atomic-linked artifact and explicit invocation puts its exact body line in the Messages request"
					} else {
						ev.Criterion = "from a separately initialized nested Git repository, the root artifact is absent from native inventory and the ordinary prompt's Messages request"
					}
					defer func() { recordScenario(t, ev) }()

					dir := realm
					if !visible {
						dir = member
					}
					ev.Paths = []string{home, configDir, native, logical, dir}
					s := claudeLaunch(t, binary, home, configDir, dir, name)
					s.Args = args
					s.Env = append(consumer.launchOverrides(),
						"HTTP_PROXY=http://127.0.0.1:1", "http_proxy=http://127.0.0.1:1",
						"HTTPS_PROXY=http://127.0.0.1:1", "https_proxy=http://127.0.0.1:1",
						"ALL_PROXY=http://127.0.0.1:1", "all_proxy=http://127.0.0.1:1",
					)
					obs, requests := captureClaudeRealmStartup(t, s)
					ev.Detail = "stream sha256 " + retain(t, name+".stream.jsonl", obs.Stdout)
					for i, request := range requests {
						ev.Detail += "; Messages request sha256 " + retain(t, fmt.Sprintf("%s.request-%d.json", name, i+1), string(request))
					}
					var init map[string]any
					for _, message := range streamMessages(t, obs.Stdout) {
						if message["type"] == "system" && message["subtype"] == "init" {
							init = message
							break
						}
					}
					if init == nil {
						t.Fatalf("missing Claude init (exit %d)\nstdout: %s\nstderr: %s", obs.ExitCode, obs.Stdout, obs.Stderr)
					}
					version, _ := init["claude_code_version"].(string)
					if version == "" || init["cwd"] != dir {
						t.Fatalf("expected version and init cwd %q, got version %q and cwd %q", dir, version, init["cwd"])
					}
					for _, field := range []string{"tools", "mcp_servers"} {
						items, ok := init[field].([]any)
						if !ok || len(items) != 0 {
							t.Errorf("Claude init %s = %v, want empty array", field, init[field])
						}
					}
					inventory, ok := init[artifact.inventory].([]any)
					if !ok {
						t.Fatalf("Claude init did not report %q inventory", artifact.inventory)
					}
					listed := slices.Contains(inventory, any(artifact.name))
					marker, invoked := false, false
					for _, body := range requests {
						found, delivered := claudeRealmArtifactRequest(t, body, artifact.marker, primaryPrompt, artifact.kind == "agent")
						marker = marker || found
						invoked = invoked || delivered
					}
					ev.Outcome = fmt.Sprintf("Claude Code %s: listed=%t, body marker in request=%t, explicit delivery=%t (%d loopback Messages requests; no model reply)", version, listed, marker, invoked, len(requests))
					t.Log(ev.Outcome)
					if len(requests) == 0 || listed != visible || (visible && !invoked) || (!visible && marker) {
						t.Errorf("%s: %s, want visible=%t (exit %d)\nstdout: %s\nstderr: %s", scope, ev.Outcome, visible, obs.ExitCode, obs.Stdout, obs.Stderr)
					}
				})
			}
		})
	}
}

func claudeRealmArtifactRequest(t *testing.T, body []byte, marker, primaryPrompt string, primary bool) (found, delivered bool) {
	t.Helper()
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
	if len(request.Tools) != 0 {
		t.Errorf("Claude sent %d tools with --tools set to empty", len(request.Tools))
	}
	system := claudeRealmRequestText(t, request.System)
	systemMarker := slices.Contains(strings.Split(system, "\n"), marker)
	found = strings.Contains(system, marker)
	userMarker, userPrompt := false, false
	for _, message := range request.Messages {
		text := claudeRealmRequestText(t, message.Content)
		found = found || strings.Contains(text, marker)
		if message.Role == "user" {
			lines := strings.Split(text, "\n")
			userMarker = userMarker || slices.Contains(lines, marker)
			userPrompt = userPrompt || slices.Contains(lines, primaryPrompt)
		}
	}
	if primary {
		return found, systemMarker && userPrompt
	}
	return found, userMarker
}
