// Package claudeinstall writes the embedded artifact bundle to a target directory
// (default ~/.claude) and manages backups for changed files. Config state always
// lives under ~/.atomic, independent of the artifact target.
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

func RealClock() time.Time { return time.Now().UTC() }

// DefaultProfileRefresh lets tests restore ProfileRefresh after overriding it.
var DefaultProfileRefresh = profile.RefreshIfStale

// ProfileRefresh is a test seam: stubbed with a spy so tests capture calls
// without real detection, home-dir resolution, or disk writes.
var ProfileRefresh = profile.RefreshIfStale

// ActionKind classifies what install/update will do to a file.
type ActionKind string

const (
	ActionInstalled     ActionKind = "installed"
	ActionUpdated       ActionKind = "updated"
	ActionUnchanged     ActionKind = "unchanged"
	ActionMergeRequired ActionKind = "merge_required"
	// ActionBlockReplaced: CLAUDE.md only — the <atomic> block is replaced in
	// place; user content outside it is preserved.
	ActionBlockReplaced ActionKind = "block_replaced"
)

// FileAction describes the planned or executed action for one artifact.
type FileAction struct {
	Artifact     embedded.Artifact
	Kind         ActionKind
	BackupPath   string // set when ActionUpdate
	ProposedPath string // set when ActionMergeRequired
}

// loadAgentOverrides returns the [claude.agents] override map. Best-effort: nil
// means no overrides, so callers fall back to the bundled defaults.
func loadAgentOverrides(home string) map[string]config.AgentOverride {
	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil || len(cfg.Claude.Agents) == 0 {
		return nil
	}
	return cfg.Claude.Agents
}

// patchAgentContent rewrites model: and effort: in an agent artifact's
// frontmatter, preserving every other key and its source order. The two keys
// patch independently. Returns content unchanged when there is no override, no
// parseable frontmatter, or emission fails. Both Plan and Apply call it so the
// planned SHA matches the bytes written.
func patchAgentContent(target string, content []byte, overrides map[string]config.AgentOverride) []byte {
	if len(overrides) == 0 || !strings.HasPrefix(target, "agents/") {
		return content
	}
	agentName := strings.TrimSuffix(filepath.Base(filepath.FromSlash(target)), ".md")
	ov, ok := overrides[agentName]
	if !ok || (ov.Model == "" && ov.Effort == "") {
		return content
	}

	kvs, body, err := frontmatter.ParseOrdered(string(content))
	if err != nil || len(kvs) == 0 {
		// Nothing to patch — the agent runtime falls back to its built-in default.
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
		return content
	}
	return []byte(result)
}

// setOrAppendKey sets an existing key's value or appends it, preserving the
// source order of every other key.
func setOrAppendKey(kvs []frontmatter.KV, key, value string) []frontmatter.KV {
	for i := range kvs {
		if kvs[i].Key == key {
			kvs[i].Value = value
			return kvs
		}
	}
	return append(kvs, frontmatter.KV{Key: key, Value: value})
}

// Plan computes the per-file action list without writing anything, factoring
// the [claude.agents] overrides into the SHA comparison so the plan reflects
// what Apply will write. targetDir is the artifact install root; home roots
// atomic-owned config state, split because --target can point anywhere.
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

// readPatchedEmbedded reads an artifact's embedded bytes with overrides applied,
// so every caller compares and writes the same effective content.
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

	// CLAUDE.md is block-aware: the <atomic> block is atomic-owned, everything
	// outside is user-owned. The proposed-file + LLM merge path is only for files
	// without a parseable block, where code cannot draw the boundary safely.
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

	return FileAction{Artifact: a, Kind: ActionUpdated}, nil
}

// ReapplyAgents re-patches the agent artifacts already on disk with the current
// [claude.agents] overrides; it never performs a first-time install. changed
// holds the basenames rewritten, installed counts the agents found on disk.
// Reuses Plan/Apply so writes keep normal install backup behavior.
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

