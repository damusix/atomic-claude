// Package migrate provides a versioned, replayable migration framework.
//
// Migrations are registered in a []Migration slice and applied by Run in
// ascending semver order. Semver comparison delegates to
// selfupdate.CompareSemver — no duplicate parsing logic.
package migrate

import (
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// Context carries the information available to a migration step.
// Later checkpoints extend this with config handles and other state;
// for C1 only the target root path is required.
type Context struct {
	// Root is the target root directory (e.g. ~/.claude/ for install-scope steps).
	Root string
}

// Migration is a single versioned migration step.
type Migration struct {
	// TargetVersion is the semver string this step migrates to (e.g. "1.2.0").
	// Steps are applied in ascending TargetVersion order.
	TargetVersion string

	// Scope is a routing tag (e.g. "install" or "repo"). Scope-based routing is
	// a later checkpoint; this field is stored for future use.
	Scope string

	// Up applies the migration. It is called exactly once when TargetVersion is
	// semver-greater than the recorded version. Implementations must be idempotent.
	Up func(*Context) error
}

// floor is the version below all valid releases. An empty or explicitly
// "0.0.0" recorded value is normalised here so every registered step runs on
// a pre-framework install.
const floor = "0.0.0"

// Run applies every migration in registry whose TargetVersion is strictly
// greater than recorded (semver comparison), in ascending semver order.
//
// It stops on the first error and returns the highest TargetVersion that was
// successfully applied, or recorded unchanged if no steps ran.
// An empty recorded is normalised to "0.0.0" so that all steps run.
// The caller's registry slice is never mutated.
func Run(recorded string, registry []Migration, ctx *Context) (string, error) {
	if recorded == "" {
		recorded = floor
	}

	// Stable copy so the caller's ordering is preserved.
	sorted := make([]Migration, len(registry))
	copy(sorted, registry)
	sort.SliceStable(sorted, func(i, j int) bool {
		return selfupdate.CompareSemver(sorted[i].TargetVersion, sorted[j].TargetVersion) < 0
	})

	current := recorded
	for _, m := range sorted {
		if selfupdate.CompareSemver(m.TargetVersion, recorded) <= 0 {
			continue
		}
		if err := m.Up(ctx); err != nil {
			return current, fmt.Errorf("migrate %s: %w", m.TargetVersion, err)
		}
		current = m.TargetVersion
	}
	return current, nil
}
