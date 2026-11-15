package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/pelletier/go-toml/v2"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func sessionContextGap(m harness.CapabilityMatrix) (PluginGap, bool) {
	row, open := m.Gap(harness.RoleSessionContext)
	return PluginGap{Surface: "session-context delivery", Evidence: row.Limitation}, open
}

// sessionHookPlan is how one convergence treats the hooks.json member.
type sessionHookPlan struct {
	blocker   string
	left      bool
	replace   bool
	converged bool
}

// planSessionHook: --replace overrides a recorded canonical row over a changed
// member; an unowned non-canonical member follows decision, anything else blocks.
func planSessionHook(ledger *installstate.Ledger, key, root string, decision installstate.Decision) (sessionHookPlan, error) {
	obs, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		return sessionHookPlan{}, err
	}
	path := hooks.CodexHooksPath(root)
	row, recorded := ledger.Find(key, path)
	switch {
	case obs.Conflict != managedfile.ConflictNone:
		if err := hooks.SettingsShapeError(path); err != nil {
			return sessionHookPlan{blocker: err.Error()}, nil
		}
		return sessionHookPlan{blocker: path + " does not parse as JSON; fix the file, then rerun"}, nil
	case !obs.Exists():
		return sessionHookPlan{}, nil
	case obs.Digest == hooks.CodexLegacyDigest():
		return sessionHookPlan{}, nil
	case recorded && obs.Digest == row.Applied.Digest:
		return sessionHookPlan{converged: true}, nil
	case recorded && decision == installstate.DecisionReplace && (row.Applied.Digest == hooks.CodexCanonicalDigest() || row.Applied.Digest == hooks.CodexLegacyDigest()):
		return sessionHookPlan{replace: true}, nil
	case recorded:
		return sessionHookPlan{blocker: path + ": the Atomic SessionStart member changed after Atomic wrote it; restore or remove it, or rerun with --replace"}, nil
	case obs.Digest == hooks.CodexCanonicalDigest():
		return sessionHookPlan{}, nil
	case decision == installstate.DecisionReplace:
		return sessionHookPlan{replace: true}, nil
	case decision == installstate.DecisionLeaveUnowned || decision == "" && ledger.IsLeft(key, path):
		return sessionHookPlan{left: true, converged: ledger.IsLeft(key, path)}, nil
	default:
		return sessionHookPlan{blocker: path + " holds an Atomic SessionStart handler Atomic did not write in this form; move or remove it, or rerun with --replace or --leave-unowned"}, nil
	}
}

// writeSessionHook is the hooks.json write, a seam so a test can observe the
// ledger at the moment the write begins.
var writeSessionHook = func(root string, replace bool) (bool, error) {
	if replace {
		return hooks.ReplaceCodex(root)
	}
	return hooks.InstallCodex(root)
}

// ownedHookRow is the member's ledger row for the canonical form a write
// produces.
func ownedHookRow(t harness.Target, generation, path, digest string) installstate.Row {
	return installstate.Row{
		Target:     t.Key(),
		Resource:   path,
		Consumer:   t.Key(),
		Generation: generation,
		Tier:       string(Tier),
		Applied:    installstate.AppliedValue{Path: path, Kind: managedfile.KindSettings, Digest: digest},
	}
}

// applySessionHook carries out plan and records the member's row, saved before
// the write so a crash leaves a claim to repair; a skipped or failed write undoes it.
func applySessionHook(home string, ledger *installstate.Ledger, t harness.Target, generation string, plan sessionHookPlan) (wrote, changed bool, err error) {
	path := hooks.CodexHooksPath(t.NativeRoot)
	if plan.left {
		return false, ledger.SetLeft(t.Key(), t.NativeRoot, []string{path}), nil
	}
	leftChanged := ledger.SetLeft(t.Key(), t.NativeRoot, nil)
	before, err := hooks.ObserveCodexHooksOwned(t.NativeRoot)
	if err != nil {
		return false, false, err
	}
	undo, err := journalSessionHookRow(home, t, generation, path)
	if err != nil {
		return false, false, err
	}
	skipped, err := writeSessionHook(t.NativeRoot, plan.replace)
	if err != nil {
		return false, false, errors.Join(fmt.Errorf("codex: register session-start hook: %w", err), undo())
	}
	if skipped {
		if err := undo(); err != nil {
			return false, false, err
		}
	}
	after, err := hooks.ObserveCodexHooksOwned(t.NativeRoot)
	if err != nil {
		return false, false, err
	}
	wrote = after.Digest != before.Digest
	if after.Conflict != managedfile.ConflictNone || !after.Exists() {
		return wrote, dropRow(ledger, t.Key(), after.Path) || leftChanged, nil
	}
	return wrote, ledger.Upsert(ownedHookRow(t, generation, after.Path, after.Digest)) || leftChanged, nil
}

