package artifacts

import (
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

// ClaudeGlobal renders the authored global contract directly into Claude's
// user-level file: the contract's own bytes, with no importer file at user
// scope and no path referring to one.
func (r *Renderer) ClaudeGlobal() (Projection, error) {
	steering, err := r.Steering()
	if err != nil {
		return Projection{}, err
	}
	return Projection{
		Artifact:    steering.ID,
		Target:      TargetClaude,
		Path:        bundlespec.GlobalSteering.ClaudeTarget,
		Bytes:       steering.Body,
		Delivery:    DeliveryDirect,
		Enforcement: EnforcementUnsupported,
		Digest:      ProjectionDigest(steering.Body),
	}, nil
}

// LoaderPair renders a repository or realm scope: the shared steering file and
// the thin Claude loader beside it. guidance is the scope's authored content;
// the loader imports the adjacent file by relative reference, so the guidance
// loads once whether the scope is a repository root or a nested wiki.
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
// import-free steering body, one blank line, and the Atomic output style with
// its parsed YAML frontmatter excluded. Targets without import support receive
// the guidance inline instead of through an import directive.
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
	body = ImportFree(body)
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

// steeringBody returns the Atomic-owned content of the global contract: the
// bytes inside its managed block. A target that owns its own envelope receives
// the content without a nested block.
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

// ImportFree drops import directives and the blank line each one leaves behind,
// so the same guidance reads the same with or without the directive.
func ImportFree(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if isImportDirective(line) {
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// isImportDirective reports whether a line is a native import: a whole line
// starting with @ and carrying no whitespace. Prose that merely mentions an
// @-handle keeps its words.
func isImportDirective(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "@") && !strings.ContainsAny(trimmed, " \t")
}

// terminate normalizes authored content to exactly one trailing newline.
func terminate(body []byte) []byte {
	return []byte(strings.TrimRight(string(body), "\n") + "\n")
}
