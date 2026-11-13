package omp

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// nativeKindRoles gates each package subtree OMP reads from the profile agent
// root on the capability row that proves it.
var nativeKindRoles = map[string]harness.Role{
	"commands": harness.RoleCommandDelivery,
	"skills":   harness.RoleSkillDiscovery,
}

// nativeFiles places the package files whose subtree m proves under root.
// Name is the encoded package path, so each file's commit unit is distinct.
func nativeFiles(pkg Package, m harness.CapabilityMatrix, root string) ([]harness.AgentFile, error) {
	var out []harness.AgentFile
	for _, f := range pkg.Files {
		dir, _, _ := strings.Cut(f.Path, "/")
		role, ok := nativeKindRoles[dir]
		if !ok || !m.Supports(role) {
			continue
		}
		name, err := installstate.UnitFor(f.Path)
		if err != nil {
			return nil, err
		}
		out = append(out, harness.AgentFile{Name: name, Path: filepath.Join(root, filepath.FromSlash(f.Path)), Data: f.Bytes})
	}
	return out, nil
}

// nativePlan applies the agent-file plan to every directory a subtree spans, so
// a nested skill reference gets the same conflict, leave, and retire handling.
type nativePlan struct {
	harness.AgentPlan
	// leftByDir exists because the ledger keys a left file by its parent.
	leftByDir map[string][]string
}

func underNativeKind(root, path string) bool {
	for sub := range nativeKindRoles {
		if strings.HasPrefix(path, filepath.Join(root, sub)+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// planNative runs harness.PlanAgents over every directory the claims or the
// target's owned rows occupy under root's native subtrees.
func planNative(claims []harness.Claim, ledger *installstate.Ledger, target, root string, decision installstate.Decision) (nativePlan, error) {
	dirs := map[string]bool{}
	for _, c := range claims {
		if underNativeKind(root, c.Path) {
			dirs[filepath.Dir(c.Path)] = true
		}
	}
	for _, row := range ledger.Rows {
		if row.Target == target && row.Applied.Kind == managedfile.KindFile && underNativeKind(root, row.Applied.Path) {
			dirs[filepath.Dir(row.Applied.Path)] = true
		}
	}
	for _, path := range ledger.LeftOf(target) {
		if underNativeKind(root, path) {
			dirs[filepath.Dir(path)] = true
		}
	}
	ordered := make([]string, 0, len(dirs))
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Strings(ordered)

	out := nativePlan{AgentPlan: harness.AgentPlan{Converged: true}, leftByDir: map[string][]string{}}
	for _, dir := range ordered {
		p, err := harness.PlanAgents(claims, ledger, target, dir, decision)
		if err != nil {
			return out, err
		}
		out.Left = append(out.Left, p.Left...)
		out.Retire = append(out.Retire, p.Retire...)
		out.Blockers = append(out.Blockers, p.Blockers...)
		out.Converged = out.Converged && p.Converged
		out.leftByDir[dir] = p.Left
	}
	return out, nil
}

func (p nativePlan) recordLeft(ledger *installstate.Ledger, target string) bool {
	changed := false
	for dir, left := range p.leftByDir {
		if ledger.SetLeft(target, dir, left) {
			changed = true
		}
	}
	return changed
}

// retireOwned removes the retired rows and the directories their removal
// emptied below root, so a dropped skill leaves no empty directory behind.
func retireOwned(ledger *installstate.Ledger, rows []installstate.Row, root string) ([]string, error) {
	removed, err := harness.RetireAgents(ledger, rows)
	for _, path := range removed {
		harness.PruneEmptyContainers(path, root)
	}
	return removed, err
}
