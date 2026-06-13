package realm

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// Scope identifies which resolution branch was taken.
type Scope int

const (
	// ScopeRepo: a local index was found at <cwd>/.claude/.atomic-index/atomic.db.
	// Today's behavior — unchanged from 4.5.0.
	ScopeRepo Scope = iota

	// ScopeRealmAll: cwd equals the realm root.  Fan out across all non-excluded members.
	ScopeRealmAll

	// ScopeRealmMember: cwd is inside exactly one member's subtree.  Query that member alone.
	ScopeRealmMember

	// ScopeNoIndex: no local index, not under a realm (or under a realm root but not
	// under any member path).  Caller should surface "no index — run atomic code index".
	ScopeNoIndex
)

func (s Scope) String() string {
	switch s {
	case ScopeRepo:
		return "Repo"
	case ScopeRealmAll:
		return "RealmAll"
	case ScopeRealmMember:
		return "RealmMember"
	case ScopeNoIndex:
		return "NoIndex"
	default:
		return "Unknown"
	}
}

// localIndexDB is the conventional path of the local index db relative to a cwd.
const localIndexDB = ".claude/.atomic-index/atomic.db"

// Resolution is the output of Resolve.
type Resolution struct {
	// Scope is the detected scope.
	Scope Scope

	// RealmRoot is the realm root directory.  Empty when Scope == ScopeRepo or ScopeNoIndex.
	RealmRoot string

	// Members is the subset of config members relevant to this resolution:
	//   - ScopeRealmAll:    all non-excluded members (may be nil if Config is nil).
	//   - ScopeRealmMember: exactly the one matched member.
	//   - ScopeRepo / ScopeNoIndex: nil.
	Members []MemberEntry

	// Config is the parsed code.toml for the realm, or nil when the file is absent
	// or Scope is ScopeRepo/ScopeNoIndex.
	Config *Config
}

// DBPath returns the path to the keyed db for a given member key in this resolution.
// Only meaningful when Scope is ScopeRealmAll or ScopeRealmMember.
func (r Resolution) DBPath(key string) string {
	return filepath.Join(r.RealmRoot, ".atomic", key+".db")
}

// Resolve detects the scope for atomic code verbs based on cwd and the CLAUDE.md
// registry at claudeMDPath.
//
// Position-sensing logic (highest priority first):
//  1. Local index at <cwd>/.claude/.atomic-index/atomic.db → ScopeRepo.
//  2. Walk registered <wikis> realms; derive realm root = Dir(Dir(wiki/index.md)).
//     a. cwd == realm root → ScopeRealmAll.
//     b. cwd under a member path → ScopeRealmMember (that member).
//     c. cwd under realm root but no matching member → ScopeNoIndex (false-positive guard).
//  3. No match anywhere → ScopeNoIndex.
//
// Never calls os.Exit. All errors (wiki registry read, config load) are surfaced
// as Go errors so the CLI layer can format them.
func Resolve(cwd, claudeMDPath string) (Resolution, error) {
	cwd = filepath.Clean(cwd)

	// 1. Local index short-circuit.
	dbPath := filepath.Join(cwd, localIndexDB)
	if fileExists(dbPath) {
		return Resolution{Scope: ScopeRepo}, nil
	}

	// 2. Walk registered wiki realms.
	indexPaths, err := wiki.ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		// Hard I/O error (e.g. unreadable file): propagate so the caller can
		// surface a meaningful message rather than silently treating this as
		// "no realms registered".  ReadWikiIndexPaths returns (nil, nil) for
		// the normal absent-block / absent-file case, so a non-nil error here
		// always indicates a genuine read failure.
		return Resolution{Scope: ScopeNoIndex}, err
	}

	for _, indexPath := range indexPaths {
		// Realm root is the grandparent of wiki/index.md.
		// e.g. /realm/wiki/index.md → Dir = /realm/wiki → Dir = /realm
		realmRoot := filepath.Clean(filepath.Dir(filepath.Dir(indexPath)))

		if !isUnder(cwd, realmRoot) {
			// cwd is not inside this realm at all.
			continue
		}

		// Load the realm config (may be nil if code.toml absent).
		cfg, cfgErr := LoadConfig(realmRoot)
		if cfgErr != nil {
			// Config parse error: return it; the CLI can surface it.
			return Resolution{}, cfgErr
		}

		// a. cwd == realm root → fan out.
		if cwd == realmRoot {
			res := Resolution{
				Scope:     ScopeRealmAll,
				RealmRoot: realmRoot,
				Config:    cfg,
			}
			if cfg != nil {
				res.Members = nonExcluded(cfg.Members)
			}
			return res, nil
		}

		// b/c. cwd inside realm root — check member paths.
		if cfg != nil {
			for _, m := range cfg.Members {
				memberAbs := filepath.Join(realmRoot, m.Path)
				if isUnder(cwd, memberAbs) {
					return Resolution{
						Scope:     ScopeRealmMember,
						RealmRoot: realmRoot,
						Members:   []MemberEntry{m},
						Config:    cfg,
					}, nil
				}
			}
		}

		// cwd is under realm root but not under any member path — false-positive guard.
		return Resolution{Scope: ScopeNoIndex}, nil
	}

	// 3. No realm matched.
	return Resolution{Scope: ScopeNoIndex}, nil
}

// nonExcluded returns the members where Exclude == false.
func nonExcluded(members []MemberEntry) []MemberEntry {
	var out []MemberEntry
	for _, m := range members {
		if !m.Exclude {
			out = append(out, m)
		}
	}
	return out
}

// isUnder reports whether child is equal to or under parent, using normalized
// path-prefix comparison (no symlink resolution).
func isUnder(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// fileExists returns true when path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
