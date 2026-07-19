package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type resolveEnvelopeForTest struct {
	SchemaVersion int                          `json:"schema_version"`
	Valid         bool                         `json:"valid"`
	Agents        map[string]map[string]string `json:"agents"`
	Diagnostics   []map[string]string          `json:"diagnostics"`
}

func runResolveForTest(t *testing.T, home, repo string) (int, resolveEnvelopeForTest, string, string) {
	t.Helper()
	code, out, stderr := runCLI(t, home, "resolve", "--repo", repo, "--json")
	var env resolveEnvelopeForTest
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("resolve emitted invalid JSON: %v\nout=%q\nstderr=%q", err, out, stderr)
	}
	return code, env, out, stderr
}

func TestRunResolvePiAgentsAlwaysUsesPiRepoConfig(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".pi", "atomic.toml"), []byte(`[pi.agent.scout]
model = "openai-codex/gpt-5.4-mini"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, out, stderr := runResolveForTest(t, home, repo)
	if code != 0 {
		t.Fatalf("resolve exit=%d stderr=%q env=%+v", code, stderr, env)
	}
	var rawEnvelope map[string]any
	if err := json.Unmarshal([]byte(out), &rawEnvelope); err != nil {
		t.Fatal(err)
	}
	if _, ok := rawEnvelope["diagnostics"].([]any); !ok {
		t.Fatalf("diagnostics must be a JSON array: %s", out)
	}
	if got := env.Agents["scout"]["model"]; got != "openai-codex/gpt-5.4-mini" {
		t.Fatalf("resolved model from wrong harness dir: %q", got)
	}
}

func TestRunResolvePiAgentsMergesLocalFieldsOverGlobal(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.scout]
model = "openai-codex/gpt-5.4-mini"
thinking = "off"

[pi.agent.reviewer]
model = "anthropic/claude-sonnet-4"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".pi", "atomic.toml"), []byte(`[pi.agent.scout]
thinking = "high"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, _, stderr := runResolveForTest(t, home, repo)
	if code != 0 {
		t.Fatalf("resolve exit=%d stderr=%q env=%+v", code, stderr, env)
	}
	if env.SchemaVersion != 1 || !env.Valid || len(env.Diagnostics) != 0 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if got := env.Agents["scout"]["model"]; got != "openai-codex/gpt-5.4-mini" {
		t.Fatalf("scout model=%q", got)
	}
	if got := env.Agents["scout"]["thinking"]; got != "high" {
		t.Fatalf("scout thinking=%q", got)
	}
	if got := env.Agents["reviewer"]["model"]; got != "anthropic/claude-sonnet-4" {
		t.Fatalf("reviewer model=%q", got)
	}
}

