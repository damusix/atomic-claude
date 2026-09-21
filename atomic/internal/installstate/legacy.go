package installstate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ErrAdoptionRefused reports that adoption refused the observed state instead
// of guessing. The accompanying AdoptionResult carries the explicit plan and
// blockers.
var ErrAdoptionRefused = errors.New("installstate: adoption refused")

// JournalConflictError renders the one message every lifecycle operation reports
// when recovery cannot reconcile an unresolved journal to a single safe result.
// It names the verb that resolves it, because hand-deleting the journal was
// otherwise the only documented exit.
func JournalConflictError(path, detail string) error {
	return fmt.Errorf("%w: unresolved journal conflict at %s: %s; run `atomic harness recover` to reconcile it, or `atomic harness recover --rollback` to restore the pre-mutation bytes", ErrAdoptionRefused, path, detail)
}

// Decision is the caller's per-resource choice for an artifact the selected
// generation cannot prove ownership of. Older-version non-block artifacts are
// batched for one replace-or-leave-unowned decision, overridable per resource.
type Decision string

const (
	// DecisionReplace writes the selected generation's bytes over the artifact.
	DecisionReplace Decision = "replace"
	// DecisionLeaveUnowned preserves the artifact byte-for-byte and records no
	// ownership.
	DecisionLeaveUnowned Decision = "leave-unowned"
)

// AdoptionAction is what adoption will do to one inventoried resource.
type AdoptionAction string

const (
	// ActionNone leaves an already-current resource alone, so repeat adoption of
	// the same generation is a no-op.
	ActionNone AdoptionAction = "none"
	// ActionAdopt writes the selected block into an already-owned region, or
	// records ownership of bytes that already match.
	ActionAdopt AdoptionAction = "adopt"
	// ActionRecreate recreates a listed-but-missing file — drift the caller
	// accepted by converging.
	ActionRecreate AdoptionAction = "recreate"
	// ActionReplace overwrites an unowned artifact under an explicit decision.
	ActionReplace AdoptionAction = "replace"
	// ActionLeaveUnowned preserves an unowned artifact untouched.
	ActionLeaveUnowned AdoptionAction = "leave-unowned"
	// ActionConflict is an ambiguous resource adoption never touches.
	ActionConflict AdoptionAction = "conflict"
)

// Artifact is one selected-generation resource adoption may publish. Data is
// the exact bytes the selected binary would write: whole-file bytes for a file
// resource, the full document carrying the managed block for a block resource.
type Artifact struct {
	ID   string           `json:"id"`
	Kind managedfile.Kind `json:"kind"`
	Path string           `json:"path"`
	Data []byte           `json:"-"`
}

// AdoptionResource is one resource in an adoption plan.
type AdoptionResource struct {
	ID       string               `json:"id"`
	Kind     managedfile.Kind     `json:"kind"`
	Path     string               `json:"path"`
	Evidence managedfile.Evidence `json:"evidence"`
	Listed   bool                 `json:"listed,omitempty"`
	Decision Decision             `json:"decision,omitempty"`
	Action   AdoptionAction       `json:"action"`
}

// RecoverySimulation is one unresolved journal's in-memory recovery preview.
type RecoverySimulation struct {
	Journal   string           `json:"journal"`
	Actions   []RecoveryAction `json:"actions,omitempty"`
	Conflicts []RecoveryAction `json:"conflicts,omitempty"`
}

// AdoptionStatus is the plan's disposition.
type AdoptionStatus string

const (
	// StatusReady means the plan is complete and may be applied.
	StatusReady AdoptionStatus = "ready"
	// StatusConverged means no mutation is needed: the same generation is
	// already installed.
	StatusConverged AdoptionStatus = "converged"
	// StatusBlocked means adoption refuses until an explicit resolution.
	StatusBlocked AdoptionStatus = "blocked"
	// StatusBlockedOnRecovery means an unresolved journal cannot be simulated to
	// one safe result, so no plan can be offered yet.
	StatusBlockedOnRecovery AdoptionStatus = "blocked_on_recovery"
)

