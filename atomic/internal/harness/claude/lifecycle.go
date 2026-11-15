package claude

import (
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Tier is the enforcement tier a Claude projection carries: no native
// scoped-rule surface is proven for the tested version.
const Tier = artifacts.EnforcementUnsupported

// Generation is the selected binary's Claude generation identity. It changes
// whenever the shipped corpus does, so converged rows are comparable.
func Generation() string {
	manifest := embedded.Manifest()
	parts := make([]string, 0, len(manifest))
	for _, a := range manifest {
		parts = append(parts, a.Target+"\x00"+a.SHA256)
	}
	return managedfile.Digest([]byte(strings.Join(parts, "\n") + "\n"))
}

// Lifecycle binds the Claude hook points for one home. Convergence runs the
// migration engine, which owns locking, recovery, and staging.
func (a *Adapter) Lifecycle(home string) harness.Lifecycle {
	return harness.Lifecycle{
		ProjectFn: func(t harness.Target, req harness.PlanRequest) (harness.Plan, error) {
			return a.project(home, t, req)
		},
		ConvergeFn: func(t harness.Target, p harness.Plan) (harness.Convergence, error) {
			return a.converge(home, t, p)
		},
		VerifyFn: func(t harness.Target) ([]harness.Assessment, error) {
			return a.assess(home, t)
		},
		RemoveFn: func(t harness.Target) (harness.Removal, error) {
			return harness.RemoveTargetResources(home, t)
		},
	}
}

// project plans through the migration engine so a legacy, partial, or mixed
// state reports blockers instead of being converged around.
func (a *Adapter) project(home string, t harness.Target, req harness.PlanRequest) (harness.Plan, error) {
	if t.NativeRoot == "" {
		return harness.Plan{}, fmt.Errorf("claude: project %s: no native root", t.Key())
	}
	plan, err := PlanMigration(MigrationRequest{
		Home:          home,
		NativeRoot:    t.NativeRoot,
		Target:        t.Key(),
		Generation:    Generation(),
		Tier:          string(Tier),
		BatchDecision: req.BatchDecision,
	})
	if err != nil {
		return harness.Plan{}, err
	}
	claims, err := GlobalClaims(home, t.NativeRoot)
	if err != nil {
		return harness.Plan{}, err
	}
	return harness.Plan{
		Target:        t,
		Generation:    Generation(),
		Claims:        claims,
		Blockers:      plan.Blockers,
		BatchDecision: req.BatchDecision,
		Converged:     plan.Status == installstate.StatusConverged,
		SkipHooks:     req.SkipHooks,
	}, nil
}

// converge applies the plan through the migration engine and maps the outcome
// onto the shared convergence vocabulary.
func (a *Adapter) converge(home string, t harness.Target, p harness.Plan) (harness.Convergence, error) {
	_, err := Migrate(MigrationRequest{
		Home:          home,
		NativeRoot:    t.NativeRoot,
		Target:        t.Key(),
		Consumer:      t.Key(),
		Generation:    p.Generation,
		Tier:          string(Tier),
		BatchDecision: p.BatchDecision,
	})
	if err != nil {
		return harness.Convergence{Target: t}, err
	}
	if err := ApplyOwnedSettings(home, t, p.Generation, p.SkipHooks); err != nil {
		return harness.Convergence{Target: t}, err
	}
	return harness.Convergence{Target: t, Status: harness.StatusConverged}, nil
}

// settingsResourceID names the one Claude settings resource a target owns: the
// named members of its settings.json, never the file itself.
const settingsResourceID = "settings.json"

// ApplyOwnedSettings applies the owned settings members and records their
// ledger row under one lock, so no lifecycle operation interleaves between them.
func ApplyOwnedSettings(home string, t harness.Target, generation string, skipHooks bool) error {
	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "claude-settings"})
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := applyClaudeSettings(home, t.NativeRoot, skipHooks); err != nil {
		return err
	}
	return recordSettingsOwnership(home, t, generation)
}

// recordSettingsOwnership re-observes the owned members after the apply, so the
// row records only bytes that verify now; with neither member present it drops it.
func recordSettingsOwnership(home string, t harness.Target, generation string) error {
	owned, err := hooks.ObserveSettingsOwnedInDir(t.NativeRoot)
	if err != nil {
		return fmt.Errorf("claude: observe settings ownership: %w", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return err
	}
	verifiable := owned.Conflict == managedfile.ConflictNone && owned.Digest != "" && owned.Exists()
	if !verifiable {
		return dropSettingsRow(ledger, home, t)
	}
	if ledger.Upsert(installstate.Row{
		Target:     t.Key(),
		Resource:   settingsResourceID,
		Consumer:   t.Key(),
		Generation: generation,
		Tier:       string(Tier),
		Applied:    installstate.AppliedValue{Path: owned.Path, Kind: managedfile.KindSettings, Digest: owned.Digest},
	}) {
		return ledger.Save(config.LedgerPath(home))
	}
	return nil
}

// dropSettingsRow removes any settings row for the target, so an unownable
// settings file leaves no stale claim.
func dropSettingsRow(ledger *installstate.Ledger, home string, t harness.Target) error {
	kept := ledger.Rows[:0:0]
	changed := false
	for _, row := range ledger.Rows {
		if row.Target == t.Key() && row.Resource == settingsResourceID {
			changed = true
			continue
		}
		kept = append(kept, row)
	}
	if !changed {
		return nil
	}
	ledger.Rows = kept
	return ledger.Save(config.LedgerPath(home))
}

// applyClaudeSettings resolves settings from configDir itself, never its parent:
// a root like ~/.claude-work would otherwise write the default target's file.
func applyClaudeSettings(home, configDir string, skipHooks bool) error {
	if !skipHooks {
		skipped, err := hooks.InstallInDir(configDir)
		if err != nil {
			return fmt.Errorf("claude: register session-start hook: %w", err)
		}
		if skipped {
			return nil
		}
	}
	if _, err := hooks.SeedOutputStyleInDir(configDir, configDir, home); err != nil {
		return fmt.Errorf("claude: seed output style: %w", err)
	}
	return nil
}

// assess reports the ownership verdict for every Claude claim, read-only.
func (a *Adapter) assess(home string, t harness.Target) ([]harness.Assessment, error) {
	if t.NativeRoot == "" {
		return nil, fmt.Errorf("claude: assess %s: no native root", t.Key())
	}
	claims, err := GlobalClaims(home, t.NativeRoot)
	if err != nil {
		return nil, err
	}
	c, err := installstate.Classify(installstate.ClassifyRequest{
		Home:       home,
		NativeRoot: t.NativeRoot,
		Target:     t.Key(),
		Claims:     claims,
	})
	if err != nil {
		return nil, err
	}
	out := make([]harness.Assessment, 0, len(c.Resources))
	for _, r := range c.Resources {
		out = append(out, r.Assessment)
	}
	return out, nil
}
