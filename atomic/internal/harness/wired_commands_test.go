package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

var wiredCommands = []string{"commit", "review-branch", "git-cleanup", "watch-ci", "report-issue", "autopilot", "subagent-diagnose"}

func expandedCommand(t *testing.T, cat *artifacts.Catalog, name string) string {
	t.Helper()
	a, ok := cat.Get(string(artifacts.KindCommand) + ":commands/" + name + ".md")
	if !ok {
		t.Fatalf("command %s missing from the corpus", name)
	}
	return string(a.Body)
}

func expandPartial(t *testing.T, src string) string {
	t.Helper()
	pool, err := templaterender.LoadPartials(filepath.Join(repoRoot, "context", templaterender.PartialsDir))
	if err != nil {
		t.Fatalf("LoadPartials: %v", err)
	}
	out, err := templaterender.Expand(pool, "probe.md", []byte(src))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	return string(out)
}

// Provider commands come from the atomic-git-discipline references, so a user
// on one provider never gets another provider's CLI. Bodies are checked as expanded.
func TestWiredCommands_NameNoProviderCLI(t *testing.T) {
	cat := loadSkillCorpus(t)
	providerCLI := regexp.MustCompile(`\b(gh|glab|tea|acli)\s+[a-z]`)

	for _, name := range wiredCommands {
		body := expandedCommand(t, cat, name)
		if m := providerCLI.FindString(body); m != "" {
			t.Errorf("/%s names the provider command %q", name, m)
		}
		if !strings.Contains(body, "atomic workflow resolve") {
			t.Errorf("/%s does not tell the model how to get the resolved block", name)
		}
	}
}

func TestWiredCommands_PrintTheResolvedProviderLine(t *testing.T) {
	cat := loadSkillCorpus(t)
	for name, key := range map[string]string{
		"commit":            "source-control",
		"watch-ci":          "source-control",
		"subagent-diagnose": "source-control",
		"report-issue":      "issue-tracker",
		"autopilot":         "issue-tracker",
	} {
		line := expandPartial(t, `{{ template "resolved-provider" "`+key+`" }}`)
		if !strings.Contains(line, "`"+key+": <value> (<level>)`") {
			t.Fatalf("resolved-provider for %s rendered %q", key, line)
		}
		if !strings.Contains(expandedCommand(t, cat, name), line) {
			t.Errorf("/%s does not compose the resolved-provider line for %s", name, key)
		}
	}
}

// The privacy partial is shared with /report-issue-with-atomic, which always
// targets GitHub, so it must not route through the resolved tracker.
func TestWiredCommands_PrivacyNamesNoOperation(t *testing.T) {
	body := expandPartial(t, `{{ template "report-issue-privacy" . }}`)
	for _, op := range []string{
		"default-branch", "change-request-", "push-permission-check", "ci-target-resolve", "auth-check",
		"install-hint", "closing-keywords", "issue-create", "issue-view", "issue-search", "issue-close",
		"label-list", "label-apply", "template-location", "commit-reference-syntax", "project-identifier",
	} {
		if strings.Contains(body, op) {
			t.Errorf("report-issue-privacy names the operation %q", op)
		}
	}
}

// The follow-up sink composes the privacy partial mid-section in many commands,
// so a heading of its own would break their outline. Call sites add one.
func TestWiredCommands_PrivacyCarriesNoHeading(t *testing.T) {
	body := expandPartial(t, `{{ template "report-issue-privacy" . }}`)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") {
			t.Errorf("report-issue-privacy carries the heading %q", line)
		}
	}
}

