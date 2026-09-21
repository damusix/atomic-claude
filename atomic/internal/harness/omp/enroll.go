package omp

import (
	"fmt"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Resource identities. One physical resource carries one record per consuming
// target: the shared package tree every enrolled profile reads, and the
// profile-owned AGENTS.md block. Identity is the resource's own path, because a
// single home holds several profiles whose same-named files are different
// physical resources; a name alone would merge them.

// PackageResource is the shared package tree at
// ~/.atomic/packages/omp/atomic, the one generated directory every enrolled
// profile consumes.
func PackageResource(home string) string {
	return config.PackageRoot(home, string(harness.KindOMP))
}

// SteeringResource is the Atomic-owned block inside one profile's AGENTS.md.
func SteeringResource(root string) string { return SteeringPath(root) }

// ExtensionResource is one profile's discovered extension module, the native
// artifact OMP loads. It is profile-owned: two profiles under one home are two
// physical modules, and each is recorded as that profile's own row.
func ExtensionResource(root string) string { return ExtensionPath(root) }

// Commit-unit names. Each is a single safe path segment, which the journal and
// staging paths require.
const (
	packageUnit   = "package"
	steeringUnit  = "agents-md"
	extensionUnit = "extension"
)

// Tier is the enforcement tier the package and steering carry. Every OMP
// projection is instruction-only until a runtime checkpoint proves otherwise,
// so nothing here reads as a native boundary.
const Tier = artifacts.EnforcementUnsupported

// EnrollRequest is the caller's intent to enroll one OMP profile.
type EnrollRequest struct {
	Home string
	// Profile is the profile to enroll; Root is required.
	Profile Profile
	// Deny carries the exact machine predicates the profile's delivered module
	// blocks. An empty list ships no deny, which is the selected generation's
	// default: only an operator-supplied exact predicate can deny an operation,
	// and every predicate must be the CP0-exercised bash form.
	Deny []DenyPredicate
	// OperationID names the journal and transaction directories. Empty derives a
	// unique id from the clock.
	OperationID string
	Now         func() time.Time
}

// EnrollResult reports what enrollment planned, committed, and observed.
type EnrollResult struct {
	Target       harness.Target `json:"target"`
	Profile      Profile        `json:"profile"`
	Status       harness.Status `json:"status"`
	Generation   string         `json:"generation"`
	PackageRoot  string         `json:"package_root"`
	SteeringPath string         `json:"steering_path"`
	// ExtensionPath is the profile-owned module OMP discovers under the agent
	// root's extensions directory.
	ExtensionPath string `json:"extension_path"`
	// PackageFiles names the generated package tree's files in path order.
	PackageFiles []string `json:"package_files,omitempty"`
	// Gaps are the CP0 capability rows the package cannot promise.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native package surfaces with no CP0 observation.
	Unproven []PackageGap `json:"unproven,omitempty"`
	// Applied names the native resources this operation published; empty means
	// the target was already converged and nothing was written.
	Applied     []string           `json:"applied,omitempty"`
	JournalPath string             `json:"journal_path,omitempty"`
	Resources   []harness.Resource `json:"resources,omitempty"`
}

// Enroll publishes the selected generation's shared package, the profile's
// steering block, and the profile's discovered extension module through the
// transaction engine: the lifecycle lock is held, unresolved journals are
// recovered oldest-first, the journal and backups precede every native write,
// staged bytes are validated, effective content is verified, and only then are
// the ledger rows recorded with their generation and tier. A profile already
// converged on the same generation is a no-op: no native byte, journal, backup,
// or staging is written.
//
// The shared package is one physical resource every enrolled profile consumes,
// so moving it records each enrolled consumer's row in the same commit. That is
// why a consumer's recorded generation is never a reason to refuse: it is what
// Atomic last wrote, and this operation rewrites it. A row for a target the
// ledger does not enroll is the one genuine pin and refuses before any
// mutation, reporting both targets.
func (a *Adapter) Enroll(req EnrollRequest) (EnrollResult, error) {
	var result EnrollResult
	if req.Home == "" {
		return result, fmt.Errorf("omp: enroll: no home")
	}
	if req.Profile.Root == "" {
		return result, fmt.Errorf("omp: enroll: no profile root")
	}

	cat, err := a.corpus()
	if err != nil {
		return result, err
	}
	caps := a.Capabilities()
	pkg, err := BuildPackage(cat, caps, req.Deny)
	if err != nil {
		return result, err
	}
	module, err := pkg.ExtensionModule()
	if err != nil {
		return result, err
	}
	block, err := SteeringBlock(cat)
	if err != nil {
		return result, err
	}

	target := harness.Target{Kind: harness.KindOMP, Instance: req.Profile.Root, NativeRoot: req.Profile.Root}
	packageRoot := config.PackageRoot(req.Home, string(harness.KindOMP))
	steeringPath := SteeringPath(req.Profile.Root)
	extensionPath := ExtensionPath(req.Profile.Root)
	result = EnrollResult{
		Target:        target,
		Profile:       req.Profile,
		Generation:    pkg.Generation,
		PackageRoot:   packageRoot,
		SteeringPath:  steeringPath,
		ExtensionPath: extensionPath,
		PackageFiles:  pkg.Paths(),
		Gaps:          pkg.Gaps,
		Unproven:      pkg.Unproven,
	}

	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	operationID := req.OperationID
	if operationID == "" {
		operationID = fmt.Sprintf("omp-%d", now().UnixNano())
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
			return result, installstate.JournalConflictError(action.Path, action.Detail)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return result, err
	}

	if err := harness.EnsureSharedGeneration(ledger, "omp", target.Key(), PackageResource(req.Home), "package", pkg.Generation); err != nil {
		return result, err
	}

	treeDigest, err := pkg.TreeDigest()
	if err != nil {
		return result, err
	}
	blockDigest, err := SteeringDigest(block)
	if err != nil {
		return result, err
	}
	moduleDigest := artifacts.ProjectionDigest(module)

	intended := []struct {
		id     string
		unit   string
		path   string
		kind   managedfile.Kind
		digest string
	}{
		{id: PackageResource(req.Home), unit: packageUnit, path: packageRoot, kind: managedfile.KindTree, digest: treeDigest},
		{id: SteeringResource(req.Profile.Root), unit: steeringUnit, path: steeringPath, kind: managedfile.KindBlock, digest: blockDigest},
		{id: ExtensionResource(req.Profile.Root), unit: extensionUnit, path: extensionPath, kind: managedfile.KindFile, digest: moduleDigest},
	}

	var mutations []installstate.Mutation
	rowsChanged := false
	for _, r := range intended {
		obs, err := managedfile.Observe(r.path, r.kind)
		if err != nil {
			return result, err
		}
		row := installstate.Row{
			Target:     target.Key(),
			Resource:   r.id,
			Consumer:   target.Key(),
			Generation: pkg.Generation,
			Tier:       string(Tier),
			Applied:    installstate.AppliedValue{Path: r.path, Kind: r.kind, Digest: r.digest},
		}
		if obs.Digest != "" && obs.Digest == r.digest {
			// Already applied: the observation is the verification, so the
			// ownership row is recorded without touching native bytes.
			if ledger.Upsert(row) {
				rowsChanged = true
			}
			continue
		}
		if obs.Conflict == managedfile.ConflictMalformedBlock && managedfile.HasBlockTags(obs.Bytes) {
			return result, fmt.Errorf("omp: enroll %s: %s carries an ambiguous %s block; refusing to guess a boundary", target.Key(), r.path, managedfile.BlockOpen)
		}
		mutations = append(mutations, installstate.Mutation{
			Unit:       r.unit,
			Resource:   r.id,
			Target:     target.Key(),
			Consumer:   target.Key(),
			Generation: pkg.Generation,
			Tier:       string(Tier),
			Kind:       r.kind,
			Path:       r.path,
			Intended:   r.digest,
		})
	}

	if ledger.UpsertTarget(installstate.TargetRecord{
		Harness:    string(target.Kind),
		Instance:   target.Instance,
		NativeRoot: target.NativeRoot,
		Status:     string(harness.StatusConverged),
	}) {
		rowsChanged = true
	}

	// One physical package cannot hold two generations, and every enrolled
	// consumer of it requires the selected one: the running binary is the only
	// writer. The operation that moves the package therefore records every
	// consumer's row in the same commit, so no consumer's record describes bytes
	// that are gone and no later convergence sees a stale generation as a
	// requirement.
	for _, consumer := range harness.SharedConsumers(ledger, harness.KindOMP, packageRoot) {
		if consumer == target.Key() {
			continue
		}
		if ledger.Upsert(installstate.Row{
			Target:     consumer,
			Resource:   packageRoot,
			Consumer:   consumer,
			Generation: pkg.Generation,
			Tier:       string(Tier),
			Applied:    installstate.AppliedValue{Path: packageRoot, Kind: managedfile.KindTree, Digest: treeDigest},
		}) {
			rowsChanged = true
		}
	}

	if len(mutations) == 0 {
		if rowsChanged {
			if err := ledger.Save(config.LedgerPath(req.Home)); err != nil {
				return result, err
			}
		}
		result.Status = harness.StatusConverged
		return a.finish(req.Home, result)
	}

	tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: mutations})
	if err != nil {
		return result, err
	}
	// The transaction commits whatever its ledger holds, so the rows already
	// verified by observation ride along in the same durable write instead of a
	// second one.
	tx.Ledger = ledger
	result.JournalPath = tx.JournalPath

	for _, m := range mutations {
		switch m.Kind {
		case managedfile.KindTree:
			if _, err := tx.StageTree(m.Unit, pkg.Render); err != nil {
				return result, err
			}
			if err := tx.PublishTree(m.Unit); err != nil {
				return result, err
			}
		case managedfile.KindBlock:
			if _, err := tx.StageFile(m.Unit, block, 0o644); err != nil {
				return result, err
			}
			if err := tx.PublishFile(m.Unit); err != nil {
				return result, err
			}
		case managedfile.KindFile:
			if _, err := tx.StageFile(m.Unit, module, 0o644); err != nil {
				return result, err
			}
			if err := tx.PublishFile(m.Unit); err != nil {
				return result, err
			}
		default:
			return result, fmt.Errorf("omp: enroll %s: %s has no publication path for kind %s", target.Key(), m.Unit, m.Kind)
		}
		result.Applied = append(result.Applied, m.Path)
	}
	if err := tx.Complete(); err != nil {
		return result, err
	}
	if _, err := installstate.CleanupOperation(req.Home, tx.ID); err != nil {
		return result, err
	}

	result.Status = harness.StatusConverged
	return a.finish(req.Home, result)
}

