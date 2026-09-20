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

// Tier is the enforcement tier a Claude projection carries. No CP0 role proved a
// native scoped-rule surface for the tested version, so every Claude resource is
// reported instruction-only and nothing reads as a native boundary.
const Tier = artifacts.EnforcementUnsupported

// Generation is the selected binary's Claude generation identity: one line per
// embedded artifact, target and digest. It changes whenever the shipped corpus
// changes, which is what makes a converged target's row comparable.
func Generation() string {
	manifest := embedded.Manifest()
	parts := make([]string, 0, len(manifest))
	for _, a := range manifest {
		parts = append(parts, a.Target+"\x00"+a.SHA256)
	}
	return managedfile.Digest([]byte(strings.Join(parts, "\n") + "\n"))
}

// Lifecycle binds the Claude projection, convergence, verification, and removal
// seams for one home. Convergence runs the CP1D migration engine, which owns the
// lifecycle lock, oldest-first recovery, staging, verification-before-ledger, and
// the one-time mutable import; this file only translates the generic plan.
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

// project builds the read-only plan for one Claude target from the migration
// engine, so a legacy, partial, or mixed state reports its blockers instead of
// being converged around.
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
	return harness.Plan{
		Target:        t,
		Generation:    Generation(),
		Claims:        GlobalClaims(t.NativeRoot),
		Blockers:      plan.Blockers,
		BatchDecision: req.BatchDecision,
		Converged:     plan.Status == installstate.StatusConverged,
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
	if err := ApplyOwnedSettings(home, t, p.Generation); err != nil {
		return harness.Convergence{Target: t}, err
	}
	return harness.Convergence{Target: t, Status: harness.StatusConverged}, nil
}

// settingsResourceID names the one Claude settings resource a target owns: the
// named members of its settings.json, never the file itself.
const settingsResourceID = "settings.json"

// ApplyOwnedSettings applies the two narrow settings mutations convergence owns
// — the inline SessionStart registration and the outputStyle seed — and records
// their ledger ownership, so a target uninstall removes exactly the members
// Atomic wrote and a later user edit to one of them refuses the removal instead
// of being overwritten. Both the adapter convergence and the explicit legacy
// adoption call it, so every ledger-owned Claude target carries the row. The
// apply and the record share one lifecycle lock so a concurrent lifecycle
// operation cannot interleave between them.
func ApplyOwnedSettings(home string, t harness.Target, generation string) error {
	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "claude-settings"})
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := applyClaudeSettings(home, t.NativeRoot); err != nil {
		return err
	}
	return recordSettingsOwnership(home, t, generation)
}

// recordSettingsOwnership writes the settings row for a converged target: the
// digest of the members Atomic owns, re-observed after the apply so the row can
// only record bytes that verify now. A settings file carrying neither owned
// member — a read-only target the apply skipped, or one the user reverted —
// leaves no row, so a claim that can never be verified is never recorded.
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

// dropSettingsRow removes any settings row for the target and reports whether
// the ledger changed. It covers the converged-but-unownable case without
// leaving a stale claim behind.
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

// applyClaudeSettings runs the two narrow Claude settings mutations
// convergence owns: the inline SessionStart registration and the outputStyle
// seed. Both reuse the JWCC-preserving hooks helpers, so a user's own settings
// survive byte for byte and only a missing adapter default is added —
// registration is a no-op when the hook is already present, and the seed only
// writes when the style file is installed, seeding is enabled, and the key is
// unset. A read-only settings.json is left untouched rather than failing an
// otherwise-complete converge; a malformed one is the only hard error.
//
// Settings resolve from the target's own config directory, never from its
// parent: the two coincide for the default ~/.claude, but a root named anything
// else (CLAUDE_CONFIG_DIR=~/.config/claude, --instance ~/.claude-work) would
// otherwise write a sibling `<parent>/.claude/settings.json` Claude never reads
// — and, for ~/.claude-work, the default target's own settings file.
func applyClaudeSettings(home, configDir string) error {
	skipped, err := hooks.InstallInDir(configDir)
	if err != nil {
		return fmt.Errorf("claude: register session-start hook: %w", err)
	}
	if skipped {
		return nil
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
	c, err := installstate.Classify(installstate.ClassifyRequest{
		Home:       home,
		NativeRoot: t.NativeRoot,
		Claims:     GlobalClaims(t.NativeRoot),
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
