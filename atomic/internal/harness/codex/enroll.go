package codex

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// PackageResource is the shared plugin tree every enrolled CODEX_HOME consumes.
// Identity is its absolute path, because a name alone would merge homes.
func PackageResource(home string) string {
	return config.PackageRoot(home, string(harness.KindCodex))
}

// Commit-unit names. Each is a single safe path segment, which the journal and
// staging paths require.
const (
	packageUnit  = "plugin"
	steeringUnit = "steering"
)

// Tier is the enforcement tier the plugin carries: instruction-only until a
// runtime probe proves a native boundary.
const Tier = artifacts.EnforcementUnsupported

// EnrollRequest is the caller's intent to enroll one Codex home.
type EnrollRequest struct {
	Home string
	// Root is the CODEX_HOME native root to enroll.
	Root string
	// Decision is the caller's replace-or-leave-unowned choice for an agent file
	// Atomic never wrote; empty refuses.
	Decision installstate.Decision
	// OperationID names the journal and transaction directories. Empty derives a
	// unique id from the clock.
	OperationID string
	Now         func() time.Time
}

// RegistrationState is the read-only observation of Codex's own registry for the
// generated plugin. A missing entry is false, never an inferred success.
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
	// Gaps are the runtime-proven capability rows the plugin cannot promise.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native plugin surfaces with no runtime observation.
	Unproven []PluginGap `json:"unproven,omitempty"`
	// Hooks is the withheld hook registration plan.
	Hooks HookRegistration `json:"hooks"`
	// Registration is the observed native registry state, read-only.
	Registration RegistrationState `json:"registration"`
	// Surfaces are the runtime surface reports doctor consumes, carried with the
	// enrollment outcome so they are not discovered separately.
	Surfaces []SurfaceState `json:"surfaces,omitempty"`
	// Applied names the native resources this operation published; empty means
	// the target was already converged and nothing was written.
	Applied []string `json:"applied,omitempty"`
	// Retired names the agent files deleted because the corpus no longer ships them.
	Retired     []string           `json:"retired,omitempty"`
	JournalPath string             `json:"journal_path,omitempty"`
	Resources   []harness.Resource `json:"resources,omitempty"`
}

