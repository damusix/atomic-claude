package runtimeproof

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCodexRealmSharingStartupRequest(t *testing.T) {
	const (
		teamPath          = ".agents/realm.md"
		privatePath       = "AGENTS.local.md"
		unselectedPath    = "notes.md"
		teamMarker        = "RUNTIMEPROOF_CODEX_REALM_TEAM_4FE2A371"
		privateMarker     = "RUNTIMEPROOF_CODEX_REALM_JOINER_1738FEA2"
		publisherMarker   = "RUNTIMEPROOF_CODEX_REALM_PUBLISHER_F64A25D3"
		unselectedMarker  = "RUNTIMEPROOF_CODEX_REALM_UNSELECTED_9E70A42D"
		installedBaseline = "Walk this ladder before writing anything"
		hookBaseline      = "issue-tracker-project: CODEXREALMPROBE"
		realmConfig       = "scope = \"realm\"\n"
	)
	ev := ScenarioEvidence{
		Scenario:  "codex/realm-sharing-startup-request",
		Group:     "codex",
		Engine:    "runtimeproof (real Codex, loopback provider)",
		Criterion: "both fresh requests contain installed steering, and only exact native hook trust delivers the ordinary hook context, reviewed team instructions, and the joiner's private guidance",
		Command:   "codex app-server hooks/list; codex exec --json before and after trusting the reported currentHash",
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_") || strings.HasPrefix(name, "OPENAI_") ||
			strings.HasPrefix(name, "CODEX_") || strings.HasPrefix(name, "CHATGPT_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	t.Setenv("GIT_AUTHOR_NAME", "runtimeproof")
	t.Setenv("GIT_AUTHOR_EMAIL", "runtimeproof@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "runtimeproof")
	t.Setenv("GIT_COMMITTER_EMAIL", "runtimeproof@example.invalid")

	c := newCodexRedesign(t, ev, map[string]string{"README.md": "# Codex realm startup probe\n"}, func(h *redesignHome) {
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
			t.Setenv(name, "http://127.0.0.1:0")
		}
		t.Setenv("NO_PROXY", "127.0.0.1,localhost")
		t.Setenv("no_proxy", "127.0.0.1,localhost")
		t.Setenv("GIT_ALLOW_PROTOCOL", "file")
		t.Setenv("ATOMIC_STATE_DIR", "")
		for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
			t.Setenv(name, filepath.Join(h.root, name))
		}
		writeTree(t, h.codexHome, map[string]string{
			"config.toml": "cli_auth_credentials_store = \"file\"\ncheck_for_update_on_startup = false\n",
		})
		writeTree(t, h.home, map[string]string{".atomic/atomic.toml": "[update]\ncheck = false\n"})
	})
	t.Logf("Codex CLI: %s", c.version)
	publisher := newRedesignHome(t, ev, map[string]string{
		".atomic/atomic.toml": realmConfig,
		teamPath:              teamMarker + "\n",
		privatePath:           publisherMarker + "\n",
		unselectedPath:        unselectedMarker + "\n",
	})
	remote := filepath.Join(publisher.root, "remote.git")
	ev.Paths = []string{c.home, c.codexHome, c.repo, publisher.home, publisher.repo, remote}
	ev.Outcome = c.version + ": loopback rejects model requests without a model reply"
	defer func() { recordScenario(t, ev) }()

	writeTree(t, c.repo, map[string]string{
		".atomic/atomic.toml":             realmConfig,
		".atomic/workflow-preferences.md": "<workflow-preferences level='realm'>\n- issue-tracker: linear\n- " + hookBaseline + "\n</workflow-preferences>\n",
		privatePath:                       privateMarker + "\n",
	})
	guidanceGit(t, publisher.root, "init", "--bare", "-b", "main", remote)
	reviewDigest := regexp.MustCompile(`(?m)^Review digest: (sha256:[[:xdigit:]]{64})$`)
	approve := func(h *redesignHome, args ...string) {
		t.Helper()
		preview := h.mustRun(t, h.repo, append(args, "--dry-run")...)
		if args[1] == "link" && !strings.Contains(preview, `"`+teamMarker+`"`) {
			t.Fatalf("atomic %v did not preview the exact team marker:\n%s", args, preview)
		}
		digests := reviewDigest.FindAllStringSubmatch(preview, -1)
		if len(digests) != 1 {
			t.Fatalf("expected one review digest in atomic %v output:\n%s", args, preview)
		}
		h.mustRun(t, h.repo, append(args, "--yes", "--review", digests[0][1])...)
	}
	approve(publisher, "realm", "share", teamPath, unselectedPath, "--remote", remote, "--branch", "main")
	join := c.mustRun(t, c.repo, "realm", "join", remote)
	if !strings.Contains(join, unselectedPath) {
		t.Fatalf("join did not list the shared but unselected path %q:\n%s", unselectedPath, join)
	}
	approve(c.redesignHome, "realm", "link", teamPath)
	linkedTeam := filepath.Join(c.repo, teamPath)
	if _, err := os.Readlink(linkedTeam); err != nil {
		t.Fatalf("read reviewed team link: %v", err)
	}
	if got := readFileString(t, linkedTeam); got != teamMarker+"\n" {
		t.Fatalf("linked team artifact = %q, want %q", got, teamMarker+"\n")
	}
	if _, err := os.Lstat(filepath.Join(c.repo, unselectedPath)); !os.IsNotExist(err) {
		t.Fatalf("unselected artifact must stay unlinked: %v", err)
	}

	verifyRequest := func(name string, body string, trusted bool) {
		t.Helper()
		values := jsonStrings(t, body)
		for _, want := range []string{DefaultPrompt, installedBaseline} {
			if !anyContains(values, want) {
				t.Errorf("%s request lacks %q", name, want)
			}
		}
		for _, absent := range []string{publisherMarker, unselectedMarker} {
			if anyContains(values, absent) {
				t.Errorf("%s request contains out-of-scope marker %q", name, absent)
			}
		}
		for _, marker := range []string{hookBaseline, teamMarker, privateMarker} {
			if got := anyContains(values, marker); got != trusted {
				t.Errorf("%s request contains %q = %t, want %t", name, marker, got, trusted)
			}
		}
	}

	hook, untrustedList := c.sessionHook(t)
	wantKey := filepath.Join(c.codexHome, "hooks.json") + ":session_start:0:0"
	if hook.Source != "user" || hook.TrustStatus != "untrusted" || hook.Key != wantKey || !strings.HasPrefix(hook.CurrentHash, "sha256:") {
		t.Fatalf("expected installed, untrusted native SessionStart member in the isolated Codex home:\n%s", untrustedList)
	}
	_, untrusted := c.exec(t, "realm-startup-untrusted", "gpt-5.2")
	verifyRequest("untrusted", untrusted, false)

	c.setHookState(t, fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\nenabled = true\n", hook.Key, hook.CurrentHash))
	trustedHook, trustedList := c.sessionHook(t)
	if trustedHook.TrustStatus != "trusted" || trustedHook.Key != hook.Key || trustedHook.CurrentHash != hook.CurrentHash {
		t.Fatalf("expected native trust for the exact installed SessionStart member:\n%s", trustedList)
	}
	_, trusted := c.exec(t, "realm-startup-trusted", "gpt-5.2")
	verifyRequest("trusted", trusted, true)
	if got := readFileString(t, filepath.Join(c.repo, privatePath)); got != privateMarker+"\n" {
		t.Errorf("joiner's private source changed: %q", got)
	}
	ev.Detail = "untrusted hooks/list sha256 " + retain(t, "realm-sharing-codex.untrusted.hooks-list.json", untrustedList) +
		"; untrusted request sha256 " + retain(t, "realm-sharing-codex.untrusted.request.jsonl", untrusted) +
		"; trusted hooks/list sha256 " + retain(t, "realm-sharing-codex.trusted.hooks-list.json", trustedList) +
		"; trusted request sha256 " + retain(t, "realm-sharing-codex.trusted.request.jsonl", trusted)
}
