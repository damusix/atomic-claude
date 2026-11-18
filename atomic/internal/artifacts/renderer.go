package artifacts

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Renderer projects one canonical corpus into target-native files.
type Renderer struct {
	catalog *Catalog
}

func NewRenderer(c *Catalog) *Renderer {
	return &Renderer{catalog: c}
}

// Steering returns the sole authored global steering artifact.
func (r *Renderer) Steering() (Artifact, error) {
	steering := r.catalog.OfKind(KindSteering)
	if len(steering) != 1 {
		return Artifact{}, fmt.Errorf("artifacts: want exactly one global steering artifact, found %d", len(steering))
	}
	return steering[0], nil
}

// OutputStyle returns the Atomic output-style artifact.
func (r *Renderer) OutputStyle() (Artifact, error) {
	for _, a := range r.catalog.OfKind(KindOutputStyle) {
		if filepath.Base(a.Source) == "atomic.md" {
			return a, nil
		}
	}
	return Artifact{}, fmt.Errorf("artifacts: no atomic output style in the corpus")
}

// claudeVocabulary maps the corpus's neutral behavior words to Claude Code tools,
// named per https://code.claude.com/docs/en/tools-reference.
const claudeVocabulary = "## Native tools\n\n" +
	"On Claude Code, dispatching a named agent, in the foreground or the background, is the `Agent` tool with `subagent_type` set to the agent's name. " +
	"An omitted `subagent_type` falls to a general-purpose agent without that agent's skills, and the general-purpose agent itself is `subagent_type: \"general-purpose\"`. " +
	"Asking the user a question is `AskUserQuestion`. " +
	"Entering a worktree is `EnterWorktree`. " +
	"A background listener is `Monitor` with `timeout_ms` (at most 30 minutes), stopped with `TaskStop`."

// ClaudeGlobal renders the authored global contract directly into Claude's
// user-level file, with no importer file.
func (r *Renderer) ClaudeGlobal() (Projection, error) {
	steering, err := r.Steering()
	if err != nil {
		return Projection{}, err
	}
	body, err := ClaudeSteering(steering.Body)
	if err != nil {
		return Projection{}, err
	}
	return Projection{
		Artifact:    steering.ID,
		Target:      TargetClaude,
		Path:        bundlespec.GlobalSteering.ClaudeTarget,
		Bytes:       body,
		Delivery:    DeliveryDirect,
		Enforcement: EnforcementUnsupported,
		Digest:      ProjectionDigest(body),
	}, nil
}

// ClaudeSteering returns the global steering as Claude receives it, with the
// vocabulary paragraph inside the Atomic block so it is owned and replaced with it.
func ClaudeSteering(file []byte) ([]byte, error) {
	start, end, err := managedfile.BlockBoundaries(file)
	if err != nil {
		return nil, fmt.Errorf("artifacts: global steering: %w", err)
	}
	closeLine := start + bytes.LastIndexByte(bytes.TrimRight(file[start:end], "\n"), '\n') + 1
	before := bytes.TrimRight(file[:closeLine], "\n")

	out := make([]byte, 0, len(file)+len(claudeVocabulary)+4)
	out = append(out, before...)
	out = append(out, "\n\n"+claudeVocabulary+"\n\n"...)
	return append(out, file[closeLine:]...), nil
}

// LoaderPair renders a scope's shared steering file and the thin Claude loader
// that imports it by relative reference, so guidance loads once at any depth.
func (r *Renderer) LoaderPair(dir string, guidance []byte) ([]Projection, error) {
	source := Projection{
		Target:      TargetClaude,
		Path:        scopedPath(dir, bundlespec.ScopeSteering.Source),
		Bytes:       terminate(guidance),
		Delivery:    DeliveryShared,
		Enforcement: EnforcementUnsupported,
	}
	source.Digest = ProjectionDigest(source.Bytes)

	loader := Projection{
		Target:      TargetClaude,
		Path:        scopedPath(dir, bundlespec.ScopeSteering.ClaudeTarget),
		Bytes:       []byte(bundlespec.ScopeSteering.LoaderBody()),
		Delivery:    DeliveryLoader,
		Enforcement: EnforcementUnsupported,
	}
	loader.Digest = ProjectionDigest(loader.Bytes)

	return []Projection{source, loader}, nil
}

