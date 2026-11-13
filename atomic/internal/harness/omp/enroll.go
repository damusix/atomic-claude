package omp

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Resource identity is the resource's own path: one home holds several profiles
// whose same-named files are different physical resources.

// PackageResource is the shared package tree every enrolled profile consumes.
func PackageResource(home string) string {
	return config.PackageRoot(home, string(harness.KindOMP))
}

// SteeringResource is the Atomic-owned block inside one profile's AGENTS.md.
func SteeringResource(root string) string { return SteeringPath(root) }

// ExtensionResource is one profile's extension module, the native artifact OMP
// loads; each profile records its own module as its own row.
func ExtensionResource(root string) string { return ExtensionPath(root) }

// extensionFile is the module as an owned file, so it takes the same
// replace-or-leave-unowned decision as an agent file.
func extensionFile(root string, module []byte) harness.AgentFile {
	return harness.AgentFile{Name: extensionUnit, Path: ExtensionPath(root), Data: module}
}

// Commit-unit names. Each is a single safe path segment, which the journal and
// staging paths require.
const (
	packageUnit   = "package"
	steeringUnit  = "agents-md"
	extensionUnit = "extension"
)

// Tier is the enforcement tier the package and steering carry: instruction-only
// until a runtime probe proves a native boundary.
const Tier = artifacts.EnforcementUnsupported

// EnrollRequest is the caller's intent to enroll one OMP profile.
type EnrollRequest struct {
	Home string
	// Profile is the profile to enroll; Root is required.
	Profile Profile
	// Deny carries the operator-supplied predicates the delivered module blocks,
	// each in the probed bash form; empty ships no deny.
	Deny []DenyPredicate
	// Decision is the caller's replace-or-leave-unowned choice for an agent file
	// Atomic never wrote; empty refuses.
	Decision installstate.Decision
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
	// Gaps are the capability rows the package cannot promise.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native package surfaces with no runtime observation.
	Unproven []PackageGap `json:"unproven,omitempty"`
	// Applied names the native resources this operation published; empty means
	// the target was already converged and nothing was written.
	Applied []string `json:"applied,omitempty"`
	// Retired names the agent files deleted because the corpus no longer ships them.
	Retired     []string           `json:"retired,omitempty"`
	JournalPath string             `json:"journal_path,omitempty"`
	Resources   []harness.Resource `json:"resources,omitempty"`
}

