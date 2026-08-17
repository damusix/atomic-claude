package bus

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// position is a joining client's resolved filesystem position: the repo/realm
// basenames Member stores and a member's name is stacked from. Resolved from the
// joining process's own cwd — the daemon has none of its own — so unlike
// From/FromKind this is client-reported input at join time.
type position struct {
	// repo is where.Resolve's RepoRoot basename.
	repo string
	// realm is empty when the session is not inside a registered realm — valid
	// and common, never fabricated.
	realm string
}

// resolvePosition computes a joining client's position from cwd. home supplies
// the CLAUDE.md path where.Resolve reads the <wikis> registry from.
//
// One resolver suffices because the name *is* the position (stackedName), so
// there is a single repo-root basename to resolve. where.Resolve's repo-root
// fallback is cwd itself, so repo is never empty just because cwd sits outside
// version control.
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

// name turns "--as <role>" (or no --as) into the name joinAction sends to
// Hub.Join. See stackedName for the construction rule.
func (p position) name(as string) string {
	return stackedName(p.realm, p.repo, as)
}

// JoinIdentity resolves the member identity a non-CLI client (atomic serve's bus
// chat) joins with: the stacked name plus the repo/realm basenames Hub.Join
// stores. Same resolution joinAction uses — one naming rule for every join path.
func JoinIdentity(home, cwd, as string) (name, repo, realm string, err error) {
	pos, err := resolvePosition(home, cwd)
	if err != nil {
		return "", "", "", err
	}
	return pos.name(as), pos.repo, pos.realm, nil
}

// stackedName builds "<realm>-<repo>-<as>", dropping empty segments and
// collapsing one that repeats the segment before it. as is always optional:
// realm and repo alone are already a usable deterministic name.
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
