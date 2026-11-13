package harness

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/pelletier/go-toml/v2"
)

// Agent projections carry canonical metadata only where the target's capability
// record proves the surface; unproven fields are reported unsupported.

// UserPolicyKeys select a model, effort, or tool surface. User policy owns
// them, so an agent declaring one is refused rather than projected.
var UserPolicyKeys = []string{"model", "effort", "tools", "disallowedTools", "mcpServers"}

// ClaudeAgent renders a canonical agent into Claude Code's native agent file.
// The canonical frontmatter is already Claude-native, so the bytes ship unchanged.
func ClaudeAgent(cat *artifacts.Catalog, a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedAgent(cat, a); err != nil {
		return artifacts.Projection{}, err
	}
	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetClaude,
		Path:        agentMarkdownPath(a),
		Bytes:       a.Body,
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Digest:      artifacts.ProjectionDigest(a.Body),
	}, nil
}

// OMPAgent renders a canonical agent into OMP's native agent Markdown. OMP has
// no proven skills-metadata row, so a skills list is dropped and reported.
func OMPAgent(cat *artifacts.Catalog, a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedAgent(cat, a); err != nil {
		return artifacts.Projection{}, err
	}
	body, err := artifacts.AgentBody(a)
	if err != nil {
		return artifacts.Projection{}, err
	}

	var unsupported []string
	fields := []frontmatter.KV{{Key: "name", Value: a.Semantics.Name}}
	if a.Semantics.Description != "" {
		fields = append(fields, frontmatter.KV{Key: "description", Value: a.Semantics.Description})
	}
	if len(a.Semantics.Requires) > 0 {
		unsupported = append(unsupported, "skills")
	}

	doc, err := frontmatter.EmitOrdered(fields, string(body))
	if err != nil {
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for OMP: %w", a.ID, err)
	}

	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetOMP,
		Path:        agentMarkdownPath(a),
		Bytes:       []byte(doc),
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Unsupported: unsupported,
		Digest:      artifacts.ProjectionDigest([]byte(doc)),
	}, nil
}

// CodexAgentDoc is Codex's custom-agent document. Model, effort, sandbox, and
// tool settings stay with the user's Codex configuration.
type CodexAgentDoc struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

// CodexAgent renders a canonical agent into Codex's TOML custom-agent document.
// Codex has no proven skill-preload row, so a skills list is reported instead.
func CodexAgent(cat *artifacts.Catalog, a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedAgent(cat, a); err != nil {
		return artifacts.Projection{}, err
	}
	body, err := artifacts.AgentBody(a)
	if err != nil {
		return artifacts.Projection{}, err
	}

	data, err := toml.Marshal(CodexAgentDoc{
		Name:                  a.Semantics.Name,
		Description:           a.Semantics.Description,
		DeveloperInstructions: string(body),
	})
	if err != nil {
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for Codex: %w", a.ID, err)
	}

	var unsupported []string
	if len(a.Semantics.Requires) > 0 {
		unsupported = append(unsupported, "skills")
	}

	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetCodex,
		Path:        "agents/" + a.Semantics.Name + ".toml",
		Bytes:       data,
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Unsupported: unsupported,
		Digest:      artifacts.ProjectionDigest(data),
	}, nil
}

// DecodeCodexAgent parses a projected Codex agent document back into its fields.
func DecodeCodexAgent(data []byte) (CodexAgentDoc, error) {
	var doc CodexAgentDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return CodexAgentDoc{}, fmt.Errorf("harness: decode Codex agent document: %w", err)
	}
	return doc, nil
}

// checkedAgent refuses an agent with unresolved dependencies, no name, or a
// user-policy key.
func checkedAgent(cat *artifacts.Catalog, a artifacts.Artifact) error {
	if err := cat.ValidateAgent(a); err != nil {
		return err
	}
	if a.Semantics.Name == "" {
		return fmt.Errorf("harness: agent %s carries no name", a.ID)
	}
	meta, _, err := frontmatter.Parse(string(a.Body))
	if err != nil {
		return fmt.Errorf("harness: agent %s frontmatter: %w", a.ID, err)
	}
	for _, key := range UserPolicyKeys {
		if _, ok := meta[key]; ok {
			return fmt.Errorf("harness: agent %s declares %q, which user model policy owns; a projection never writes model, effort, or tool restrictions", a.ID, key)
		}
	}
	return nil
}

// AgentProjection renders one canonical agent into a harness's native document.
type AgentProjection func(*artifacts.Catalog, artifacts.Artifact) (artifacts.Projection, error)

// AgentDir is the directory under a native root that holds its agent files.
func AgentDir(root string) string { return filepath.Join(root, "agents") }

// AgentFile is one native agent file under a target root. Data carries the
// stored override, and is both the written and the claimed bytes.
type AgentFile struct {
	Name string
	Path string
	Data []byte
}

// Unit is the file's commit-unit name, a single safe path segment.
func (f AgentFile) Unit() string { return "agent-" + f.Name }

