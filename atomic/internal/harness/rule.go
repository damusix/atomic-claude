package harness

import (
	"fmt"
	"path"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// Claude rule syntax is the canonical syntax, so a Claude rule projection ships
// the authored bytes verbatim; the capability record alone sets the tier.

// RuleSource pairs a parsed rule record with its authored bytes. Bytes that
// disagree with the record's source digest are refused rather than written.
type RuleSource struct {
	Record rules.RuleRecord
	Bytes  []byte
}

// ShippedRuleSources pairs every canonical rule with its bytes. Projection and
// runtime delivery share it so a rule cannot ship in one and miss the other.
func ShippedRuleSources(cat *artifacts.Catalog) ([]RuleSource, error) {
	ofKind := cat.OfKind(artifacts.KindRule)
	sources := make([]RuleSource, 0, len(ofKind))
	for _, a := range ofKind {
		record, err := rules.ParseShipped(a.Source, a.Body)
		if err != nil {
			return nil, fmt.Errorf("harness: parse %s: %w", a.ID, err)
		}
		sources = append(sources, RuleSource{Record: record, Bytes: a.Body})
	}
	return sources, nil
}

// RuleOrderingRecordID orders projections by ascending RecordID. It is Atomic's
// generation order, not native precedence, which was never measured.
const RuleOrderingRecordID = "record-id"

// ClaudeRule is one Claude-native rule file plus the metadata that fixes how a
// consumer may describe it.
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
	// Scope is the proven native scope mapping, empty when no proven role maps
	// the record.
	Scope []rules.ScopeField
	// Delivery is how Claude would supply the matched body at runtime.
	Delivery rules.RuntimeDelivery
	// Tier is the strongest native guarantee the capability record proves.
	Tier artifacts.EnforcementTier
	// Order is the record's position in the projected generation, ascending by
	// RecordID. It is Atomic's declared order, not native precedence.
	Order int
}

// ClaudeRuleReport is one Claude target's projected rule generation plus the
// surfaces it could not prove.
type ClaudeRuleReport struct {
	Target   artifacts.Target `json:"target"`
	Ordering string           `json:"ordering"`
	Rules    []ClaudeRule     `json:"rules"`
	Gaps     []Capability     `json:"gaps,omitempty"`
}

// ClaudeRulePath places wiki cards under the repository .claude/ and shipped
// rules under the user root; Validate still rejects a NativeName collision.
func ClaudeRulePath(r rules.RuleRecord) string {
	if r.Class == rules.ClassWikiPointer {
		return path.Join("rules", r.NativeName())
	}
	return path.Join("rules", "atomic", r.NativeName())
}

// ruleRoles are the rule-delivery surfaces a projection reports on. Session
// baseline is one because Codex delivers rule context through that event.
var ruleRoles = []Role{
	RoleSessionBaseline,
	RoleStaticScope,
	RolePreOperationTargets,
	RoleContextReturn,
	RoleDeterministicDeny,
}

// RuleGaps returns the rule-delivery surfaces m leaves unproven, in stable role
// order.
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

// WikiCardTier is hook-required once the delivery row is observed in any
// degree, else unsupported.
func WikiCardTier(m CapabilityMatrix) string {
	if m.Capability(RoleWikiCardDelivery).Status == StatusUnsupported {
		return string(artifacts.EnforcementUnsupported)
	}
	return string(artifacts.EnforcementHookRequired)
}

// RuleEvidence derives projection evidence from a capability record; a partial
// role counts as false.
func RuleEvidence(m CapabilityMatrix) rules.Evidence {
	return rules.Evidence{
		StaticScope:   m.Supports(RoleStaticScope),
		PreOperation:  m.Supports(RolePreOperationTargets),
		ContextReturn: m.Supports(RoleContextReturn),
		Event:         m.Capability(RolePreOperationTargets).Native,
	}
}

// ProjectClaudeRules renders canonical rule records into Claude-native rule
// files. A native-name collision fails before any target is touched.
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

	ev := RuleEvidence(m)
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
