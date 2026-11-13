package installstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ErrAdoptionRefused reports that adoption refused the observed state instead
// of guessing. The AdoptionResult carries the plan and blockers.
var ErrAdoptionRefused = errors.New("installstate: adoption refused")

// ErrAdoptionMarkFailed: the adoption committed but the legacy record could not
// be marked adopted; the next update writes the marker.
var ErrAdoptionMarkFailed = errors.New("installstate: mark legacy install adopted")

// JournalConflictError renders the message for a journal recovery cannot
// reconcile, naming the resolving verb so hand-deletion is not the only exit.
func JournalConflictError(path, detail string) error {
	return fmt.Errorf("%w: unresolved journal conflict at %s: %s; run `atomic harness recover` to reconcile it, or `atomic harness recover --rollback` to restore the pre-mutation bytes", ErrAdoptionRefused, path, detail)
}

// Decision is the caller's choice for an artifact whose ownership cannot be
// proven. Older non-block artifacts share one batch decision, overridable.
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
// the exact bytes the selected binary would write.
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
	Batch    []string             `json:"batch,omitempty"`
	Blockers []string             `json:"blockers,omitempty"`
	Recovery []RecoverySimulation `json:"recovery,omitempty"`
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
	// ImportMutable, when set, runs once under the lock after recovery proves
	// the plan ready. The harness layer supplies it because it owns those paths.
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

// PlanAdoption is the read-only dry-run: no lock, no writes. A journal with no
// single safe result yields StatusBlockedOnRecovery.
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

// Adopt is the real migration under the lifecycle lock. It refuses mixed,
// pending, corrupt, and ambiguous states with a report instead of guessing.
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

	// Importing any earlier would write mutable state a refused or conflicted
	// adoption never authorized.
	if req.ImportMutable != nil && plan.Classification.Legacy.Present() &&
		(plan.Status == StatusReady || plan.Status == StatusConverged) {
		imported, importErr := req.ImportMutable()
		if importErr != nil {
			return result, importErr
		}
		result.Imported = imported
	}

	if plan.Status == StatusConverged {
		// Converged still records rows; without them the next run reads legacy
		// evidence again and status, repair, and uninstall have nothing to act on.
		return result, finishAdoption(req, plan)
	}

	if err := applyAdoption(req, plan, operationID, &result); err != nil {
		return result, err
	}
	// Resources the plan did not have to write still carry verified ownership,
	// and recording them completes the adoption.
	return result, finishAdoption(req, plan)
}

// finishAdoption records the verified ownership, then marks a present legacy
// install record adopted so `atomic update` never adopts it twice.
func finishAdoption(req AdoptionRequest, plan AdoptionPlan) error {
	if err := recordVerifiedOwnership(req, plan); err != nil {
		return err
	}
	return MarkLegacyAdopted(req.Home)
}

// MarkLegacyAdopted marks a present, unmarked legacy install record adopted.
func MarkLegacyAdopted(home string) error {
	path := config.TOMLPath(home)
	cfg, _, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAdoptionMarkFailed, err)
	}
	if !claudeinstall.InstallRecordOf(cfg).Present() || cfg.Install.Adopted {
		return nil
	}
	cfg.Install.Adopted = true
	if err := config.WritePersist(path, cfg); err != nil {
		return fmt.Errorf("%w: %w", ErrAdoptionMarkFailed, err)
	}
	return nil
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
func applyAdoption(req AdoptionRequest, plan AdoptionPlan, operationID string, result *AdoptionResult) error {
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
		unit, err := UnitFor(r.ID)
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

	if err := keepRecoveryCopies(req, plan, operationID); err != nil {
		return err
	}

	tx, err := NewTransaction(req.Home, operationID, Plan{Mutations: mutations})
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
	// A target without a record is invisible to status, repair, and uninstall,
	// so the record rides in the same ledger write as the rows.
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
	// With the rows committed, a leftover journal is a dead operation every later
	// classify would parse. JournalPath stays as the operation's identity.
	if _, err := CleanupOperation(req.Home, tx.ID); err != nil {
		return err
	}
	return nil
}

// keepRecoveryCopies saves each file adoption replaces under
// ~/.atomic/backups/<operation>/; transaction backups are consumed on success.
func keepRecoveryCopies(req AdoptionRequest, plan AdoptionPlan, operationID string) error {
	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	for _, r := range plan.Resources {
		if r.Action != ActionReplace || r.Kind != managedfile.KindFile {
			continue
		}
		if _, err := os.Stat(r.Path); os.IsNotExist(err) {
			continue
		}
		unit, err := UnitFor(r.ID)
		if err != nil {
			return err
		}
		dest := filepath.Join(config.BackupStampDir(req.Home, operationID), unit)
		if _, err := managedfile.BackupFile(r.Path, dest, operationID, now()); err != nil {
			return err
		}
	}
	return nil
}

