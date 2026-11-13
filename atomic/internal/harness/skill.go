package harness

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// Skill files ship beside their SKILL.md manifest so relative references keep
// their meaning; the capability record alone decides what a projection claims.

// portableSkillKeys are the manifest keys a target receives; any other key is
// reported unsupported.
var portableSkillKeys = map[string]bool{"name": true, "description": true}

// SkillPolicy is the user's skill disablement, by canonical artifact identity.
// A disabled file is never projected and always reported.
type SkillPolicy struct {
	Disabled []string
}

func (p SkillPolicy) disabled(id string) bool {
	for _, d := range p.Disabled {
		if d == id {
			return true
		}
	}
	return false
}

// SkillName returns the directory name a canonical skill file lives under.
func SkillName(a artifacts.Artifact) (string, error) {
	segments := strings.Split(a.Source, "/")
	if a.Kind != artifacts.KindSkill || len(segments) < 3 || segments[0] != "skills" || segments[1] == "" {
		return "", fmt.Errorf("harness: %s is not a skill file", a.ID)
	}
	return segments[1], nil
}

// SkillManifest reports whether a is a skill's SKILL.md manifest, the file a
// harness discovers.
func SkillManifest(a artifacts.Artifact) bool {
	return a.Kind == artifacts.KindSkill && path.Base(a.Source) == "SKILL.md"
}

// ClaudeSkill renders a canonical skill file into Claude's native skill tree.
// The canonical frontmatter is Claude's own, so the bytes ship unchanged.
func ClaudeSkill(a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedSkill(a); err != nil {
		return artifacts.Projection{}, err
	}
	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetClaude,
		Path:        a.Source,
		Bytes:       a.Body,
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Digest:      artifacts.ProjectionDigest(a.Body),
	}, nil
}

// OMPSkill renders a canonical skill file into OMP's package tree.
func OMPSkill(a artifacts.Artifact) (artifacts.Projection, error) {
	return nativeSkill(a, artifacts.TargetOMP)
}

// CodexSkill renders a canonical skill file into the Codex plugin tree.
func CodexSkill(a artifacts.Artifact) (artifacts.Projection, error) {
	return nativeSkill(a, artifacts.TargetCodex)
}

// nativeSkill is the projection OMP and Codex share, since both read only the
// portable name/description pair from a manifest.
func nativeSkill(a artifacts.Artifact, target artifacts.Target) (artifacts.Projection, error) {
	if err := checkedSkill(a); err != nil {
		return artifacts.Projection{}, err
	}

	if !SkillManifest(a) {
		return artifacts.Projection{
			Artifact:    a.ID,
			Target:      target,
			Path:        a.Source,
			Bytes:       a.Body,
			Delivery:    artifacts.DeliveryDirect,
			Enforcement: artifacts.EnforcementUnsupported,
			Digest:      artifacts.ProjectionDigest(a.Body),
		}, nil
	}

	body, err := artifacts.SkillBody(a)
	if err != nil {
		return artifacts.Projection{}, err
	}
	kvs, _, err := frontmatter.ParseOrdered(string(a.Body))
	if err != nil {
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for %s: %w", a.ID, target, err)
	}

	fields := []frontmatter.KV{{Key: "name", Value: a.Semantics.Name}}
	if a.Semantics.Description != "" {
		fields = append(fields, frontmatter.KV{Key: "description", Value: a.Semantics.Description})
	}
	var unsupported []string
	for _, kv := range kvs {
		if !portableSkillKeys[kv.Key] {
			unsupported = append(unsupported, kv.Key)
		}
	}

	doc, err := frontmatter.EmitOrdered(fields, string(body))
	if err != nil {
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for %s: %w", a.ID, target, err)
	}

	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      target,
		Path:        a.Source,
		Bytes:       []byte(doc),
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Unsupported: unsupported,
		Digest:      artifacts.ProjectionDigest([]byte(doc)),
	}, nil
}

// checkedSkill validates a canonical skill file before projection: it must live
// under a skills/ directory, and a manifest must carry a name.
func checkedSkill(a artifacts.Artifact) error {
	if a.Kind != artifacts.KindSkill {
		return fmt.Errorf("harness: %s is not a skill", a.ID)
	}
	if _, err := SkillName(a); err != nil {
		return err
	}
	if SkillManifest(a) && a.Semantics.Name == "" {
		return fmt.Errorf("harness: skill manifest %s carries no name", a.ID)
	}
	return nil
}

