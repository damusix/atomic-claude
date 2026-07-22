// Package claudeinstall writes the embedded artifact bundle to a target directory
// (default ~/.claude) and manages backups for changed files. Config state
// (config.toml, backups, profile.md, ...) always lives under the user's home
// directory (~/.atomic — fixed, D1), independent of the artifact target.
package claudeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// Clock allows injecting a fixed time in tests.
type Clock func() time.Time

// RealClock returns time.Now().UTC().
func RealClock() time.Time { return time.Now().UTC() }

// DefaultProfileRefresh is the real implementation used in production.
// Exposed so tests can restore the original after overriding ProfileRefresh.
var DefaultProfileRefresh = profile.RefreshIfStale

// ProfileRefresh is an injectable seam for tests: swap it with a spy to capture
// calls without real detection, home-dir resolution, or disk writes. Production
// code always calls DefaultProfileRefresh; only tests override this.
var ProfileRefresh = profile.RefreshIfStale

// ActionKind classifies what install/update will do to a file.
type ActionKind string

const (
	ActionInstalled     ActionKind = "installed"
	ActionUpdated       ActionKind = "updated"
	ActionUnchanged     ActionKind = "unchanged"
	ActionMergeRequired ActionKind = "merge_required"
	// ActionBlockReplaced: CLAUDE.md only — the on-disk file carries an
	// <atomic> block that differs from the embedded one. The block is
	// replaced in place; user content outside it is preserved.
	ActionBlockReplaced ActionKind = "block_replaced"
)

// FileAction describes the planned or executed action for one artifact.
type FileAction struct {
	Artifact     embedded.Artifact
	Kind         ActionKind
	BackupPath   string // set when ActionUpdate
	ProposedPath string // set when ActionMergeRequired
}

// loadAgentOverrides reads the config and returns the [agents] override map.
// Best-effort: returns nil when the config is absent, unreadable, or has no
// [agents] entries so callers treat nil as "no overrides, use bundled defaults".
func loadAgentOverrides(home string) map[string]config.AgentOverride {
	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil || len(cfg.Agents) == 0 {
		return nil
	}
	return cfg.Agents
}

// patchAgentContent rewrites the model: and/or effort: keys in an agent
// artifact's frontmatter to the configured overrides, preserving all other
// keys and their source order.
//
// It is a no-op when:
//   - overrides is nil or has no entry for this agent name
//   - target does not start with "agents/"
//   - the entry has both Model and Effort empty
//   - the file has no parseable frontmatter block
//   - frontmatter parsing or emission fails (returns original content unchanged)
//
// Model and Effort are patched independently: an effort-only override leaves
// model: untouched, and vice versa.
//
// This is called from both Plan (to compute the correct expected SHA) and
// Apply (to write the patched bytes) so both sides agree on the on-disk content.
func patchAgentContent(target string, content []byte, overrides map[string]config.AgentOverride) []byte {
	if len(overrides) == 0 || !strings.HasPrefix(target, "agents/") {
		return content
	}
	// Agent name is the basename without the .md suffix.
	agentName := strings.TrimSuffix(filepath.Base(filepath.FromSlash(target)), ".md")
	ov, ok := overrides[agentName]
	if !ok || (ov.Model == "" && ov.Effort == "") {
		return content
	}

	kvs, body, err := frontmatter.ParseOrdered(string(content))
	if err != nil || len(kvs) == 0 {
		// No parseable frontmatter — leave unchanged; the agent runtime will use
		// its built-in default (LLM-exception: we cannot patch without a block).
		return content
	}

	if ov.Model != "" {
		kvs = setOrAppendKey(kvs, "model", ov.Model)
	}
	if ov.Effort != "" {
		kvs = setOrAppendKey(kvs, "effort", ov.Effort)
	}

	result, err := frontmatter.EmitOrdered(kvs, body)
	if err != nil {
		return content // best-effort: return original on serialisation failure
	}
	return []byte(result)
}

// setOrAppendKey sets the value of an existing key or appends it when absent,
// preserving the source order of every other key.
func setOrAppendKey(kvs []frontmatter.KV, key, value string) []frontmatter.KV {
	for i := range kvs {
		if kvs[i].Key == key {
			kvs[i].Value = value
			return kvs
		}
	}
	return append(kvs, frontmatter.KV{Key: key, Value: value})
}

