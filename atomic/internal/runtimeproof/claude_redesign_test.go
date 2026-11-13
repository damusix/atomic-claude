package runtimeproof

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeRedesign is one isolated home and git repository that the built atomic
// binary installed into, launched with the real Claude Code.
type claudeRedesign struct {
	atomic, claude, version string
	root, home, configDir   string
	repo                    string
}

// newClaudeRedesign skips the row loudly when it cannot launch both binaries.
func newClaudeRedesign(t *testing.T, ev ScenarioEvidence, repoFiles map[string]string) *claudeRedesign {
	t.Helper()
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real Claude launch is skipped")
	}
	claudeBin, err := ClaudeCLI()
	if err != nil {
		recordScenarioSkip(t, ev, "claude unavailable: "+err.Error())
	}
	atomicBin, err := atomicBinary(t)
	if err != nil {
		recordScenarioSkip(t, ev, "atomic binary could not be built: "+err.Error())
	}
	root := isolatedHome(t)
	c := &claudeRedesign{
		atomic: atomicBin, claude: claudeBin, root: root,
		home: filepath.Join(root, "home"), configDir: filepath.Join(root, "home", ".claude"),
		repo: filepath.Join(root, "repo"),
	}
	writeTree(t, c.home, map[string]string{".atomic/profile.md": "# Runtimeproof profile\n"})
	writeTree(t, c.repo, repoFiles)
	guidanceGit(t, c.repo, "init", "-q", "-b", "main")

	version := exec.Command(claudeBin, "--version")
	version.Env = launchEnv(c.scenario("claude-version", c.repo, nil))
	out, err := version.Output()
	if err != nil {
		t.Fatalf("claude --version: %v", err)
	}
	c.version = strings.TrimSpace(string(out))
	return c
}

// run executes the built atomic binary against the isolated home only.
func (c *claudeRedesign) run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(c.atomic, args...)
	cmd.Dir = dir
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "HOME" || key == "CODEX_HOME" || key == "OMP_PROFILE" || key == "CLAUDECODE" ||
			strings.HasPrefix(key, "CLAUDE_") || strings.HasPrefix(key, "ATOMIC_") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env, "HOME="+c.home, "CLAUDE_CONFIG_DIR="+c.configDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *claudeRedesign) mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := c.run(t, dir, args...)
	if err != nil {
		t.Fatalf("atomic %v: %v\n%s", args, err, out)
	}
	return out
}

// scenario puts the atomic binary on PATH, because the installed SessionStart
// hook invokes it by name.
func (c *claudeRedesign) scenario(name, dir string, args []string) Scenario {
	return Scenario{
		Name: name, Harness: c.claude, Home: c.home, ConfigDir: c.configDir,
		WorkDir: dir, EventLog: filepath.Join(c.root, name+".events.jsonl"),
		Timeout: 60 * time.Second, Args: args,
		Env: []string{"PATH=" + filepath.Dir(c.atomic) + string(os.PathListSeparator) + os.Getenv("PATH")},
	}
}

// launch runs one print-mode session whose stream carries the init event and
// every hook response.
func (c *claudeRedesign) launch(t *testing.T, name, dir, prompt string) Observation {
	t.Helper()
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-hook-events", prompt}
	obs, err := Run(context.Background(), c.scenario(name, dir, args))
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("claude exceeded its bound: %+v", obs)
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return obs
}

// retain writes one raw output beside the evidence records and returns its
// SHA-256, so the record names the exact bytes it rests on.
func retain(t *testing.T, name, data string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(data))
	digest := hex.EncodeToString(sum[:])
	if root := scenarioRecorder.root; root != "" {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Errorf("create evidence root: %v", err)
		} else if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Errorf("retain %s: %v", name, err)
		}
	}
	return digest
}

func streamMessages(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var messages []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stream line is not JSON: %v: %q", err, line)
		}
		messages = append(messages, m)
	}
	return messages
}

