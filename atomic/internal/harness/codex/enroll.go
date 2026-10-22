package codex

import (
	"fmt"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// PackageResource is the shared plugin tree at ~/.atomic/packages/codex/atomic,
// the one generated marketplace every enrolled CODEX_HOME consumes. Identity is
// the resource's own absolute path, because several homes consume the same
// physical tree and a name alone would merge them.
func PackageResource(home string) string {
	return config.PackageRoot(home, string(harness.KindCodex))
}

// Commit-unit name. It is a single safe path segment, which the journal and
// staging paths require.
const packageUnit = "plugin"

// Tier is the enforcement tier the plugin carries. Every Codex projection is
// instruction-only until a runtime checkpoint proves otherwise, so nothing here
// reads as a native boundary.
const Tier = artifacts.EnforcementUnsupported

// EnrollRequest is the caller's intent to enroll one Codex home.
type EnrollRequest struct {
	Home string
	// Root is the CODEX_HOME native root to enroll.
	Root string
	// OperationID names the journal and transaction directories. Empty derives a
	// unique id from the clock.
	OperationID string
	Now         func() time.Time
}

// RegistrationState is the read-only observation of what Codex's own registry
// records for the generated plugin. It reports only bytes Codex wrote: a
// missing marketplace or plugin entry is `false`, never an inferred success, and
// the adapter never edits this file.
type RegistrationState struct {
	ConfigPath string `json:"config_path"`
	// Marketplace reports a `[marketplaces.<name>]` entry naming this tree.
	Marketplace bool `json:"marketplace"`
	// Plugin reports a `[plugins."<plugin>@<marketplace>"]` entry.
	Plugin bool `json:"plugin"`
	// Enabled reports the plugin entry's `enabled` flag, false when absent.
	Enabled bool `json:"enabled"`
	// CachePath is the versioned cache directory Codex materializes on install.
	CachePath string `json:"cache_path"`
	// CachePresent reports whether that directory exists on disk.
	CachePresent bool   `json:"cache_present"`
	Evidence     string `json:"evidence,omitempty"`
}

// EnrollResult reports what enrollment planned, committed, and observed.
type EnrollResult struct {
	Target      harness.Target `json:"target"`
	Status      harness.Status `json:"status"`
	Generation  string         `json:"generation"`
	PackageRoot string         `json:"package_root"`
	// PackageFiles names the generated plugin tree's files in path order.
	PackageFiles []string `json:"package_files,omitempty"`
	// Gaps are the CP0 capability rows the plugin cannot promise.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native plugin surfaces with no CP0 observation.
	Unproven []PluginGap `json:"unproven,omitempty"`
	// Hooks is the withheld hook registration plan.
	Hooks HookRegistration `json:"hooks"`
	// Steering is the instruction materialization the plugin does not emit.
	Steering PluginGap `json:"steering"`
	// Registration is the observed native registry state, read-only.
	Registration RegistrationState `json:"registration"`
	// Surfaces are the read-only runtime surface reports doctor consumes, so the
	// capability-disabled guarantees travel with the enrollment outcome instead
	// of being discovered separately.
	Surfaces []SurfaceState `json:"surfaces,omitempty"`
	// Applied names the native resources this operation published; empty means
	// the target was already converged and nothing was written.
	Applied     []string           `json:"applied,omitempty"`
	JournalPath string             `json:"journal_path,omitempty"`
	Resources   []harness.Resource `json:"resources,omitempty"`
}

// Enroll publishes the selected generation's plugin tree through the transaction
// engine: the lifecycle lock is held, unresolved journals are recovered
// oldest-first, the journal and backups precede every native write, staged bytes
// are validated, effective content is verified, and only then is the ledger row
// recorded with its generation and tier. A home already converged on the same
// generation is a no-op: no native byte, journal, backup, or staging is written.
//
// Enrollment publishes the plugin package only. Native marketplace registration
// is a separate CP0-proven CLI surface with its own observer (Register), because
// Codex owns the byte format of its registry and cache; enrollment records only
// what it can verify, and reports the observed registration state read-only.
//
// The shared plugin tree is one physical resource every enrolled CODEX_HOME
// consumes, so moving it records each enrolled consumer's row in the same
// commit. A home whose row records an older generation is stale, not a
// requirement, and does not block: this operation rewrites it. A row for a
// target the ledger does not enroll is the one genuine pin, and refuses before
// any mutation, naming both targets.
func (a *Adapter) Enroll(req EnrollRequest) (EnrollResult, error) {
	var result EnrollResult
	if req.Home == "" {
		return result, fmt.Errorf("codex: enroll: no home")
	}
	if req.Root == "" {
		return result, fmt.Errorf("codex: enroll: no CODEX_HOME root")
	}

	cat, err := a.corpus()
	if err != nil {
		return result, err
	}
	plugin, err := BuildPlugin(cat, a.Capabilities())
	if err != nil {
		return result, err
	}

	target := harness.Target{Kind: harness.KindCodex, Instance: req.Root, NativeRoot: req.Root}
	packageRoot := PackageResource(req.Home)
	result = EnrollResult{
		Target:       target,
		Generation:   plugin.Generation,
		PackageRoot:  packageRoot,
		PackageFiles: plugin.Paths(),
		Gaps:         plugin.Gaps,
		Unproven:     plugin.Unproven,
		Hooks:        plugin.Hooks,
		Steering:     plugin.Steering,
	}

	operationID := req.OperationID
	if operationID == "" {
		operationID = fmt.Sprintf("codex-%d", now(req.Now)().UnixNano())
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
	consumers, err := harness.EnsureSharedGeneration(ledger, harness.KindCodex, "codex", target.Key(), packageRoot, "plugin tree", plugin.Generation)
	if err != nil {
		return result, err
	}

	treeDigest, err := plugin.TreeDigest()
	if err != nil {
		return result, err
	}

	obs, err := managedfile.Observe(packageRoot, managedfile.KindTree)
	if err != nil {
		return result, err
	}
	rowsChanged := ledger.Upsert(installstate.Row{
		Target:     target.Key(),
		Resource:   packageRoot,
		Consumer:   target.Key(),
		Generation: plugin.Generation,
		Tier:       string(Tier),
		Applied:    installstate.AppliedValue{Path: packageRoot, Kind: managedfile.KindTree, Digest: treeDigest},
	})
	if ledger.UpsertTarget(installstate.TargetRecord{
		Harness:    string(target.Kind),
		Instance:   target.Instance,
		NativeRoot: target.NativeRoot,
		Status:     string(harness.StatusConverged),
	}) {
		rowsChanged = true
	}

	// One physical marketplace tree cannot hold two generations, and every
	// enrolled CODEX_HOME consumes it: the running binary is the only writer, so
	// the operation that moves the tree records each consumer's row in the same
	// commit rather than leaving a stale generation to look like a requirement.
	// AssertSharedConsumers proves the set the generation check approved was the
	// set this loop converged.
	for _, consumer := range consumers {
		if ledger.Upsert(installstate.Row{
			Target:     consumer,
			Resource:   packageRoot,
			Consumer:   consumer,
			Generation: plugin.Generation,
			Tier:       string(Tier),
			Applied:    installstate.AppliedValue{Path: packageRoot, Kind: managedfile.KindTree, Digest: treeDigest},
		}) {
			rowsChanged = true
		}
	}
	if err := harness.AssertSharedConsumers(ledger, "codex", packageRoot, plugin.Generation, consumers); err != nil {
		return result, err
	}

	if obs.Digest != "" && obs.Digest == treeDigest {
		// Already applied: the observation is the verification, so the ownership
		// row is recorded without touching native bytes.
		if rowsChanged {
			if err := ledger.Save(config.LedgerPath(req.Home)); err != nil {
				return result, err
			}
		}
		result.Status = harness.StatusConverged
		return a.finish(req, result)
	}

	tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: []installstate.Mutation{{
		Unit:       packageUnit,
		Resource:   packageRoot,
		Target:     target.Key(),
		Consumer:   target.Key(),
		Generation: plugin.Generation,
		Tier:       string(Tier),
		Kind:       managedfile.KindTree,
		Path:       packageRoot,
		Intended:   treeDigest,
	}}})
	if err != nil {
		return result, err
	}
	tx.Ledger = ledger
	result.JournalPath = tx.JournalPath

	if _, err := tx.StageTree(packageUnit, plugin.Render); err != nil {
		return result, err
	}
	if err := tx.PublishTree(packageUnit); err != nil {
		return result, err
	}
	result.Applied = append(result.Applied, packageRoot)
	if err := tx.Complete(); err != nil {
		return result, err
	}
	if _, err := installstate.CleanupOperation(req.Home, tx.ID); err != nil {
		return result, err
	}

	result.Status = harness.StatusConverged
	return a.finish(req, result)
}