// AdoptionPlan is the complete, read-only evidence and decision plan.
type AdoptionPlan struct {
	Status         AdoptionStatus     `json:"status"`
	Classification Classification     `json:"classification"`
	Resources      []AdoptionResource `json:"resources,omitempty"`
	// Batch names the resources awaiting a replace-or-leave-unowned decision.
	Batch          []string             `json:"batch,omitempty"`
	Blockers       []string             `json:"blockers,omitempty"`
	Recovery       []RecoverySimulation `json:"recovery,omitempty"`
	StateSelection string               `json:"state_selection,omitempty"`
}

// AdoptionRequest is the caller's explicit, per-resource adoption intent.
type AdoptionRequest struct {
	Home       string
	NativeRoot string
	// Target keys the ledger rows adoption records ("harness:instance").
	Target     string
	Consumer   string
	Generation string
	Tier       string
	// OperationID names the journal and transaction directories. Empty derives a
	// unique id from the clock.
	OperationID string
	Artifacts   []Artifact
	// BatchDecision applies to every unowned artifact the selected generation
	// cannot prove; Decisions overrides it per artifact ID.
	BatchDecision Decision
	Decisions     map[string]Decision
	// AcknowledgeSnapshot accepts that absent or corrupt historical restoration
	// evidence cannot be restored. Without it those states block adoption.
	AcknowledgeSnapshot bool
	// RepoRoot, when set, adopts the repository-state root and journals the
	// selection record as a dependency.
	RepoRoot string
	// ImportMutable, when set, runs once inside the locked section, after
	// recovery and after the post-recovery plan proved ready or converged. The
	// harness layer owns the legacy mutable locations, so it supplies the
	// closure; running it here keeps the one-time import under the lifecycle
	// lock and behind journal recovery instead of before either.
	ImportMutable func() ([]string, error)
	Now           func() time.Time
}

// AdoptionResult reports what adoption observed, planned, and committed.
type AdoptionResult struct {
	Plan     AdoptionPlan     `json:"plan"`
	Recovery []RecoveryAction `json:"recovery,omitempty"`
	Applied  []string         `json:"applied,omitempty"`
	// JournalPath is empty when the operation needed no journal because nothing
	// had to change.
	JournalPath string `json:"journal_path,omitempty"`
	// Imported names the authoritative mutable files a one-time import created.
	Imported []string `json:"imported,omitempty"`
}

// PlanAdoption is the read-only dry-run: it classifies the observed state,
// simulates unresolved journals in memory, and reports an advisory plan. It
// opens no lock, writes no journal, backup, staging, selection, or cleanup, and
// leaves the filesystem byte-identical. A journal whose safe result depends on
// a later edit or conflict yields StatusBlockedOnRecovery.
func PlanAdoption(req AdoptionRequest) (AdoptionPlan, error) {
	c, err := Classify(ClassifyRequest{Home: req.Home, NativeRoot: req.NativeRoot, Target: req.Target, Claims: claimsFor(req.Artifacts)})
	if err != nil {
		return AdoptionPlan{}, err
	}

	plan := buildPlan(req, c)

	if len(c.V2.Journals) > 0 {
		sims, _, blocked, err := SimulateRecoveries(req.Home)
		if err != nil {
			return AdoptionPlan{}, err
		}
		if blocked {
			plan.Recovery = sims
			plan.Status = StatusBlockedOnRecovery
			plan.Blockers = append(plan.Blockers, "an unresolved journal cannot be recovered to one safe result without an unavailable observation or a later edit")
		} else {
			// Every journal simulated to one safe result: show the advisory
			// post-recovery plan the caller would reach after recovery runs.
			plan = buildPlan(req, c.WithJournalsRecovered())
			plan.Recovery = sims
		}
	}
	return plan, nil
}

