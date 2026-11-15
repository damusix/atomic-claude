package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// Plugin-relative paths the generated marketplace tree publishes. The plugin
// manifest carries no hooks key because no hook row is proven.
const (
	// MarketplaceManifestPath is the marketplace descriptor Codex reads when it
	// registers the tree.
	MarketplaceManifestPath = ".agents/plugins/marketplace.json"
	// PluginRoot is the plugin directory the marketplace descriptor points at.
	PluginRoot = "plugins/" + PluginName
	// PluginManifestPath is the plugin descriptor Codex reads on install.
	PluginManifestPath = PluginRoot + "/.codex-plugin/plugin.json"
	// RuleIndexPath is the compiled scope index the matcher evaluates.
	RuleIndexPath = PluginRoot + "/rules/index.json"
	// MatcherPath is the structured-path matcher module the plugin carries.
	MatcherPath = PluginRoot + "/matcher.mjs"
	// RulesDir is the plugin directory shipped rule bodies live under.
	RulesDir = PluginRoot + "/rules"
	// SkillsDir is the plugin directory projected skills live under.
	SkillsDir = PluginRoot + "/skills"
)

// InstructionByteBound is Codex's documented `project_doc_max_bytes` default,
// applied as Atomic's own bound; an index over it is refused, never truncated.
const InstructionByteBound = 32 * 1024

// ProvenToolInput is one tool whose input carried an exact structured path in a
// runtime observation. Codex proved none, so every operation is uncovered.
type ProvenToolInput struct {
	Tool      string `json:"tool"`
	PathField string `json:"path_field"`
	Evidence  string `json:"evidence"`
}

// IndexPattern is one scope pattern in the runtime index. Regex comes from
// rules.CompileGlob so the runtime and canonical matchers agree by construction.
type IndexPattern struct {
	Glob  string `json:"glob"`
	Regex string `json:"regex"`
}

// IndexEntry is one rule the index carries.
type IndexEntry struct {
	RecordID     string                    `json:"id"`
	Class        rules.Class               `json:"class"`
	Source       string                    `json:"source"`
	SourceDigest string                    `json:"source_digest"`
	Patterns     []IndexPattern            `json:"patterns"`
	Tier         artifacts.EnforcementTier `json:"tier"`
	Delivery     rules.RuntimeDelivery     `json:"delivery"`
	Order        int                       `json:"order"`
}

// UndeliverableRule is one projected rule whose patterns leave the translated
// dialect; an approximated pattern would match paths the canonical matcher does not.
type UndeliverableRule struct {
	RecordID string `json:"record_id"`
	Reason   string `json:"reason"`
}

// HookRegistration records the plugin-hook configuration Atomic withholds,
// because Codex 0.147.0 proved no hook event.
type HookRegistration struct {
	// ConfigFile is the plugin-relative hook configuration Atomic would publish.
	// It is empty while no hook role is proven.
	ConfigFile string `json:"config_file,omitempty"`
	// Registered is the event set the plugin actually wires: empty, because every
	// justifying role is unsupported.
	Registered []harness.Capability `json:"registered,omitempty"`
	// Withheld is every unproven rule-delivery role, named so the gap cannot read
	// as a deliberate no-op.
	Withheld []harness.Capability `json:"withheld,omitempty"`
}

// PluginFile is one file in the generated plugin tree.
type PluginFile struct {
	// Path is the plugin-relative path, slash-separated.
	Path  string `json:"path"`
	Bytes []byte `json:"-"`
	// Source is the canonical artifact identity the file was projected from;
	// empty for a generated manifest, index, or matcher.
	Source string `json:"source,omitempty"`
	// Unsupported names the canonical fields the file could not carry natively.
	Unsupported []string `json:"unsupported,omitempty"`
	// Tier is the enforcement tier the capability record proves for this file.
	Tier artifacts.EnforcementTier `json:"tier"`
	// Digest is the digest of Bytes.
	Digest string `json:"digest"`
}

// PluginGap is one native plugin surface no probe could prove, with the evidence
// that fixes it.
type PluginGap struct {
	Surface  string `json:"surface"`
	Evidence string `json:"evidence"`
}