// recordVerifiedOwnership records rows for resources adoption did not mutate,
// re-observing digests so a row only records bytes that verify now.
func recordVerifiedOwnership(req AdoptionRequest, plan AdoptionPlan) error {
	ledger, err := LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return err
	}
	changed := false
	// A file the user keeps is theirs, so a row from Atomic's earlier write
	// would otherwise report it as drift forever.
	released := map[string]bool{}
	for _, res := range plan.Resources {
		if res.Action == ActionLeaveUnowned {
			released[res.ID] = true
		}
	}
	if len(released) > 0 {
		before := len(ledger.Rows)
		ledger.Rows = slices.DeleteFunc(ledger.Rows, func(r Row) bool { return r.Target == req.Target && released[r.Resource] })
		changed = len(ledger.Rows) != before
	}
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
	if recordLeft(ledger, req.Target, plan) {
		changed = true
	}
	if !changed {
		return nil
	}
	return ledger.Save(config.LedgerPath(req.Home))
}

// recordLeft makes the plan's left-unowned files the target's whole left set,
// so a replaced, recreated, or matching file stops being left.
func recordLeft(ledger *Ledger, target string, plan AdoptionPlan) bool {
	var paths []string
	for _, r := range plan.Resources {
		if r.Action == ActionLeaveUnowned {
			paths = append(paths, r.Path)
		}
	}
	if slices.Equal(ledger.LeftOf(target), paths) {
		return false
	}
	ledger.ForgetLeft(target)
	for _, p := range paths {
		ledger.Left = append(ledger.Left, LeftResource{Target: target, Path: p})
	}
	return true
}

// consumerOf is the consumer a row records, defaulting to the target itself.
func consumerOf(req AdoptionRequest) string {
	if req.Consumer != "" {
		return req.Consumer
	}
	return req.Target
}

// enrollmentRecord is the one place installstate splits the "harness:instance"
// key, so it lives beside the ledger that owns the format.
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

	if c.Legacy.Proposed {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("a proposed CLAUDE.md is pending at %s; merge it with `atomic prompt claude-merge` or delete it", claudeinstall.ProposedPath(req.Home)))
	}
	if c.Legacy.Merged {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("a CLAUDE.md.atomic-merged candidate is pending at %s; apply it over CLAUDE.md or delete it", claudeinstall.MergedPath(req.NativeRoot)))
	}
	if len(c.V2.Journals) > 0 {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("%d unresolved journal(s) must be recovered first (run `atomic harness recover`): %s", len(c.V2.Journals), strings.Join(c.V2.Journals, ", ")))
	}
	if len(c.V2.Orphans) > 0 {
		plan.Blockers = append(plan.Blockers, "unjournaled v2 remnant(s) require explicit resolution: "+strings.Join(c.V2.Orphans, ", "))
	}
	if c.State == StateMixed {
		blocker := fmt.Sprintf("legacy and v2 ownership evidence overlap or disagree at %s", req.NativeRoot)
		if len(c.Conflicts) > 0 {
			blocker += ": " + strings.Join(c.Conflicts, "; ")
		}
		plan.Blockers = append(plan.Blockers, blocker)
	}

	// The snapshot matters only while legacy state governs. A v2 block looks
	// legacy-shaped, so the classification, not the inventory, decides.
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
			// The ledger proves Atomic wrote these older bytes, so no batch
			// decision is needed to replace them.
			r.Action = ActionReplace
		case managedfile.EvidenceUnowned:
			switch {
			case decision == DecisionReplace:
				r.Action = ActionReplace
			case decision == DecisionLeaveUnowned, decision == "" && c.V2.isLeft(req.Target, claim.Path):
				r.Action = ActionLeaveUnowned
			default:
				r.Action = ActionLeaveUnowned
				plan.Batch = append(plan.Batch, claim.ID)
			}
		}
		plan.Resources = append(plan.Resources, r)
	}

	if len(plan.Batch) > 0 {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("%d older-version artifact(s) await a replace-or-leave-unowned decision (--replace or --leave-unowned): %s", len(plan.Batch), strings.Join(plan.Batch, ", ")))
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

// UnitFor converts a resource ID to a safe single-segment unit name, encoding
// disallowed bytes as ".hex" so distinct IDs cannot collide.
func UnitFor(id string) (string, error) {
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

// RecoverJournals reconciles every unresolved journal oldest-first under the
// caller's lock. A conflict decision means the caller must refuse to plan.
func RecoverJournals(home string) ([]RecoveryAction, error) {
	return recoverAll(home)
}

// RollbackJournals restores every unresolved journal's verified backups under
// the caller's lock; bytes changed after Atomic wrote them are a conflict.
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

// SimulateRecoveries is the write-free dry run of RecoverJournals. It returns
// the unsaved post-recovery ledger, and blocked means blocked_on_recovery.
func SimulateRecoveries(home string) ([]RecoverySimulation, *Ledger, bool, error) {
	return simulateJournals(home, false)
}

// SimulateRollbacks is the write-free dry run of RollbackJournals; blocked
// reports a journal that cannot be rolled back to one safe result.
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

// neutralizeWriteSeams turns every write seam of a recovery into a no-op so a
// simulated pass reports decisions without performing them.
func neutralizeWriteSeams(r *Recovery) {
	r.LedgerPath = ""
	r.JournalPath = ""
	r.RemoveStaging = func(string) error { return nil }
	r.MoveTreeFn = func(string, string) error { return nil }
	r.RestoreBackupFn = func(managedfile.BackupRecord, managedfile.Kind) error { return nil }
}