// Adopt is the real migration: it acquires the lifecycle lock, refuses newer
// schemas, recovers unresolved journals oldest-first, re-observes, writes a
// durable journal and backups before the first mutation, publishes and verifies
// each unit, and only then commits ledger rows. It refuses mixed, pending,
// corrupt, and ambiguous states with an explicit report instead of guessing.
func Adopt(req AdoptionRequest) (AdoptionResult, error) {
	var result AdoptionResult
	if req.Home == "" {
		return result, fmt.Errorf("installstate: adopt: no home")
	}
	if req.Target == "" {
		return result, fmt.Errorf("installstate: adopt: no target key")
	}
	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	operationID := req.OperationID
	if operationID == "" {
		operationID = fmt.Sprintf("adopt-%d", now().UnixNano())
	}

	lock, err := AcquireLock(req.Home, WriterIdentity{OperationID: operationID})
	if err != nil {
		return result, err
	}
	defer lock.Release()

	if _, err := LoadLedger(config.LedgerPath(req.Home)); err != nil {
		return result, err
	}

	recovery, err := recoverAll(req.Home)
	if err != nil {
		return result, err
	}
	result.Recovery = recovery
	for _, action := range recovery {
		if action.Decision == DecisionConflict {
			plan, planErr := PlanAdoption(req)
			if planErr != nil {
				return result, planErr
			}
			result.Plan = plan
			return result, JournalConflictError(action.Path, action.Detail)
		}
	}

	plan, err := planAfterRecovery(req)
	if err != nil {
		return result, err
	}
	result.Plan = plan

	if plan.Status == StatusBlocked {
		return result, fmt.Errorf("%w: %s", ErrAdoptionRefused, strings.Join(plan.Blockers, "; "))
	}

	// The one-time mutable import runs only now: the lock is held, recovery has
	// reconciled every journal, and the post-recovery plan is ready or already
	// converged. Importing before either would write a mutable authority a
	// refused or conflicted adoption never authorized.
	if req.ImportMutable != nil && plan.Classification.Legacy.Present() &&
		(plan.Status == StatusReady || plan.Status == StatusConverged) {
		imported, importErr := req.ImportMutable()
		if importErr != nil {
			return result, importErr
		}
		result.Imported = imported
	}

	if plan.Status == StatusConverged {
		// Already converged is still an adoption: the operation ran to record the
		// target and the ownership its verified bytes prove. Without the rows the
		// next classification would read the target as legacy evidence all over
		// again, and status, repair, and uninstall would have nothing to act on.
		return result, recordVerifiedOwnership(req, plan)
	}

	guard, err := adoptStateRoot(req)
	if err != nil {
		return result, err
	}

	if err := applyAdoption(req, plan, operationID, guard, &result); err != nil {
		if guard != nil {
			if rollbackErr := guard.rollback(); rollbackErr != nil {
				return result, fmt.Errorf("%w (state-root rollback: %v)", err, rollbackErr)
			}
		}
		return result, err
	}
	// Resources the plan did not have to write still carry verified ownership —
	// bytes already matching the selected generation, or a parseable block the
	// journal never had to publish. Recording them completes the adoption; it is
	// a no-op for the units applyAdoption already committed.
	return result, recordVerifiedOwnership(req, plan)
}

// planAfterRecovery classifies the state after recovery has run and builds a
// plan from it. Recovery already ran, so no simulation is attached.
func planAfterRecovery(req AdoptionRequest) (AdoptionPlan, error) {
	c, err := Classify(ClassifyRequest{Home: req.Home, NativeRoot: req.NativeRoot, Target: req.Target, Claims: claimsFor(req.Artifacts)})
	if err != nil {
		return AdoptionPlan{}, err
	}
	return buildPlan(req, c), nil
}

