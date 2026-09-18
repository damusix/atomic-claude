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

// Skill projections render a canonical skill into a target's native skill tree
// without touching the canonical corpus. They are offline, like the agent
// projections: no enrollment, journal, or native write is involved.
//
// A skill is a directory. SKILL.md is the manifest a harness discovers; every
// sibling file is a referenced file that ships beside it, so a relative
// reference inside the body keeps its meaning under each target layout. The
// CP0 record gates the native claim: no tested harness has a skill-install or
// skill-metadata row, so no projection claims the target discovers the skill,
// carries metadata beyond the portable pair, resolves references, or honors
// disablement. The bytes ship; the gap is explicit and reported.

// portableSkillKeys are the manifest frontmatter keys a target receives as its
// own fields. Any other canonical key is reported unsupported rather than
// copied with an implied native claim.
var portableSkillKeys = map[string]bool{"name": true, "description": true}

// SkillPolicy is the user's skill disablement, by canonical artifact identity.
// A disabled manifest or referenced file is honored — never projected — and
// reported, so a projection can never silently drop it.
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
// harness discovers. Every other file under a skill directory is a referenced
// file that ships beside it.
func SkillManifest(a artifacts.Artifact) bool {
	return a.Kind == artifacts.KindSkill && path.Base(a.Source) == "SKILL.md"
}

// ClaudeSkill renders a canonical skill file into Claude's native skill tree.
// Claude's manifest frontmatter is exactly the canonical name/description pair,
// so the bytes ship unchanged and every referenced file ships beside them.
// Claude carries the metadata natively; there is no metadata gap.
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

// OMPSkill renders a canonical skill file into OMP's package tree. A manifest
// becomes the canonical body with only the portable name/description
// frontmatter; every other canonical metadata key is reported unsupported
// because OMP has no skill-metadata row. A referenced file ships unchanged
// beside its manifest, so relative references keep their meaning.
func OMPSkill(a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedSkill(a); err != nil {
		return artifacts.Projection{}, err
	}

	if !SkillManifest(a) {
		return artifacts.Projection{
			Artifact:    a.ID,
			Target:      artifacts.TargetOMP,
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
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for OMP: %w", a.ID, err)
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
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for OMP: %w", a.ID, err)
	}

	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetOMP,
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
// A role a harness later proves drops out of the list; until then the
// projection ships bytes and claims nothing.
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

// SkillCollision names two canonical skills that project to the same native
// path. A collision is never resolved silently: two skills cannot share one
// native file.
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

// SkillReferenceGap reports a relative reference a shipped skill file declares
// that the skill tree cannot resolve. The gap names the declaring file, so a
// nested reference deeper than one level is reported at its source.
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
// carries, classified by the surface it drives. A harness-tool use is a gap the
// projection reports instead of letting the wire token ship as if it resolved.
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

// ProjectSkills renders the canonical skill corpus for one target. A
// user-disabled skill ships nothing and is reported disabled; every relative
// reference any shipped file declares — a manifest's or a nested reference's —
// must resolve inside its shipped skill tree or it is reported as a gap; each
// shipped file's runtime-oriented instructions are classified by surface, so a
// harness-specific wire token is an explicit gap rather than a silent ship;
// two native files mapping to one path are reported as a collision; every skill
// surface without a CP0 row is an explicit gap.
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

// skillProjector resolves the per-target skill projection function. Codex skill
// installation is a later milestone, so it reports no projector rather than an
// invented one.
func skillProjector(target artifacts.Target) (func(artifacts.Artifact) (artifacts.Projection, error), error) {
	switch target {
	case artifacts.TargetClaude:
		return ClaudeSkill, nil
	case artifacts.TargetOMP:
		return OMPSkill, nil
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

// declaredSkillRefs returns the relative references one shipped skill file
// declares, in source order and deduplicated, resolved to skill-relative paths
// against the declaring file's directory. Only the `references/...` and `./...`
// forms count as shipped skill references; a project path such as
// docs/page.md is content. Resolution makes a nested reference — a reference
// declared by a referenced file deeper in the tree — resolve exactly as a
// manifest's does.
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
// relative to its skill root: "" for the manifest, "references" for a file one
// level down, "references/nested" for one deeper.
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

// harnessRuntimePatterns are the harness-specific runtime markers a portable
// skill file must not carry: a backticked harness tool name, a tool-call shape,
// an installed Claude state path, a model/effort selection, or an instruction
// whose delivery depends on harness background output injection. The list is
// deliberately literal so prose cannot trip it.
var harnessRuntimePatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"harness tool name", regexp.MustCompile("`(?:Bash|Read|Write|Edit|Glob|Grep|Task|Monitor|WebFetch|WebSearch|TodoWrite|SlashCommand|NotebookEdit|AskUserQuestion|BashOutput|KillShell)`")},
	{"harness tool call", regexp.MustCompile(`\b(?:Monitor|Task|TodoWrite|WebFetch|WebSearch)\(`)},
	{"TaskStop", regexp.MustCompile("`?TaskStop`?")},
	{"subagent_type", regexp.MustCompile("subagent_type")},
	{"claude state path", regexp.MustCompile(`~?/?\.claude/`)},
	{"model or effort selection", regexp.MustCompile(`(?i)\b(?:haiku|sonnet|opus)\b|model:|effort:`)},
	{"harness background delivery", regexp.MustCompile(`(?i)persistent background command|streams its output into this session`)},
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
// file carries, in source order and deduplicated by surface and name. A
// harness-tool use is the gap a projection reports; an atomic-binary invocation
// or a canonical agent dispatch is portable and reported as such.
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