// Plan computes the per-file action list without writing anything.
// It loads the [agents] config overrides and factors the patched content into
// the SHA comparison so the plan correctly reflects what Apply will write.
//
// targetDir is the Claude artifact install root (commands/, agents/, ...);
// home is the user's home directory, the root of atomic-owned config state
// (~/.atomic). The two are split because --target can point install anywhere,
// while config state always lives under the real home (D1/D4).
func Plan(targetDir, home string, manifest []embedded.Artifact) ([]FileAction, error) {
	overrides := loadAgentOverrides(home)
	var plan []FileAction
	for _, a := range manifest {
		fa, err := planArtifact(targetDir, home, a, overrides)
		if err != nil {
			return nil, err
		}
		plan = append(plan, fa)
	}
	return plan, nil
}

// readPatchedEmbedded reads an embedded artifact's bytes and applies the
// [agents] tier override (a no-op for non-agent artifacts or when overrides
// is nil), so every caller compares/writes against the same effective content.
func readPatchedEmbedded(a embedded.Artifact, overrides map[string]config.AgentOverride) ([]byte, error) {
	data, err := fs.ReadFile(embedded.FS, a.Source)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", a.Source, err)
	}
	return patchAgentContent(a.Target, data, overrides), nil
}

func planArtifact(targetDir, home string, a embedded.Artifact, agentOverrides map[string]config.AgentOverride) (FileAction, error) {
	onDiskPath := filepath.Join(targetDir, filepath.FromSlash(a.Target))

	// Patch before SHA so the plan reflects the bytes Apply will write to disk.
	embeddedData, err := readPatchedEmbedded(a, agentOverrides)
	if err != nil {
		return FileAction{}, err
	}

	embeddedSHA := hexSHA256(embeddedData)

	diskData, err := os.ReadFile(onDiskPath)
	if os.IsNotExist(err) {
		return FileAction{Artifact: a, Kind: ActionInstalled}, nil
	}
	if err != nil {
		return FileAction{}, fmt.Errorf("read on-disk %s: %w", onDiskPath, err)
	}

	diskSHA := hexSHA256(diskData)
	if diskSHA == embeddedSHA {
		return FileAction{Artifact: a, Kind: ActionUnchanged}, nil
	}

	// Differs. CLAUDE.md is block-aware: the <atomic>...</atomic> block is
	// atomic-owned, everything outside is user-owned. When both sides carry a
	// parseable block, compare and replace only the block — user content
	// outside it never causes drift or a merge. The proposed-file + LLM merge
	// path remains only for files without a parseable block (pre-tag installs,
	// malformed tags) where code cannot draw the ownership boundary safely.
	if a.Target == "CLAUDE.md" {
		embBlock, embOK := extractAtomicBlock(string(embeddedData))
		diskBlock, diskOK := extractAtomicBlock(string(diskData))
		if embOK && diskOK {
			if embBlock == diskBlock {
				return FileAction{Artifact: a, Kind: ActionUnchanged}, nil
			}
			return FileAction{Artifact: a, Kind: ActionBlockReplaced}, nil
		}
		proposedPath := config.ProposedCLAUDEMD(home)
		return FileAction{Artifact: a, Kind: ActionMergeRequired, ProposedPath: proposedPath}, nil
	}

	// Bundle-managed artifact: back up + overwrite.
	return FileAction{Artifact: a, Kind: ActionUpdated}, nil
}

// ReapplyAgents re-patches only the agent artifacts already installed on
// disk at targetDir with the current [agents] config overrides from home. It
// never performs a first-time install: an agent artifact absent from disk is
// left untouched. changed holds the basenames (without .md) of the agent
// files actually rewritten; installed counts every agent artifact found
// already present on disk (in sync or rewritten).
//
// Reuses Plan/Apply so writes get the same backup behavior as a normal
// install/update, filtered down to the agent-artifact ActionUpdated subset.
func ReapplyAgents(targetDir, home string) (changed []string, installed int, err error) {
	plan, err := Plan(targetDir, home, embedded.Manifest())
	if err != nil {
		return nil, 0, err
	}

	var toApply []FileAction
	for _, fa := range plan {
		if fa.Artifact.Kind != "agent" || fa.Kind == ActionInstalled {
			continue // not an agent, or absent on disk — never a first-time install
		}
		installed++
		if fa.Kind == ActionUpdated {
			toApply = append(toApply, fa)
		}
	}

	if err := Apply(targetDir, home, toApply, false, RealClock); err != nil {
		return nil, installed, err
	}

	for _, fa := range toApply {
		changed = append(changed, strings.TrimSuffix(filepath.Base(filepath.FromSlash(fa.Artifact.Target)), ".md"))
	}
	return changed, installed, nil
}