// Codex receives workflow preferences only through this paragraph until its
// hook is trusted, so the steering source must carry the spec's text exactly.
func TestSteering_CarriesTheWorkflowPreferencesParagraph(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join(repoRoot, "docs", "spec", "workflow-preferences.md"))
	if err != nil {
		t.Fatal(err)
	}
	fence := regexp.MustCompile("(?s)\n    ```text\n(    ## Workflow preferences\n.*?)    ```\n").FindSubmatch(spec)
	if fence == nil {
		t.Fatal("spec carries no fenced steering paragraph")
	}
	lines := strings.SplitAfter(string(fence[1]), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "    ")
	}
	want := strings.Join(lines, "")

	steering, err := os.ReadFile(filepath.Join(repoRoot, "context", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(steering)
	start := strings.Index(src, "## Workflow preferences\n")
	if start < 0 {
		t.Fatal("context/AGENTS.md lacks ## Workflow preferences")
	}
	if !strings.HasPrefix(src[start:], want) || !strings.HasPrefix(src[start+len(want):], "\n## ") {
		t.Errorf("steering section differs from the spec; want exactly:\n%s", want)
	}
	if strings.LastIndex(src[:start], "\n## ") != strings.Index(src, "\n## User profile") {
		t.Error("## Workflow preferences does not follow ## User profile")
	}
}

// The steering file loads every turn, so the worktrees row points to the
// reference instead of carrying the per-harness table itself.
func TestSteering_WorktreeRowPointsToTheReference(t *testing.T) {
	steering, err := os.ReadFile(filepath.Join(repoRoot, "context", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `<worktrees>/<branch>/` \\|.*$").FindString(string(steering))
	if row == "" {
		t.Fatal("context/AGENTS.md has no <worktrees>/<branch>/ row")
	}
	for _, want := range []string{"atomic where --json", "references/worktrees.md"} {
		if !strings.Contains(row, want) {
			t.Errorf("worktrees row lacks %q", want)
		}
	}
	if strings.Contains(row, "state-root") {
		t.Error("worktrees row restates the placement table")
	}
}

func TestAtomicHelp_NamesNoProviderCLI(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "atomic-help")
	if m := regexp.MustCompile(`\b(gh|glab|tea|acli)\s+[a-z]`).FindString(body); m != "" {
		t.Errorf("/atomic-help names the provider command %q", m)
	}
}

// A linked worktree can sit anywhere the harness or preference puts it, and
// git before 2.31 lacks --path-format, so /atomic-help resolves both dirs with pwd -P.
func TestAtomicHelp_DerivesInWorktreeFromTheGitDirs(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "atomic-help")
	for _, want := range []string{`cd "$(git rev-parse --git-dir)" 2>/dev/null && pwd -P`, `cd "$(git rev-parse --git-common-dir)" 2>/dev/null && pwd -P`} {
		if !strings.Contains(body, want) {
			t.Errorf("/atomic-help lacks %q", want)
		}
	}
	for _, banned := range []string{"--path-format", "path includes"} {
		if strings.Contains(body, banned) {
			t.Errorf("/atomic-help uses %q", banned)
		}
	}
}

// Shell variables do not survive between tool calls, so the count takes the
// base the model resolved rather than $BASE.
func TestAtomicHelp_TakesTheBaseFromBaseResolution(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "atomic-help")
	if !strings.Contains(body, expandPartial(t, `{{ template "base-resolution" . }}`)) {
		t.Error("/atomic-help does not compose base-resolution")
	}
	if !strings.Contains(body, "git rev-list --count <base>..HEAD") || strings.Contains(body, `"$BASE"..HEAD`) {
		t.Error("/atomic-help does not count commits against the resolved <base>")
	}
}

// The worktree location differs by harness and preference, so no flow may
// assume the Claude Code default.
func TestWorktreeFlows_UseTheResolvedLocation(t *testing.T) {
	setup := expandPartial(t, `{{ template "worktree-setup" . }}`)
	for _, want := range []string{
		"atomic where --json", "git worktree add <dir>/<branch> -b <branch>", "worktrees: off", "worktree-entry capability",
		"worktree location unresolved. working in place.", "git check-ignore -q <dir>/probe", "add to .gitignore: /<dir>/",
	} {
		if !strings.Contains(setup, want) {
			t.Errorf("worktree-setup lacks %q", want)
		}
	}
	cleanup := expandPartial(t, `{{ template "worktree-cleanup-prompt" . }}`)
	for _, want := range []string{"git worktree list", "references/worktrees.md"} {
		if !strings.Contains(cleanup, want) {
			t.Errorf("worktree-cleanup-prompt lacks %q", want)
		}
	}
	for name, body := range map[string]string{"worktree-setup": setup, "worktree-cleanup-prompt": cleanup} {
		if strings.Contains(body, ".claude/worktrees") {
			t.Errorf("%s hard-codes .claude/worktrees", name)
		}
	}
}

// An explicit worktrees path is outside what atomic repo init ignores, so the
// user needs the literal line or the linked worktree shows as untracked.
func TestSetupWiki_ReportsTheIgnoreLineForAnExplicitPath(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "setup-wiki")
	for _, want := range []string{"atomic where --json", "`/<worktrees>/`", "atomic repo init", "references/worktrees.md", "audit row was not n/a"} {
		if !strings.Contains(body, want) {
			t.Errorf("/setup-wiki lacks %q", want)
		}
	}
	if strings.Contains(body, "check-ignore -q .claude/worktrees/") {
		t.Error("/setup-wiki probes the Claude default instead of the resolved location")
	}
}

// With no hosted provider, merges still work locally and the remote-only verbs
// refuse instead of failing halfway through a push or change request.
func TestWiredCommands_CommitHandlesSourceControlNone(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "commit")
	for _, want := range []string{"source-control: none", "local-merge path", "atomic workflow set source-control"} {
		if !strings.Contains(body, want) {
			t.Errorf("/commit lacks %q", want)
		}
	}
}