func TestRunResolvePiAgentsInvalidLocalSuppressesGlobal(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.scout]
model = "openai-codex/gpt-5.4-mini"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".pi", "atomic.toml"), []byte(`[pi.agent.scout]
thinking = "loud"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, _, _ := runResolveForTest(t, home, repo)
	if code != 0 || !env.Valid {
		t.Fatalf("per-agent invalid entries should not fail envelope: code=%d env=%+v", code, env)
	}
	if _, ok := env.Agents["scout"]; ok {
		t.Fatalf("invalid local scout should suppress global scout: %+v", env.Agents)
	}
}

func TestRunResolvePiAgentsInvalidEntryIsIsolated(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.good]
model = "openai-codex/gpt-5.4-mini"
thinking = "minimal"

[pi.agent.bad]
model = ""
thinking = "loud"
prompt = "nope"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, _, _ := runResolveForTest(t, home, repo)
	if code != 0 || !env.Valid {
		t.Fatalf("per-agent invalid entries should not fail envelope: code=%d env=%+v", code, env)
	}
	if _, ok := env.Agents["good"]; !ok {
		t.Fatalf("valid agent missing: %+v", env.Agents)
	}
	if _, ok := env.Agents["bad"]; ok {
		t.Fatalf("invalid agent should be omitted: %+v", env.Agents)
	}
	if len(env.Diagnostics) < 3 {
		t.Fatalf("expected diagnostics for bad entry, got %+v", env.Diagnostics)
	}
	for _, d := range env.Diagnostics {
		if d["agent"] == "bad" && d["severity"] == "error" {
			return
		}
	}
	t.Fatalf("expected error diagnostic for bad agent, got %+v", env.Diagnostics)
}

func TestRunResolvePiAgentsValidatesAuthoredNameGrammar(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	long := "a" + strings.Repeat("b", 64)
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.valid-name-1]
model = "provider/model/with/slash"

[pi.agent.Bad]
model = "provider/model"

[pi.agent.bad_underscore]
model = "provider/model"

[pi.agent.bad-]
model = "provider/model"

[pi.agent.`+long+`]
model = "provider/model"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, _, _ := runResolveForTest(t, home, repo)
	if code != 0 || !env.Valid {
		t.Fatalf("invalid agents should be isolated: code=%d env=%+v", code, env)
	}
	if got := env.Agents["valid-name-1"]["model"]; got != "provider/model/with/slash" {
		t.Fatalf("valid model with slash not preserved: %q", got)
	}
	for _, name := range []string{"Bad", "bad_underscore", "bad-", long} {
		if _, ok := env.Agents[name]; ok {
			t.Fatalf("invalid name %q should not resolve: %+v", name, env.Agents)
		}
	}
	invalidNameDiagnostics := 0
	for _, diagnostic := range env.Diagnostics {
		if strings.Contains(diagnostic["message"], "agent name") {
			invalidNameDiagnostics++
			if diagnostic["agent"] != "" {
				t.Fatalf("invalid authored names must not be returned as diagnostic agent identities: %+v", diagnostic)
			}
		}
	}
	if invalidNameDiagnostics != 4 {
		t.Fatalf("expected one diagnostic per invalid name, got %+v", env.Diagnostics)
	}
}

func TestRunResolvePiAgentsOmitsInvalidNonTableAgentIdentity(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent]
Bad_Name = "not a table"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, env, _, _ := runResolveForTest(t, home, repo)
	if code != 0 || !env.Valid || len(env.Diagnostics) != 1 {
		t.Fatalf("invalid entry should be isolated: code=%d env=%+v", code, env)
	}
	if env.Diagnostics[0]["agent"] != "" || env.Diagnostics[0]["code"] != "config-agent-invalid" {
		t.Fatalf("unsafe diagnostic agent identity leaked: %+v", env.Diagnostics[0])
	}
}

func TestRunResolvePiAgentsPiNonTablesAreSourceInvalid(t *testing.T) {
	for _, content := range []string{`pi = "nope"`, `[pi]
agent = "nope"
`} {
		t.Run(content, func(t *testing.T) {
			home := t.TempDir()
			repo := t.TempDir()
			if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(TOMLPath(home), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			code, env, _, _ := runResolveForTest(t, home, repo)
			if code == 0 || env.Valid {
				t.Fatalf("non-table source should be invalid: code=%d env=%+v", code, env)
			}
			if len(env.Diagnostics) != 1 || env.Diagnostics[0]["code"] != "config-source-invalid" || env.Diagnostics[0]["source"] != "global" {
				t.Fatalf("unexpected diagnostics: %+v", env.Diagnostics)
			}
		})
	}
}

func TestRunResolvePiAgentsEmptyTablesAndInvalidModels(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.empty]

[pi.agent.star]
model = "openai/*"

[pi.agent.space]
model = "openai/gpt 5"

[pi.agent.prefix]
model = "openai/gpt-*"

[pi.agent.question]
model = "openai/gpt?"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, env, _, _ := runResolveForTest(t, home, repo)
	if code != 0 || !env.Valid {
		t.Fatalf("agent invalids should be isolated: code=%d env=%+v", code, env)
	}
	if len(env.Agents) != 0 {
		t.Fatalf("empty/invalid tables should not produce overrides: %+v", env.Agents)
	}
	if len(env.Diagnostics) != 4 {
		t.Fatalf("expected 4 model diagnostics, got %+v", env.Diagnostics)
	}
	for _, d := range env.Diagnostics {
		if d["code"] != "config-agent-invalid" {
			t.Fatalf("unexpected diagnostic code: %+v", env.Diagnostics)
		}
	}
}

func TestRunResolvePiAgentsDiagnosticsAreDeterministic(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.zed]
prompt = "nope"
thinking = "loud"
model = ""

[pi.agent.alpha]
tools = []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, first, out1, _ := runResolveForTest(t, home, repo)
	_, second, out2, _ := runResolveForTest(t, home, repo)
	if out1 != out2 {
		t.Fatalf("resolve output is not deterministic:\n%s\n---\n%s", out1, out2)
	}
	if len(first.Diagnostics) != len(second.Diagnostics) || len(first.Diagnostics) != 4 {
		t.Fatalf("unexpected diagnostics: %+v %+v", first.Diagnostics, second.Diagnostics)
	}
	wantAgents := []string{"alpha", "zed", "zed", "zed"}
	for i, want := range wantAgents {
		if first.Diagnostics[i]["agent"] != want {
			t.Fatalf("diagnostic %d agent=%q want %q: %+v", i, first.Diagnostics[i]["agent"], want, first.Diagnostics)
		}
	}
}

func TestRunResolvePiAgentsRejectsUnsafeRepoPath(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "resolve", "--repo", filepath.Join(t.TempDir(), "missing"), "--json")
	if code != 1 {
		t.Fatalf("expected repo path failure exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "repo path") {
		t.Fatalf("expected repo path diagnostic in stderr, got %q", stderr)
	}
}

func TestRunResolvePiAgentsSourceParseFailureEmitsInvalidEnvelope(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[pi.agent.scout]
model = `), 0o644); err != nil {
		t.Fatal(err)
	}

	code, env, out, stderr := runResolveForTest(t, home, repo)
	if code == 0 || env.Valid {
		t.Fatalf("parse failure should exit nonzero with valid=false: code=%d out=%q stderr=%q", code, out, stderr)
	}
	if env.SchemaVersion != 1 || len(env.Diagnostics) == 0 {
		t.Fatalf("missing versioned diagnostics: %+v", env)
	}
}