// Apply executes a plan. If dryRun is true, no filesystem writes occur.
// clock is used for the backup timestamp — pass RealClock for production use.
//
// Apply loads the [agents] config overrides from home and patches each
// agent artifact's model: and effort: frontmatter keys before writing, so the
// user's configured model/effort overrides are always re-applied on every
// install/update.
func Apply(targetDir, home string, plan []FileAction, dryRun bool, clock Clock) error {
	// Capture the run-start time once so all backups in this run share the same
	// timestamp directory, regardless of when the first ActionUpdated is encountered.
	runStart := clock()

	// Compute the backup timestamp only when there are updates to make.
	var backupTimestamp string
	for _, fa := range plan {
		if fa.Kind == ActionUpdated || fa.Kind == ActionBlockReplaced {
			backupTimestamp = formatTimestamp(runStart)
			break
		}
	}

	// Load agent model-tier overrides once for the whole apply run.
	// Best-effort: nil means no overrides → bundled defaults used.
	agentOverrides := loadAgentOverrides(home)

	for i := range plan {
		if err := applyAction(targetDir, home, &plan[i], dryRun, backupTimestamp, agentOverrides); err != nil {
			return err
		}
	}
	if !dryRun {
		if err := ensureResolvedConfigStub(home); err != nil {
			return err
		}
	}
	return nil
}

// ensureResolvedConfigStub creates <home>/.atomic/config.resolved.md as an empty
// file if it does not already exist. Idempotent: leaves any existing content untouched.
// This file is @-referenced from CLAUDE.md so every Claude session can load it.
func ensureResolvedConfigStub(home string) error {
	resolvedPath := config.ResolvedPath(home)
	if _, err := os.Stat(resolvedPath); err == nil {
		return nil // already exists — leave it alone
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for config.resolved.md: %w", err)
	}
	return os.WriteFile(resolvedPath, []byte{}, 0o644)
}

// ProfileNudge is the bootstrap message printed to stdout when profile.md is
// created for the first time. Tests reference this constant to avoid duplicating
// the verbatim string.
const ProfileNudge = "Profile created at ~/.atomic/profile.md. Mention your role, projects, and preferences in conversation and Claude will record them. Run /retrospective-learning to review drift."

// ensureProfileStub creates <home>/.atomic/profile.md with the initial schema
// template if it does not already exist. Idempotent: leaves any existing content untouched.
// When the file is created, it prints a bootstrap nudge to out.
// Returns (true, nil) when the file was created, (false, nil) when it already existed.
func ensureProfileStub(home string, out io.Writer) (bool, error) {
	profilePath := config.ProfilePath(home)
	if _, err := os.Stat(profilePath); err == nil {
		return false, nil // already exists — leave it alone
	}
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir for profile.md: %w", err)
	}
	e := profile.CaptureEnv()
	content := profile.RenderStub(e)
	if err := os.WriteFile(profilePath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write profile.md: %w", err)
	}
	fmt.Fprintln(out, ProfileNudge)
	return true, nil
}

// populateProfile calls the profileRefresh seam in a best-effort manner.
// Any error returned by the seam and any panic are silently swallowed so
// install/update never fails due to a detection error.
// today is derived from clock so tests can inject a fixed date.
func populateProfile(home string, clock Clock) {
	defer func() { recover() }() // best-effort: swallows any panic from detection so install never fails
	today := clock().Format("2006-01-02")
	_, _ = ProfileRefresh(home, today, profile.DefaultRefreshDays)
}