// finish attaches the post-state registration observation and the resource
// report, separating consumers from visibility. Discovery and registration
// observation are read-only reporting steps: when either fails the committed
// enrollment is still reported, because a failure to read one more fact must not
// read as a failure to enroll.
func (a *Adapter) finish(req EnrollRequest, result EnrollResult) (EnrollResult, error) {
	state, err := ObserveRegistration(req.Root)
	if err != nil {
		state = RegistrationState{ConfigPath: ConfigPath(req.Root), Evidence: err.Error()}
	}
	result.Registration = state
	result.Surfaces = append(a.RuntimeState(req.Root), a.RegistrationStateRow(req.Root))

	ledger, err := installstate.LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return result, err
	}
	instances, err := a.Discover(req.Home)
	if err != nil {
		instances = nil
	}
	result.Resources = harness.Resources(ledger, instances, harness.SharedRoots(req.Home))
	return result, nil
}

// Claims returns the ownership claims the selected generation makes for one
// home: the shared plugin tree. Codex has no proven global instruction surface,
// so no steering claim exists.
func (a *Adapter) Claims(home string) ([]harness.Claim, error) {
	cat, err := a.corpus()
	if err != nil {
		return nil, err
	}
	plugin, err := BuildPlugin(cat, a.Capabilities())
	if err != nil {
		return nil, err
	}
	treeDigest, err := plugin.TreeDigest()
	if err != nil {
		return nil, err
	}
	return []harness.Claim{{
		ID:             PackageResource(home),
		Kind:           managedfile.KindTree,
		Path:           PackageResource(home),
		SelectedDigest: treeDigest,
	}}, nil
}

// Resources reports the physical resources Atomic owns for Codex, merging the
// ledger's per-target rows into one record per resource so consumers and
// visibility stay separate.
func (a *Adapter) Resources(home string) ([]harness.Resource, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}
	instances, err := a.Discover(home)
	if err != nil {
		instances = nil
	}
	return harness.Resources(ledger, instances, harness.SharedRoots(home)), nil
}
