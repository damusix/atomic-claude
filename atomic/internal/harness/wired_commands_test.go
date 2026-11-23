package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

var wiredCommands = []string{"review-branch", "report-issue"}

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
		"report-issue": "issue-tracker",
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

func TestAtomicHelp_RoutesFromIntentAndEvidence(t *testing.T) {
	body := expandedCommand(t, loadSkillCorpus(t), "atomic-help")
	for _, want := range []string{"user's intent takes precedence", "failed probe is unknown", "one action and one reason"} {
		if !strings.Contains(body, want) {
			t.Errorf("/atomic-help lacks %q", want)
		}
	}
}

// The worktree location differs by harness and preference, so no flow may
// assume the Claude Code default.
func TestWorktreeFlows_UseTheResolvedLocation(t *testing.T) {
	path := filepath.Join(repoRoot, "context", "skills", "atomic-git-discipline", "references", "worktrees.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"atomic where --json", "atomic workflow resolve", "git worktree add <dir>/<branch> -b <branch>", "worktrees: off", "worktree-entry capability", "git worktree list"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("worktrees reference lacks %q", want)
		}
	}
	setup := expandedCommand(t, loadSkillCorpus(t), "setup-wiki")
	for _, want := range []string{"atomic where --json", "atomic repo init"} {
		if !strings.Contains(setup, want) {
			t.Errorf("/setup-wiki lacks %q", want)
		}
	}
}

func TestShipping_RequiresAuthorization(t *testing.T) {
	path := filepath.Join(repoRoot, "context", "skills", "atomic-git-discipline", "references", "shipping.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"does not authorize a push", "without explicit authorization", "Preserve unrelated work"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("shipping reference lacks %q", want)
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

// The Claude steering vocabulary maps neutral words to native tools; a Claude
// tool or model spelled in the canonical body is dead text on other harnesses.
func TestCanonicalCommandsAndAgents_CarryNoClaudeWireToken(t *testing.T) {
	cat := loadSkillCorpus(t)
	for _, kind := range []artifacts.Kind{artifacts.KindCommand, artifacts.KindAgent} {
		for _, a := range cat.OfKind(kind) {
			body := string(a.Body)
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

	const source = "skill:skills/atomic-wiki/references/repo.md"
	a, ok := cat.Get(source)
	if !ok || !strings.Contains(string(a.Body), "Send `atomic-reviewer`") {
		t.Errorf("%s must name the wiki reviewer", source)
	}
}