// Plugin is the generated Codex plugin: the marketplace tree the selected
// generation publishes and every native surface it cannot promise.
type Plugin struct {
	// Generation is the content identity: one line per projected file in path
	// order, so a change anywhere changes it.
	Generation string       `json:"generation"`
	Files      []PluginFile `json:"files"`
	// Index is the compiled rule index in canonical identity order.
	Index []IndexEntry `json:"index,omitempty"`
	// IndexBytes is the rendered index size the bound is checked against.
	IndexBytes int `json:"index_bytes"`
	// Bound is the preflight ceiling applied to IndexBytes.
	Bound int `json:"bound"`
	// Undeliverable names the records whose patterns the matcher cannot express.
	Undeliverable []UndeliverableRule `json:"undeliverable,omitempty"`
	// Tools is the structured inputs the matcher may match. Empty for Codex
	// 0.147.0: no hook event ran, so no tool input is proven.
	Tools []ProvenToolInput `json:"tools"`
	// Hooks is the withheld registration plan.
	Hooks HookRegistration `json:"hooks"`
	// Gaps are the capability rows the plugin cannot promise, in stable role
	// order.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native plugin surfaces with no runtime observation.
	Unproven []PluginGap `json:"unproven,omitempty"`
}

// BuildPlugin projects the canonical corpus into the Codex plugin tree, offline
// and deterministically. An unproven canonical field is dropped and reported.
func BuildPlugin(cat *artifacts.Catalog, m harness.CapabilityMatrix) (Plugin, error) {
	if m.Harness != harness.KindCodex {
		return Plugin{}, fmt.Errorf("codex: plugin generation needs the Codex capability record, got %q", m.Harness)
	}

	plugin := Plugin{
		Gaps:     append(harness.SkillGaps(m), harness.RuleGaps(m)...),
		Unproven: append(pluginGaps(), kindGaps(m)...),
		Hooks:    hookRegistration(m),
		Tools:    ProvenToolInputs(),
		Bound:    InstructionByteBound,
	}

	sources, err := harness.ShippedRuleSources(cat)
	if err != nil {
		return Plugin{}, err
	}
	if err := plugin.addRules(cat, sources, m); err != nil {
		return Plugin{}, err
	}
	if err := plugin.addSkills(cat, m); err != nil {
		return Plugin{}, err
	}
	if err := plugin.addManifest(); err != nil {
		return Plugin{}, err
	}
	if err := plugin.addMatcher(); err != nil {
		return Plugin{}, err
	}

	sort.Slice(plugin.Files, func(i, j int) bool { return plugin.Files[i].Path < plugin.Files[j].Path })
	for _, f := range plugin.Files {
		if !harness.ProviderID(string(f.Bytes)) {
			continue
		}
		identity := f.Source
		if identity == "" {
			identity = f.Path
		}
		return Plugin{}, fmt.Errorf("codex: %s carries a concrete provider or model identifier; Atomic ships semantic preferences only", identity)
	}
	for i := 1; i < len(plugin.Files); i++ {
		if plugin.Files[i].Path == plugin.Files[i-1].Path {
			return Plugin{}, fmt.Errorf("codex: two artifacts project to the plugin path %s", plugin.Files[i].Path)
		}
	}
	plugin.Generation = pluginGeneration(plugin.Files)
	return plugin, nil
}

// addRules ships every path-scoped rule and compiles the matcher's scope index.
// Codex proved no scoped-rule delivery, so no record claims a native scope.
func (p *Plugin) addRules(cat *artifacts.Catalog, sources []harness.RuleSource, m harness.CapabilityMatrix) error {
	identity := make(map[string]string)
	for _, a := range cat.OfKind(artifacts.KindRule) {
		identity[a.Source] = a.ID
	}

	records := make([]rules.RuleRecord, 0, len(sources))
	for i, s := range sources {
		if s.Record.ID == "" {
			return fmt.Errorf("codex: rule source %d has no record identity", i)
		}
		if s.Record.Producer != rules.ProducerShipped {
			return fmt.Errorf("codex: %s is not a shipped rule", s.Record.ID)
		}
		if got := managedfile.Digest(s.Bytes); got != s.Record.SourceDigest {
			return fmt.Errorf("codex: project %s: source bytes digest %s does not match record digest %s", s.Record.ID, got, s.Record.SourceDigest)
		}
		records = append(records, s.Record)
	}
	if err := rules.Validate(records); err != nil {
		return err
	}

	ordered := make([]harness.RuleSource, len(sources))
	copy(ordered, sources)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Record.ID < ordered[j].Record.ID })

	ev := harness.RuleEvidence(m)
	for i, s := range ordered {
		native := RulesDir + "/" + strings.TrimPrefix(s.Record.Source, "rules/")
		sel := rules.Select(s.Record, artifacts.TargetCodex, ev)
		p.Files = append(p.Files, PluginFile{
			Path:   native,
			Bytes:  s.Bytes,
			Source: identity[s.Record.Source],
			Tier:   sel.Tier,
			Digest: artifacts.ProjectionDigest(s.Bytes),
		})

		entry := IndexEntry{
			RecordID:     s.Record.ID,
			Class:        s.Record.Class,
			Source:       s.Record.Source,
			SourceDigest: s.Record.SourceDigest,
			Tier:         sel.Tier,
			Delivery:     sel.Delivery,
			Order:        i,
		}
		untranslatable := ""
		for _, glob := range s.Record.Include {
			compiled, err := rules.CompileGlob(glob)
			if err != nil {
				untranslatable = err.Error()
				break
			}
			entry.Patterns = append(entry.Patterns, IndexPattern{Glob: glob, Regex: compiled})
		}
		if untranslatable != "" {
			p.Undeliverable = append(p.Undeliverable, UndeliverableRule{RecordID: s.Record.ID, Reason: untranslatable})
			continue
		}
		p.Index = append(p.Index, entry)
	}

	index, err := renderIndex(p)
	if err != nil {
		return err
	}
	p.IndexBytes = len(index)
	if p.IndexBytes > p.Bound {
		return fmt.Errorf("codex: rule index is %d bytes, over the %d-byte preflight bound; refusing the generation rather than truncating the index", p.IndexBytes, p.Bound)
	}
	p.Files = append(p.Files, PluginFile{
		Path:   RuleIndexPath,
		Bytes:  index,
		Tier:   artifacts.EnforcementUnsupported,
		Digest: artifacts.ProjectionDigest(index),
	})
	return nil
}

