package codex

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/pelletier/go-toml/v2"
)

func findFile(p Plugin, path string) (PluginFile, bool) {
	for _, f := range p.Files {
		if f.Path == path {
			return f, true
		}
	}
	return PluginFile{}, false
}

func TestBuildPluginIsDeterministicAndCapabilityDisabled(t *testing.T) {
	cat := fixtureCorpus(t)
	first, err := BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if first.Generation != second.Generation {
		t.Errorf("generation %s != %s", first.Generation, second.Generation)
	}
	if strings.Join(first.Paths(), ",") != strings.Join(second.Paths(), ",") {
		t.Errorf("plugin paths differ between builds")
	}
	for i := range first.Files {
		if string(first.Files[i].Bytes) != string(second.Files[i].Bytes) || first.Files[i].Digest != second.Files[i].Digest {
			t.Errorf("file %s is not deterministic", first.Files[i].Path)
		}
	}

	// No hook configuration is emitted anywhere: no hooks.json, no hooks key in
	// the plugin manifest, and no registered event.
	for _, f := range first.Files {
		if strings.Contains(f.Path, "hooks") {
			t.Errorf("plugin carries hook configuration at %s", f.Path)
		}
		if strings.Contains(string(f.Bytes), `"hooks"`) {
			t.Errorf("plugin file %s carries a hooks key", f.Path)
		}
	}
	if first.Hooks.ConfigFile != "" {
		t.Errorf("hook config file = %q, want none while no hook role is proven", first.Hooks.ConfigFile)
	}
	if len(first.Hooks.Registered) != 0 {
		t.Errorf("registered events = %+v, want none", first.Hooks.Registered)
	}
	if len(first.Hooks.Withheld) == 0 {
		t.Error("withheld hook roles are empty; the unproven surface must be reported, not silently absent")
	}
	if first.Steering.Surface == "" {
		t.Error("plugin does not report the steering surface it does not emit")
	}
	if len(first.Tools) != 0 {
		t.Errorf("proven tool inputs = %+v, want none for Codex 0.147.0", first.Tools)
	}

	for _, f := range first.Files {
		if f.Tier != artifacts.EnforcementUnsupported {
			t.Errorf("file %s claims tier %q; the CP0 record proves instruction-only delivery", f.Path, f.Tier)
		}
	}
	if len(first.Gaps) == 0 || len(first.Unproven) == 0 {
		t.Error("plugin reports no capability gap; no hook row is proven for Codex 0.147.0")
	}

	// The marketplace tree carries the CP0-observed shapes and the corpus.
	for _, path := range []string{
		MarketplaceManifestPath,
		PluginManifestPath,
		RuleIndexPath,
		MatcherPath,
		RulesDir + "/typescript/style.md",
		AgentsDir + "/atomic-example.toml",
		SkillsDir + "/atomic-example/SKILL.md",
	} {
		if _, ok := findFile(first, path); !ok {
			t.Errorf("plugin is missing %s", path)
		}
	}

	// The compiled index claims no delivery and carries the authored glob.
	index, ok := findFile(first, RuleIndexPath)
	if !ok {
		t.Fatal("rule index missing")
	}
	var doc struct {
		Target   string `json:"target"`
		Ordering string `json:"ordering"`
		Bound    int    `json:"bound"`
		Rules    []struct {
			ID       string `json:"id"`
			Delivery string `json:"delivery"`
			Tier     string `json:"tier"`
			Patterns []struct {
				Glob  string `json:"glob"`
				Regex string `json:"regex"`
			} `json:"patterns"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(index.Bytes, &doc); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if doc.Target != string(artifacts.TargetCodex) || doc.Ordering != harness.RuleOrderingRecordID || doc.Bound != InstructionByteBound {
		t.Errorf("index header = %+v", doc)
	}
	if len(doc.Rules) != 1 {
		t.Fatalf("index rules = %+v, want the one fixture rule", doc.Rules)
	}
	rule := doc.Rules[0]
	if rule.Delivery != "none" || rule.Tier != string(artifacts.EnforcementUnsupported) {
		t.Errorf("index rule claims delivery %q tier %q, want no proven delivery", rule.Delivery, rule.Tier)
	}
	if len(rule.Patterns) != 1 || rule.Patterns[0].Glob != "**/*.ts" || !strings.HasPrefix(rule.Patterns[0].Regex, "^") {
		t.Errorf("index patterns = %+v", rule.Patterns)
	}
}

// TestPluginTreeGolden pins the generated tree's identity: every file's path,
// digest, and tier in order, plus the reported gaps and unproven surfaces.
func TestPluginTreeGolden(t *testing.T) {
	plugin, err := BuildPlugin(fixtureCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var b strings.Builder
	b.WriteString("generation " + plugin.Generation + "\n")
	for _, f := range plugin.Files {
		tier := f.Tier
		if tier == "" {
			tier = "unsupported"
		}
		b.WriteString(f.Path + " " + f.Digest + " " + string(tier) + "\n")
	}
	for _, gap := range plugin.Gaps {
		b.WriteString("gap " + string(gap.Role) + " " + string(gap.Status) + "\n")
	}
	for _, gap := range plugin.Unproven {
		b.WriteString("unproven " + gap.Surface + "\n")
	}
	b.WriteString("steering " + plugin.Steering.Surface + "\n")
	b.WriteString("hooks withheld " + strconv.Itoa(len(plugin.Hooks.Withheld)) + "\n")
	checkGolden(t, "plugin.txt", []byte(b.String()))
}

// TestBuildPluginRefusesOverBoundIndex proves an index over Atomic's own
// preflight bound refuses the whole generation instead of truncating it.
func TestBuildPluginRefusesOverBoundIndex(t *testing.T) {
	glob := strings.Repeat("a", InstructionByteBound+1024)
	cat, err := artifacts.NewCatalog([]artifacts.Artifact{{
		ID:     "rule:rules/big.md",
		Kind:   artifacts.KindRule,
		Source: "rules/big.md",
		Body:   []byte("---\npaths:\n  - \"" + glob + "\"\n---\n\nBody.\n"),
	}})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	_, err = BuildPlugin(cat, harness.CodexCapabilities())
	if err == nil {
		t.Fatal("plugin generated with an over-bound rule index")
	}
	if !strings.Contains(err.Error(), "refusing") || !strings.Contains(err.Error(), "truncating") {
		t.Errorf("error %q does not state the refuse-not-truncate contract", err)
	}
}

// TestBuildPluginRejectsProviderID covers the verbatim ship path: a rule body is
// copied byte-for-byte, so a concrete provider identifier there must fail
// generation naming the artifact.
func TestBuildPluginRejectsProviderID(t *testing.T) {
	cat, err := artifacts.NewCatalog([]artifacts.Artifact{{
		ID:     "rule:rules/provider.md",
		Kind:   artifacts.KindRule,
		Source: "rules/provider.md",
		Body:   []byte("---\npaths:\n  - \"**/*.ts\"\n---\n\nPrefer the anthropic/opus model for reviews.\n"),
	}})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	_, err = BuildPlugin(cat, harness.CodexCapabilities())
	if err == nil {
		t.Fatal("plugin generated with a concrete provider identifier in a rule")
	}
	if !strings.Contains(err.Error(), "rules/provider.md") {
		t.Errorf("error %q does not name the offending artifact", err)
	}
}

// TestPluginAgentsRoundTripToCanonicalBody proves the Codex TOML boundary: the
// projected agent decodes back to the canonical instruction body and carries no
// user model policy.
func TestPluginAgentsRoundTripToCanonicalBody(t *testing.T) {
	cat := fixtureCorpus(t)
	plugin, err := BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	file, ok := findFile(plugin, AgentsDir+"/atomic-example.toml")
	if !ok {
		t.Fatal("projected agent missing")
	}
	doc, err := harness.DecodeCodexAgent(file.Bytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var agent artifacts.Artifact
	for _, a := range cat.OfKind(artifacts.KindAgent) {
		agent = a
	}
	body, err := artifacts.AgentBody(agent)
	if err != nil {
		t.Fatal(err)
	}
	if doc.DeveloperInstructions != string(body) {
		t.Errorf("developer_instructions do not round-trip:\n--- got ---\n%s\n--- want ---\n%s", doc.DeveloperInstructions, body)
	}
	var keys map[string]any
	if err := toml.Unmarshal(file.Bytes, &keys); err != nil {
		t.Fatalf("decode native keys: %v", err)
	}
	for key := range keys {
		for _, policy := range harness.UserPolicyKeys {
			if key == policy {
				t.Errorf("projected agent carries user model policy key %q", key)
			}
		}
	}
	if !contains(file.Unsupported, "skills") {
		t.Errorf("agent projection does not report the dropped skills dependency: %+v", file.Unsupported)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// TestPluginSkillsReportUnsupportedSurfaces proves skill bytes ship while every
// skill surface stays a reported gap.
func TestPluginSkillsReportUnsupportedSurfaces(t *testing.T) {
	cat := fixtureCorpus(t)
	plugin, err := BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := findFile(plugin, SkillsDir+"/atomic-example/SKILL.md"); !ok {
		t.Fatal("skill manifest missing from the plugin")
	}
	report, err := harness.ProjectSkills(cat, artifacts.TargetCodex, harness.SkillPolicy{}, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("project skills: %v", err)
	}
	if len(report.Gaps) == 0 {
		t.Error("skill projection reports no gap; Codex proved no skill surface")
	}
	seen := 0
	for _, gap := range plugin.Gaps {
		switch gap.Role {
		case harness.RoleSkillDiscovery, harness.RoleSkillMetadata, harness.RoleSkillReferences, harness.RoleSkillDisablement:
			seen++
		}
	}
	if seen == 0 {
		t.Errorf("plugin gaps do not name any skill surface: %+v", plugin.Gaps)
	}
}

// TestMatcherModuleNeverClaimsDelivery executes the generated matcher: a rule
// path matches, and an operation whose tool was never proven is uncovered and
// receives no matched body. A shell command string is never parsed for a path.
func TestMatcherModuleNeverClaimsDelivery(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("skip matcher execution: node is not installed")
	}
	plugin, err := BuildPlugin(fixtureCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	file, ok := findFile(plugin, MatcherPath)
	if !ok {
		t.Fatal("matcher module missing")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "matcher.mjs")
	if err := os.WriteFile(path, file.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `const m = await import(` + jsString("file://"+path) + `);
console.log(JSON.stringify({
  path: m.matchPath("nested/target.ts"),
  absolute: m.matchPath("/etc/passwd"),
  shell: m.matchOperation({ tool: "bash", input: { command: "rm -rf /tmp/x" } }),
  edit: m.matchOperation({ tool: "apply_patch", input: { path: "nested/target.ts" } })
}));`
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run matcher: %v\n%s", err, out)
	}
	var got struct {
		Path     []string `json:"path"`
		Absolute []string `json:"absolute"`
		Shell    struct {
			Covered bool     `json:"covered"`
			Matched []string `json:"matched"`
		} `json:"shell"`
		Edit struct {
			Covered bool `json:"covered"`
		} `json:"edit"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode matcher output %q: %v", out, err)
	}
	if len(got.Path) != 1 {
		t.Errorf("matchPath(nested/target.ts) = %v, want the fixture rule", got.Path)
	}
	if len(got.Absolute) != 0 {
		t.Errorf("matchPath accepted an absolute path: %v", got.Absolute)
	}
	if got.Shell.Covered || len(got.Shell.Matched) != 0 {
		t.Errorf("a shell command operation was treated as covered: %+v", got.Shell)
	}
	if got.Edit.Covered {
		t.Errorf("apply_patch was treated as covered although no input schema is proven: %+v", got.Edit)
	}
}

// jsString encodes s as a JavaScript string literal.
func jsString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