// applyAdoption stages, publishes, and ledger-commits every mutating resource.
func applyAdoption(req AdoptionRequest, plan AdoptionPlan, operationID string, guard *selectionGuard, result *AdoptionResult) error {
	var mutations []Mutation
	selected := map[string][]byte{}
	for _, a := range req.Artifacts {
		selected[a.ID] = a.Data
	}
	for _, r := range plan.Resources {
		if r.Action != ActionAdopt && r.Action != ActionRecreate && r.Action != ActionReplace {
			continue
		}
		data := selected[r.ID]
		digest, err := managedfile.DigestResourceBytes(data, r.Kind)
		if err != nil {
			return err
		}
		unit, err := unitFor(r.ID)
		if err != nil {
			return err
		}
		mutations = append(mutations, Mutation{
			Unit:       unit,
			Resource:   r.ID,
			Target:     req.Target,
			Consumer:   consumerOf(req),
			Generation: req.Generation,
			Tier:       req.Tier,
			Kind:       r.Kind,
			Path:       r.Path,
			Intended:   digest,
		})
	}

	deps := []string{}
	if guard != nil && guard.recordPath != "" {
		deps = append(deps, guard.recordPath)
	}

	tx, err := NewTransaction(req.Home, operationID, Plan{Mutations: mutations, SelectionDependencies: deps})
	if err != nil {
		return err
	}
	result.JournalPath = tx.JournalPath

	for _, m := range mutations {
		if _, err := tx.StageFile(m.Unit, selected[m.Resource], 0o644); err != nil {
			return err
		}
		if err := tx.PublishFile(m.Unit); err != nil {
			return err
		}
		result.Applied = append(result.Applied, m.Path)
	}
	// The enrollment record rides in the same durable ledger write as the rows
	// verification just authorized: an adopted target is enrolled, and a target
	// without a record is invisible to status, repair, and uninstall.
	if record, ok := enrollmentRecord(req.Target, req.NativeRoot); ok {
		tx.Ledger.UpsertTarget(record)
	}
	if err := tx.Complete(); err != nil {
		return err
	}
	if len(mutations) == 0 {
		result.JournalPath = ""
		return nil
	}
	// The journal and its transaction tree are consumed: the rows are committed,
	// so leaving them behind only makes every later classify and status parse a
	// dead operation. JournalPath stays as the operation's identity.
	if _, err := CleanupOperation(req.Home, tx.ID); err != nil {
		return err
	}
	return nil
}

// recordVerifiedOwnership writes the enrollment record and the ownership rows
// for every resource an adoption did not have to mutate: bytes already matching
// the selected generation, or a parseable block the journal never published. The
// digests are re-observed, so a row can only record bytes that verify now. It is
// the converged-path counterpart to applyAdoption, and a no-op for units the
// journal already committed.
func recordVerifiedOwnership(req AdoptionRequest, plan AdoptionPlan) error {
	ledger, err := LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return err
	}
	changed := false
	for _, res := range plan.Resources {
		if res.Evidence != managedfile.EvidenceSelection && res.Evidence != managedfile.EvidenceBlock {
			continue
		}
		obs, err := managedfile.Observe(res.Path, res.Kind)
		if err != nil {
			return err
		}
		if obs.Digest == "" {
			continue
		}
		if ledger.Upsert(Row{
			Target:     req.Target,
			Resource:   res.ID,
			Consumer:   consumerOf(req),
			Generation: req.Generation,
			Tier:       req.Tier,
			Applied:    AppliedValue{Path: res.Path, Kind: res.Kind, Digest: obs.Digest},
		}) {
			changed = true
		}
	}
	if record, ok := enrollmentRecord(req.Target, req.NativeRoot); ok && ledger.UpsertTarget(record) {
		changed = true
	}
	if !changed {
		return nil
	}
	return ledger.Save(config.LedgerPath(req.Home))
}

// consumerOf is the consumer a row records: the explicit consumer when the
// caller named one, else the target itself, which is what an enrolled instance
// consumes.
func consumerOf(req AdoptionRequest) string {
	if req.Consumer != "" {
		return req.Consumer
	}
	return req.Target
}

// enrollmentRecord maps a target key and native root onto the ledger's enrolled
// record. It is the one place installstate spells the "harness:instance" pair
// apart, which is why it lives beside the ledger that owns the format.
func enrollmentRecord(key, nativeRoot string) (TargetRecord, bool) {
	harness, instance, ok := strings.Cut(key, ":")
	if !ok || harness == "" || instance == "" {
		return TargetRecord{}, false
	}
	return TargetRecord{Harness: harness, Instance: instance, NativeRoot: nativeRoot, Status: "converged"}, true
}