// Enroll publishes the selected generation's plugin tree through the transaction
// engine and writes files only; registration is Register's job (Codex owns its registry format).
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
	}
	if gap, open := agentDeliveryGap(a.Capabilities()); open {
		result.Unproven = append(result.Unproven, gap)
	}
	if gap, open := sessionContextGap(a.Capabilities()); open {
		result.Unproven = append(result.Unproven, gap)
	}
	agents, err := harness.AgentFiles(req.Home, req.Root, harness.KindCodex, cat, harness.CodexAgent)
	if err != nil {
		return result, err
	}
	block, err := SteeringBlock(cat)
	if err != nil {
		return result, err
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
	agentPlan, err := harness.PlanAgents(harness.AgentClaims(agents, ledger, target.Key()), ledger, target.Key(), harness.AgentDir(req.Root), req.Decision)
	if err != nil {
		return result, err
	}
	if len(agentPlan.Blockers) > 0 {
		return result, fmt.Errorf("codex: enroll %s: %s", target.Key(), strings.Join(agentPlan.Blockers, "; "))
	}
	hookPlan, err := planSessionHook(ledger, target.Key(), req.Root, req.Decision)
	if err != nil {
		return result, err
	}
	if hookPlan.blocker != "" {
		return result, fmt.Errorf("codex: enroll %s: %s", target.Key(), hookPlan.blocker)
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
	record := installstate.TargetRecord{
		Harness:    string(target.Kind),
		Instance:   target.Instance,
		NativeRoot: target.NativeRoot,
		Status:     string(harness.StatusConverged),
	}
	if ledger.UpsertTarget(record) {
		rowsChanged = true
	}

	// One shared tree cannot hold two generations, so moving it records every
	// consumer's row in the same commit; a stale row is not a requirement.
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

	var mutations []installstate.Mutation
	if obs.Digest != treeDigest {
		mutations = append(mutations, installstate.Mutation{
			Unit:       packageUnit,
			Resource:   packageRoot,
			Target:     target.Key(),
			Consumer:   target.Key(),
			Consumers:  consumers,
			Generation: plugin.Generation,
			Tier:       string(Tier),
			Kind:       managedfile.KindTree,
			Path:       packageRoot,
			Intended:   treeDigest,
		})
	}
	blockDigest, err := steeringDigest(block)
	if err != nil {
		return result, err
	}
	type owned struct {
		unit, path, digest string
		kind               managedfile.Kind
		data               []byte
	}
	files := []owned{{unit: steeringUnit, path: SteeringPath(req.Root), digest: blockDigest, kind: managedfile.KindBlock, data: block}}
	for _, f := range agents {
		if !slices.Contains(agentPlan.Left, f.Path) {
			files = append(files, owned{unit: f.Unit(), path: f.Path, digest: managedfile.Digest(f.Data), kind: managedfile.KindFile, data: f.Data})
		}
	}
	staged := map[string][]byte{}
	for _, f := range files {
		row := installstate.Row{
			Target:     target.Key(),
			Resource:   f.path,
			Consumer:   target.Key(),
			Generation: plugin.Generation,
			Tier:       string(Tier),
			Applied:    installstate.AppliedValue{Path: f.path, Kind: f.kind, Digest: f.digest},
		}
		obs, err := managedfile.Observe(f.path, f.kind)
		if err != nil {
			return result, err
		}
		if obs.Digest == f.digest {
			if ledger.Upsert(row) {
				rowsChanged = true
			}
			continue
		}
		if obs.Conflict == managedfile.ConflictMalformedBlock && managedfile.HasBlockTags(obs.Bytes) {
			return result, fmt.Errorf("codex: enroll %s: %s carries an ambiguous %s block; refusing to guess a boundary", target.Key(), f.path, managedfile.BlockOpen)
		}
		staged[f.unit] = f.data
		mutations = append(mutations, installstate.Mutation{
			Unit:       f.unit,
			Resource:   f.path,
			Target:     target.Key(),
			Consumer:   target.Key(),
			Generation: plugin.Generation,
			Tier:       string(Tier),
			Kind:       f.kind,
			Path:       f.path,
			Intended:   f.digest,
		})
	}

	if ledger.SetLeft(target.Key(), harness.AgentDir(req.Root), agentPlan.Left) {
		rowsChanged = true
	}

	hookWrote, hookChanged, err := applySessionHook(req.Home, ledger, target, plugin.Generation, hookPlan)
	if err != nil {
		return result, err
	}
	if hookWrote {
		result.Applied = append(result.Applied, hooks.CodexHooksPath(req.Root))
	}
	if hookChanged {
		rowsChanged = true
	}

	if len(mutations) == 0 {
		result.Retired, err = harness.RetireAgents(ledger, agentPlan.Retire)
		if err != nil {
			return result, err
		}
		if rowsChanged || len(result.Retired) > 0 {
			if err := ledger.Save(config.LedgerPath(req.Home)); err != nil {
				return result, err
			}
		}
		result.Status = harness.StatusConverged
		return a.finish(req, result)
	}

	tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: mutations, Targets: []installstate.TargetRecord{record}, Left: ledger.LeftRecordsOf(target.Key()), LeftSnapshot: true})
	if err != nil {
		return result, err
	}
	tx.Ledger = ledger
	result.JournalPath = tx.JournalPath

	for _, m := range mutations {
		if m.Kind == managedfile.KindTree {
			if _, err := tx.StageTree(m.Unit, plugin.Render); err != nil {
				return result, err
			}
			if err := tx.PublishTree(m.Unit); err != nil {
				return result, err
			}
		} else {
			if _, err := tx.StageFile(m.Unit, staged[m.Unit], 0o644); err != nil {
				return result, err
			}
			if err := tx.PublishFile(m.Unit); err != nil {
				return result, err
			}
		}
		result.Applied = append(result.Applied, m.Path)
	}
	result.Retired, err = harness.RetireAgents(ledger, agentPlan.Retire)
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
	return a.finish(req, result)
}

// finish attaches the registration observation and resource report. A failure to
// read either still reports the committed enrollment, not a failure to enroll.
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
// home: the shared plugin tree, the steering block, and one claim per agent file.
func (a *Adapter) Claims(home, root string) ([]harness.Claim, error) {
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
	agents, err := harness.ProjectAgentClaims(home, harness.Target{Kind: harness.KindCodex, Instance: root, NativeRoot: root}, cat, harness.CodexAgent)
	if err != nil {
		return nil, err
	}
	block, err := SteeringBlock(cat)
	if err != nil {
		return nil, err
	}
	blockDigest, err := steeringDigest(block)
	if err != nil {
		return nil, err
	}
	return append([]harness.Claim{
		{ID: PackageResource(home), Kind: managedfile.KindTree, Path: PackageResource(home), SelectedDigest: treeDigest},
		{ID: SteeringPath(root), Kind: managedfile.KindBlock, Path: SteeringPath(root), SelectedDigest: blockDigest},
	}, agents...), nil
}

// Resources reports the physical resources Atomic owns for Codex, one record per
// resource merged from the ledger's per-target rows.
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
