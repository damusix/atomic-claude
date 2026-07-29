package bus

import (
	"fmt"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// position is a joining client's resolved filesystem position: a
// deterministic default member name plus the repo/realm basenames Member
// stores on the roster (docs/spec/atomic-bus.md's 2026-07-29
// "position-derived member naming" entry). Resolved once, from the joining
// process's own cwd — the daemon has no cwd of its own to resolve against,
// so this is client-reported input at join time, unlike From/FromKind,
// which the daemon never trusts from the wire at publish time (see room.go's
// Publish doc).
type position struct {
	// defaultName is repoctx.ResolveFrom's repo-root basename — the --as
	// default (docs/spec/atomic-bus.md: "--as is optional. Omitted, it
	// defaults to the repo-root basename from repoctx.ResolveFrom(cwd,
	// \"\")"). A different resolver than repo/realm below is deliberate,
	// not an inconsistency — see resolvePosition's doc.
	defaultName string
	// repo is where.Resolve's RepoRoot basename.
	repo string
	// realm is where.Resolve's RealmScope.RealmRoot basename, empty when
	// the session is not inside a registered realm (RealmNone) — empty is
	// valid and common, never fabricated.
	realm string
}

// resolvePosition computes a joining client's position from cwd. home
// supplies the CLAUDE.md path where.Resolve reads the <wikis> registry
// from, following the same convention every other bus path helper and
// main.go's runWhere already use (filepath.Join(home, ".claude",
// "CLAUDE.md")).
//
// defaultName and repo/realm deliberately go through two different
// resolvers per docs/spec/atomic-bus.md's own wording: repoctx.ResolveFrom
// for the --as default, where.Resolve for repo/realm. repoctx runs "git
// rev-parse --show-toplevel" and so understands submodules and GIT_DIR
// overrides a plain .git stat walk does not; where.Resolve's own package
// doc notes the two only diverge when no scope="repo" marker exists, and
// even then only in the rare case a plain .git stat walk disagrees with the
// git subprocess — not worth a second resolver just to unify the two call
// sites here.
func resolvePosition(home, cwd string) (position, error) {
	repoRoot, _, err := repoctx.ResolveFrom(cwd, "")
	if err != nil {
		return position{}, fmt.Errorf("bus: resolve repo root: %w", err)
	}

	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	report, err := where.Resolve(cwd, claudeMDPath)
	if err != nil {
		return position{}, fmt.Errorf("bus: resolve position: %w", err)
	}

	p := position{
		defaultName: filepath.Base(repoRoot),
		repo:        filepath.Base(report.RepoRoot.Path),
	}
	if report.RealmScope.Position != where.RealmNone {
		p.realm = filepath.Base(report.RealmScope.RealmRoot)
	}
	return p, nil
}