// finish attaches the post-state resource report, separating consumers from
// visibility. Discovery is a read-only reporting step: when it fails, visibility
// is simply unknown and the committed enrollment is still reported, because a
// failure to read one more fact must not read as a failure to enroll.
func (a *Adapter) finish(home string, result EnrollResult) (EnrollResult, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return result, err
	}
	instances, err := a.Discover(home)
	if err != nil {
		instances = nil
	}
	result.Resources = harness.Resources(ledger, instances, harness.SharedRoots(home))
	return result, nil
}

// The shared-package generation refusal lives in harness.EnsureSharedGeneration,
// so every adapter that publishes a shared physical resource applies one rule.

// Claims returns the ownership claims the selected generation makes for one
// profile: the shared package tree, the profile-owned steering block, and the
// profile-owned extension module OMP discovers. It reads the corpus and the
// current bytes; it writes nothing.
func (a *Adapter) Claims(home, root string) ([]harness.Claim, error) {
	cat, err := a.corpus()
	if err != nil {
		return nil, err
	}
	pkg, err := BuildPackage(cat, a.Capabilities(), nil)
	if err != nil {
		return nil, err
	}
	treeDigest, err := pkg.TreeDigest()
	if err != nil {
		return nil, err
	}
	module, err := pkg.ExtensionModule()
	if err != nil {
		return nil, err
	}
	return []harness.Claim{
		{
			ID:             PackageResource(home),
			Kind:           managedfile.KindTree,
			Path:           PackageResource(home),
			SelectedDigest: treeDigest,
		},
		{
			ID:   SteeringResource(root),
			Kind: managedfile.KindBlock,
			Path: SteeringPath(root),
		},
		{
			ID:             ExtensionResource(root),
			Kind:           managedfile.KindFile,
			Path:           ExtensionPath(root),
			SelectedDigest: artifacts.ProjectionDigest(module),
		},
	}, nil
}

// Resources reports the physical resources Atomic owns for OMP, merging the
// ledger's per-target rows into one record per resource so consumers and
// visibility stay separate. An enrolled profile is a consumer; a discovered
// profile that can read the same physical package is merely able to see it, and
// visibility never becomes a dependency.
func (a *Adapter) Resources(home string) ([]harness.Resource, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}
	instances, err := a.Discover(home)
	if err != nil {
		return nil, err
	}
	return harness.Resources(ledger, instances, harness.SharedRoots(home)), nil
}
