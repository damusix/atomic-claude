package omp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// CP0 launched OMP at a repository root whose `.omp/rules/` directory it
// discovered, alongside `.omp/AGENTS.md` and `.omp/RULES.md`; a rule carrying
// `paths:` in that directory never reached the model, before or after the
// matching read. The directory is a real native surface, but its matched-body
// behavior is unproven, so Atomic ships card bodies there while recording every
// rule as unsupported. Shipping is never an install failure: a capability row
// that is missing or partial produces `unsupported`, not an error.
const (
	// ProjectRulesDir is the repository-relative OMP project rule directory.
	ProjectRulesDir = ".omp/rules"
	// CardsDirName is the pipeline-owned subdirectory repository refresh cards
	// project into, so a repository's own `.omp/rules/*.md` are never touched.
	CardsDirName = "atomic-wiki"
	// cardsUnit is the commit unit the card tree publishes under.
	cardsUnit = "cards"
)

// ShippedRule is one shipped rule's OMP projection: the authored bytes that
// ship inside the Atomic package, the package-relative native resource, and the
// CP0-selected scope mapping, delivery, and enforcement tier.
type ShippedRule struct {
	// RecordID is the producer-qualified canonical identity.
	RecordID string
	// Class is the record class.
	Class rules.Class
	// Source is the producer-relative authored source path and, for OMP, the
	// package-relative native path: the rule ships where the corpus declares it.
	Source string
	// SourceDigest is the digest of the authored source bytes.
	SourceDigest string
	// NativeResource is the package-relative native path.
	NativeResource string
	// Bytes is the authored rule bytes, shipped verbatim.
	Bytes []byte
	// Digest is the projection digest of Bytes.
	Digest string
	// Scope is the CP0-selected native scope mapping. It is empty whenever no
	// proven role maps the record, which is every record for OMP 18.1.18.
	Scope []rules.ScopeField
	// Delivery is how OMP would supply the matched body at runtime.
	Delivery rules.RuntimeDelivery
	// Tier is the strongest native guarantee the CP0 record proves.
	Tier artifacts.EnforcementTier
	// Order is the record's position in the projected generation, ascending by
	// RecordID.
	Order int
}

// ShippedRuleReport is the shipped corpus's OMP rule projection plus the
// rule-delivery surfaces OMP cannot promise.
type ShippedRuleReport struct {
	// Ordering names the deterministic generation order.
	Ordering string
	Rules    []ShippedRule
	// Gaps are the CP0 rule-delivery surfaces the record leaves unproven.
	Gaps []harness.Capability
}

// ProjectShippedRules renders the shipped rule corpus for OMP. Authored bytes
// ship verbatim — OMP's rule files use the same `paths:` frontmatter, so
// nothing is re-emitted — while scope, delivery, and tier come from the CP0
// evidence through rules.Select. OMP 18.1.18 proved a pre-operation event but no
// context return, so no record reaches native-scope or hook-required; a missing
// row records unsupported without failing the projection.
func ProjectShippedRules(cat *artifacts.Catalog, m harness.CapabilityMatrix) (ShippedRuleReport, error) {
	if m.Harness != harness.KindOMP {
		return ShippedRuleReport{}, fmt.Errorf("omp: shipped rule projection needs the OMP capability record, got %q", m.Harness)
	}
	sources, err := ShippedRuleSources(cat)
	if err != nil {
		return ShippedRuleReport{}, err
	}
	return projectShippedRules(sources, m)
}

// ShippedRuleSources pairs every canonical path-scoped rule with its authored
// bytes. The rule projection and the runtime delivery consume the same pairs, so
// a rule cannot ship in one and be missing from the other.
func ShippedRuleSources(cat *artifacts.Catalog) ([]harness.RuleSource, error) {
	artifactsOfKind := cat.OfKind(artifacts.KindRule)
	sources := make([]harness.RuleSource, 0, len(artifactsOfKind))
	for _, a := range artifactsOfKind {
		record, err := rules.ParseShipped(a.Source, a.Body)
		if err != nil {
			return nil, fmt.Errorf("omp: parse %s: %w", a.ID, err)
		}
		sources = append(sources, harness.RuleSource{Record: record, Bytes: a.Body})
	}
	return sources, nil
}

