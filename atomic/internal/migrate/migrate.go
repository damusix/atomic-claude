// Package migrate applies versioned, replayable migration steps in ascending
// semver order, comparing versions via selfupdate.CompareSemver.
package migrate

import (
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// Context carries the information available to a migration step. Root is the
// target directory, e.g. ~/.claude/ for install-scope steps.
type Context struct {
	Root string
}

// Migration is one versioned step. Scope ("install", "repo") is a routing tag
// reserved for future use. Up must be idempotent: it runs once per install,
// but a crashed run replays it.
type Migration struct {
	TargetVersion string
	Scope         string
	Up            func(*Context) error
}

// floor normalises an empty recorded version so every step runs on a
// pre-framework install.
const floor = "0.0.0"

// Run applies every migration newer than recorded, ascending, stopping at the
// first error and returning the highest version actually applied. The caller's
// registry slice is never mutated.
func Run(recorded string, registry []Migration, ctx *Context) (string, error) {
	if recorded == "" {
		recorded = floor
	}

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
