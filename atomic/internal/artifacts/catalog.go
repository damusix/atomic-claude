package artifacts

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

// Catalog is the canonical corpus: every artifact the selected binary ships,
// enumerated deterministically by kind then source.
type Catalog struct {
	Artifacts []Artifact
	byID      map[string]Artifact
}

// Load enumerates repoRoot/context/ into the canonical corpus. Partial
// expansion happens here and only here, so every projection consumes
// already-rendered bytes.
func Load(repoRoot string) (*Catalog, error) {
	contextRoot := bundlespec.SourceRoot(repoRoot)

	partials, err := templaterender.LoadPartials(filepath.Join(contextRoot, templaterender.PartialsDir))
	if err != nil {
		return nil, err
	}

	cat := &Catalog{}
	add := func(source string, kind Kind) error {
		a, err := loadArtifact(partials, contextRoot, source, kind)
		if err != nil {
			return err
		}
		cat.Artifacts = append(cat.Artifacts, a)
		return nil
	}

	if err := add(bundlespec.GlobalSteering.Source, KindSteering); err != nil {
		return nil, fmt.Errorf("global steering source: %w", err)
	}

	agentsDir := filepath.Join(contextRoot, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !bundlespec.MatchesAgent(e.Name()) {
			continue
		}
		if err := add("agents/"+e.Name(), KindAgent); err != nil {
			return nil, err
		}
	}

	skillsDir := filepath.Join(contextRoot, "skills")
	skillEntries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	for _, e := range skillEntries {
		if !e.IsDir() || !bundlespec.MatchesSkillDir(e.Name()) {
			continue
		}
		skillRoot := filepath.Join(skillsDir, e.Name())
		if _, err := os.Stat(filepath.Join(skillRoot, "SKILL.md")); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(skillRoot, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			rel, err := filepath.Rel(contextRoot, path)
			if err != nil {
				return err
			}
			return add(filepath.ToSlash(rel), KindSkill)
		})
		if err != nil {
			return nil, fmt.Errorf("walk skill %s: %w", e.Name(), err)
		}
	}

	stylesDir := filepath.Join(contextRoot, "output-styles")
	styleEntries, err := os.ReadDir(stylesDir)
	if err != nil {
		return nil, fmt.Errorf("read output-styles dir: %w", err)
	}
	for _, e := range styleEntries {
		if e.IsDir() || !bundlespec.MatchesOutputStyle(e.Name()) {
			continue
		}
		if err := add("output-styles/"+e.Name(), KindOutputStyle); err != nil {
			return nil, err
		}
	}

	if err := walk(contextRoot, "commands", KindCommand, bundlespec.MatchesCommand, add); err != nil {
		return nil, fmt.Errorf("walk commands: %w", err)
	}
	if err := walk(contextRoot, "rules", KindRule, bundlespec.MatchesRule, add); err != nil {
		return nil, fmt.Errorf("walk rules: %w", err)
	}

	sort.Slice(cat.Artifacts, func(i, j int) bool {
		if cat.Artifacts[i].Kind != cat.Artifacts[j].Kind {
			return cat.Artifacts[i].Kind < cat.Artifacts[j].Kind
		}
		return cat.Artifacts[i].Source < cat.Artifacts[j].Source
	})

	cat.byID = make(map[string]Artifact, len(cat.Artifacts))
	for _, a := range cat.Artifacts {
		cat.byID[a.ID] = a
	}

	return cat, nil
}

// walk adds every matching file under contextRoot/dir, recursively.
func walk(contextRoot, dir string, kind Kind, matches func(string) bool, add func(string, Kind) error) error {
	return filepath.WalkDir(filepath.Join(contextRoot, dir), func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !matches(path) {
			return nil
		}
		rel, err := filepath.Rel(contextRoot, path)
		if err != nil {
			return err
		}
		return add(filepath.ToSlash(rel), kind)
	})
}

// expandedKinds may compose a shared partial: commands, agents, and the authored
// global steering source. Skills, rules, and output styles are copied
// byte-for-byte — the template engine would read a literal {{ in their prose as
// a directive.
var expandedKinds = map[Kind]bool{KindCommand: true, KindAgent: true, KindSteering: true}

