package runtimeproof

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOMPRealmSharingStartupRequest(t *testing.T) {
	const (
		teamPath       = ".agents/realm.md"
		unselectedPath = "notes/unselected.md"
		teamMarker     = "RUNTIMEPROOF_OMP_REALM_TEAM_EA040850"
		privateMarker  = "RUNTIMEPROOF_OMP_REALM_PRIVATE_33B4C890"
		unsharedMarker = "RUNTIMEPROOF_OMP_REALM_UNSELECTED_CC519571"
		modelID        = "runtimeproof-realm-probe"
		contextOpen    = `<atomic-session-context marker="atomic-omp-session-context">`
		ruleBaseline   = `<atomic-rules source="omp-runtime" delivery="session-baseline" marker="atomic-omp-runtime">`
	)
	ev := ScenarioEvidence{
		Scenario:  "omp/realm-sharing-startup-request",
		Group:     "omp",
		Engine:    "runtimeproof (real OMP)",
		Criterion: "fresh persisted startup composes the reviewed team and local private guidance; no-session and resume retain only the Atomic baseline",
		Command:   "omp -p --mode json --no-tools with a fresh persisted session, --no-session, then --resume <transcript>",
	}
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real OMP launch is skipped")
	}
	binary, err := OMPCLI()
	if err == nil {
		binary, err = exec.LookPath(binary)
	}
	if err != nil {
		recordScenarioSkip(t, ev, "omp unavailable: "+err.Error())
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}

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
	t.Setenv("ATOMIC_RUNTIMEPROOF_OMP_MODEL", "runtimeproof/"+modelID)
	t.Setenv("CP0_OMP_API_KEY", "")
	t.Setenv("CP0_OMP_AUTH_HOME", "")
	t.Setenv("ATOMIC_STATE_DIR", "")
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(sandbox, key))
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	t.Setenv("GIT_AUTHOR_NAME", "runtimeproof")
	t.Setenv("GIT_AUTHOR_EMAIL", "runtimeproof@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "runtimeproof")
	t.Setenv("GIT_COMMITTER_EMAIL", "runtimeproof@example.invalid")

	var requests atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !r.URL.IsAbs() && r.URL.Path == "/v1/chat/completions" {
			requests.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":{"message":"runtimeproof: no model reply","type":"invalid_request_error"}}`)
	}))
	defer provider.Close()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, provider.URL)
	}
	t.Setenv("NO_PROXY", "127.0.0.1,::1")
	t.Setenv("no_proxy", "127.0.0.1,::1")

	o := newOMPRedesign(t, ev, map[string]string{"README.md": "# Install fixture\n"}, func(h *redesignHome) {
		writeTree(t, h.home, map[string]string{
			".atomic/config.toml": "[update]\ncheck = false\n",
			".atomic/profile.md":  "# Runtimeproof profile\n\n<deterministic lastcheck=" + time.Now().UTC().Format(time.DateOnly) + ">\n</deterministic>\n",
		})
	})
	t.Logf("OMP CLI: %s", o.version)
	publisher, joiner := filepath.Join(o.root, "publisher"), filepath.Join(o.root, "joiner")
	remote := filepath.Join(sandbox, "remote.git")
	ev.Paths = []string{o.home, o.omp.AgentRoot, publisher, joiner, remote}
	ev.Outcome = o.version + ": loopback rejects requests without a model reply"
	defer func() { recordScenario(t, ev) }()

	writeTree(t, o.omp.AgentRoot, map[string]string{"models.yml": fmt.Sprintf(`providers:
  runtimeproof:
    baseUrl: %s/v1
    api: openai-completions
    apiKey: %s
    models:
      - id: %s
        name: Realm startup probe
        contextWindow: 128000
        maxTokens: 2048
`, provider.URL, ompPlaceholderKey, modelID)})
	writeTree(t, publisher, map[string]string{
		".atomic/atomic.toml": "scope = \"realm\"\n",
		teamPath:              teamMarker + "\n",
		unselectedPath:        unsharedMarker + "\n",
	})
	writeTree(t, joiner, map[string]string{
		".atomic/atomic.toml": "scope = \"realm\"\n",
		"AGENTS.local.md":     privateMarker + "\n",
	})
	guidanceGit(t, sandbox, "init", "--bare", "-b", "main", remote)
	digest := regexp.MustCompile(`(?m)^Review digest: (sha256:[[:xdigit:]]{64})$`)
	approve := func(dir string, args ...string) {
		t.Helper()
		preview := o.mustRun(t, dir, append(args, "--dry-run")...)
		if args[1] == "link" && !strings.Contains(preview, `"`+teamMarker+`"`) {
			t.Fatalf("atomic %v did not preview the exact team marker:\n%s", args, preview)
		}
		matches := digest.FindAllStringSubmatch(preview, -1)
		if len(matches) != 1 {
			t.Fatalf("expected one review digest in atomic %v output:\n%s", args, preview)
		}
		o.mustRun(t, dir, append(args, "--yes", "--review", matches[0][1])...)
	}
	approve(publisher, "realm", "share", teamPath, unselectedPath, "--remote", remote, "--branch", "main")
	git := exec.CommandContext(t.Context(), "git", "--git-dir", remote, "show", "main:"+unselectedPath)
	git.Dir, git.Env = sandbox, o.env()
	if out, err := git.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != unsharedMarker {
		t.Fatalf("unselected shared fixture was not published: %v\n%s", err, out)
	}
	o.mustRun(t, joiner, "realm", "join", remote)
	approve(joiner, "realm", "link", teamPath)
	if _, err := os.Lstat(filepath.Join(joiner, unselectedPath)); !os.IsNotExist(err) {
		t.Fatalf("unselected shared path is not absent locally: %v", err)
	}

	probe, err := filepath.Abs(filepath.Join("testdata", "omp-payload-probe.ts"))
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(o.root, "sessions")
	launch := func(t *testing.T, phase string, save bool, resume string) string {
		t.Helper()
		name := "omp-realm-startup-" + phase
		prompt := "This is the " + phase + " startup probe. Reply OK."
		payload := filepath.Join(o.root, name+".payload.jsonl")
		s, err := o.omp.Launch(OMPLaunch{
			Name: name, WorkDir: joiner, Prompt: prompt, Save: save, Session: resume,
			Args: []string{"--no-tools", "--no-title", "--offline", "--session-dir", sessionDir, "--extension", probe},
		})
		if err != nil {
			t.Fatal(err)
		}
		s.Args = slices.DeleteFunc(s.Args, func(arg string) bool { return arg == "--auto-approve" })
		s.Env = append(append(s.Env, o.launchOverrides()...), "ATOMIC_OMP_PAYLOAD_PROBE="+payload)
		s.Prepare = func(string) error { return nil }
		before := requests.Load()
		obs, err := Run(t.Context(), s)
		if err != nil {
			t.Fatalf("OMP %s: %v\nstdout: %s\nstderr: %s", phase, err, obs.Stdout, obs.Stderr)
		}
		data, err := os.ReadFile(payload)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 || requests.Load() == before {
			t.Fatalf("OMP %s did not capture a request to loopback: %v (exit %d)\nstdout: %s\nstderr: %s",
				phase, err, obs.ExitCode, obs.Stdout, obs.Stderr)
		}
		ev.Detail += phase + " payload sha256 " + retain(t, name+".payload.jsonl", string(data)) + "; "
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var request struct {
				Model    string            `json:"model"`
				Tools    []json.RawMessage `json:"tools"`
				Messages []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal([]byte(line), &request); err != nil {
				t.Fatalf("decode OMP provider payload: %v", err)
			}
			text := jsonStrings(t, line)
			if request.Model != modelID || len(request.Tools) != 0 || !anyContains(text, prompt) {
				t.Errorf("expected the tools-disabled %s request with the %s probe prompt", modelID, phase)
			}
			if !anyContains(text, ruleBaseline) {
				t.Error("provider payload lacks the Atomic OMP rule baseline")
			}
			if anyContains(text, unsharedMarker) {
				t.Errorf("provider payload contains the unselected shared marker %q", unsharedMarker)
			}
			if !save && (anyContains(text, teamMarker) || anyContains(text, privateMarker)) {
				t.Errorf("provider payload leaks realm guidance at %s startup", phase)
			}
			var baseline, pair bool
			for _, message := range request.Messages {
				if message.Role != "system" && message.Role != "developer" {
					continue
				}
				var content string
				if err := json.Unmarshal(message.Content, &content); err != nil {
					t.Fatalf("decode OMP %s message: %v", message.Role, err)
				}
				_, tail, opened := strings.Cut(content, contextOpen)
				body, _, closed := strings.Cut(tail, "</atomic-session-context>")
				if opened && closed {
					baseline = baseline || strings.Contains(body, "<workflow-preferences level='resolved'>")
					pair = pair || (strings.Contains(body, teamMarker) && strings.Contains(body, privateMarker))
				}
			}
			if !baseline {
				t.Error("provider payload lacks the fixture Atomic CLI workflow baseline")
			}
			if save && !pair {
				t.Errorf("persisted startup did not compose %q and %q in the same Atomic context block", teamMarker, privateMarker)
			}
		}
		id, err := SessionID(obs.Stdout)
		if err != nil {
			t.Fatalf("%s: %v\nstdout: %s\nstderr: %s", phase, err, obs.Stdout, obs.Stderr)
		}
		return id
	}
	primaryID := launch(t, "primary", true, "")
	transcript, err := filepath.Glob(filepath.Join(sessionDir, "*_"+primaryID+".jsonl"))
	if err != nil || len(transcript) != 1 {
		t.Fatalf("persisted primary session %q has %d native transcripts: %v", primaryID, len(transcript), err)
	}
	t.Run("ephemeral", func(t *testing.T) {
		id := launch(t, "ephemeral", false, "")
		files, err := filepath.Glob(filepath.Join(sessionDir, "*_"+id+".jsonl"))
		if err != nil || len(files) != 0 {
			t.Errorf("--no-session created %d native transcripts for %q: %v", len(files), id, err)
		}
	})
	t.Run("resume", func(t *testing.T) {
		if id := launch(t, "resume", false, transcript[0]); id != primaryID {
			t.Errorf("--resume loaded session %q, want persisted primary %q", id, primaryID)
		}
	})
	ev.Detail = strings.TrimSuffix(ev.Detail, "; ")
}