// skillRoles are the native skill surfaces a projection reports on. No tested
// harness has a row for any of them, so every one is an explicit gap.
var skillRoles = []Role{RoleSkillDiscovery, RoleSkillMetadata, RoleSkillReferences, RoleSkillDisablement}

// SkillGaps returns the skill surfaces m leaves unproven, in stable role order.
func SkillGaps(m CapabilityMatrix) []Capability {
	out := make([]Capability, 0, len(skillRoles))
	for _, role := range skillRoles {
		c := m.Capability(role)
		if c.Status == StatusSupported {
			continue
		}
		if c.Limitation == "" {
			c.Limitation = "no CP0 skill observation for " + string(m.Harness) + " " + m.Version
		}
		out = append(out, c)
	}
	return out
}

// SkillCollision names canonical skills that project to the same native path.
type SkillCollision struct {
	Target  artifacts.Target `json:"target"`
	Path    string           `json:"path"`
	Sources []string         `json:"sources"`
}

// SkillCollisions returns every native path two or more projections target, in
// stable target-then-path order.
func SkillCollisions(projections []artifacts.Projection) []SkillCollision {
	type key struct {
		target artifacts.Target
		path   string
	}
	byPath := map[key][]string{}
	for _, p := range projections {
		k := key{p.Target, p.Path}
		byPath[k] = append(byPath[k], p.Artifact)
	}

	var out []SkillCollision
	for k, sources := range byPath {
		if len(sources) < 2 {
			continue
		}
		sort.Strings(sources)
		out = append(out, SkillCollision{Target: k.target, Path: k.path, Sources: sources})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// SkillReferenceGap reports an unresolvable relative reference at the file that
// declares it, so a nested reference is reported at its source.
type SkillReferenceGap struct {
	Skill string `json:"skill"`
	File  string `json:"file"`
	Ref   string `json:"ref"`
}

// SkillRuntimeSurface is the surface a runtime-oriented instruction drives.
type SkillRuntimeSurface string

const (
	// SkillRuntimeBinary is the atomic binary surface (atomic bus, atomic repl,
	// atomic code, ...), portable to any harness that carries the binary.
	SkillRuntimeBinary SkillRuntimeSurface = "atomic-binary"
	// SkillRuntimeAgent is a canonical agent dispatch, portable by identity.
	SkillRuntimeAgent SkillRuntimeSurface = "agent-dispatch"
	// SkillRuntimeHarness is a harness-specific tool or runtime primitive, a
	// wire token no other target resolves. It is the explicit gap.
	SkillRuntimeHarness SkillRuntimeSurface = "harness-tool"
)

// SkillRuntimeUse is one runtime-oriented instruction a shipped skill file
// carries, classified by the surface it drives.
type SkillRuntimeUse struct {
	Skill   string              `json:"skill"`
	File    string              `json:"file"`
	Surface SkillRuntimeSurface `json:"surface"`
	Name    string              `json:"name"`
}

// SkillFile pairs a canonical identity with its native projection.
type SkillFile struct {
	Artifact   string               `json:"artifact"`
	Projection artifacts.Projection `json:"projection"`
}

// SkillReport is one target's projected skill corpus plus the explicit outcomes
// it could not honor.
type SkillReport struct {
	Target     artifacts.Target    `json:"target"`
	Files      []SkillFile         `json:"files"`
	Disabled   []string            `json:"disabled,omitempty"`
	Collisions []SkillCollision    `json:"collisions,omitempty"`
	Reference  []SkillReferenceGap `json:"reference_gaps,omitempty"`
	Runtime    []SkillRuntimeUse   `json:"runtime,omitempty"`
	Gaps       []Capability        `json:"gaps,omitempty"`
}

// ProjectSkills renders the canonical skill corpus for one target, reporting
// disabled files, unresolved references, runtime uses, collisions, and gaps.
func ProjectSkills(cat *artifacts.Catalog, target artifacts.Target, policy SkillPolicy, m CapabilityMatrix) (SkillReport, error) {
	project, err := skillProjector(target)
	if err != nil {
		return SkillReport{}, err
	}

	report := SkillReport{Target: target, Gaps: SkillGaps(m)}
	agents := canonicalAgents(cat)
	var projections []artifacts.Projection

	for _, manifest := range cat.OfKind(artifacts.KindSkill) {
		if !SkillManifest(manifest) {
			continue
		}
		name, err := SkillName(manifest)
		if err != nil {
			return SkillReport{}, err
		}
		if policy.disabled(manifest.ID) {
			report.Disabled = append(report.Disabled, manifest.ID)
			continue
		}

		shipped := map[string]bool{}
		add := func(a artifacts.Artifact) error {
			p, err := project(a)
			if err != nil {
				return err
			}
			report.Files = append(report.Files, SkillFile{Artifact: a.ID, Projection: p})
			projections = append(projections, p)
			shipped[a.Source] = true
			return nil
		}

		if err := add(manifest); err != nil {
			return SkillReport{}, err
		}
		files := []artifacts.Artifact{manifest}
		for _, ref := range skillReferences(cat, manifest) {
			if policy.disabled(ref.ID) {
				report.Disabled = append(report.Disabled, ref.ID)
				continue
			}
			if err := add(ref); err != nil {
				return SkillReport{}, err
			}
			files = append(files, ref)
		}

		for _, file := range files {
			for _, ref := range declaredSkillRefs(file.Source, string(file.Body)) {
				if !shipped["skills/"+name+"/"+ref] {
					report.Reference = append(report.Reference, SkillReferenceGap{Skill: name, File: file.Source, Ref: ref})
				}
			}
			report.Runtime = append(report.Runtime, runtimeUses(name, file, agents)...)
		}
	}

	report.Collisions = SkillCollisions(projections)
	return report, nil
}

// skillProjector resolves the per-target skill projection function.
func skillProjector(target artifacts.Target) (func(artifacts.Artifact) (artifacts.Projection, error), error) {
	switch target {
	case artifacts.TargetClaude:
		return ClaudeSkill, nil
	case artifacts.TargetOMP:
		return OMPSkill, nil
	case artifacts.TargetCodex:
		return CodexSkill, nil
	}
	return nil, fmt.Errorf("harness: no skill projection for target %q", target)
}

// skillReferences returns the referenced files that ship beside a skill's
// manifest, in corpus order.
func skillReferences(cat *artifacts.Catalog, manifest artifacts.Artifact) []artifacts.Artifact {
	name, err := SkillName(manifest)
	if err != nil {
		return nil
	}
	prefix := "skills/" + name + "/"
	var out []artifacts.Artifact
	for _, a := range cat.OfKind(artifacts.KindSkill) {
		if a.Source != manifest.Source && strings.HasPrefix(a.Source, prefix) {
			out = append(out, a)
		}
	}
	return out
}

// skillRef matches a relative reference a shipped skill file declares: a
// markdown link target or a backticked path (the form the authored skills use).
var skillRef = regexp.MustCompile("\\]\\(([^)\\s]+)\\)|`([^`\\n]+)`")

// declaredSkillRefs returns a file's `references/...` and `./...` references,
// resolved against its own directory; any other path is content, not a reference.
func declaredSkillRefs(source, body string) []string {
	dir := skillRelativeDir(source)
	var out []string
	seen := map[string]bool{}
	add := func(target string) {
		target = strings.TrimSpace(target)
		if !strings.HasPrefix(target, "references/") && !strings.HasPrefix(target, "./") {
			return
		}
		if i := strings.IndexByte(target, '#'); i >= 0 {
			target = target[:i]
		}
		if target == "" || strings.HasSuffix(target, "/") {
			return
		}
		resolved := path.Clean(path.Join(dir, target))
		if resolved == "." || strings.HasPrefix(resolved, "..") || seen[resolved] {
			return
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	for _, m := range skillRef.FindAllStringSubmatch(body, -1) {
		if m[1] != "" {
			add(m[1])
			continue
		}
		add(m[2])
	}
	return out
}

// skillRelativeDir returns the directory a shipped skill file lives in,
// relative to its skill root ("" for the manifest).
func skillRelativeDir(source string) string {
	segments := strings.Split(source, "/")
	if len(segments) < 3 {
		return ""
	}
	rel := strings.Join(segments[2:], "/")
	if dir := path.Dir(rel); dir != "." {
		return dir
	}
	return ""
}

// harnessRuntimePattern is one harness-specific runtime marker. A wire marker
// is Claude-only syntax that leaks in another target; others are reported gaps.
type harnessRuntimePattern struct {
	name    string
	pattern *regexp.Regexp
	wire    bool
}

// harnessRuntimePatterns are the runtime markers a portable skill file must not
// silently carry. They are deliberately literal so prose cannot trip them.
var harnessRuntimePatterns = []harnessRuntimePattern{
	{"harness tool name", regexp.MustCompile("`(?:Agent|Bash|Read|Write|Edit|Glob|Grep|Task|Monitor|WebFetch|WebSearch|TodoWrite|SlashCommand|NotebookEdit|AskUserQuestion|EnterWorktree|BashOutput|KillShell)`"), true},
	{"harness tool call", regexp.MustCompile(`\b(?:Monitor|Task|TodoWrite|WebFetch|WebSearch)\(`), true},
	{"TaskStop", regexp.MustCompile("TaskStop"), true},
	{"subagent_type", regexp.MustCompile("subagent_type"), true},
	{"claude state path", regexp.MustCompile(`\.claude/`), false},
	{"model or effort selection", regexp.MustCompile(`(?i)\b(?:haiku|sonnet|opus)\b|model:|effort:`), true},
	{"harness background delivery", regexp.MustCompile(`(?i)streams each output line into this session`), false},
}

// HarnessRuntimeUse is one harness-specific runtime marker a byte body carries.
type HarnessRuntimeUse struct {
	// Name is the marker label, e.g. "harness tool name".
	Name string `json:"name"`
	// Wire reports a Claude-only wire token, as opposed to a classified surface
	// the projection reports as a gap.
	Wire bool `json:"wire"`
}

// HarnessRuntimeUses reports every harness-specific runtime marker body
// carries, using the same patterns the skill projection classifies.
func HarnessRuntimeUses(body []byte) []HarnessRuntimeUse {
	text := string(body)
	var out []HarnessRuntimeUse
	for _, t := range harnessRuntimePatterns {
		if t.pattern.MatchString(text) {
			out = append(out, HarnessRuntimeUse{Name: t.name, Wire: t.wire})
		}
	}
	return out
}

// atomicRuntimeVerb matches an atomic binary invocation in a skill body.
var atomicRuntimeVerb = regexp.MustCompile(`\batomic ([a-z][a-z-]*)`)

// backtickedAtomic matches a backticked canonical agent identity.
var backtickedAtomic = regexp.MustCompile("`(atomic-[a-z0-9-]+)`")

// canonicalAgents returns the canonical agent names in the corpus, the portable
// identities a runtime instruction may dispatch.
func canonicalAgents(cat *artifacts.Catalog) map[string]bool {
	names := map[string]bool{}
	for _, a := range cat.OfKind(artifacts.KindAgent) {
		if a.Semantics.Name != "" {
			names[a.Semantics.Name] = true
		}
	}
	return names
}

// runtimeUses classifies the runtime-oriented instructions one shipped skill
// file carries, deduplicated by surface and name.
func runtimeUses(name string, file artifacts.Artifact, agents map[string]bool) []SkillRuntimeUse {
	text := string(file.Body)
	verbs := cliusage.TopLevelVerbs()
	var out []SkillRuntimeUse
	seen := map[string]bool{}
	add := func(surface SkillRuntimeSurface, token string) {
		key := string(surface) + "\x00" + token
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, SkillRuntimeUse{Skill: name, File: file.Source, Surface: surface, Name: token})
	}

	for _, t := range harnessRuntimePatterns {
		if t.pattern.MatchString(text) {
			add(SkillRuntimeHarness, t.name)
		}
	}
	for _, m := range atomicRuntimeVerb.FindAllStringSubmatch(text, -1) {
		if verbs[m[1]] {
			add(SkillRuntimeBinary, "atomic "+m[1])
		}
	}
	for _, m := range backtickedAtomic.FindAllStringSubmatch(text, -1) {
		if agents[m[1]] {
			add(SkillRuntimeAgent, m[1])
		}
	}
	return out
}