// buildPlan derives the per-resource plan and blockers from one classification.
func buildPlan(req AdoptionRequest, c Classification) AdoptionPlan {
	plan := AdoptionPlan{Classification: c, Status: StatusReady}
	if req.RepoRoot != "" {
		plan.StateSelection = config.StateLocationRecordPath(req.RepoRoot)
	}

	if c.Legacy.Proposed {
		plan.Blockers = append(plan.Blockers, "a proposed CLAUDE.md is pending; finish, discard, or explicitly supersede it")
	}
	if c.Legacy.Merged {
		plan.Blockers = append(plan.Blockers, "a CLAUDE.md.atomic-merged candidate is pending")
	}
	if c.V2.Journals != nil && len(c.V2.Journals) > 0 {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("%d unresolved journal(s) must be recovered first", len(c.V2.Journals)))
	}
	if len(c.V2.Orphans) > 0 {
		plan.Blockers = append(plan.Blockers, "unjournaled v2 remnant(s) require explicit resolution: "+strings.Join(c.V2.Orphans, ", "))
	}
	if c.State == StateMixed {
		plan.Blockers = append(plan.Blockers, "legacy and v2 ownership evidence overlap or disagree")
	}

	// The snapshot guards a legacy restoration, so it is decisive only while a
	// legacy state governs: a fresh install has nothing to restore, and a v2
	// install has already migrated. The managed block a v2 install writes is
	// itself legacy-shaped evidence, so the classification — not the raw
	// inventory — decides whether the limit applies.
	switch c.State {
	case StateLegacyComplete, StateLegacyPartial, StateMixed:
		switch c.Legacy.Snapshot.State {
		case claudeinstall.SnapshotCorrupt:
			if !req.AcknowledgeSnapshot {
				plan.Blockers = append(plan.Blockers, "the legacy pre-install snapshot is corrupt; acknowledge that restoration evidence is unusable before adoption")
			}
		case claudeinstall.SnapshotMissing:
			if !req.AcknowledgeSnapshot {
				plan.Blockers = append(plan.Blockers, "no historical restoration evidence exists; acknowledge that limit before adoption")
			}
		}
	}

	selected := map[string]Artifact{}
	for _, a := range req.Artifacts {
		selected[a.ID] = a
	}

	for _, res := range c.Resources {
		claim := res.Assessment.Claim
		r := AdoptionResource{ID: claim.ID, Kind: claim.Kind, Path: claim.Path, Evidence: res.Assessment.Evidence, Listed: res.Listed}
		decision := req.Decisions[claim.ID]
		if decision == "" {
			decision = req.BatchDecision
		}
		r.Decision = decision

		switch res.Assessment.Evidence {
		case managedfile.EvidenceConflict:
			r.Action = ActionConflict
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s carries an ambiguous managed block", claim.Path))
		case managedfile.EvidenceSelection:
			r.Action = ActionNone
		case managedfile.EvidenceBlock:
			r.Action = ActionAdopt
		case managedfile.EvidenceNoBlock:
			r.Action = ActionAdopt
		case managedfile.EvidenceMissing:
			r.Action = ActionRecreate
		case managedfile.EvidenceLedger:
			// The ledger records Atomic's own write of these bytes at an older
			// generation. Ownership is proven, so the resource is replaced with
			// the selected generation without a batch decision.
			r.Action = ActionReplace
		case managedfile.EvidenceUnowned:
			switch decision {
			case DecisionReplace:
				r.Action = ActionReplace
			case DecisionLeaveUnowned:
				r.Action = ActionLeaveUnowned
			default:
				r.Action = ActionLeaveUnowned
				plan.Batch = append(plan.Batch, claim.ID)
			}
		}
		plan.Resources = append(plan.Resources, r)
	}

	if len(plan.Batch) > 0 {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("%d older-version artifact(s) await a replace-or-leave-unowned decision: %s", len(plan.Batch), strings.Join(plan.Batch, ", ")))
	}

	switch {
	case len(plan.Blockers) > 0:
		plan.Status = StatusBlocked
	case adoptNeeded(plan):
		plan.Status = StatusReady
	default:
		plan.Status = StatusConverged
	}
	return plan
}

// adoptNeeded reports whether any resource needs a mutation.
func adoptNeeded(plan AdoptionPlan) bool {
	for _, r := range plan.Resources {
		switch r.Action {
		case ActionAdopt, ActionRecreate, ActionReplace:
			return true
		}
	}
	return false
}