func applyAction(targetDir, home string, fa *FileAction, dryRun bool, backupTimestamp string, agentOverrides map[string]config.AgentOverride) error {
	onDiskPath := filepath.Join(targetDir, filepath.FromSlash(fa.Artifact.Target))

	// Patch agent frontmatter with the configured model/effort overrides before
	// any write. This ensures the user's choices survive every install/update
	// cycle, including binary upgrades that ship new bundled agent content.
	embeddedData, err := readPatchedEmbedded(fa.Artifact, agentOverrides)
	if err != nil {
		return err
	}

	switch fa.Kind {
	case ActionInstalled:
		if dryRun {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(onDiskPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", onDiskPath, err)
		}
		return os.WriteFile(onDiskPath, embeddedData, 0o644)

	case ActionUpdated:
		backupPath := filepath.Join(config.BackupDir(home), backupTimestamp, filepath.FromSlash(fa.Artifact.Target))
		fa.BackupPath = backupPath
		if dryRun {
			return nil
		}
		// Back up existing file.
		existing, err := os.ReadFile(onDiskPath)
		if err != nil {
			return fmt.Errorf("read existing for backup %s: %w", onDiskPath, err)
		}
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return fmt.Errorf("mkdir backup: %w", err)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return fmt.Errorf("write backup %s: %w", backupPath, err)
		}
		return os.WriteFile(onDiskPath, embeddedData, 0o644)

	case ActionMergeRequired:
		proposedPath := fa.ProposedPath
		if dryRun {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(proposedPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for proposed: %w", err)
		}
		return os.WriteFile(proposedPath, embeddedData, 0o644)

	case ActionBlockReplaced:
		backupPath := filepath.Join(config.BackupDir(home), backupTimestamp, filepath.FromSlash(fa.Artifact.Target))
		fa.BackupPath = backupPath
		if dryRun {
			return nil
		}
		existing, err := os.ReadFile(onDiskPath)
		if err != nil {
			return fmt.Errorf("read existing for backup %s: %w", onDiskPath, err)
		}
		embBlock, ok := extractAtomicBlock(string(embeddedData))
		if !ok {
			return fmt.Errorf("embedded %s has no parseable <atomic> block", fa.Artifact.Target)
		}
		merged, ok := replaceAtomicBlock(string(existing), embBlock)
		if !ok {
			// Plan saw a parseable block; the file changed between plan and
			// apply. Fail loud rather than guessing the boundary.
			return fmt.Errorf("%s lost its parseable <atomic> block between plan and apply", onDiskPath)
		}
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return fmt.Errorf("mkdir backup: %w", err)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return fmt.Errorf("write backup %s: %w", backupPath, err)
		}
		return os.WriteFile(onDiskPath, []byte(merged), 0o644)

	case ActionUnchanged:
		// Nothing to do.
		return nil
	}
	return nil
}

// Install computes and applies the install plan. Equivalent to Update — same semantics.
// Profile nudge goes to os.Stdout.
//
// targetDir is the Claude artifact install root; home is the user's home
// directory, the root of atomic-owned config state (~/.atomic).
func Install(targetDir, home string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installWithOutput(targetDir, home, dryRun, clock, os.Stdout)
}

// installWithOutput is Install with a configurable writer for the profile bootstrap nudge.
// Unexported — exported via export_test.go for test use only.
func installWithOutput(targetDir, home string, dryRun bool, clock Clock, out io.Writer) ([]FileAction, error) {
	return installOrUpdate(targetDir, home, dryRun, clock, out)
}

// Update is the same flow as Install.
func Update(targetDir, home string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installOrUpdate(targetDir, home, dryRun, clock, os.Stdout)
}