func loadArtifact(partials *template.Template, contextRoot, source string, kind Kind) (Artifact, error) {
	data, err := os.ReadFile(filepath.Join(contextRoot, filepath.FromSlash(source)))
	if err != nil {
		return Artifact{}, fmt.Errorf("read %s: %w", source, err)
	}

	body := data
	if expandedKinds[kind] {
		body, err = templaterender.Expand(partials, filepath.Base(source), data)
		if err != nil {
			return Artifact{}, err
		}
	}

	sem := parseSemantics(kind, data)

	return Artifact{
		ID:           string(kind) + ":" + source,
		Kind:         kind,
		Source:       source,
		Body:         body,
		Semantics:    sem,
		SourceDigest: managedfile.Digest(data),
	}, nil
}

// parseSemantics reads the portable metadata from authored frontmatter. A parse
// failure marks the semantics malformed instead of failing the render: bodies
// ship byte-for-byte, and host frontmatter is more permissive than YAML (an
// unquoted colon in a description is valid to Claude Code). Corpus validation,
// not rendering, rejects malformed metadata.
func parseSemantics(kind Kind, source []byte) Semantics {
	meta, _, err := frontmatter.Parse(string(source))
	if err != nil {
		return Semantics{Malformed: true}
	}

	var sem Semantics
	if v, ok := meta["name"].(string); ok {
		sem.Name = v
	}
	if v, ok := meta["description"].(string); ok {
		sem.Description = strings.TrimSpace(v)
	}
	if kind != KindAgent {
		return sem
	}
	skills, ok := meta["skills"].([]any)
	if !ok {
		return sem
	}
	for _, item := range skills {
		name, ok := item.(string)
		if !ok {
			return Semantics{Malformed: true}
		}
		sem.Requires = append(sem.Requires, SkillID(name))
	}
	return sem
}

// Get returns the artifact with the given canonical ID. A catalog built by Load
// carries its index; a catalog assembled directly still resolves by identity.
func (c *Catalog) Get(id string) (Artifact, bool) {
	if a, ok := c.byID[id]; ok {
		return a, true
	}
	for _, a := range c.Artifacts {
		if a.ID == id {
			return a, true
		}
	}
	return Artifact{}, false
}

// OfKind returns every artifact of one kind, in corpus order.
func (c *Catalog) OfKind(kind Kind) []Artifact {
	var out []Artifact
	for _, a := range c.Artifacts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// Validate rejects identity collisions and dependencies that name an artifact
// the corpus does not carry. An adapter must never be handed a projection whose
// required artifact is missing.
func (c *Catalog) Validate() error {
	seen := make(map[string]bool, len(c.Artifacts))
	for _, a := range c.Artifacts {
		if a.ID == "" || a.Kind == "" || a.Source == "" {
			return fmt.Errorf("artifacts: incomplete identity %+v", a)
		}
		if seen[a.ID] {
			return fmt.Errorf("artifacts: duplicate identity %s", a.ID)
		}
		seen[a.ID] = true
	}
	for _, a := range c.Artifacts {
		if err := c.validateRequires(a); err != nil {
			return err
		}
	}
	for _, a := range c.Artifacts {
		if a.Semantics.Malformed {
			return fmt.Errorf("artifacts: %s carries frontmatter that is not valid YAML", a.ID)
		}
	}
	return nil
}

// ValidateAgent rejects a canonical agent whose declared dependency does not
// resolve in the corpus. A projection calls it before rendering, so an agent
// missing a required skill or command fails loudly instead of shipping with a
// silent hole.
func (c *Catalog) ValidateAgent(a Artifact) error {
	if a.Kind != KindAgent {
		return fmt.Errorf("artifacts: %s is not an agent", a.ID)
	}
	return c.validateRequires(a)
}

// validateRequires resolves every declared dependency by stable identity.
func (c *Catalog) validateRequires(a Artifact) error {
	for _, req := range a.Semantics.Requires {
		if _, ok := c.Get(req); !ok {
			return fmt.Errorf("artifacts: %s requires unknown artifact %s", a.ID, req)
		}
	}
	return nil
}