func projectShippedRules(sources []harness.RuleSource, m harness.CapabilityMatrix) (ShippedRuleReport, error) {
	records := make([]rules.RuleRecord, 0, len(sources))
	for i, s := range sources {
		if s.Record.ID == "" {
			return ShippedRuleReport{}, fmt.Errorf("omp: rule source %d has no record identity", i)
		}
		if s.Record.Producer != rules.ProducerShipped {
			return ShippedRuleReport{}, fmt.Errorf("omp: %s is not a shipped rule", s.Record.ID)
		}
		if got := managedfile.Digest(s.Bytes); got != s.Record.SourceDigest {
			return ShippedRuleReport{}, fmt.Errorf("omp: project %s: source bytes digest %s does not match record digest %s", s.Record.ID, got, s.Record.SourceDigest)
		}
		records = append(records, s.Record)
	}
	if err := rules.Validate(records); err != nil {
		return ShippedRuleReport{}, err
	}

	ordered := make([]harness.RuleSource, len(sources))
	copy(ordered, sources)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Record.ID < ordered[j].Record.ID })

	ev := harness.RuleEvidence(m)
	report := ShippedRuleReport{
		Ordering: harness.RuleOrderingRecordID,
		Gaps:     harness.RuleGaps(m),
	}
	for i, s := range ordered {
		sel := rules.Select(s.Record, artifacts.TargetOMP, ev)
		report.Rules = append(report.Rules, ShippedRule{
			RecordID:       s.Record.ID,
			Class:          s.Record.Class,
			Source:         s.Record.Source,
			SourceDigest:   s.Record.SourceDigest,
			NativeResource: s.Record.Source,
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

// Card is one projected native pointer card in a repository's OMP project rule
// surface.
type Card struct {
	RecordID string
	Domain   string
	// Name is the card's file name inside the cards directory.
	Name         string
	Bytes        []byte
	SourceDigest string
	Digest       string
	Scope        []rules.ScopeField
	Delivery     rules.RuntimeDelivery
	Tier         artifacts.EnforcementTier
}

// ProjectCards is one repository's projected OMP card generation: the files the
// native project rule surface should hold, their content identity, and the CP0
// surfaces the projection cannot promise.
type ProjectCards struct {
	ProjectKey string
	// Base is the absolute, symlink-resolved repository (or worktree) root the
	// cards bind to.
	Base string
	// Dir is the absolute native cards directory under the resolved base.
	Dir   string
	Cards []Card
	// Instances are the CP2F1 bindings, aligned with Cards order: each
	// immutable record bound to this project key and the resolved base.
	Instances []rules.RuleInstance
	// Generation is the card set's content identity.
	Generation string
	// Gaps are the CP0 rule-delivery surfaces the projection cannot promise.
	Gaps []harness.Capability
}

// LoadProjectCards reads one repository's canonical wiki pointer cards from its
// selected state root and projects them onto the native OMP project rule
// surface. It writes nothing.
func LoadProjectCards(stateRoot, projectKey, base string, m harness.CapabilityMatrix) (ProjectCards, error) {
	if stateRoot == "" {
		return ProjectCards{}, fmt.Errorf("omp: project cards: empty state root")
	}
	rulesRoot := filepath.Join(stateRoot, "rules")
	records, err := rules.LoadWiki(rulesRoot, projectKey)
	if err != nil {
		return ProjectCards{}, err
	}
	sources := make([]harness.RuleSource, 0, len(records))
	for _, rec := range records {
		source := filepath.Join(rulesRoot, filepath.FromSlash(strings.TrimPrefix(rec.Source, "rules/")))
		data, err := os.ReadFile(source)
		if err != nil {
			return ProjectCards{}, fmt.Errorf("omp: read card %s: %w", rec.Source, err)
		}
		sources = append(sources, harness.RuleSource{Record: rec, Bytes: data})
	}
	return BuildProjectCards(sources, projectKey, base, m)
}

// BuildProjectCards projects bound wiki pointer card records onto the OMP
// project rule surface. It is offline and deterministic: the same cards produce
// identical bytes and an identical generation, and no native write happens
// here. Authored card bytes ship verbatim, because OMP's rule files use the
// same `paths:` frontmatter; scope, delivery, and tier come from the CP0
// evidence. Two projects' cards never share one directory — a native-name
// collision fails validation before any mutation.
func BuildProjectCards(sources []harness.RuleSource, projectKey, base string, m harness.CapabilityMatrix) (ProjectCards, error) {
	if m.Harness != harness.KindOMP {
		return ProjectCards{}, fmt.Errorf("omp: card projection needs the OMP capability record, got %q", m.Harness)
	}
	if projectKey == "" {
		return ProjectCards{}, fmt.Errorf("omp: card projection: empty project key")
	}
	if !filepath.IsAbs(base) {
		return ProjectCards{}, fmt.Errorf("omp: card projection: base %q is not absolute", base)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Clean(base))
	if err != nil {
		return ProjectCards{}, fmt.Errorf("omp: card projection: resolve base %q: %w", base, err)
	}
	cards := ProjectCards{
		ProjectKey: projectKey,
		Base:       resolved,
		Dir:        filepath.Join(resolved, filepath.FromSlash(ProjectRulesDir), CardsDirName),
		Gaps:       harness.RuleGaps(m),
	}

	ordered := make([]harness.RuleSource, len(sources))
	copy(ordered, sources)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Record.ID < ordered[j].Record.ID })

	records := make([]rules.RuleRecord, 0, len(ordered))
	for i, s := range ordered {
		if s.Record.ID == "" {
			return ProjectCards{}, fmt.Errorf("omp: card source %d has no record identity", i)
		}
		if s.Record.Producer != rules.ProducerWiki {
			return ProjectCards{}, fmt.Errorf("omp: %s is not a repository wiki card", s.Record.ID)
		}
		if got := managedfile.Digest(s.Bytes); got != s.Record.SourceDigest {
			return ProjectCards{}, fmt.Errorf("omp: project %s: source bytes digest %s does not match record digest %s", s.Record.ID, got, s.Record.SourceDigest)
		}
		records = append(records, s.Record)
	}
	if err := rules.Validate(records); err != nil {
		return ProjectCards{}, err
	}

	// CP2F1 binding: the immutable record gains one project key and one
	// symlink-resolved runtime base. Two worktrees of a repository share record
	// identity and source digest but not bases, and the native directory is the
	// resolved base so a symlinked checkout publishes to one location.
	if len(records) > 0 {
		instances, err := rules.BindAll(records, projectKey, base)
		if err != nil {
			return ProjectCards{}, err
		}
		cards.Instances = instances
	}
	ev := harness.RuleEvidence(m)
	for _, s := range ordered {
		domain := cardDomain(s.Record)
		if domain == "" || strings.Contains(domain, "/") || domain == "." || domain == ".." {
			return ProjectCards{}, fmt.Errorf("omp: card %s has no usable domain", s.Record.ID)
		}
		sel := rules.Select(s.Record, artifacts.TargetOMP, ev)
		cards.Cards = append(cards.Cards, Card{
			RecordID:     s.Record.ID,
			Domain:       domain,
			Name:         domain + ".md",
			Bytes:        s.Bytes,
			SourceDigest: s.Record.SourceDigest,
			Digest:       artifacts.ProjectionDigest(s.Bytes),
			Scope:        sel.Scope,
			Delivery:     sel.Delivery,
			Tier:         sel.Tier,
		})
	}
	cards.Generation = cardGeneration(cards.Cards)
	return cards, nil
}

// cardDomain reads a wiki record's domain from its producer-relative source,
// which rules.ParseWiki already validates as rules/wiki/<domain>.md.
func cardDomain(r rules.RuleRecord) string {
	return strings.TrimSuffix(strings.TrimPrefix(r.Source, "rules/wiki/"), ".md")
}

// cardGeneration is the card set's content identity: each card's file name and
// projection digest in name order, so identical cards always produce an
// identical generation and a change anywhere changes it.
func cardGeneration(cards []Card) string {
	h := sha256.New()
	for _, c := range cards {
		h.Write([]byte(c.Name))
		h.Write([]byte{0})
		h.Write([]byte(c.Digest))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Render writes the projected cards into dir. The caller owns dir; every card
// is written with the generated-tree identity's file mode.
func (c ProjectCards) Render(dir string) error {
	for _, card := range c.Cards {
		if err := safePackagePath(card.Name); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, card.Name), card.Bytes, 0o644); err != nil {
			return fmt.Errorf("omp: write card %s: %w", card.Name, err)
		}
	}
	return nil
}

// TreeDigest renders the cards into a scratch directory and digests the result
// with the same generated-tree identity the publication path checks, so a
// plan-time digest cannot drift from the published tree.
func (c ProjectCards) TreeDigest() (string, error) {
	dir, err := os.MkdirTemp("", "atomic-omp-cards-")
	if err != nil {
		return "", fmt.Errorf("omp: stage cards for digest: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := c.Render(dir); err != nil {
		return "", err
	}
	digest, _, err := managedfile.TreeDigest(dir)
	return digest, err
}

// CardState is one projected card's published state.
type CardState string

const (
	// CardApplied: the native file holds exactly the projected bytes.
	CardApplied CardState = "applied"
	// CardStale: the native file exists but no longer holds the projected
	// bytes, so canonical authority moved ahead of this surface.
	CardStale CardState = "stale"
	// CardAbsent: the projected card has no native file yet.
	CardAbsent CardState = "absent"
)

// CardStatus is one card's state on the native surface.
type CardStatus struct {
	RecordID        string                    `json:"record_id"`
	Domain          string                    `json:"domain"`
	State           CardState                 `json:"state"`
	Tier            artifacts.EnforcementTier `json:"tier"`
	SourceDigest    string                    `json:"source_digest"`
	ProjectedDigest string                    `json:"projected_digest"`
	PublishedDigest string                    `json:"published_digest,omitempty"`
}

// CardAssessment is a read-only comparison of one projection against what the
// native project rule surface currently holds.
type CardAssessment struct {
	Cards []CardStatus `json:"cards"`
	// Removed names published card files the projection no longer produces, so a
	// renamed or deleted domain is visible before any mutation.
	Removed []string `json:"removed,omitempty"`
	// Published reports whether the cards directory exists on disk.
	Published bool `json:"published"`
	// PublishedDigest is the current native tree digest; empty when absent.
	PublishedDigest string `json:"published_digest,omitempty"`
	// ProjectedDigest is the digest of the tree the projection would publish.
	ProjectedDigest string `json:"projected_digest"`
	// Fresh reports that every projected card is applied and no dropped card
	// remains, so publishing would be a no-op.
	Fresh bool `json:"fresh"`
}

// Assess compares the projection against the native project rule surface. It
// writes nothing.
func (c ProjectCards) Assess() (CardAssessment, error) {
	assessment := CardAssessment{}
	projectedDigest, err := c.TreeDigest()
	if err != nil {
		return CardAssessment{}, err
	}
	assessment.ProjectedDigest = projectedDigest

	published := map[string]string{}
	publishedDigest, entries, err := managedfile.TreeDigest(c.Dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return CardAssessment{}, err
	default:
		assessment.Published = true
		assessment.PublishedDigest = publishedDigest
		for _, e := range entries {
			if e.Digest != "" {
				published[e.Path] = e.Digest
			}
		}
	}

	fresh := true
	for _, card := range c.Cards {
		status := CardStatus{
			RecordID:        card.RecordID,
			Domain:          card.Domain,
			Tier:            card.Tier,
			SourceDigest:    card.SourceDigest,
			ProjectedDigest: card.Digest,
		}
		switch got, ok := published[card.Name]; {
		case !ok:
			status.State = CardAbsent
			fresh = false
		case got == card.Digest:
			status.State = CardApplied
			status.PublishedDigest = got
		default:
			status.State = CardStale
			status.PublishedDigest = got
			fresh = false
		}
		delete(published, card.Name)
		assessment.Cards = append(assessment.Cards, status)
	}
	for name := range published {
		assessment.Removed = append(assessment.Removed, name)
		fresh = false
	}
	sort.Strings(assessment.Removed)
	assessment.Fresh = fresh
	return assessment, nil
}

// ProjectRuleTarget is the ledger target one repository's OMP project rule
// surface is recorded under: the OMP kind with the project key as instance, so
// two worktrees of one repository share a target and differ by resource path.
// NativeRoot stays empty: a project key names several checkouts, so no single
// native root describes it, and the ownership row's resource path is the native
// location.
func ProjectRuleTarget(projectKey string) harness.Target {
	return harness.Target{Kind: harness.KindOMP, Instance: projectKey}
}

// CardPublishRequest is the caller's intent to converge one repository's wiki
// cards into the OMP project rule surface.
type CardPublishRequest struct {
	Home string
	// StateRoot is the repository's selected state directory, which holds the
	// canonical rules/wiki cards.
	StateRoot string
	// ProjectKey identifies the repository across worktrees.
	ProjectKey string
	// Base is the absolute repository (or worktree) root the cards bind to.
	Base string
	// OperationID names the journal and transaction directories. Empty derives a
	// unique id from the clock.
	OperationID string
	Now         func() time.Time
}

// CardPublishResult reports what the publication planned, committed, and
// observed.
type CardPublishResult struct {
	Target harness.Target `json:"target"`
	// Dir is the native cards directory.
	Dir        string         `json:"dir"`
	Status     harness.Status `json:"status"`
	Generation string         `json:"generation"`
	Cards      []CardStatus   `json:"cards"`
	Removed    []string       `json:"removed,omitempty"`
	// Applied reports that this operation published native bytes; false means
	// the surface was already converged.
	Applied bool `json:"applied"`
	// Fresh reports whether the surface holds the projection when the operation
	// returns. A refusal leaves it false; a publication, a converged no-op, and
	// an empty project all leave it true. Cards and Removed report the
	// pre-mutation comparison the operation acted on.
	Fresh       bool                 `json:"fresh"`
	JournalPath string               `json:"journal_path,omitempty"`
	Gaps        []harness.Capability `json:"gaps,omitempty"`
	Unproven    []PackageGap         `json:"unproven,omitempty"`
}

// PublishProjectCards converges one repository's wiki cards into the OMP
// project rule surface through the transaction engine: the lifecycle lock is
// held, unresolved journals are recovered oldest-first, the journal and backups
// precede the native write, the staged tree is validated, effective content is
// verified, and only then is the ownership row recorded with its generation and
// tier. A card set already applied is a no-op: no native byte, journal, backup,
// or staging is written.
//
// A published tree that is neither the projection nor the digest Atomic last
// recorded writing is a changed derivative: the operation refuses before
// mutating and reports conflicted, preserving the bytes for resolution.
func (a *Adapter) PublishProjectCards(req CardPublishRequest) (CardPublishResult, error) {
	var result CardPublishResult
	if req.Home == "" {
		return result, fmt.Errorf("omp: publish project cards: no home")
	}
	if req.Base == "" {
		return result, fmt.Errorf("omp: publish project cards: no base")
	}

	cards, err := LoadProjectCards(req.StateRoot, req.ProjectKey, req.Base, a.Capabilities())
	if err != nil {
		return result, err
	}
	target := ProjectRuleTarget(req.ProjectKey)
	resource := cards.Dir
	result = CardPublishResult{
		Target:     target,
		Dir:        cards.Dir,
		Generation: cards.Generation,
		Gaps:       cards.Gaps,
		Unproven:   packageGaps(),
	}

	assessment, err := cards.Assess()
	if err != nil {
		return result, err
	}
	result.Cards = assessment.Cards
	result.Removed = assessment.Removed

	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	operationID := req.OperationID
	if operationID == "" {
		operationID = fmt.Sprintf("omp-cards-%d", now().UnixNano())
	}

	lock, err := installstate.AcquireLock(req.Home, installstate.WriterIdentity{OperationID: operationID})
	if err != nil {
		return result, err
	}
	defer lock.Release()

	recovery, err := installstate.RecoverJournals(req.Home)
	if err != nil {
		return result, err
	}
	for _, action := range recovery {
		if action.Decision == installstate.DecisionConflict {
			return result, fmt.Errorf("omp: publish project cards %s: unresolved journal conflict at %s: %s", target.Key(), action.Path, action.Detail)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return result, err
	}
	row := installstate.Row{
		Target:     target.Key(),
		Resource:   resource,
		Consumer:   req.ProjectKey,
		Generation: cards.Generation,
		Tier:       string(Tier),
		Applied:    installstate.AppliedValue{Path: cards.Dir, Kind: managedfile.KindTree, Digest: assessment.ProjectedDigest},
	}

	if !assessment.Published && len(cards.Cards) == 0 {
		// Nothing was ever published and nothing is projected: no state to
		// create, so no directory, journal, or ledger row is written.
		result.Status = harness.StatusConverged
		result.Fresh = true
		return result, nil
	}

	if assessment.Published && assessment.PublishedDigest == assessment.ProjectedDigest {
		// Already applied: the observation is the verification, so the ownership
		// row is recorded without touching native bytes. This is also the
		// recovery path for an operation interrupted after publication but
		// before its ledger commit.
		if ledger.Upsert(row) {
			if err := ledger.Save(config.LedgerPath(req.Home)); err != nil {
				return result, err
			}
		}
		result.Status = harness.StatusConverged
		result.Fresh = true
		return result, nil
	}

	if assessment.Published {
		// The projected tree is pipeline-owned, so bytes Atomic cannot prove it
		// wrote — because no row was recorded, or the tree moved away from the
		// digest the row holds — are a changed derivative. The operation refuses
		// before mutating and preserves them for resolution.
		if recorded, ok := ledger.Find(target.Key(), resource); !ok || recorded.Applied.Digest == "" || recorded.Applied.Digest != assessment.PublishedDigest {
			result.Status = harness.StatusConflicted
			return result, fmt.Errorf("omp: publish project cards %s: %s holds bytes Atomic did not record writing; preserving it for resolution", target.Key(), cards.Dir)
		}
	}

	tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: []installstate.Mutation{{
		Unit:       cardsUnit,
		Resource:   resource,
		Target:     target.Key(),
		Consumer:   req.ProjectKey,
		Generation: cards.Generation,
		Tier:       string(Tier),
		Kind:       managedfile.KindTree,
		Path:       cards.Dir,
		Intended:   assessment.ProjectedDigest,
	}}})
	if err != nil {
		return result, err
	}
	tx.Ledger = ledger
	result.JournalPath = tx.JournalPath

	if _, err := tx.StageTree(cardsUnit, cards.Render); err != nil {
		return result, err
	}
	if err := tx.PublishTree(cardsUnit); err != nil {
		return result, err
	}
	if err := tx.Complete(); err != nil {
		return result, err
	}

	result.Applied = true
	result.Status = harness.StatusConverged
	result.Fresh = true
	return result, nil
}

// ProjectRuleStatus reports one repository's card surface without writing:
// the projection, what the native directory currently holds, and the CP0
// surfaces the projection cannot promise. It is the read-only seam a status
// command or doctor check consumes.
func (a *Adapter) ProjectRuleStatus(stateRoot, projectKey, base string) (CardAssessment, []harness.Capability, error) {
	cards, err := LoadProjectCards(stateRoot, projectKey, base, a.Capabilities())
	if err != nil {
		return CardAssessment{}, nil, err
	}
	assessment, err := cards.Assess()
	if err != nil {
		return CardAssessment{}, nil, err
	}
	return assessment, cards.Gaps, nil
}
