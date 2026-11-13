package codex

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Lifecycle binds the Codex projection, convergence, verification, and removal
// seams for one home; the enrollment engine owns locking and recovery.
func (a *Adapter) Lifecycle(home string) harness.Lifecycle {
	return harness.Lifecycle{
		ProjectFn: func(t harness.Target, req harness.PlanRequest) (harness.Plan, error) {
			return a.project(home, t, req)
		},
		ConvergeFn: func(t harness.Target, p harness.Plan) (harness.Convergence, error) {
			return a.converge(home, t, p.BatchDecision)
		},
		VerifyFn: func(t harness.Target) ([]harness.Assessment, error) {
			return a.assess(home, t)
		},
		RemoveFn: func(t harness.Target) (harness.Removal, error) {
			return harness.RemoveTargetResources(home, t)
		},
		RemovedFn: func(t harness.Target, removal harness.Removal) (harness.Removal, error) {
			return a.unregister(home, t, removal)
		},
	}
}

// project builds the read-only plan for one Codex home: its claims, the selected
// generation, and every blocker that forbids mutation.
func (a *Adapter) project(home string, t harness.Target, req harness.PlanRequest) (harness.Plan, error) {
	if t.NativeRoot == "" {
		return harness.Plan{}, fmt.Errorf("codex: project %s: %w", t.Key(), harness.ErrUnsupported)
	}
	cat, err := a.corpus()
	if err != nil {
		return harness.Plan{}, err
	}
	plugin, err := BuildPlugin(cat, a.Capabilities())
	if err != nil {
		return harness.Plan{}, err
	}
	plan := harness.Plan{Target: t, Generation: plugin.Generation, Converged: true, BatchDecision: req.BatchDecision}
	unproven := plugin.Unproven
	if gap, open := agentDeliveryGap(a.Capabilities()); open {
		unproven = append(unproven, gap)
	}
	if gap, open := sessionContextGap(a.Capabilities()); open {
		unproven = append(unproven, gap)
	}
	for _, gap := range unproven {
		plan.Unproven = append(plan.Unproven, gap.Surface+": "+gap.Evidence)
	}
	plan.Claims, err = a.Claims(home, t.NativeRoot)
	if err != nil {
		return harness.Plan{}, err
	}
	cache := CachePath(t.NativeRoot)
	if a.RegisterFn == nil {
		plan.Unproven = append(plan.Unproven, registrationGap(harness.ErrUnsupported))
	} else {
		removed, err := removedByUser(home, t)
		if err != nil {
			return harness.Plan{}, err
		}
		if removed && req.BatchDecision != installstate.DecisionReplace {
			plan.Unproven = append(plan.Unproven, userRemovalGap)
		} else {
			digest, err := plugin.CacheDigest()
			if err != nil {
				return harness.Plan{}, err
			}
			plan.Claims = append(plan.Claims, harness.Claim{ID: cache, Kind: managedfile.KindTree, Path: cache, SelectedDigest: digest})
			current, err := registrationCurrent(t.NativeRoot, digest)
			switch {
			case err != nil:
				plan.Unproven = append(plan.Unproven, registrationGap(err))
			case !current:
				plan.Converged = false
				plan.Notices = append(plan.Notices, fmt.Sprintf("runs `codex plugin marketplace add %s` and `codex plugin add %s`, which edit %s",
					PackageResource(home), PluginID(), ConfigPath(t.NativeRoot)))
			}
		}
	}

	agentDir := harness.AgentDir(t.NativeRoot)
	for _, claim := range plan.Claims {
		if filepath.Dir(claim.Path) == agentDir || claim.Path == cache {
			continue
		}
		obs, err := managedfile.Observe(claim.Path, claim.Kind)
		if err != nil {
			return harness.Plan{}, err
		}
		if obs.Conflict != managedfile.ConflictNone {
			plan.Converged = false
			if obs.Conflict == managedfile.ConflictMalformedBlock && !managedfile.HasBlockTags(obs.Bytes) {
				continue
			}
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s carries an ambiguous %s block", claim.Path, managedfile.BlockOpen))
			continue
		}
		if obs.Digest != claim.SelectedDigest {
			plan.Converged = false
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return harness.Plan{}, err
	}
	agents, err := harness.PlanAgents(plan.Claims, ledger, t.Key(), agentDir, req.BatchDecision)
	if err != nil {
		return harness.Plan{}, err
	}
	plan.Blockers = append(plan.Blockers, agents.Blockers...)
	plan.Converged = plan.Converged && agents.Converged
	hookPlan, err := planSessionHook(ledger, t.Key(), t.NativeRoot, req.BatchDecision)
	if err != nil {
		return harness.Plan{}, err
	}
	if hookPlan.blocker != "" {
		plan.Blockers = append(plan.Blockers, hookPlan.blocker)
	}
	plan.Converged = plan.Converged && hookPlan.converged
	if _, err := harness.EnsureSharedGeneration(ledger, harness.KindCodex, "codex", t.Key(), PackageResource(home), "plugin tree", plugin.Generation); err != nil {
		plan.Blockers = append(plan.Blockers, err.Error())
	}
	return plan, nil
}

// converge applies the Codex plan through the enrollment engine, then registers
// the published plugin unless Codex already holds the current copy.
func (a *Adapter) converge(home string, t harness.Target, decision installstate.Decision) (harness.Convergence, error) {
	result, err := a.Enroll(EnrollRequest{Home: home, Root: t.NativeRoot, Decision: decision})
	if err != nil {
		return harness.Convergence{Target: t}, err
	}
	c := harness.Convergence{Target: t, Status: result.Status, Applied: result.Applied, Itemized: true}
	if a.RegisterFn == nil {
		c.Unproven = append(c.Unproven, registrationGap(harness.ErrUnsupported))
		return c, nil
	}
	want, _, err := managedfile.TreeDigest(filepath.Join(PackageResource(home), filepath.FromSlash(PluginRoot)))
	if err != nil {
		return c, err
	}
	current, err := registrationCurrent(t.NativeRoot, want)
	if err != nil {
		c.Unproven = append(c.Unproven, registrationGap(err))
		return c, nil
	}
	if current {
		return c, refreshCacheRow(home, t)
	}
	if decision != installstate.DecisionReplace {
		removed, err := removedByUser(home, t)
		if err != nil || removed {
			return c, err
		}
	}
	reg, err := a.Register(RegisterRequest{Home: home, Root: t.NativeRoot})
	if err != nil {
		c.Unproven = append(c.Unproven, registrationGap(err))
		return c, nil
	}
	c.Applied = append(c.Applied, reg.Applied...)
	return c, nil
}

const registrationSurface = "native plugin registration"

func registrationGap(err error) string {
	fix := "resolve the error in Codex, then run `atomic install --harness codex`"
	if errors.Is(err, harness.ErrUnsupported) {
		fix = fmt.Sprintf("put codex-cli on PATH or set %s, then run `atomic install --harness codex`", BinaryEnv)
	}
	return fmt.Sprintf("%s: %v; fix: %s", registrationSurface, err, fix)
}

// userRemovalGap is reported instead of re-registering a plugin the user
// removed from Codex after Atomic registered it.
var userRemovalGap = registrationSurface + ": the plugin Atomic registered was removed from Codex; fix: run `atomic install --harness codex --replace` to re-register"

// refreshCacheRow re-records the digest of a cache Atomic already owns, since
// Codex can re-copy it while the registration stays current.
func refreshCacheRow(home string, t harness.Target) error {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return err
	}
	row, ok := ledger.Find(t.Key(), CachePath(t.NativeRoot))
	if !ok {
		return nil
	}
	digest, _, err := managedfile.TreeDigest(CachePath(t.NativeRoot))
	if err != nil || digest == row.Applied.Digest {
		return err
	}
	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "codex-cache-refresh"})
	if err != nil {
		return err
	}
	defer lock.Release()
	return recordCacheOwnership(home, t, CachePath(t.NativeRoot))
}

