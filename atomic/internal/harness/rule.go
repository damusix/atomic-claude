package harness

import (
	"fmt"
	"path"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// Rule projections render canonical rule records into a target's native
// scoped-rule tree without touching the canonical corpus. They are offline,
// like the agent and skill projections: no enrollment, journal, or native write
// is involved.
//
// The CP0 capability record gates the tier. Claude Code 2.1.273 loaded
// unconditional user and project rules at startup but never loaded a
// path-scoped rule, and reached no tool call, so its static scope,
// pre-operation targets, context return, and deny surfaces are all unproven.
// The adapter ships the authored bytes and reports the tier as unsupported: a
// Claude rule file never reads as a proven native scoped-rule surface, and
// nothing claims a hook path CP0 never observed.
//
// Claude rule syntax is the canonical syntax, so a record's own source bytes are
// the native bytes. The projection preserves the frontmatter — a `paths:` list
// or none — and the body byte-for-byte; it never re-emits or normalizes them.

// RuleSource pairs a parsed rule record with the authored source bytes it was
// parsed from. The bytes are the projection's output, so a mismatch between them
// and the record's source digest is refused rather than written.
type RuleSource struct {
	Record rules.RuleRecord
	Bytes  []byte
}

// RuleOrderingRecordID is the deterministic identity order the projections are
// materialized in: ascending canonical RecordID. It is Atomic's declared
// generation order, not native precedence — CP0 measured Claude membership and
// startup ordering but not semantic precedence.
const RuleOrderingRecordID = "record-id"

// ClaudeRule is one Claude-native rule file plus the metadata that fixes how a
// consumer may describe it. NativeResource is relative to the Claude
// configuration root; Bytes are the authored rule bytes and Digest is their
// projection digest.
type ClaudeRule struct {
	// RecordID is the producer-qualified canonical identity.
	RecordID string
	// Class is the record class, which selects the native rule home.
	Class rules.Class
	// Source is the producer-relative authored source path.
	Source string
	// SourceDigest is the digest of the authored source bytes.
	SourceDigest string
	// NativeResource is the Claude-native path relative to the Claude
	// configuration root.
	NativeResource string
	// Bytes is the authored rule bytes, written verbatim.
	Bytes []byte
	// Digest is the projection digest of Bytes.
	Digest string
	// Scope is the CP0-selected native scope mapping. It is empty when no proven
	// role maps the record.
	Scope []rules.ScopeField
	// Delivery is how Claude would supply the matched body at runtime.
	Delivery rules.RuntimeDelivery
	// Tier is the strongest native guarantee the CP0 record proves.
	Tier artifacts.EnforcementTier
	// Order is the record's position in the projected generation, ascending by
	// RecordID. It is Atomic's declared order, not native precedence.
	Order int
}

// ClaudeRuleReport is one Claude target's projected rule generation plus the
// surfaces it could not prove. Ordering names the deterministic generation
// order; native precedence is unmeasured for the tested version, so it is never
// presented as one.
type ClaudeRuleReport struct {
	Target   artifacts.Target `json:"target"`
	Ordering string           `json:"ordering"`
	Rules    []ClaudeRule     `json:"rules"`
	Gaps     []Capability     `json:"gaps,omitempty"`
}

// ClaudeRulePath returns a record's Claude-native path relative to the Claude
// configuration root. The class selects the native rule home the design assigns:
// shipped Atomic rules live under rules/atomic/, and repository wiki cards live
// under rules/atomic-wiki/. Collision safety is NOT implied by this path: rules
// validation keys on RuleRecord.NativeName, which the class prefix does not
// disambiguate, so a shipped source under context/rules/atomic-wiki/ collides
// with a wiki card of the same domain (see TestProjectClaudeRules_RejectsNativeCollision).
func ClaudeRulePath(r rules.RuleRecord) string {
	if r.Class == rules.ClassWikiPointer {
		return path.Join("rules", r.NativeName())
	}
	return path.Join("rules", "atomic", r.NativeName())
}

// ruleRoles are the native rule-delivery surfaces a projection reports on. Being
// unproven is the normal state: a role a harness later proves drops out of the
// gap list.
var ruleRoles = []Role{
	RoleStaticScope,
	RolePreOperationTargets,
	RoleContextReturn,
	RoleDeterministicDeny,
}

// RuleGaps returns the rule-delivery surfaces m leaves unproven, in stable role
// order. The deny role is included because a prose record selects deny-enforced
// only through an exact machine predicate, which no tested harness proves.
func RuleGaps(m CapabilityMatrix) []Capability {
	out := make([]Capability, 0, len(ruleRoles))
	for _, role := range ruleRoles {
		c := m.Capability(role)
		if c.Status == StatusSupported {
			continue
		}
		if c.Limitation == "" {
			c.Limitation = "no CP0 rule-delivery observation for " + string(m.Harness) + " " + m.Version
		}
		out = append(out, c)
	}
	return out
}

// ruleEvidence derives the shared projection evidence from a capability record.
// A role that is partial or unsupported is false, so unproven behavior can never
// read as support.
func ruleEvidence(m CapabilityMatrix) rules.Evidence {
	return rules.Evidence{
		StaticScope:   m.Supports(RoleStaticScope),
		PreOperation:  m.Supports(RolePreOperationTargets),
		ContextReturn: m.Supports(RoleContextReturn),
		Event:         m.Capability(RolePreOperationTargets).Native,
	}
}

// ProjectClaudeRules renders a set of canonical rule records into Claude-native
// rule files. Source bytes are written verbatim and checked against each
// record's source digest; a native-name collision fails before any target is
// touched; the generation is ordered by canonical identity; and every rule
// carries the tier the Claude CP0 record proves, which is unsupported until a
// path-scoped activation is observed.
func ProjectClaudeRules(sources []RuleSource, m CapabilityMatrix) (ClaudeRuleReport, error) {
	if m.Harness != KindClaude {
		return ClaudeRuleReport{}, fmt.Errorf("harness: Claude rule projection needs the Claude capability record, got %q", m.Harness)
	}

	records := make([]rules.RuleRecord, 0, len(sources))
	for i, s := range sources {
		if s.Record.ID == "" {
			return ClaudeRuleReport{}, fmt.Errorf("harness: rule source %d has no record identity", i)
		}
		if got := managedfile.Digest(s.Bytes); got != s.Record.SourceDigest {
			return ClaudeRuleReport{}, fmt.Errorf("harness: project %s: source bytes digest %s does not match record digest %s", s.Record.ID, got, s.Record.SourceDigest)
		}
		records = append(records, s.Record)
	}
	if err := rules.Validate(records); err != nil {
		return ClaudeRuleReport{}, err
	}

	ordered := make([]RuleSource, len(sources))
	copy(ordered, sources)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Record.ID < ordered[j].Record.ID })

	ev := ruleEvidence(m)
	report := ClaudeRuleReport{
		Target:   artifacts.TargetClaude,
		Ordering: RuleOrderingRecordID,
		Gaps:     RuleGaps(m),
	}
	for i, s := range ordered {
		sel := rules.Select(s.Record, artifacts.TargetClaude, ev)
		report.Rules = append(report.Rules, ClaudeRule{
			RecordID:       s.Record.ID,
			Class:          s.Record.Class,
			Source:         s.Record.Source,
			SourceDigest:   s.Record.SourceDigest,
			NativeResource: ClaudeRulePath(s.Record),
			Bytes:          s.Bytes,
			Digest:         artifacts.ProjectionDigest(s.Bytes),
			Scope:          sel.Scope,
			Delivery:       sel.Delivery,
			Tier:           sel.Tier,
			Order:          i,
		})
	}
	return report, nil
}
