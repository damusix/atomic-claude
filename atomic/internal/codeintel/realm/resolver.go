package realm

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// Scope identifies which resolution branch was taken.
type Scope int

const (
	// ScopeRepo: a local index exists under cwd.
	ScopeRepo Scope = iota

	// ScopeRealmAll: cwd is the realm root; fan out across non-excluded members.
	ScopeRealmAll

	// ScopeRealmMember: cwd is inside exactly one member's subtree.
	ScopeRealmMember

	// ScopeNoIndex: no local index and no matching member.
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

// Resolution is the output of Resolve.
type Resolution struct {
	Scope Scope

	// RealmRoot is empty outside realm scopes.
	RealmRoot string

	// Members holds every non-excluded member under ScopeRealmAll, the single
	// matched member under ScopeRealmMember, and nil otherwise.
	Members []MemberEntry

	// Config is nil when code.toml is absent or the scope is not a realm.
	Config *Config
}

// DBPath is meaningful only in realm scopes.
func (r Resolution) DBPath(key string) string {
	return filepath.Join(r.RealmRoot, ".atomic", key+".db")
}

// Resolve picks a scope from cwd and the <wikis> registry at claudeMDPath. A
// local index wins outright; otherwise cwd's position within a registered realm
// decides. Sitting under a realm root but under no member is deliberately
// ScopeNoIndex, guarding against a false realm match.
func Resolve(cwd, claudeMDPath string) (Resolution, error) {
	cwd = filepath.Clean(cwd)

	dbPath := config.IndexDBPath(cwd)
	if fileExists(dbPath) {
		return Resolution{Scope: ScopeRepo}, nil
	}

	indexPaths, err := wiki.ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		// An absent block or file comes back (nil, nil), so any error here is a
		// real read failure and must not be mistaken for "no realms registered".
		return Resolution{Scope: ScopeNoIndex}, err
	}

	for _, indexPath := range indexPaths {
		// The realm root is the grandparent of wiki/index.md.
		realmRoot := filepath.Clean(filepath.Dir(filepath.Dir(indexPath)))

		if !isUnder(cwd, realmRoot) {
			continue
		}

		cfg, cfgErr := LoadConfig(realmRoot)
		if cfgErr != nil {
			return Resolution{}, cfgErr
		}

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

		// Under the realm root but under no member: not a realm query.
		return Resolution{Scope: ScopeNoIndex}, nil
	}

	return Resolution{Scope: ScopeNoIndex}, nil
}

func nonExcluded(members []MemberEntry) []MemberEntry {
	var out []MemberEntry
	for _, m := range members {
		if !m.Exclude {
			out = append(out, m)
		}
	}
	return out
}

// isUnder compares normalized path prefixes and does not resolve symlinks.
func isUnder(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