// Enroll publishes the selected generation's shared package, the profile's
// steering block, extension module, and agent files through the transaction engine.
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
	agents, err := harness.AgentFiles(req.Home, req.Profile.Root, harness.KindOMP, cat, harness.OMPAgent)
	if err != nil {
		return result, err
	}
	natives, err := nativeFiles(pkg, caps, req.Profile.Root)
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
	result.Unproven = append(result.Unproven, kindGaps(caps)...)

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

	consumers, err := harness.EnsureSharedGeneration(ledger, harness.KindOMP, "omp", target.Key(), PackageResource(req.Home), "package", pkg.Generation)
	if err != nil {
		return result, err
	}
	agentPlan, err := harness.PlanAgents(harness.AgentClaims(agents, ledger, target.Key()), ledger, target.Key(), harness.AgentDir(req.Profile.Root), req.Decision)
	if err != nil {
		return result, err
	}
	nativePlan, err := planNative(harness.AgentClaims(natives, ledger, target.Key()), ledger, target.Key(), req.Profile.Root, req.Decision)
	if err != nil {
		return result, err
	}
	extensionPlan, err := harness.PlanAgents(harness.AgentClaims([]harness.AgentFile{extensionFile(req.Profile.Root, module)}, ledger, target.Key()), ledger, target.Key(), filepath.Dir(extensionPath), req.Decision)
	if err != nil {
		return result, err
	}
	if blockers := slices.Concat(agentPlan.Blockers, nativePlan.Blockers, extensionPlan.Blockers); len(blockers) > 0 {
		return result, fmt.Errorf("omp: enroll %s: %s", target.Key(), strings.Join(blockers, "; "))
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

	type resource struct {
		id     string
		unit   string
		path   string
		kind   managedfile.Kind
		digest string
		data   []byte
	}
	intended := []resource{
		{id: PackageResource(req.Home), unit: packageUnit, path: packageRoot, kind: managedfile.KindTree, digest: treeDigest},
		{id: SteeringResource(req.Profile.Root), unit: steeringUnit, path: steeringPath, kind: managedfile.KindBlock, digest: blockDigest, data: block},
	}
	if len(extensionPlan.Left) == 0 {
		intended = append(intended, resource{id: ExtensionResource(req.Profile.Root), unit: extensionUnit, path: extensionPath, kind: managedfile.KindFile, digest: moduleDigest, data: module})
	}
	left := slices.Concat(agentPlan.Left, nativePlan.Left)
	for _, f := range slices.Concat(agents, natives) {
		if !slices.Contains(left, f.Path) {
			intended = append(intended, resource{id: f.Path, unit: f.Unit(), path: f.Path, kind: managedfile.KindFile, digest: managedfile.Digest(f.Data), data: f.Data})
		}
	}
	retire := slices.Concat(agentPlan.Retire, nativePlan.Retire, extensionPlan.Retire)
	staged := map[string][]byte{}
	for _, r := range intended {
		staged[r.unit] = r.data
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
		var shared []string
		if r.unit == packageUnit {
			shared = consumers
		}
		mutations = append(mutations, installstate.Mutation{
			Unit:       r.unit,
			Resource:   r.id,
			Target:     target.Key(),
			Consumer:   target.Key(),
			Consumers:  shared,
			Generation: pkg.Generation,
			Tier:       string(Tier),
			Kind:       r.kind,
			Path:       r.path,
			Intended:   r.digest,
		})
	}

	record := installstate.TargetRecord{
		Harness:    string(target.Kind),
		Instance:   target.Instance,
		NativeRoot: target.NativeRoot,
		Status:     string(harness.StatusConverged),
	}
	if ledger.UpsertTarget(record) {
		rowsChanged = true
	}

	// One shared package cannot hold two generations, so moving it records every
	// consumer's row in the same commit; a stale row is not a requirement.
	for _, consumer := range consumers {
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
	if err := harness.AssertSharedConsumers(ledger, "omp", packageRoot, pkg.Generation, consumers); err != nil {
		return result, err
	}
	if ledger.SetLeft(target.Key(), harness.AgentDir(req.Profile.Root), agentPlan.Left) {
		rowsChanged = true
	}
	if nativePlan.recordLeft(ledger, target.Key()) {
		rowsChanged = true
	}
	if ledger.SetLeft(target.Key(), filepath.Dir(extensionPath), extensionPlan.Left) {
		rowsChanged = true
	}

	if len(mutations) == 0 {
		result.Retired, err = retireOwned(ledger, retire, req.Profile.Root)
		if err != nil {
			return result, err
		}
		if rowsChanged || len(result.Retired) > 0 {
			if err := ledger.Save(config.LedgerPath(req.Home)); err != nil {
				return result, err
			}
		}
		result.Status = harness.StatusConverged
		return a.finish(req.Home, result)
	}

	tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: mutations, Targets: []installstate.TargetRecord{record}, Left: ledger.LeftRecordsOf(target.Key()), LeftSnapshot: true})
	if err != nil {
		return result, err
	}
	// Rows already verified by observation ride along in the transaction's one
	// durable ledger write.
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
		case managedfile.KindBlock, managedfile.KindFile:
			if _, err := tx.StageFile(m.Unit, staged[m.Unit], 0o644); err != nil {
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
	result.Retired, err = retireOwned(ledger, retire, req.Profile.Root)
	if err != nil {
		return result, err
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

// finish attaches the post-state resource report. A discovery failure leaves
// visibility unknown rather than reading as a failure to enroll.
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

// Claims returns the selected generation's read-only claims for one profile.
func (a *Adapter) Claims(home, root string) ([]harness.Claim, error) {
	cat, err := a.corpus()
	if err != nil {
		return nil, err
	}
	caps := a.Capabilities()
	pkg, err := BuildPackage(cat, caps, nil)
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
	target := harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}
	agents, err := harness.ProjectAgentClaims(home, target, cat, harness.OMPAgent)
	if err != nil {
		return nil, err
	}
	natives, err := nativeFiles(pkg, caps, root)
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}
	agents = append(agents, harness.AgentClaims(natives, ledger, target.Key())...)
	return append([]harness.Claim{
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
		harness.AgentClaims([]harness.AgentFile{extensionFile(root, module)}, ledger, target.Key())[0],
	}, agents...), nil
}

// Resources reports the physical resources Atomic owns for OMP. A discovered
// profile that can see the package is not a consumer; visibility is never a dependency.
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