// The local ledger has no sign-in or project, so readiness checks would block
// every filing; its entries need a severity or kind instead of labels.
func TestWiredCommands_ReportIssueFilesToTheLedger(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "report-issue")
	for _, want := range []string{"issue-tracker: filesystem", "--severity risk", "--severity question", "--kind plan"} {
		if !strings.Contains(body, want) {
			t.Errorf("/report-issue lacks %q", want)
		}
	}
}

// The "`Agent` column" table header in /atomic-plan trips the wire-token
// pattern, so the command wire check below strips it.
func TestAtomicPlanAgentColumnIsKnownWireFalsePositive(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "atomic-plan")
	if !strings.Contains(body, "The `Agent` column") {
		t.Fatal("/atomic-plan no longer carries the `Agent` column text; drop this test")
	}
	wire := false
	for _, use := range HarnessRuntimeUses([]byte("The `Agent` column")) {
		wire = wire || use.Wire
	}
	if !wire {
		t.Error("the `Agent` column text no longer trips the wire-token pattern; drop this test")
	}
}

// /watch-ci names its background dispatch in neutral words the steering
// vocabulary maps; a Claude-only token would be dead text on other harnesses.
func TestWatchCI_DispatchesNamedAgentInBackground(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "watch-ci")
	if _, _, err := frontmatter.Parse(body); err != nil {
		t.Fatalf("/watch-ci frontmatter: %v", err)
	}
	if !strings.Contains(body, "Dispatch the harness's default general agent, by name, in the background") {
		t.Error("/watch-ci does not dispatch a named agent in the background")
	}
}

// The Claude steering vocabulary maps neutral words to native tools; a Claude
// tool or model spelled in the canonical body is dead text on other harnesses.
func TestCanonicalCommandsAndAgents_CarryNoClaudeWireToken(t *testing.T) {
	cat := loadSkillCorpus(t)
	for _, kind := range []artifacts.Kind{artifacts.KindCommand, artifacts.KindAgent} {
		for _, a := range cat.OfKind(kind) {
			body := string(a.Body)
			if a.Source == "commands/atomic-plan.md" {
				body = strings.ReplaceAll(body, "The `Agent` column", "")
			}
			for _, use := range HarnessRuntimeUses([]byte(body)) {
				if use.Wire {
					t.Errorf("%s carries the Claude-only token %q", a.Source, use.Name)
				}
			}
			if strings.Contains(body, `"general-purpose"`) || strings.Contains(body, "`general-purpose`") {
				t.Errorf("%s names Claude's general-purpose agent; say the harness's default general agent", a.Source)
			}
		}
	}
}

// An unnamed dispatch lands on a general agent without the Atomic agent's skills.
// Pins known shapes; mermaid labels and lines naming an `atomic-` agent are exempt.
func TestCanonicalDispatches_NameTheAtomicAgent(t *testing.T) {
	unnamed := regexp.MustCompile(`(?i)\bdispatch (?:a|an|the|one)? ?(?:fresh[- ]context |fresh |isolated )?(?:reviewer|implementer|investigator|auditor|strategist|inferrer|writer|deslopper|sub-?agent)\b|\bdispatch sub-?agents only\b|^dispatch:\s*$`)
	unnamedGeneral := regexp.MustCompile(`(?i)\b(?:generic|isolated(?: read-only)?) sub-?agents?\b`)
	cat := loadSkillCorpus(t)
	for _, kind := range []artifacts.Kind{artifacts.KindCommand, artifacts.KindAgent, artifacts.KindSkill} {
		for _, a := range cat.OfKind(kind) {
			for _, line := range strings.Split(string(a.Body), "\n") {
				if m := unnamedGeneral.FindString(line); m != "" {
					t.Errorf("%s names %q; say the harness's default general agent, by name", a.Source, m)
				}
				if strings.Contains(line, "`atomic-") || strings.Contains(line, `["`) {
					continue
				}
				if m := unnamed.FindString(line); m != "" {
					t.Errorf("%s dispatches without naming the agent: %q", a.Source, m)
				}
			}
		}
	}

	for id, named := range map[string]string{
		"skill:skills/atomic-wiki/references/repo.md": "dispatch `atomic-reviewer`",
		"command:commands/subagent-diagnose.md":       "Dispatch `atomic-investigator`",
		"command:commands/retrospective-learning.md":  "Dispatch `atomic-investigator`",
	} {
		a, ok := cat.Get(id)
		if !ok {
			t.Fatalf("%s missing from the corpus", id)
		}
		if !strings.Contains(string(a.Body), named) {
			t.Errorf("%s does not carry the named dispatch %q", id, named)
		}
	}
}