// addSkills projects the skill corpus into the plugin. A collision or an
// unresolvable relative reference refuses it: such a tree must not ship.
func (p *Plugin) addSkills(cat *artifacts.Catalog, m harness.CapabilityMatrix) error {
	report, err := harness.ProjectSkills(cat, artifacts.TargetCodex, harness.SkillPolicy{}, m)
	if err != nil {
		return err
	}
	if len(report.Collisions) > 0 {
		return fmt.Errorf("codex: skill projections collide on %s", report.Collisions[0].Path)
	}
	if len(report.Reference) > 0 {
		gap := report.Reference[0]
		return fmt.Errorf("codex: skill %s file %s references %s, which the shipped skill tree does not carry", gap.Skill, gap.File, gap.Ref)
	}
	for _, file := range report.Files {
		p.Files = append(p.Files, PluginFile{
			Path:        SkillsDir + "/" + strings.TrimPrefix(file.Projection.Path, "skills/"),
			Bytes:       file.Projection.Bytes,
			Source:      file.Artifact,
			Unsupported: file.Projection.Unsupported,
			Tier:        file.Projection.Enforcement,
			Digest:      file.Projection.Digest,
		})
	}
	return nil
}

// marketplacePlugin is one plugin entry in the marketplace descriptor, in the
// shape Codex was observed to register.
type marketplacePlugin struct {
	Name   string `json:"name"`
	Source struct {
		Source string `json:"source"`
		Path   string `json:"path"`
	} `json:"source"`
	Policy struct {
		Installation   string `json:"installation"`
		Authentication string `json:"authentication"`
	} `json:"policy"`
	Category string `json:"category"`
}

// marketplaceDoc is the marketplace descriptor Codex reads when it registers the
// generated tree.
type marketplaceDoc struct {
	Name    string              `json:"name"`
	Plugins []marketplacePlugin `json:"plugins"`
}

// pluginDoc is the plugin descriptor Codex reads on install. It deliberately has
// no hooks field: no hook role is proven, so no hook configuration is emitted.
type pluginDoc struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// addManifest publishes the marketplace and plugin descriptors in the shapes
// Codex was observed to register.
func (p *Plugin) addManifest() error {
	entry := marketplacePlugin{Name: PluginName, Category: "Developer Tools"}
	entry.Source.Source = "local"
	entry.Source.Path = "./plugins/" + PluginName
	entry.Policy.Installation = "AVAILABLE"
	entry.Policy.Authentication = "ON_INSTALL"

	marketplace, err := encodeDoc(marketplaceDoc{Name: MarketplaceName, Plugins: []marketplacePlugin{entry}})
	if err != nil {
		return fmt.Errorf("codex: encode marketplace manifest: %w", err)
	}
	p.Files = append(p.Files, PluginFile{
		Path:   MarketplaceManifestPath,
		Bytes:  marketplace,
		Tier:   artifacts.EnforcementUnsupported,
		Digest: artifacts.ProjectionDigest(marketplace),
	})

	manifest, err := encodeDoc(pluginDoc{
		Name:        PluginName,
		Version:     PluginVersion,
		Description: "Atomic policy, workflow, and rule corpus for Codex.",
	})
	if err != nil {
		return fmt.Errorf("codex: encode plugin manifest: %w", err)
	}
	p.Files = append(p.Files, PluginFile{
		Path:   PluginManifestPath,
		Bytes:  manifest,
		Tier:   artifacts.EnforcementUnsupported,
		Digest: artifacts.ProjectionDigest(manifest),
	})
	return nil
}