// journalSessionHookRow commits the hooks.json row alone, leaving the other rows
// to their transaction; undo restores the row as it was.
func journalSessionHookRow(home string, t harness.Target, generation, path string) (undo func() error, err error) {
	ledgerPath := config.LedgerPath(home)
	disk, err := installstate.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	prior, hadPrior := disk.Find(t.Key(), path)
	_, hadTarget := disk.FindTarget(string(t.Kind), t.Instance)
	changed := disk.Upsert(ownedHookRow(t, generation, path, hooks.CodexCanonicalDigest()))
	// A first enrollment records its target with the row, so a crash leaves a
	// row that repair, status and uninstall can reach.
	if !hadTarget {
		disk.UpsertTarget(installstate.TargetRecord{Harness: string(t.Kind), Instance: t.Instance, NativeRoot: t.NativeRoot, Status: string(harness.StatusEnrolled)})
		changed = true
	}
	if !changed {
		return func() error { return nil }, nil
	}
	if err := disk.Save(ledgerPath); err != nil {
		return nil, err
	}
	return func() error {
		disk, err := installstate.LoadLedger(ledgerPath)
		if err != nil {
			return err
		}
		dropRow(disk, t.Key(), path)
		if hadPrior {
			disk.Upsert(prior)
		}
		if !hadTarget {
			disk.Targets = slices.DeleteFunc(disk.Targets, func(r installstate.TargetRecord) bool {
				return r.Harness == string(t.Kind) && r.Instance == t.Instance
			})
		}
		return disk.Save(ledgerPath)
	}, nil
}

func dropRow(ledger *installstate.Ledger, key, resource string) bool {
	kept := ledger.Rows[:0:0]
	for _, row := range ledger.Rows {
		if row.Target != key || row.Resource != resource {
			kept = append(kept, row)
		}
	}
	changed := len(kept) != len(ledger.Rows)
	ledger.Rows = kept
	return changed
}

// HookTrust is Codex's recorded trust in an owned hooks.json member.
type HookTrust string

const (
	HookTrusted   HookTrust = "trusted"
	HookUntrusted HookTrust = "untrusted"
	// HookTrustModified is a trusted hash that no longer matches the member, which
	// Codex reports as modified and does not run.
	HookTrustModified HookTrust = "modified"
	HookTrustUnknown  HookTrust = "unknown"
)

// ObserveSessionHookTrust reads, never writes, Codex's trust record for the
// owned member. See docs/research/harness-capability-matrix.md, Session context.
func ObserveSessionHookTrust(home, root string) (HookTrust, string) {
	return observeHookTrust(home, root, sessionStartTrust)
}

// ObservePreToolUseHookTrust is ObserveSessionHookTrust for the PreToolUse member.
func ObservePreToolUseHookTrust(home, root string) (HookTrust, string) {
	return observeHookTrust(home, root, preToolUseTrust)
}

func observeHookTrust(home, root string, m trustedMember) (HookTrust, string) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return HookTrustUnknown, err.Error()
	}
	target := harness.Target{Kind: harness.KindCodex, Instance: root, NativeRoot: root}
	if ledger.IsLeft(target.Key(), hooks.CodexHooksPath(root)) {
		return HookTrustUnknown, "member left unowned; Atomic does not report trust for a hook it does not own"
	}
	group, handler, found, err := m.position(root)
	if err != nil {
		return HookTrustUnknown, err.Error()
	}
	if !found {
		return HookTrustUnknown, "hooks.json carries no Atomic " + m.name + " member"
	}
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return HookTrustUnknown, err.Error()
	}
	var doc struct {
		Hooks struct {
			State map[string]struct {
				Enabled     *bool  `toml:"enabled"`
				TrustedHash string `toml:"trusted_hash"`
			} `toml:"state"`
		} `toml:"hooks"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return HookTrustUnknown, fmt.Sprintf("%s does not parse: %v", ConfigPath(root), err)
	}

	if len(doc.Hooks.State) == 0 {
		return HookUntrusted, fmt.Sprintf("%s records no hook trust; review and trust the hook in Codex's /hooks", ConfigPath(root))
	}
	suffix := fmt.Sprintf(":%s:%d:%d", m.eventKey, group, handler)
	path := hooks.CodexHooksPath(root)
	keys := []string{path + suffix}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		keys = append(keys, resolved+suffix)
	}
	for _, key := range keys {
		state, ok := doc.Hooks.State[key]
		if !ok {
			continue
		}
		disabled := ""
		if state.Enabled != nil && !*state.Enabled {
			disabled = "; the hook is disabled"
		}
		if state.TrustedHash == "" {
			return HookUntrusted, fmt.Sprintf("%s records the member with no trusted hash%s; review and trust it in Codex's /hooks", ConfigPath(root), disabled)
		}
		if state.TrustedHash == m.identityHash() {
			return HookTrusted, fmt.Sprintf("%s records the member's current hash%s", ConfigPath(root), disabled)
		}
		return HookTrustModified, fmt.Sprintf("%s records a trusted hash that differs from the member's current hash%s; review the hook in Codex's /hooks and trust it again", ConfigPath(root), disabled)
	}
	return HookUntrusted, fmt.Sprintf("%s records no trust for this member, only for other hooks; review and trust it in Codex's /hooks", ConfigPath(root))
}

// trustedMember is an owned member as Codex keys and hashes it in config.toml.
type trustedMember struct {
	name, eventKey, matcher, command string
	position                         func(root string) (group, handler int, found bool, err error)
}

var (
	sessionStartTrust = trustedMember{"SessionStart", "session_start", hooks.CodexSessionStartMatcher, hooks.CodexSessionStartCommand, hooks.CodexSessionStartPosition}
	preToolUseTrust   = trustedMember{"PreToolUse", "pre_tool_use", hooks.CodexPreToolUseMatcher, hooks.CodexPreToolUseCommand, hooks.CodexPreToolUsePosition}
)

// identityHash mirrors upstream Codex's hook_hash, whose normalized handler
// carries the 600-second default timeout and async false.
func (m trustedMember) identityHash() string {
	identity := map[string]any{
		"event_name": m.eventKey,
		"matcher":    m.matcher,
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": m.command,
			"timeout": 600,
			"async":   false,
		}},
	}
	data, _ := json.Marshal(identity)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