func streamInit(t *testing.T, stdout string) map[string]any {
	t.Helper()
	for _, m := range streamMessages(t, stdout) {
		if m["type"] == "system" && m["subtype"] == "init" {
			return m
		}
	}
	t.Fatalf("the stream carried no init event:\n%s", stdout)
	return nil
}

// sessionStartContexts returns the additionalContext each SessionStart hook
// handed back to Claude, as the stream reports it.
func sessionStartContexts(t *testing.T, stdout string) []string {
	t.Helper()
	var contexts []string
	for _, m := range streamMessages(t, stdout) {
		output, _ := m["output"].(string)
		if m["subtype"] != "hook_response" || m["hook_event"] != "SessionStart" || output == "" {
			continue
		}
		contexts = append(contexts, hookAdditionalContext(t, output))
	}
	return contexts
}

func hookAdditionalContext(t *testing.T, output string) string {
	t.Helper()
	var payload struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("hook output is not JSON: %v: %q", err, output)
	}
	return payload.HookSpecificOutput.AdditionalContext
}

func listed(values any, want string) bool {
	items, _ := values.([]any)
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// Claude reads a migrated repository's cards through .claude/rules/atomic from
// the root, and from a subdirectory only after an approval print mode cannot give.
func TestClaudeLoadsMigratedWikiCardsThroughTheLinkFromTheRootOnly(t *testing.T) {
	migrated := ScenarioEvidence{
		Scenario:  "claude/migration",
		Group:     "claude",
		Engine:    "atomic state migrate (built binary)",
		Criterion: "legacy .claude state moves to .atomic, where resolves .atomic, and the card link is written",
		Command:   "atomic install --harness claude --yes; atomic state migrate; atomic where --json",
	}
	card := "---\npaths:\n  - \"src/**\"\n---\n\nRUNTIMEPROOF_WIKI_CARD source domain.\n"
	c := newClaudeRedesign(t, migrated, map[string]string{
		"src/a.ts":                        "export const a = 1;\n",
		".claude/.scratchpad/s1/BRIEF.md": "# brief\n",
		".claude/project/followups/f1.md": "# followup\n",
		".claude/atomic.toml":             "[comments]\nmax_lines = 2\n",
		".claude/rules/wiki/src.md":       card,
		// A regular path-scoped rule shows a subdirectory session still loads
		// repository rules, so a missing card is the link and not discovery.
		".claude/rules/control.md": "---\npaths:\n  - \"src/**\"\n---\n\nRUNTIMEPROOF_CONTROL_RULE\n",
	})
	c.mustRun(t, c.repo, "install", "--harness", "claude", "--yes")
	before := c.mustRun(t, c.repo, "where", "--json")
	migrateOut := c.mustRun(t, c.repo, "state", "migrate")
	after := c.mustRun(t, c.repo, "where", "--json")

	var where struct {
		StateRoot string `json:"state_root"`
	}
	if err := json.Unmarshal([]byte(after), &where); err != nil {
		t.Fatalf("where --json: %v\n%s", err, after)
	}
	if want := filepath.Join(c.repo, ".atomic"); where.StateRoot != want {
		t.Errorf("state_root after migrate = %q, want %q", where.StateRoot, want)
	}
	for _, moved := range []string{".scratchpad/s1/BRIEF.md", "project/followups/f1.md", "atomic.toml", "rules/wiki/src.md"} {
		if !fileExists(filepath.Join(c.repo, ".atomic", moved)) {
			t.Errorf("migrate did not move %s into .atomic", moved)
		}
	}
	link := filepath.Join(c.repo, ".claude", "rules", "atomic")
	if target, err := os.Readlink(link); err != nil || target != "../../.atomic/rules" {
		t.Errorf(".claude/rules/atomic = %q (%v), want a link to ../../.atomic/rules", target, err)
	}
	migrated.Paths = []string{c.home, c.repo}
	migrated.Outcome = "where resolved .atomic; scratchpad, followups, atomic.toml, and the wiki card moved; link ../../.atomic/rules written"
	migrated.Detail = "level files; sha256 " + retain(t, "migration.txt", before+"\n--- state migrate\n"+migrateOut+"\n--- where after\n"+after)
	recordScenario(t, migrated)

	canonical := filepath.Join(c.repo, ".atomic", "rules", "wiki", "src.md")
	control := filepath.Join(c.repo, ".claude", "rules", "control.md")

	fromRoot := ScenarioEvidence{
		Scenario:  "claude/wiki-card-root",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "a session at the repository root loads a path-scoped wiki card through .claude/rules/atomic",
		Command:   "claude -p --output-format stream-json --verbose --include-hook-events 'Summarize @src/a.ts' (cwd repo)",
		Paths:     []string{c.home, c.repo},
	}
	root := c.launch(t, "wiki-card-root", c.repo, "Summarize @src/a.ts")
	if reason := claudeLoadReason(root, canonical); reason != "path_glob_match" {
		t.Errorf("root session loaded the card for %q, want path_glob_match; loads: %v", reason, sortedCounts(claudeGuidanceCounts(root)))
	}
	fromRoot.Outcome = c.version + ": card loaded with load_reason path_glob_match, reported at its resolved .atomic path"
	fromRoot.Detail = "level startup; sha256 " + retain(t, "wiki-card-root.stream.jsonl", root.Stdout) +
		"; events sha256 " + retain(t, "wiki-card-root.events.jsonl", rawEvents(root))
	recordScenario(t, fromRoot)

	fromSub := ScenarioEvidence{
		Scenario:  "claude/wiki-card-subdirectory",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "a session started in a subdirectory does not load the card through the link, while a regular rule still loads",
		Command:   "claude -p --output-format stream-json --verbose --include-hook-events 'Summarize @a.ts' (cwd repo/src)",
		Paths:     []string{c.home, c.repo},
	}
	sub := c.launch(t, "wiki-card-subdirectory", filepath.Join(c.repo, "src"), "Summarize @a.ts")
	if reason := claudeLoadReason(sub, control); reason != "path_glob_match" {
		t.Errorf("subdirectory session loaded the control rule for %q, want path_glob_match", reason)
	}
	if reason := claudeLoadReason(sub, canonical); reason != "" {
		t.Errorf("subdirectory session loaded the card (%q); the subdirectory limit no longer holds, so doctor category 25 and the spec overstate it", reason)
	}
	fromSub.Outcome = c.version + ": control rule loaded, card not loaded, no approval surfaced in print mode"
	fromSub.Detail = "level startup; sha256 " + retain(t, "wiki-card-subdirectory.stream.jsonl", sub.Stdout) +
		"; events sha256 " + retain(t, "wiki-card-subdirectory.events.jsonl", rawEvents(sub))
	recordScenario(t, fromSub)
}

// The installed SessionStart hook must hand Claude exactly what the verb
// emits, workflow-preferences block included.
func TestClaudeSessionStartHookDeliversTheResolvedWorkflowBlock(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "claude/session-context",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "the SessionStart hook ran and its additionalContext equals `atomic hooks session-start` output, carrying the resolved workflow-preferences block",
		Command:   "claude -p --output-format stream-json --verbose --include-hook-events 'Reply OK.'; atomic hooks session-start --harness claude --cwd <repo>",
	}
	c := newClaudeRedesign(t, ev, map[string]string{
		".atomic/workflow-preferences.md": "<workflow-preferences level='repo'>\n- issue-tracker: linear\n- issue-tracker-project: RUNTIMEPROOF\n</workflow-preferences>\n",
	})
	ev.Paths = []string{c.home, c.repo}
	c.mustRun(t, c.repo, "install", "--harness", "claude", "--yes")

	obs := c.launch(t, "session-context", c.repo, DefaultPrompt)
	started := false
	for _, e := range obs.Events {
		started = started || e.Name == "SessionStart"
	}
	if !started {
		t.Errorf("no SessionStart event reached the observer")
	}
	direct := c.mustRun(t, c.repo, "hooks", "session-start", "--harness", "claude", "--cwd", c.repo)
	want := hookAdditionalContext(t, direct)
	if !strings.Contains(want, "<workflow-preferences level='resolved'>") || !strings.Contains(want, "- issue-tracker: linear") {
		t.Fatalf("the verb's context lacks the resolved repo preferences:\n%s", want)
	}
	delivered := false
	for _, got := range sessionStartContexts(t, obs.Stdout) {
		delivered = delivered || got == want
	}
	if !delivered {
		t.Errorf("no SessionStart hook response carried the verb's context %q:\n%s", want, obs.Stdout)
	}

	ev.Outcome = c.version + ": hook ran; its additionalContext equals the verb output and carries <workflow-preferences level='resolved'> with issue-tracker: linear"
	ev.Detail = "level startup (no credential, so no model turn); stream sha256 " + retain(t, "session-context.stream.jsonl", obs.Stdout) +
		"; verb sha256 " + retain(t, "session-context.verb.json", direct)
	recordScenario(t, ev)
}

