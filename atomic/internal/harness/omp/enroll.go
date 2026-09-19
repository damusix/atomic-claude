package omp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
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

// Commit-unit names. Each is a single safe path segment, which the journal and
// staging paths require.
const (
	packageUnit  = "package"
	steeringUnit = "agents-md"
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

// Enroll publishes the selected generation's shared package and the profile's
// steering block through the transaction engine: the lifecycle lock is held,
// unresolved journals are recovered oldest-first, the journal and backups
// precede every native write, staged bytes are validated, effective content is
// verified, and only then are the ledger rows recorded with their generation
// and tier. A profile already converged on the same generation is a no-op: no
// native byte, journal, backup, or staging is written.
//
// An enrolled profile that requires a different generation of the shared
// package refuses before any mutation, reporting both targets: one physical
// package cannot hold two generations.
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
	pkg, err := BuildPackage(cat, caps)
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
	result = EnrollResult{
		Target:       target,
		Profile:      req.Profile,
		Generation:   pkg.Generation,
		PackageRoot:  packageRoot,
		SteeringPath: steeringPath,
		PackageFiles: pkg.Paths(),
		Gaps:         pkg.Gaps,
		Unproven:     pkg.Unproven,
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
			return result, fmt.Errorf("omp: enroll %s: unresolved journal conflict at %s: %s", target.Key(), action.Path, action.Detail)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return result, err
	}

	if err := ensureCompatible(ledger, target.Key(), PackageResource(req.Home), pkg.Generation); err != nil {
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

	intended := []struct {
		id     string
		unit   string
		path   string
		kind   managedfile.Kind
		digest string
	}{
		{id: PackageResource(req.Home), unit: packageUnit, path: packageRoot, kind: managedfile.KindTree, digest: treeDigest},
		{id: SteeringResource(req.Profile.Root), unit: steeringUnit, path: steeringPath, kind: managedfile.KindBlock, digest: blockDigest},
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
		default:
			return result, fmt.Errorf("omp: enroll %s: %s has no publication path for kind %s", target.Key(), m.Unit, m.Kind)
		}
		result.Applied = append(result.Applied, m.Path)
	}
	if err := tx.Complete(); err != nil {
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
	result.Resources = resourcesFor(ledger, instances, PackageResource(home))
	return result, nil
}

// ensureCompatible refuses when another enrolled profile's recorded generation
// of the shared package is not the one this plan publishes. One physical
// package cannot hold two generations, so a plan that moves it must move every
// consumer with it; converging one profile while another records a different
// generation would leave that profile's ledger row describing bytes that are no
// longer there. The refusal happens before any mutation — no staging, journal,
// backup, or native write precedes it — and names both targets so the operator
// converges the shared package as one operation.
func ensureCompatible(ledger *installstate.Ledger, target, resource, generation string) error {
	var others []string
	for _, row := range ledger.Rows {
		if row.Resource != resource || row.Target == target {
			continue
		}
		if row.Generation == generation {
			continue
		}
		want := row.Generation
		if want == "" {
			want = "an unrecorded generation"
		}
		others = append(others, fmt.Sprintf("%s requires %s", row.Target, want))
	}
	if len(others) == 0 {
		return nil
	}
	sort.Strings(others)
	return fmt.Errorf("omp: shared package %s: %s requires generation %s, but %s; convergence refuses before package mutation",
		resource, target, generation, strings.Join(others, ", "))
}

// Claims returns the ownership claims the selected generation makes for one
// profile: the shared package tree and the profile-owned steering block. It
// reads the corpus and the current bytes; it writes nothing.
func (a *Adapter) Claims(home, root string) ([]harness.Claim, error) {
	cat, err := a.corpus()
	if err != nil {
		return nil, err
	}
	pkg, err := BuildPackage(cat, a.Capabilities())
	if err != nil {
		return nil, err
	}
	treeDigest, err := pkg.TreeDigest()
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
	return resourcesFor(ledger, instances, PackageResource(home)), nil
}

// resourcesFor merges ledger rows and discovered instances into one record per
// physical resource. A resource under the shared package root is visible to
// every discovered profile; a profile-owned resource is visible only to the
// profile whose root holds it.
func resourcesFor(ledger *installstate.Ledger, instances []harness.Instance, packageRoot string) []harness.Resource {
	byID := map[string]*harness.Resource{}
	var order []string
	for _, row := range ledger.Rows {
		r, ok := byID[row.Resource]
		if !ok {
			r = &harness.Resource{
				ID:   row.Resource,
				Kind: row.Applied.Kind,
				Path: row.Applied.Path,
			}
			byID[row.Resource] = r
			order = append(order, row.Resource)
		}
		if r.Owner == "" {
			r.Owner = row.Target
		}
		r.Consumers = appendUnique(r.Consumers, row.Target)
	}

	for _, id := range order {
		r := byID[id]
		shared := underDir(r.Path, packageRoot)
		for _, inst := range instances {
			if !inst.Exists {
				continue
			}
			key := harness.Target{Kind: inst.Kind, Instance: inst.ID}.Key()
			if contains(r.Consumers, key) {
				continue
			}
			if shared || underDir(r.Path, inst.NativeRoot) {
				r.VisibleTo = appendUnique(r.VisibleTo, key)
			}
		}
	}

	out := make([]harness.Resource, 0, len(order))
	for _, id := range order {
		r := byID[id]
		sort.Strings(r.Consumers)
		sort.Strings(r.VisibleTo)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// underDir reports whether path is directory dir itself or lies inside it.
func underDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// appendUnique appends value when it is not already present.
func appendUnique(list []string, value string) []string {
	if contains(list, value) {
		return list
	}
	return append(list, value)
}

// contains reports whether list holds value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