func TestWritePersistPreservesPiAgentTables(t *testing.T) {
	home := t.TempDir()
	path := TOMLPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[pi.agent.scout]
model = "openai-codex/gpt-5.4-mini"
thinking = "off"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
		t.Fatal(err)
	}
	if err := WritePersist(path, cfg); err != nil {
		t.Fatal(err)
	}
	resolved := ResolvePiAgents(path, filepath.Join(t.TempDir(), "missing.toml"))
	if got := resolved.Agents["scout"]; got.Model != "openai-codex/gpt-5.4-mini" || got.Thinking != "off" {
		t.Fatalf("Pi agent override lost after config write: %+v", resolved)
	}
}

func TestLoadRecognizesPiAgentTablesWithoutStructuralWarnings(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".atomic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(`[agents]
atomic-reviewer = "sonnet"

[pi.agent.scout]
model = "openai-codex/gpt-5.4-mini"
thinking = "off"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, warn := range warns {
		if strings.Contains(warn.Message, "pi") {
			t.Fatalf("unexpected pi warning: %+v", warns)
		}
	}
}

func TestLoadRepoConfigRecognizesPiAgentTablesAndPreservesCodeIgnore(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "atomic.toml")
	if err := os.WriteFile(repoPath, []byte(`[code]
ignore = ["vendor/**"]

[pi.agent.scout]
model = "openai-codex/gpt-5.4-mini"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warns, err := LoadRepoConfig(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Code.Ignore) != 1 || cfg.Code.Ignore[0] != "vendor/**" {
		t.Fatalf("code ignore not preserved: %+v", cfg.Code.Ignore)
	}
	for _, warn := range warns {
		if strings.Contains(warn.Message, "pi") {
			t.Fatalf("unexpected pi warning: %+v", warns)
		}
	}
}