// A stored per-agent override reaches the projected agent file, and Claude
// lists the agent; an agent without one carries no model pin.
func TestClaudeAgentOverrideIsProjectedAndTheAgentListed(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "claude/agent-override",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "the override lands in agents/atomic-investigator.md and the init event lists atomic-investigator",
		Command:   "[claude.agents.atomic-investigator] model=haiku effort=low in ~/.atomic/config.toml; atomic config list; atomic install --harness claude --yes; claude -p --output-format stream-json --verbose",
	}
	c := newClaudeRedesign(t, ev, map[string]string{"README.md": "# probe\n"})
	ev.Paths = []string{c.home, c.repo}
	// `atomic config agents` is interactive only, and `config set` refuses agent
	// keys, so the row stores the table the form would write.
	writeTree(t, c.home, map[string]string{
		".atomic/config.toml": "[claude.agents.atomic-investigator]\nmodel = \"haiku\"\neffort = \"low\"\n",
	})
	listedConfig := c.mustRun(t, c.repo, "config", "list")
	c.mustRun(t, c.repo, "install", "--harness", "claude", "--yes")

	investigator := readFileString(t, filepath.Join(c.configDir, "agents", "atomic-investigator.md"))
	front, _, _ := strings.Cut(strings.TrimPrefix(investigator, "---\n"), "\n---")
	if !strings.Contains(front, "\nmodel: haiku") && !strings.HasPrefix(front, "model: haiku") {
		t.Errorf("atomic-investigator frontmatter carries no model: haiku:\n%s", front)
	}
	if !strings.Contains(front, "effort: low") {
		t.Errorf("atomic-investigator frontmatter carries no effort: low:\n%s", front)
	}
	reviewer := readFileString(t, filepath.Join(c.configDir, "agents", "atomic-reviewer.md"))
	reviewerFront, _, _ := strings.Cut(strings.TrimPrefix(reviewer, "---\n"), "\n---")
	if strings.Contains(reviewerFront, "model:") {
		t.Errorf("an agent with no override carries a model pin:\n%s", reviewerFront)
	}

	obs := c.launch(t, "agent-override", c.repo, DefaultPrompt)
	init := streamInit(t, obs.Stdout)
	if !listed(init["agents"], "atomic-investigator") {
		t.Errorf("init event agents = %v, want atomic-investigator", init["agents"])
	}

	ev.Outcome = c.version + ": frontmatter carries model: haiku and effort: low; atomic-reviewer carries no model; init lists atomic-investigator"
	if !strings.Contains(listedConfig, "claude.agents.atomic-investigator.model=haiku") {
		t.Errorf("config list does not show the stored override:\n%s", listedConfig)
	}
	ev.Detail = "levels files + startup; a model turn naming the override needs a credential and was not reached; agent file sha256 " +
		retain(t, "agent-override.atomic-investigator.md", investigator) + "; stream sha256 " + retain(t, "agent-override.stream.jsonl", obs.Stdout)
	recordScenario(t, ev)
}