func installOrUpdate(targetDir, home string, dryRun bool, clock Clock, out io.Writer) ([]FileAction, error) {
	manifest := embedded.Manifest()

	// Capture pre-install state before any files are written. Write-once: if the
	// snapshot dir already exists this is a no-op. Skip when dry-running.
	if !dryRun {
		if err := writePreInstallSnapshot(targetDir, home, manifest, clock); err != nil {
			return nil, fmt.Errorf("pre-install snapshot: %w", err)
		}
	}

	// Load the old config NOW to detect stale artifacts from the prior install.
	// Must happen before Plan/Apply so we read the old manifest, not the one we
	// are about to write. Best-effort: if the config doesn't exist yet (first-ever
	// install) storedTargetSlice returns nil → prune is a no-op.
	var staleTargets []string
	if !dryRun {
		cfgPath := config.TOMLPath(home)
		if oldCfg, _, cfgErr := config.Load(cfgPath); cfgErr == nil {
			stored := storedTargetSlice(oldCfg)
			if len(stored) > 0 {
				staleTargets = PruneDiff(stored, currentBundleTargetSet())
			}
		}
	}

	plan, err := Plan(targetDir, home, manifest)
	if err != nil {
		return nil, err
	}
	if err := Apply(targetDir, home, plan, dryRun, clock); err != nil {
		return nil, err
	}
	if !dryRun {
		// ensureProfileStub is intentionally called here (install/update level),
		// NOT inside Apply. Apply handles bundle artifacts; profile.md is user-data
		// that should never be overwritten by a plain Apply call (e.g. a dry-run
		// caller or a future Apply-only code path). Keeping it here ensures the file
		// is only created when a real install/update is requested.
		if _, err := ensureProfileStub(home, out); err != nil {
			return nil, err
		}
		// After the stub exists, populate the ## Environment fingerprint.
		// Called unconditionally: RefreshIfStale self-gates on lastcheck.
		// Fresh install: stub has no lastcheck → stale → full detect.
		// Re-install/update with fresh lastcheck: no-op.
		// Best-effort: any error or panic is swallowed — install must not fail
		// because detection failed (mirrors the session-start hook's swallow behavior).
		populateProfile(home, clock)

		// Prune stale artifacts — those present in the old [install.artifacts] but
		// absent from the current bundle (removed or renamed upstream).
		// Batched confirm: the seam (PruneConfirm) lists all stale paths to the user
		// before removing anything. Non-interactive terminals silently skip.
		if _, err := runPrune(targetDir, staleTargets); err != nil {
			return nil, err
		}

		// Write the updated [install] manifest so the next run knows what is installed.
		if err := writeInstallManifest(home, plan); err != nil {
			return nil, fmt.Errorf("write install manifest: %w", err)
		}
	}
	return plan, nil
}

// DiffStatus is the per-artifact comparison result for the diff verb.
type DiffStatus string

const (
	DiffMatch  DiffStatus = "match"
	DiffDiffer DiffStatus = "diff"
	DiffAbsent DiffStatus = "absent"
)

// DiffRow is one row in the diff output.
type DiffRow struct {
	Status   DiffStatus
	Artifact embedded.Artifact
}

// Diff compares each manifest artifact against the on-disk state. Read-only.
// Loads the [agents] config overrides once so agent rows are compared against
// the patched content Apply would have written, not the raw bundle bytes —
// otherwise a correct install with a configured tier override falsely reports
// as drifted (issue #129).
func Diff(targetDir, home string) ([]DiffRow, error) {
	manifest := embedded.Manifest()
	agentOverrides := loadAgentOverrides(home)
	var rows []DiffRow
	for _, a := range manifest {
		onDiskPath := filepath.Join(targetDir, filepath.FromSlash(a.Target))

		embeddedData, err := readPatchedEmbedded(a, agentOverrides)
		if err != nil {
			return nil, err
		}

		diskData, err := os.ReadFile(onDiskPath)
		if os.IsNotExist(err) {
			rows = append(rows, DiffRow{Status: DiffAbsent, Artifact: a})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read on-disk %s: %w", onDiskPath, err)
		}

		switch {
		case hexSHA256(embeddedData) == hexSHA256(diskData):
			rows = append(rows, DiffRow{Status: DiffMatch, Artifact: a})
		case a.Target == "CLAUDE.md" && atomicBlocksEqual(embeddedData, diskData):
			// Merged CLAUDE.md: user content outside the <atomic> block is
			// expected and is not drift. Only a stale block differs.
			rows = append(rows, DiffRow{Status: DiffMatch, Artifact: a})
		default:
			rows = append(rows, DiffRow{Status: DiffDiffer, Artifact: a})
		}
	}
	return rows, nil
}

// ListRow is one row in the list output.
type ListRow struct {
	Kind   string
	Target string
	SHA256 string
}