// encodeDoc renders a native descriptor deterministically, with one trailing
// newline.
func encodeDoc(doc any) ([]byte, error) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// addMatcher publishes the structured-path matcher module.
func (p *Plugin) addMatcher() error {
	module, err := renderMatcher(p)
	if err != nil {
		return err
	}
	p.Files = append(p.Files, PluginFile{
		Path:   MatcherPath,
		Bytes:  module,
		Tier:   artifacts.EnforcementUnsupported,
		Digest: artifacts.ProjectionDigest(module),
	})
	return nil
}

// Render writes the plugin tree into dir. The caller owns dir; every file is
// written with the mode the generated-tree identity records.
func (p Plugin) Render(dir string) error {
	for _, f := range p.Files {
		if err := safePluginPath(f.Path); err != nil {
			return err
		}
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("codex: mkdir for %s: %w", f.Path, err)
		}
		if err := os.WriteFile(path, f.Bytes, 0o644); err != nil {
			return fmt.Errorf("codex: write %s: %w", f.Path, err)
		}
	}
	return nil
}

// TreeDigest renders the plugin into a scratch directory and digests it the way
// publication does, so a plan-time digest cannot drift from the published tree.
func (p Plugin) TreeDigest() (string, error) { return p.digest("") }

// CacheDigest is the digest of the plugin directory Codex copies, byte for byte,
// into CachePath on registration.
func (p Plugin) CacheDigest() (string, error) { return p.digest(PluginRoot) }

func (p Plugin) digest(sub string) (string, error) {
	dir, err := os.MkdirTemp("", "atomic-codex-plugin-")
	if err != nil {
		return "", fmt.Errorf("codex: stage plugin for digest: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := p.Render(dir); err != nil {
		return "", err
	}
	digest, _, err := managedfile.TreeDigest(filepath.Join(dir, filepath.FromSlash(sub)))
	return digest, err
}

// Paths returns the plugin's file paths in order.
func (p Plugin) Paths() []string {
	out := make([]string, 0, len(p.Files))
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}

// ProvenToolInputs returns the structured inputs proven for Codex 0.147.0: none,
// because no hook event ran.
func ProvenToolInputs() []ProvenToolInput { return nil }

// renderIndex encodes the compiled rule index with its bound, so a reader can
// tell a refused generation from a truncated one.
func renderIndex(p *Plugin) ([]byte, error) {
	doc := struct {
		Target   artifacts.Target    `json:"target"`
		Version  string              `json:"version"`
		Ordering string              `json:"ordering"`
		Bound    int                 `json:"bound"`
		Rules    []IndexEntry        `json:"rules"`
		Dropped  []UndeliverableRule `json:"dropped,omitempty"`
		Tools    []ProvenToolInput   `json:"tools"`
	}{
		Target:   artifacts.TargetCodex,
		Version:  harness.CodexCapabilities().Version,
		Ordering: harness.RuleOrderingRecordID,
		Bound:    p.Bound,
		Rules:    p.Index,
		Dropped:  p.Undeliverable,
		Tools:    p.Tools,
	}
	if doc.Tools == nil {
		doc.Tools = []ProvenToolInput{}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("codex: encode rule index: %w", err)
	}
	return append(data, '\n'), nil
}

// pluginGeneration is the plugin's content identity over each file's path and
// digest in path order.
func pluginGeneration(files []PluginFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write([]byte(f.Digest))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// safePluginPath rejects a plugin path that is absolute or escapes the tree.
func safePluginPath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return fmt.Errorf("codex: plugin path %q is not a relative slash path", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean != path || strings.HasPrefix(clean, "../") || clean == ".." {
		return fmt.Errorf("codex: plugin path %q escapes the plugin tree", path)
	}
	return nil
}

// hookRegistration is the withheld hook plan: every justifying role is unproven.
func hookRegistration(m harness.CapabilityMatrix) HookRegistration {
	return HookRegistration{Withheld: harness.RuleGaps(m)}
}

// pluginGaps are the Codex plugin surfaces left unsupported: the probe's account
// rejected the model before any hook event ran.
func pluginGaps() []PluginGap {
	return []PluginGap{
		{Surface: "plugin-hook trust and changed-definition re-review", Evidence: "codex.hook-absence; replay target codex-runtime (absence): no hook review or execution occurred"},
		{Surface: "deliberate hook disablement and config precedence", Evidence: "codex.hook-absence: not exercised, no replay target"},
		{Surface: "registration cardinality and collisions", Evidence: "codex.registration: one marketplace and one plugin were registered"},
		{Surface: "instruction and payload size limits", Evidence: "no boundary test; the documented 32 KiB project-instruction default is not runtime proof"},
		{Surface: "hosted-tool and specialized-tool bypasses", Evidence: "codex.hook-absence: not exercised, no replay target"},
	}
}
