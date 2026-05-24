package claudeinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// UninstallRestoreEntry is one file that should be restored from the pre-install
// snapshot during uninstall.
type UninstallRestoreEntry struct {
	// RelPath is the path relative to the target dir (e.g. "settings.json").
	RelPath string
	// NeedsMerge is true when the current on-disk file differs from the
	// pre-install snapshot — meaning the user modified it post-install and a
	// plain copy-back would lose their changes.
	NeedsMerge bool
}

// UninstallPlan holds the computed plan for `atomic claude uninstall`.
type UninstallPlan struct {
	// Restore is the set of files to restore from the pre-install snapshot.
	Restore []UninstallRestoreEntry
	// Delete is the set of paths to remove (atomic created them; none pre-existed).
	Delete []string
}

// BuildUninstallPlan reads ~/.claude/.atomic/pre-install/manifest.json and
// computes the restore/delete plan using the embedded bundle's SHAs to
// distinguish user modifications from atomic-only writes. Returns an error
// (with "no pre-install snapshot" in the message) when the manifest is absent.
func BuildUninstallPlan(targetDir string) (UninstallPlan, error) {
	artifacts := embedded.Manifest()
	embeddedSHAs := make(map[string]string, len(artifacts))
	for _, a := range artifacts {
		embeddedSHAs[a.Target] = a.SHA256
	}
	return BuildUninstallPlanWithManifest(targetDir, embeddedSHAs)
}

// BuildUninstallPlanWithManifest is the core implementation of BuildUninstallPlan
// with an injectable embeddedSHAs map for testing. Keys are Target paths
// (e.g. "CLAUDE.md", "agents/atomic-builder.md"); values are hex-encoded SHA256.
//
// Three-way merge detection for files that existed before install:
//   - current == pre-install SHA → Restore (unchanged since install, safe to copy back)
//   - current == embedded SHA    → Delete (atomic wrote it, user never touched it)
//   - current != pre-install AND current != embedded → Restore+NeedsMerge (user modified)
func BuildUninstallPlanWithManifest(targetDir string, embeddedSHAs map[string]string) (UninstallPlan, error) {
	preInstallDir := config.PreInstallDir(targetDir)
	manifestPath := filepath.Join(preInstallDir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return UninstallPlan{}, fmt.Errorf("no pre-install snapshot found at %s", manifestPath)
	}
	if err != nil {
		return UninstallPlan{}, fmt.Errorf("read pre-install manifest: %w", err)
	}

	var m PreInstallManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return UninstallPlan{}, fmt.Errorf("parse pre-install manifest: %w", err)
	}

	var plan UninstallPlan

	for _, f := range m.Files {
		if !f.Existed {
			plan.Delete = append(plan.Delete, f.Path)
			continue
		}

		// File existed before install. Read the current on-disk state and apply
		// three-way detection:
		//   current == pre-install → unchanged, safe restore
		//   current == embedded    → atomic-only write, user never modified → delete
		//   otherwise              → user modified post-install, needs merge
		needsMerge := false
		atomicOnly := false

		// Assumption: when Existed=true the snapshot writer always populates SHA256.
		// snapshotFile() only sets SHA256 for files it successfully reads, so an
		// empty SHA here would mean a corrupt or hand-edited manifest. In that case
		// we skip three-way detection and fall through to a plain Restore (safest
		// default: return the pre-install copy rather than deleting or merging blindly).
		if f.SHA256 != "" {
			onDiskPath := filepath.Join(targetDir, filepath.FromSlash(f.Path))
			onDiskData, readErr := os.ReadFile(onDiskPath)
			if readErr == nil {
				currentSHA := hexSHA256(onDiskData)
				switch {
				case currentSHA == f.SHA256:
					// Matches pre-install snapshot — unchanged since install.
					// needsMerge stays false, atomicOnly stays false.
				case embeddedSHAs[f.Path] != "" && currentSHA == embeddedSHAs[f.Path]:
					// Matches embedded bundle — atomic wrote it, user never touched it.
					atomicOnly = true
				default:
					// Differs from both — user modified post-install.
					needsMerge = true
				}
			}
			// If the file is missing on disk, no merge needed — restore straight.
		}

		if atomicOnly {
			// Treat as if it never existed — plain delete.
			plan.Delete = append(plan.Delete, f.Path)
		} else {
			plan.Restore = append(plan.Restore, UninstallRestoreEntry{
				RelPath:    f.Path,
				NeedsMerge: needsMerge,
			})
		}
	}

	return plan, nil
}