// claimsFor turns adoption artifacts into ownership claims for classification.
func claimsFor(artifacts []Artifact) []managedfile.Claim {
	out := make([]managedfile.Claim, 0, len(artifacts))
	for _, a := range artifacts {
		claim := managedfile.Claim{ID: a.ID, Kind: a.Kind, Path: a.Path}
		if digest, err := managedfile.DigestResourceBytes(a.Data, a.Kind); err == nil {
			claim.SelectedDigest = digest
		}
		out = append(out, claim)
	}
	return out
}

// unitFor converts a resource ID to the safe single-segment commit-unit name
// the journal, staging, and backup paths require. Disallowed bytes are encoded
// as ".hex" so distinct IDs cannot collide.
func unitFor(id string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, ".%02x", c)
		}
	}
	unit := b.String()
	if unit == "" || unit == "." || unit == ".." {
		return "", fmt.Errorf("installstate: resource id %q has no safe commit-unit name", id)
	}
	return unit, nil
}

// RecoverJournals reconciles every unresolved journal oldest-first. It returns
// the decisions it made; a conflict decision means the caller must refuse to
// plan rather than mutate around bytes recovery could not reconcile. It is the
// entry point an adapter's lifecycle operation runs before planning any
// mutation, and it acquires no lock of its own: the operation holds one.
func RecoverJournals(home string) ([]RecoveryAction, error) {
	return recoverAll(home)
}

// RollbackJournals reconciles every unresolved journal oldest-first by
// restoring its digest-verified transaction backups — the explicit rollback
// alternative to RecoverJournals' roll-forward. A unit whose native bytes
// changed after Atomic wrote them is a conflict, never an overwrite. It is what
// `atomic harness recover --rollback` runs, and like RecoverJournals it acquires
// no lock of its own: the operation holds one.
func RollbackJournals(home string) ([]RecoveryAction, error) {
	paths, err := JournalPaths(config.JournalsDir(home))
	if err != nil {
		return nil, err
	}
	var actions []RecoveryAction
	for _, path := range paths {
		j, err := LoadJournal(path)
		if err != nil {
			return actions, err
		}
		if j.Completed {
			continue
		}
		ledger, err := LoadLedger(config.LedgerPath(home))
		if err != nil {
			return actions, err
		}
		r := NewRecovery(home, j, ledger)
		res, err := r.Rollback()
		if err != nil {
			return actions, err
		}
		actions = append(actions, res.Actions...)
		if len(res.Conflicts) > 0 {
			return actions, nil
		}
		if err := r.ConsumeJournal(path, res); err != nil {
			return actions, err
		}
		if _, err := CleanupOperation(home, j.OperationID); err != nil {
			return actions, err
		}
	}
	return actions, nil
}

// recoverAll reconciles every unresolved journal oldest-first. It returns the
// decisions it made; a conflict decision stops the caller from planning.
func recoverAll(home string) ([]RecoveryAction, error) {
	paths, err := JournalPaths(config.JournalsDir(home))
	if err != nil {
		return nil, err
	}
	var actions []RecoveryAction
	for _, path := range paths {
		j, err := LoadJournal(path)
		if err != nil {
			return actions, err
		}
		if j.Completed {
			continue
		}
		ledger, err := LoadLedger(config.LedgerPath(home))
		if err != nil {
			return actions, err
		}
		r := NewRecovery(home, j, ledger)
		res, err := r.Recover()
		if err != nil {
			return actions, err
		}
		actions = append(actions, res.Actions...)
		if len(res.Conflicts) > 0 {
			return actions, nil
		}
		if err := r.ConsumeJournal(path, res); err != nil {
			return actions, err
		}
		if _, err := CleanupOperation(home, j.OperationID); err != nil {
			return actions, err
		}
	}
	return actions, nil
}

// SimulateRecoveries previews every unresolved journal in memory, rolling
// forward as RecoverJournals would. It opens no lock and neutralizes every write
// seam — staging removal, the interrupted tree publication's rename, and the
// transaction backup restore — so the filesystem stays byte-identical. It is the
// dry-run counterpart of RecoverJournals: blocked reports that a journal cannot
// be reconciled to one safe result, which is what a plan reports as
// blocked_on_recovery.
//
// The returned ledger is the simulated post-recovery view: the rows the
// simulated decisions would commit, threaded in memory and never saved. A
// caller that offers an advisory plan plans against that view, so applied bytes
// the journal proves but the ledger has not yet recorded are visible exactly as
// a real recovery would make them.
func SimulateRecoveries(home string) ([]RecoverySimulation, *Ledger, bool, error) {
	return simulateJournals(home, false)
}