// List returns all manifest artifacts in stable sort order (kind asc, target asc).
func List() []ListRow {
	manifest := embedded.Manifest()
	rows := make([]ListRow, len(manifest))
	for i, a := range manifest {
		rows[i] = ListRow{Kind: a.Kind, Target: a.Target, SHA256: a.SHA256}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Target < rows[j].Target
	})
	return rows
}

// Report renders the final summary for install/update.
func Report(plan []FileAction, targetDir string) string {
	var installed, updated, unchanged, mergeRequired, blockReplaced []FileAction

	for _, fa := range plan {
		switch fa.Kind {
		case ActionInstalled:
			installed = append(installed, fa)
		case ActionUpdated:
			updated = append(updated, fa)
		case ActionUnchanged:
			unchanged = append(unchanged, fa)
		case ActionMergeRequired:
			mergeRequired = append(mergeRequired, fa)
		case ActionBlockReplaced:
			blockReplaced = append(blockReplaced, fa)
		}
	}

	// Compute the shared backup directory from the first updated action's BackupPath.
	// BackupPath shape: <targetDir>/.atomic/backups/<timestamp>/<relpath>
	// We want: <targetDir>/.atomic/backups/<timestamp>
	backupDir := ""
	for _, fa := range append(append([]FileAction{}, updated...), blockReplaced...) {
		if fa.BackupPath != "" {
			// The relpath inside BackupPath matches fa.Artifact.Target, so strip it off.
			rel := filepath.FromSlash(fa.Artifact.Target)
			candidate := strings.TrimSuffix(fa.BackupPath, string(os.PathSeparator)+rel)
			if candidate != fa.BackupPath {
				backupDir = candidate
			} else {
				backupDir = filepath.Dir(fa.BackupPath)
			}
			break
		}
	}

	var sb strings.Builder
	sb.WriteString("Atomic Claude install summary\n")

	if len(installed) > 0 {
		fmt.Fprintf(&sb, "\nInstalled (%d):\n", len(installed))
		for _, fa := range installed {
			fmt.Fprintf(&sb, "  ✓ %s\n", fa.Artifact.Target)
		}
	}

	if len(updated) > 0 {
		fmt.Fprintf(&sb, "\nUpdated (%d, backed up to %s/):\n", len(updated), backupDir)
		for _, fa := range updated {
			fmt.Fprintf(&sb, "  ↻ %s\n", fa.Artifact.Target)
		}
	}

	if len(unchanged) > 0 {
		fmt.Fprintf(&sb, "\nUnchanged (%d):\n", len(unchanged))
		for _, fa := range unchanged {
			fmt.Fprintf(&sb, "  • %s\n", fa.Artifact.Target)
		}
	}

	if len(blockReplaced) > 0 {
		fmt.Fprintf(&sb, "\nUpdated <atomic> block (%d, backed up to %s/):\n", len(blockReplaced), backupDir)
		for _, fa := range blockReplaced {
			fmt.Fprintf(&sb, "  ↻ %s (your content outside <atomic> preserved)\n", fa.Artifact.Target)
		}
	}

	if len(mergeRequired) > 0 {
		fmt.Fprintf(&sb, "\nNeeds review (%d):\n", len(mergeRequired))
		for _, fa := range mergeRequired {
			absTarget := filepath.Join(targetDir, fa.Artifact.Target)
			fmt.Fprintf(&sb, "  ⚠ %s\n", absTarget)
			fmt.Fprintf(&sb, "    proposed at %s\n", fa.ProposedPath)
			fmt.Fprintf(&sb, "    next step: in a Claude Code session, run `atomic prompt claude-merge` to merge your config\n")
		}
	}

	return sb.String()
}

// formatTimestamp formats a time as ISO-8601 with colons replaced by hyphens
// so the string is safe for use as a directory name on all platforms.
func formatTimestamp(t time.Time) string {
	s := t.UTC().Format(time.RFC3339)
	// Replace colons with hyphens: 2026-05-16T18:32:11Z → 2026-05-16T18-32-11Z
	return strings.ReplaceAll(s, ":", "-")
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ResolveTarget expands "~" in the target path.
func ResolveTarget(target string) (string, error) {
	if target == "" || target == "~/.claude" || target == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if target == "~" {
			return home, nil
		}
		return filepath.Join(home, ".claude"), nil
	}
	if strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Join(home, target[2:]), nil
	}
	return target, nil
}