// removedByUser reports a registration Atomic recorded whose plugin entry and
// cache Codex no longer holds; `codex plugin remove` keeps the marketplace.
func removedByUser(home string, t harness.Target) (bool, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return false, err
	}
	if _, ok := ledger.Find(t.Key(), CachePath(t.NativeRoot)); !ok {
		return false, nil
	}
	state, err := ObserveRegistration(t.NativeRoot)
	return err == nil && !state.Plugin && !state.CachePresent, nil
}

// registrationCurrent is shared by plan and converge so the two never disagree
// on whether registration runs; a disabled plugin still reads current.
func registrationCurrent(root, want string) (bool, error) {
	state, err := ObserveRegistration(root)
	if err != nil || !state.Marketplace || !state.Plugin || !state.CachePresent {
		return false, err
	}
	got, _, err := managedfile.TreeDigest(CachePath(root))
	return got == want, err
}

// unregister reads t.Instance: a parsed key has no NativeRoot. A failure keeps
// no enrollment, or `atomic update` would re-converge an uninstalled home.
func (a *Adapter) unregister(home string, t harness.Target, removal harness.Removal) (harness.Removal, error) {
	cache := CachePath(t.Instance)
	if !slices.Contains(removal.Removed, cache) && !slices.Contains(removal.Discarded, cache) {
		return removal, nil
	}
	unregister := a.UnregisterFn
	if unregister == nil {
		unregister = func(RegisterRequest) error { return harness.ErrUnsupported }
	}
	if err := unregister(RegisterRequest{Home: home, Root: t.Instance}); err != nil {
		removal.Notices = append(removal.Notices, fmt.Sprintf("Codex may still register %s: %v; with CODEX_HOME=%s run `codex plugin remove %s` and `codex plugin marketplace remove %s`",
			PluginID(), err, t.Instance, PluginID(), MarketplaceName))
	}
	return removal, nil
}

// assess reports the ownership verdict for every Codex claim, read-only.
func (a *Adapter) assess(home string, t harness.Target) ([]harness.Assessment, error) {
	if t.NativeRoot == "" {
		return nil, fmt.Errorf("codex: assess %s: %w", t.Key(), harness.ErrUnsupported)
	}
	claims, err := a.Claims(home, t.NativeRoot)
	if err != nil {
		return nil, err
	}
	return harness.AssessAll(claims)
}