// AgentFiles projects every canonical agent with project and overlays the
// override home stores under kind's agents table, writing each under root.
func AgentFiles(home, root string, kind Kind, cat *artifacts.Catalog, project AgentProjection) ([]AgentFile, error) {
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		return nil, fmt.Errorf("harness: read config: %w", err)
	}
	overrides := cfg.AgentOverrides(string(kind))
	agents := cat.OfKind(artifacts.KindAgent)
	out := make([]AgentFile, 0, len(agents))
	for _, a := range agents {
		p, err := project(cat, a)
		if err != nil {
			return nil, err
		}
		data, err := Overlay(kind, p.Bytes, overrides[a.Semantics.Name])
		if err != nil {
			return nil, err
		}
		out = append(out, AgentFile{Name: a.Semantics.Name, Path: filepath.Join(root, filepath.FromSlash(p.Path)), Data: data})
	}
	return out, nil
}

// AgentClaims claims each file whole, carrying the digest ledger recorded for
// target so bytes Atomic last wrote stay owned while a hand edit does not.
func AgentClaims(files []AgentFile, ledger *installstate.Ledger, target string) []Claim {
	out := make([]Claim, 0, len(files))
	for _, f := range files {
		c := Claim{ID: f.Path, Kind: managedfile.KindFile, Path: f.Path, SelectedDigest: managedfile.Digest(f.Data)}
		if row, ok := ledger.Find(target, f.Path); ok {
			c.RecordedDigest = row.Applied.Digest
		}
		out = append(out, c)
	}
	return out
}

// ProjectAgentClaims projects t's agent files under its native root and claims
// them against the ledger home holds.
func ProjectAgentClaims(home string, t Target, cat *artifacts.Catalog, project AgentProjection) ([]Claim, error) {
	files, err := AgentFiles(home, t.NativeRoot, t.Kind, cat, project)
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}
	return AgentClaims(files, ledger, t.Key()), nil
}

// AgentPlan is how one convergence treats a target's agent files.
type AgentPlan struct {
	// Left are files Atomic never wrote that the caller chose to keep, in this
	// operation or a recorded earlier one.
	Left []string
	// Retire are owned rows of agents the corpus no longer ships whose bytes
	// are still what Atomic wrote.
	Retire   []installstate.Row
	Blockers []string
	// Converged reports that nothing under the agent directory would change.
	Converged bool
}

// PlanAgents classifies the claims and unclaimed target rows under dir. An
// unowned file follows decision; an owned file edited since blocks regardless.
func PlanAgents(claims []Claim, ledger *installstate.Ledger, target, dir string, decision installstate.Decision) (AgentPlan, error) {
	plan := AgentPlan{Converged: true}
	claimed := map[string]bool{}
	for _, c := range claims {
		if c.Kind != managedfile.KindFile || filepath.Dir(c.Path) != dir {
			continue
		}
		claimed[c.Path] = true
		a, err := Assess(c)
		if err != nil {
			return plan, err
		}
		switch {
		case a.Evidence == EvidenceSelection:
		case a.Evidence != EvidenceUnowned:
			plan.Converged = false
		case c.RecordedDigest != "":
			plan.Blockers = append(plan.Blockers, c.Path+" was edited outside Atomic; restore or remove it to converge")
		case decision == installstate.DecisionReplace:
			plan.Converged = false
		case decision == installstate.DecisionLeaveUnowned || decision == "" && ledger.IsLeft(target, c.Path):
			plan.Left = append(plan.Left, c.Path)
		default:
			plan.Blockers = append(plan.Blockers, c.Path+" exists and was not written by Atomic; move or remove it, or rerun with --replace or --leave-unowned")
		}
	}
	for _, row := range ledger.Rows {
		if row.Target != target || row.Applied.Kind != managedfile.KindFile || filepath.Dir(row.Applied.Path) != dir || claimed[row.Applied.Path] {
			continue
		}
		if err := verifyOwned(row); err != nil {
			if !errors.Is(err, ErrEvidenceConflict) {
				return plan, err
			}
			plan.Blockers = append(plan.Blockers, row.Applied.Path+" belongs to an agent Atomic no longer ships and was edited outside Atomic; restore or remove it to converge")
			continue
		}
		plan.Retire = append(plan.Retire, row)
		plan.Converged = false
	}
	if !slices.Equal(ledger.LeftIn(target, dir), plan.Left) {
		plan.Converged = false
	}
	return plan, nil
}

// RetireAgents deletes each retired file and drops its row from ledger,
// returning the paths it removed. The caller commits the ledger.
func RetireAgents(ledger *installstate.Ledger, rows []installstate.Row) ([]string, error) {
	drop := map[installstate.Row]bool{}
	var removed []string
	for _, row := range rows {
		if err := verifyOwned(row); err != nil {
			return removed, err
		}
		if _, err := removeResource(row); err != nil {
			return removed, err
		}
		drop[row] = true
		removed = append(removed, row.Applied.Path)
	}
	kept := ledger.Rows[:0:0]
	for _, row := range ledger.Rows {
		if !drop[row] {
			kept = append(kept, row)
		}
	}
	ledger.Rows = kept
	return removed, nil
}

// agentMarkdownPath is the native path a canonical agent's Markdown document
// installs to, relative to the target root.
func agentMarkdownPath(a artifacts.Artifact) string {
	return "agents/" + filepath.Base(a.Source)
}