// OMPSteering renders the Atomic-owned content for an OMP profile AGENTS.md: the
// steering body, then the output style without its frontmatter.
func (r *Renderer) OMPSteering() (Projection, error) {
	steering, err := r.Steering()
	if err != nil {
		return Projection{}, err
	}
	style, err := r.OutputStyle()
	if err != nil {
		return Projection{}, err
	}

	body, err := steeringBody(steering.Body)
	if err != nil {
		return Projection{}, err
	}
	styleBytes, err := styleBody(style.Body)
	if err != nil {
		return Projection{}, err
	}

	composed := make([]byte, 0, len(body)+len(styleBytes)+1)
	composed = append(composed, body...)
	composed = append(composed, '\n')
	composed = append(composed, styleBytes...)

	return Projection{
		Artifact:    steering.ID,
		Target:      TargetOMP,
		Path:        bundlespec.ScopeSteering.Source,
		Bytes:       composed,
		Delivery:    DeliveryComposed,
		Enforcement: EnforcementUnsupported,
		Digest:      ProjectionDigest(composed),
	}, nil
}

// CodexSteering is the OMP composition for $CODEX_HOME/AGENTS.md, minus the
// import lines Codex leaves literal.
func (r *Renderer) CodexSteering() (Projection, error) {
	p, err := r.OMPSteering()
	if err != nil {
		return p, err
	}
	p.Target = TargetCodex
	p.Bytes = stripImportLines(p.Bytes)
	p.Digest = ProjectionDigest(p.Bytes)
	return p, nil
}

// stripImportLines drops whole-line `@` imports outside code fences, plus the
// blank line after one that would otherwise double the paragraph gap.
func stripImportLines(body []byte) []byte {
	lines := strings.SplitAfter(string(body), "\n")
	var out strings.Builder
	fence := ""
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		for _, marker := range []string{"```", "~~~"} {
			if strings.HasPrefix(line, marker) && (fence == "" || fence == marker) {
				if fence == "" {
					fence = marker
				} else {
					fence = ""
				}
			}
		}
		if fence != "" || !strings.HasPrefix(line, "@") {
			out.WriteString(lines[i])
			continue
		}
		if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "" && strings.HasSuffix(out.String(), "\n\n") {
			i++
		}
	}
	return []byte(out.String())
}

// steeringBody returns the bytes inside the global contract's managed block, for a
// target that owns its own envelope.
func steeringBody(file []byte) ([]byte, error) {
	block, err := managedfile.ManagedBlock(file)
	if err != nil {
		return nil, fmt.Errorf("artifacts: global steering: %w", err)
	}

	lines := strings.SplitAfter(string(block), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) < 2 {
		return nil, fmt.Errorf("artifacts: global steering block carries no content")
	}

	inner := strings.Join(lines[1:len(lines)-1], "")
	return []byte(strings.Trim(inner, "\n") + "\n"), nil
}

// styleBody returns the output-style body with its parsed YAML frontmatter
// excluded; the frontmatter is harness metadata, not instructions.
func styleBody(file []byte) ([]byte, error) {
	_, body, err := frontmatter.Parse(string(file))
	if err != nil {
		return nil, fmt.Errorf("artifacts: output style: %w", err)
	}
	return []byte(strings.TrimRight(strings.TrimLeft(body, "\n"), "\n") + "\n"), nil
}

// terminate normalizes authored content to exactly one trailing newline.
func terminate(body []byte) []byte {
	return []byte(strings.TrimRight(string(body), "\n") + "\n")
}