// GenerateUninstallPrompt builds the structured markdown prompt that Claude
// executes to perform the uninstall. The prompt is written to stdout and either
// run directly inside a Claude Code session or pasted into one.
func GenerateUninstallPrompt(targetDir string, plan UninstallPlan) string {
	var sb strings.Builder

	sb.WriteString("## Atomic Claude Uninstall\n\n")
	sb.WriteString("Run these steps in order. Confirm the plan with the user before executing.\n\n")
	sb.WriteString("### Plan\n\n")

	if len(plan.Restore) > 0 {
		sb.WriteString("Restore from pre-install:\n")
		for _, r := range plan.Restore {
			line := fmt.Sprintf("- %s", filepath.Join(targetDir, filepath.FromSlash(r.RelPath)))
			if r.NeedsMerge {
				line += " (NEEDS MERGE — user modified post-install)"
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(plan.Delete) > 0 {
		sb.WriteString("Delete (no pre-install counterpart):\n")
		for _, p := range plan.Delete {
			sb.WriteString(fmt.Sprintf("- %s\n", filepath.Join(targetDir, filepath.FromSlash(p))))
		}
		sb.WriteString("\n")
	}

	atomicDir := filepath.Join(targetDir, ".atomic")
	sb.WriteString("Remove directory:\n")
	sb.WriteString(fmt.Sprintf("- %s\n\n", atomicDir))

	sb.WriteString("### Instructions\n\n")
	sb.WriteString("1. Show this plan to the user. Get one confirmation before proceeding.\n")

	hasMerge := false
	for _, r := range plan.Restore {
		if r.NeedsMerge {
			hasMerge = true
			break
		}
	}

	if hasMerge {
		preInstallDir := config.PreInstallDir(targetDir)
		sb.WriteString("2. For files marked \"NEEDS MERGE\":\n")
		sb.WriteString(fmt.Sprintf("   - Read the current file and the pre-install snapshot at %s/<path>\n", preInstallDir))
		sb.WriteString("   - Identify what the user added post-install (permissions, MCP servers, env vars, custom sections)\n")
		sb.WriteString("   - Write a merged result: pre-install base + user additions, minus atomic hook/config entries\n")
		sb.WriteString("   - Show the diff to the user before writing\n")
		sb.WriteString(fmt.Sprintf("3. For files marked \"Restore\": copy from %s/<path>\n", preInstallDir))
		sb.WriteString("4. For files marked \"Delete\": rm the file\n")
		sb.WriteString(fmt.Sprintf("5. rm -rf %s\n", atomicDir))
		sb.WriteString("6. Print: \"Uninstall complete. Binary still at <path>. Run: rm <path>\"\n")
	} else {
		preInstallDir := config.PreInstallDir(targetDir)
		sb.WriteString(fmt.Sprintf("2. For each file in the Restore list: copy from %s/<path> to %s/<path>\n", preInstallDir, targetDir))
		sb.WriteString("3. For each file in the Delete list: rm the file\n")
		sb.WriteString(fmt.Sprintf("4. rm -rf %s\n", atomicDir))
		sb.WriteString("5. Print: \"Uninstall complete. Binary still at <path>. Run: rm <path>\"\n")
	}

	return sb.String()
}