// A user CLAUDE.md that does not import AGENTS.md hides AGENTS.md from Claude,
// so the wiki pipeline's wiring must leave the index ref where Claude loads it.
func TestClaudeLoadsTheWikiIndexRefWiredBesideAUserClaudeMd(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "claude/wiki-index-ref",
		Group:     "claude",
		Engine:    "runtimeproof (real Claude)",
		Criterion: "after the /setup-wiki wiring step, a root session loads docs/wiki/index.md through the user's CLAUDE.md",
		Command:   "bash <setup-wiki ref-wiring block>; claude -p --output-format stream-json --verbose 'Reply OK.' (cwd repo)",
	}
	c := newClaudeRedesign(t, ev, map[string]string{
		"CLAUDE.md":          "# project\n",
		"AGENTS.md":          "# shared\n",
		"docs/wiki/index.md": "# RUNTIMEPROOF_WIKI_INDEX\n",
	})
	ev.Paths = []string{c.home, c.repo}
	c.mustRun(t, c.repo, "install", "--harness", "claude", "--yes")
	index := filepath.Join(c.repo, "docs", "wiki", "index.md")

	unwired := c.launch(t, "wiki-index-unwired", c.repo, DefaultPrompt)
	if got := claudeGuidanceCounts(unwired)[index]; got != 0 {
		t.Fatalf("index loaded %d times before wiring; the fixture does not reproduce the gap", got)
	}

	raw := readFileString(t, filepath.Join(moduleRoot(), "..", "context", "commands", "setup-wiki.md"))
	_, block, ok := strings.Cut(raw, "**Project steering file missing `@-ref`**")
	if ok {
		_, block, ok = strings.Cut(block, "```bash\n")
	}
	if ok {
		block, _, ok = strings.Cut(block, "\n```\n")
	}
	if !ok {
		t.Fatal("setup-wiki.md lost its ref-wiring bash block")
	}
	wire := exec.Command("bash", "-c", block)
	wire.Dir = c.repo
	if out, err := wire.CombinedOutput(); err != nil {
		t.Fatalf("wiring block: %v\n%s", err, out)
	}

	wired := c.launch(t, "wiki-index-wired", c.repo, DefaultPrompt)
	if got := claudeGuidanceCounts(wired)[index]; got != 1 {
		t.Errorf("index loaded %d times after wiring, want 1; loads: %v", got, sortedCounts(claudeGuidanceCounts(wired)))
	}
	loader := readFileString(t, filepath.Join(c.repo, "CLAUDE.md"))
	shared := readFileString(t, filepath.Join(c.repo, "AGENTS.md"))

	ev.Outcome = c.version + ": index not loaded before wiring; after it CLAUDE.md imports @AGENTS.md, which carries @docs/wiki/index.md, and the index loads once"
	ev.Detail = "levels files + startup; unwired sha256 " + retain(t, "wiki-index-unwired.events.jsonl", rawEvents(unwired)) +
		"; wired sha256 " + retain(t, "wiki-index-wired.events.jsonl", rawEvents(wired)) +
		"; steering sha256 " + retain(t, "wiki-index-steering.md", "--- CLAUDE.md\n"+loader+"--- AGENTS.md\n"+shared)
	recordScenario(t, ev)
}

func rawEvents(obs Observation) string {
	var b strings.Builder
	for _, e := range obs.Events {
		b.WriteString(e.Raw)
		b.WriteByte('\n')
	}
	return b.String()
}
