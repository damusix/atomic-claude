// Code-member discovery. A member can be indexed two ways: by realm federation
// (a code.toml plus per-member dbs under the realm root) or by self-indexing
// (`atomic code index` inside the member). A realm with no federation config
// resolves to zero members even when its members are self-indexed, so
// discoverCodeMembers unions both sources.
package serve

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// memberResolver is embedded by every code handler so member resolution lives
// in exactly one place.
type memberResolver struct {
	// realmRoot is the root of the repository or realm being served.
	realmRoot string
	// claudeMDPath lets realm.Resolve discover federation members.
	claudeMDPath string
	// wikiIndexPath locates self-indexed members; empty means
	// <realmRoot>/wiki/index.md.
	wikiIndexPath string
}

// members resolves per request — it only reads a config file and the wiki scan.
func (m memberResolver) members() []codeMember {
	res, err := realm.Resolve(m.realmRoot, m.claudeMDPath)
	if err != nil {
		return nil
	}
	wikiIndexPath := m.wikiIndexPath
	if wikiIndexPath == "" && res.RealmRoot != "" {
		wikiIndexPath = filepath.Join(res.RealmRoot, "wiki", "index.md")
	}
	return discoverCodeMembers(res, m.realmRoot, wikiIndexPath)
}

func (m memberResolver) localDBPath() string {
	return config.IndexDBPath(m.realmRoot)
}

// codeMember is one code-queryable repo within the served scope.
type codeMember struct {
	// Key is the group header in search results: the config key for a
	// federation member, the Prefix for a self-indexed one.
	Key string
	// Prefix is the realm-relative path this member's files are served under,
	// empty in single-repo scope.
	Prefix string
	// Path is the absolute repo root.
	Path string
	// DBPath may name a file that does not exist, for a federation member
	// declared but never built; callers report that as "not indexed".
	DBPath string
}

// discoverCodeMembers enumerates the members serve can query. An empty or
// unreadable wikiIndexPath simply skips self-index discovery.
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
		// Single local index at the served root. The member is returned whether
		// or not the db exists: absence surfaces downstream as "not indexed",
		// and the engine seam stays usable in tests that create no db file.
		db := config.IndexDBPath(realmRoot)
		return []codeMember{{Key: "", Prefix: "", Path: realmRoot, DBPath: db}}
	}
}

// realmCodeMembers unions declared federation members with self-indexed ones
// from the wiki scan. A declared member is always listed; a wiki member only
// when it actually carries an index.
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
					continue // unindexed and undeclared: noise, not a member
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

// memberDB prefers the federation db, then the member's self-index, then fedDB
// verbatim — which may be empty or nonexistent, and the caller reports that.
func memberDB(realmRoot, memberRelPath, fedDB string) string {
	if fedDB != "" && fileExists(fedDB) {
		return fedDB
	}
	self := config.IndexDBPath(filepath.Join(realmRoot, memberRelPath))
	if fileExists(self) {
		return self
	}
	return fedDB
}

// memberForPath returns the longest-prefix member and the remainder used to
// query its index. An empty-Prefix member matches anything but loses to any
// real prefix match.
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

// joinMemberPath turns a member-relative index path into the realm-relative
// path the /file/ and /page/ routes serve.
func joinMemberPath(prefix, rel string) string {
	rel = filepath.ToSlash(rel)
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

// fileExists reports whether path names an existing non-directory.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