// SimulateRollbacks previews the --rollback alternative in memory, with the same
// neutralized seams: the decisions RollbackJournals would make — restoring a
// digest-verified transaction backup, or refusing to overwrite a later edit —
// are reported without a byte being written. blocked reports a journal that
// cannot be rolled back to one safe result.
func SimulateRollbacks(home string) ([]RecoverySimulation, bool, error) {
	sims, _, blocked, err := simulateJournals(home, true)
	return sims, blocked, err
}

// simulateJournals is the shared in-memory preview: oldest-first over every
// unresolved journal, deciding each one without performing it.
func simulateJournals(home string, rollback bool) ([]RecoverySimulation, *Ledger, bool, error) {
	paths, err := JournalPaths(config.JournalsDir(home))
	if err != nil {
		return nil, nil, false, err
	}
	// One ledger carries every journal's simulated commit forward: real recovery
	// saves after each journal, so a later journal observes the earlier rows.
	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, nil, false, err
	}
	var sims []RecoverySimulation
	blocked := false
	for _, path := range paths {
		j, err := LoadJournal(path)
		if err != nil {
			return nil, nil, false, err
		}
		if j.Completed {
			continue
		}
		r := NewRecovery(home, j, ledger)
		neutralizeWriteSeams(r)
		var res RecoveryResult
		if rollback {
			res, err = r.Rollback()
		} else {
			res, err = r.Recover()
		}
		if err != nil {
			return nil, nil, false, err
		}
		sim := RecoverySimulation{Journal: path, Actions: res.Actions, Conflicts: res.Conflicts}
		sims = append(sims, sim)
		if len(res.Conflicts) > 0 {
			blocked = true
		}
	}
	return sims, ledger, blocked, nil
}

// neutralizeWriteSeams strips every write seam from a recovery so a simulated
// pass reports the decisions it would make without performing one: staging
// removal, the interrupted publication's tree move, and the transaction backup
// restore all become no-ops, and neither the ledger nor the journal is saved.
func neutralizeWriteSeams(r *Recovery) {
	r.LedgerPath = ""
	r.JournalPath = ""
	r.RemoveStaging = func(string) error { return nil }
	r.MoveTreeFn = func(string, string) error { return nil }
	r.RestoreBackupFn = func(managedfile.BackupRecord, managedfile.Kind) error { return nil }
}

// selectionGuard remembers the state-root selection a failed adoption must
// roll back, and refuses to roll back once a later write changed the record.
type selectionGuard struct {
	repoRoot   string
	recordPath string
	record     []byte
	prior      config.StateSelection
	hadPrior   bool
}

// adoptStateRoot persists the repository-state selection before resolution
// switches, returning the guard that can undo it.
func adoptStateRoot(req AdoptionRequest) (*selectionGuard, error) {
	if req.RepoRoot == "" {
		return nil, nil
	}
	prior, hadPrior, err := config.ReadStateSelection(req.RepoRoot)
	if err != nil {
		return nil, err
	}
	if _, err := config.AdoptStateRoot(req.RepoRoot); err != nil {
		return nil, err
	}
	recordPath := config.StateLocationRecordPath(req.RepoRoot)
	record, err := os.ReadFile(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &selectionGuard{repoRoot: req.RepoRoot, recordPath: recordPath, record: record, prior: prior, hadPrior: hadPrior}, nil
}

// rollback restores the prior selection, or withdraws the new record when there
// was none. A record that no longer holds the bytes this guard wrote means a
// later state write used it, so rollback refuses rather than clobbering.
func (g *selectionGuard) rollback() error {
	current, err := os.ReadFile(g.recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !bytes.Equal(current, g.record) {
		return fmt.Errorf("installstate: state selection %s changed after adoption began; refusing to roll back", g.recordPath)
	}
	if g.hadPrior {
		_, err := config.PersistStateSelection(g.repoRoot, g.prior.StateDir)
		return err
	}
	return config.WithdrawStateSelection(g.repoRoot, g.record)
}
