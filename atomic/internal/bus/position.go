package bus

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// position is a joining client's resolved filesystem position: the
// repo/realm basenames Member stores on the roster and a member's name is
// stacked from. Resolved once, from the joining process's own cwd — the
// daemon has no cwd of its own to resolve against, so this is client-
// reported input at join time, unlike From/FromKind, which the daemon never
// trusts from the wire at publish time (see room.go's Publish doc).
type position struct {
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
// A single resolver (where.Resolve) is enough now that repo doubles as
// both Member.Repo and the name's own repo segment — the two-resolver split
// this used to carry (repoctx.ResolveFrom for a separate --as default,
// where.Resolve for Member.Repo/Realm) existed only because the old design
// needed a repo-root basename independent of the position-derived name.
// Now that the name *is* the position (stackedName below), there is only
// one repo-root basename to resolve, and where.Resolve's own repo-root
// fallback (no marker, no .git) is cwd itself — so repo is never left empty
// just because cwd sits outside version control.
func resolvePosition(home, cwd string) (position, error) {
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	report, err := where.Resolve(cwd, claudeMDPath)
	if err != nil {
		return position{}, fmt.Errorf("bus: resolve position: %w", err)
	}

	p := position{repo: filepath.Base(report.RepoRoot.Path)}
	if report.RealmScope.Position != where.RealmNone {
		p.realm = filepath.Base(report.RealmScope.RealmRoot)
	}
	return p, nil
}

// name computes a member's name from p's resolved position plus an
// optional role suffix — the single call joinAction makes to turn
// "--as <role>" (or no --as at all) into the actual name it sends to
// Hub.Join. See stackedName's own doc for the construction rule.
func (p position) name(as string) string {
	return stackedName(p.realm, p.repo, as)
}

// JoinIdentity resolves the position-derived member identity a non-CLI
// client (e.g. atomic serve's bus chat) joins with: the stacked
// "<realm>-<repo>-<as>" name plus the repo/realm basenames Hub.Join stores
// on the roster. Same resolution joinAction uses — one naming rule for
// every join path.
func JoinIdentity(home, cwd, as string) (name, repo, realm string, err error) {
	pos, err := resolvePosition(home, cwd)
	if err != nil {
		return "", "", "", err
	}
	return pos.name(as), pos.repo, pos.realm, nil
}

// stackedName builds "<realm>-<repo>-<as>", dropping any empty segment and
// collapsing a segment that repeats the one immediately before it. as is
// the role suffix from --as, always optional — realm and repo alone
// ("taxgentic-gui") are already a usable, deterministic name; as adds a
// role on top ("taxgentic-gui-fe-main") without ever being required to
// produce one. All three empty yields "".
func stackedName(realm, repo, as string) string {
	var parts []string
	appendSegment := func(s string) {
		if s == "" || (len(parts) > 0 && parts[len(parts)-1] == s) {
			return
		}
		parts = append(parts, s)
	}
	appendSegment(realm)
	appendSegment(repo)
	appendSegment(as)
	return strings.Join(parts, "-")
}