// Apply executes a plan, re-applying the configured agent overrides on every
// write. If dryRun is true, no filesystem writes occur. clock supplies the
// backup timestamp — pass RealClock for production use.
func Apply(targetDir, home string, plan []FileAction, dryRun bool, clock Clock) error {
	// Captured once so every backup in this run shares one timestamp directory.
	runStart := clock()

	var backupTimestamp string
	for _, fa := range plan {
		if fa.Kind == ActionUpdated || fa.Kind == ActionBlockReplaced {
			backupTimestamp = formatTimestamp(runStart)
			break
		}
	}

	agentOverrides := loadAgentOverrides(home)

	for i := range plan {
		if err := applyAction(targetDir, home, &plan[i], dryRun, backupTimestamp, agentOverrides); err != nil {
			return err
		}
	}
	return nil
}

// ProfileNudge is printed when profile.md is created for the first time.
const ProfileNudge = "Profile created at ~/.atomic/profile.md. Mention your role, projects, and preferences in conversation and Claude will record them. Run /retrospective-learning to review drift."

// ensureProfileStub creates <home>/.atomic/profile.md if absent, printing a
// bootstrap nudge to out. Reports whether it created the file.
func ensureProfileStub(home string, out io.Writer) (bool, error) {
	profilePath := config.ProfilePath(home)
	if _, err := os.Stat(profilePath); err == nil {
		return false, nil
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

// populateProfile refreshes the profile fingerprint. Errors and panics are
// swallowed so install never fails because detection did.
func populateProfile(home string, clock Clock) {
	defer func() { recover() }()
	today := clock().Format("2006-01-02")
	_, _ = ProfileRefresh(home, today, profile.DefaultRefreshDays)
}

func applyAction(targetDir, home string, fa *FileAction, dryRun bool, backupTimestamp string, agentOverrides map[string]config.AgentOverride) error {
	onDiskPath := filepath.Join(targetDir, filepath.FromSlash(fa.Artifact.Target))

	// Patch before any write so the user's model/effort choices survive binary
	// upgrades that ship new bundled agent content.
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
			// Changed underneath us — fail loud rather than guess the boundary.
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
		return nil
	}
	return nil
}

// Install computes and applies the install plan; same semantics as Update.
func Install(targetDir, home string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installWithOutput(targetDir, home, dryRun, clock, os.Stdout)
}

// installWithOutput is Install with a configurable writer for the bootstrap
// nudge; reached from tests via export_test.go.
func installWithOutput(targetDir, home string, dryRun bool, clock Clock, out io.Writer) ([]FileAction, error) {
	return installOrUpdate(targetDir, home, dryRun, clock, out)
}

// Update is the same flow as Install.
func Update(targetDir, home string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installOrUpdate(targetDir, home, dryRun, clock, os.Stdout)
}

func installOrUpdate(targetDir, home string, dryRun bool, clock Clock, out io.Writer) ([]FileAction, error) {
	manifest := embedded.Manifest()

	// Write-once: a no-op if the snapshot dir already exists.
	if !dryRun {
		if err := writePreInstallSnapshot(targetDir, home, manifest, clock); err != nil {
			return nil, fmt.Errorf("pre-install snapshot: %w", err)
		}
	}

	// Must read the OLD config before Plan/Apply, so the prune diff sees the
	// prior install's manifest rather than the one about to be written.
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
		// Deliberately not inside Apply: profile.md is user data, and a bare Apply
		// (dry-run or a future Apply-only path) must never create it.
		if _, err := ensureProfileStub(home, out); err != nil {
			return nil, err
		}
		// Unconditional — RefreshIfStale self-gates on lastcheck.
		populateProfile(home, clock)

		// Prune artifacts listed in the old [install.artifacts] but absent from the
		// current bundle. PruneConfirm batches the confirm; non-interactive skips.
		if _, err := runPrune(targetDir, staleTargets); err != nil {
			return nil, err
		}

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

// Diff compares each manifest artifact against disk, read-only. Agent rows
// compare against override-patched content — against raw bundle bytes, a
// correct install with a configured override would falsely report as drifted.
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
			// User content outside the <atomic> block is expected, not drift.
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

	// Shared backup dir = BackupPath minus the artifact's relpath.
	backupDir := ""
	for _, fa := range append(append([]FileAction{}, updated...), blockReplaced...) {
		if fa.BackupPath != "" {
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

// formatTimestamp renders t as RFC3339 with ':' replaced by '-', so the string
// is safe as a directory name.
func formatTimestamp(t time.Time) string {
	s := t.UTC().Format(time.RFC3339)
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
