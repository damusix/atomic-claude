// code_members.go — realm-aware code-member discovery for serve.
//
// serve's code-intel surfaces (federated search + the code modal's intel pane)
// must find every queryable index in the served scope. There are two ways a
// member ends up indexed:
//
//   - Realm federation: a <realmRoot>/.atomic/code.toml config + per-member dbs at
//     <realmRoot>/.atomic/<key>.db (written by `atomic code index` from a realm
//     root that carries a <code-index> block).
//   - Self-index: a member indexed the natural way — `cd <member>; atomic code
//     index` — which writes <member>/.claude/.atomic-index/atomic.db.
//
// A wiki realm with no <code-index> block has no federation config, so
// realm.Resolve reports zero members even though members may be self-indexed.
// discoverCodeMembers unions both sources so "I just ran atomic code index in a
// member" works without any federation setup.
package serve

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// localIndexRel is the per-repo index path, relative to a repo root.
const localIndexRel = ".claude/.atomic-index/atomic.db"

// codeMember is one code-queryable repo within the served scope.
type codeMember struct {
	// Key is the group header shown in search results. For self-indexed members
	// it equals Prefix; for federation members it is the config key.
	Key string
	// Prefix is the realm-relative path under which this member's files are served
	// (/file/<Prefix>/...). Empty for single-repo (ScopeRepo) scope, where files
	// are served at the served root.
	Prefix string
	// Path is the absolute repo root.
	Path string
	// DBPath is the absolute path to the member's atomic.db. May point at a
	// non-existent file for a federation member that was declared but never built
	// (callers report "not indexed").
	DBPath string
}

// discoverCodeMembers enumerates the code members serve can query for a realm
// Resolution. realmRoot is the served root (or the realm root in realm scope);
// wikiIndexPath is the realm's wiki/index.md (used to enumerate members for
// self-index discovery; ignored when empty or unreadable).
func discoverCodeMembers(res realm.Resolution, realmRoot, wikiIndexPath string) []codeMember {
	switch res.Scope {
	case realm.ScopeRealmAll:
		root := res.RealmRoot
		if root == "" {
			root = realmRoot
		}
		return realmCodeMembers(res, root, wikiIndexPath)

	case realm.ScopeRealmMember:
		if len(res.Members) != 1 {
			return nil
		}
		m := res.Members[0]
		root := res.RealmRoot
		if root == "" {
			root = realmRoot
		}
		prefix := filepath.ToSlash(m.Path)
		return []codeMember{{
			Key:    m.Key,
			Prefix: prefix,
			Path:   filepath.Join(root, m.Path),
			DBPath: memberDB(root, m.Path, res.DBPath(m.Key)),
		}}

	default:
		// ScopeRepo / ScopeNoIndex: a single local index at the served root. The
		// member is always returned (db existence is the engine's call: an absent
		// db surfaces as a "not indexed" note, and the injected engine seam stays
		// usable in tests that never create a real db file).
		db := filepath.Join(realmRoot, localIndexRel)
		return []codeMember{{Key: "", Prefix: "", Path: realmRoot, DBPath: db}}
	}
}

// realmCodeMembers unions federation members (declared in code.toml) with
// self-indexed members discovered from the wiki scan. Federation members are
// always listed (a declared-but-unbuilt member surfaces a "not indexed" note);
// wiki members are added only when they actually carry a local index.
func realmCodeMembers(res realm.Resolution, realmRoot, wikiIndexPath string) []codeMember {
	var out []codeMember
	seen := make(map[string]bool)

	for _, m := range res.Members {
		prefix := filepath.ToSlash(m.Path)
		out = append(out, codeMember{
			Key:    m.Key,
			Prefix: prefix,
			Path:   filepath.Join(realmRoot, m.Path),
			DBPath: memberDB(realmRoot, m.Path, res.DBPath(m.Key)),
		})
		seen[prefix] = true
	}

	if wikiIndexPath != "" {
		members, err := wiki.ReadScanMembers(wikiIndexPath)
		if err == nil {
			for _, m := range members {
				prefix := filepath.ToSlash(m.Path)
				if seen[prefix] {
					continue
				}
				db := memberDB(realmRoot, m.Path, "")
				if db == "" {
					continue // unindexed non-federation member — omit (not noise)
				}
				out = append(out, codeMember{
					Key:    prefix,
					Prefix: prefix,
					Path:   filepath.Join(realmRoot, m.Path),
					DBPath: db,
				})
				seen[prefix] = true
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// memberDB picks the db path for a member: the federation db when it exists, else
// the member's own self-index when it exists, else fedDB verbatim (which may be
// "" or a non-existent path — the caller reports "not indexed").
func memberDB(realmRoot, memberRelPath, fedDB string) string {
	if fedDB != "" && fileExists(fedDB) {
		return fedDB
	}
	self := filepath.Join(realmRoot, memberRelPath, localIndexRel)
	if fileExists(self) {
		return self
	}
	return fedDB
}

// memberForPath returns the member whose Prefix is the longest prefix of the
// realm-relative path, together with the member-relative remainder used to query
// that member's index. A member with an empty Prefix (single-repo scope) matches
// any path with the path itself as the remainder, but loses to any real prefix
// match. ok is false when no member owns the path.
func memberForPath(members []codeMember, relPath string) (codeMember, string, bool) {
	relPath = filepath.ToSlash(strings.TrimPrefix(relPath, "/"))

	bestLen := -1
	var best codeMember
	for _, m := range members {
		if m.Prefix == "" {
			if bestLen < 0 {
				best = m
				bestLen = 0
			}
			continue
		}
		if relPath == m.Prefix || strings.HasPrefix(relPath, m.Prefix+"/") {
			if len(m.Prefix) > bestLen {
				bestLen = len(m.Prefix)
				best = m
			}
		}
	}
	if bestLen < 0 {
		return codeMember{}, "", false
	}
	rem := relPath
	if best.Prefix != "" {
		rem = strings.TrimPrefix(relPath, best.Prefix+"/")
	}
	return best, rem, true
}

// joinMemberPath prefixes a member-relative path (as stored in the member's
// index) with the member's realm-relative Prefix, producing the realm-relative
// path the /file/ and /page/ routes serve. An empty prefix (single-repo scope)
// returns the path unchanged.
func joinMemberPath(prefix, rel string) string {
	rel = filepath.ToSlash(rel)
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
