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

// UninstallRestoreEntry is one file to restore from the pre-install snapshot.
type UninstallRestoreEntry struct {
	// RelPath is relative to the target dir (e.g. "settings.json").
	RelPath string
	// NeedsMerge marks a file the user modified post-install, where a plain
	// copy-back would lose their changes.
	NeedsMerge bool
}

// UninstallPlan holds the computed plan for `atomic claude uninstall`.
type UninstallPlan struct {
	Restore []UninstallRestoreEntry
	// Delete holds paths atomic created; none pre-existed.
	Delete []string
}

// BuildUninstallPlan computes the restore/delete plan from the pre-install
// manifest, using the embedded SHAs to tell user modifications from atomic-only
// writes and [install.artifacts] to scope Delete to atomic-installed files.
// Errors with "no pre-install snapshot" when the manifest is absent.
func BuildUninstallPlan(targetDir, home string) (UninstallPlan, error) {
	artifacts := embedded.Manifest()
	embeddedSHAs := make(map[string]string, len(artifacts))
	for _, a := range artifacts {
		embeddedSHAs[a.Target] = a.SHA256
	}

	// Best-effort: nil installedTargets means a pre-framework install, where the
	// snapshot alone scopes the plan.
	cfgPath := config.TOMLPath(home)
	var installedTargets map[string]bool
	if cfg, _, err := config.Load(cfgPath); err == nil {
		installedTargets = installedTargetSetFromConfig(cfg)
	}

	return BuildUninstallPlanWithManifest(targetDir, home, embeddedSHAs, installedTargets)
}

// BuildUninstallPlanWithManifest is BuildUninstallPlan with embeddedSHAs and
// installedTargets injected. A nil installedTargets disables Delete scoping.
//
// Three-way detection for files that existed before install:
//   - current == pre-install SHA → Restore (safe copy-back)
//   - current == embedded SHA    → Delete (atomic wrote it, user never touched it)
//   - neither                    → Restore+NeedsMerge (user modified)
func BuildUninstallPlanWithManifest(targetDir, home string, embeddedSHAs map[string]string, installedTargets map[string]bool) (UninstallPlan, error) {
	preInstallDir := config.PreInstallDir(home)
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
		// profile.md is user data with no pre-install counterpart; never delete or
		// restore it, even if a future manifest change includes it.
		if f.Path == config.ProfileRelPath() {
			continue
		}

		if !f.Existed {
			// User-added files absent from [install.artifacts] are never touched.
			if installedTargets == nil || installedTargets[f.Path] {
				plan.Delete = append(plan.Delete, f.Path)
			}
			continue
		}

		needsMerge := false
		atomicOnly := false

		// An empty SHA means a corrupt or hand-edited manifest — skip three-way
		// detection and fall through to a plain Restore, the safest default.
		if f.SHA256 != "" {
			onDiskPath := filepath.Join(targetDir, filepath.FromSlash(f.Path))
			onDiskData, readErr := os.ReadFile(onDiskPath)
			if readErr == nil {
				currentSHA := hexSHA256(onDiskData)
				switch {
				case currentSHA == f.SHA256:
					// Unchanged since install — plain restore.
				case embeddedSHAs[f.Path] != "" && currentSHA == embeddedSHAs[f.Path]:
					atomicOnly = true
				default:
					needsMerge = true
				}
			}
			// Missing on disk: no merge needed, restore straight.
		}

		if atomicOnly {
			// Same scope guard as the !f.Existed path.
			if installedTargets == nil || installedTargets[f.Path] {
				plan.Delete = append(plan.Delete, f.Path)
			}
		} else {
			plan.Restore = append(plan.Restore, UninstallRestoreEntry{
				RelPath:    f.Path,
				NeedsMerge: needsMerge,
			})
		}
	}

	return plan, nil
}

// GenerateUninstallPrompt builds the markdown prompt Claude executes to perform
// the uninstall.
func GenerateUninstallPrompt(targetDir, home string, plan UninstallPlan) string {
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

	atomicDir := config.Dir(home)
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
		preInstallDir := config.PreInstallDir(home)
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
		preInstallDir := config.PreInstallDir(home)
		sb.WriteString(fmt.Sprintf("2. For each file in the Restore list: copy from %s/<path> to %s/<path>\n", preInstallDir, targetDir))
		sb.WriteString("3. For each file in the Delete list: rm the file\n")
		sb.WriteString(fmt.Sprintf("4. rm -rf %s\n", atomicDir))
		sb.WriteString("5. Print: \"Uninstall complete. Binary still at <path>. Run: rm <path>\"\n")
	}

	return sb.String()
}
