package where

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// FormatHuman renders a Report as the plain-text default output. This is a
// descriptive report, not a health check — no PASS/WARN/FAIL severity.
func FormatHuman(r Report) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "repo root:        %s — %s\n", r.RepoRoot.Path, r.RepoRoot.Source)

	sb.WriteString("repo-scope wiki:  ")
	if r.RepoScope.Found {
		fmt.Fprintf(&sb, "found — %s\n", r.RepoScope.Path)
	} else {
		sb.WriteString("not found\n")
	}

	sb.WriteString("realm-scope:      ")
	switch r.RealmScope.Position {
	case RealmRoot:
		fmt.Fprintf(&sb, "root — %s (%s)\n", r.RealmScope.RealmRoot, r.RealmScope.Source)
	case RealmMember:
		fmt.Fprintf(&sb, "member — realm root %s (%s)\n", r.RealmScope.RealmRoot, r.RealmScope.Source)
	case RealmOrphaned:
		fmt.Fprintf(&sb, "orphaned — under realm root %s, not a registered member (%s)\n", r.RealmScope.RealmRoot, r.RealmScope.Source)
	default:
		sb.WriteString("none\n")
	}

	sb.WriteString("code-index scope: ")
	sb.WriteString(r.CodeIndex.Scope.String())
	if r.CodeIndex.RealmRoot != "" {
		fmt.Fprintf(&sb, " — realm root %s", r.CodeIndex.RealmRoot)
	}
	sb.WriteString("\n")

	// A realm resolved through the <wikis> registry (not a marker) is the
	// feature's only backfill affordance — name the way to declare it.
	if r.RealmScope.Position != RealmNone && r.RealmScope.Source == config.ScopeSourceRegistry {
		sb.WriteString("hint: declare this realm's identity in .claude/atomic.toml with `atomic wiki init --scope realm`\n")
	}

	return sb.String()
}

// jsonReport is the JSON serialization shape for Report.
type jsonReport struct {
	RepoRoot   jsonRepoRoot   `json:"repo_root"`
	RepoScope  jsonRepoScope  `json:"repo_scope"`
	RealmScope jsonRealmScope `json:"realm_scope"`
	CodeIndex  jsonCodeIndex  `json:"code_index"`
}

type jsonRepoRoot struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type jsonRepoScope struct {
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`
}

type jsonRealmScope struct {
	Position  string `json:"position"`
	RealmRoot string `json:"realm_root,omitempty"`
	Source    string `json:"source"`
}

type jsonCodeIndex struct {
	Scope     string `json:"scope"`
	RealmRoot string `json:"realm_root,omitempty"`
	Members   int    `json:"members,omitempty"`
}

// FormatJSON renders a Report as indented JSON. The human-only backfill hint
// (see FormatHuman) is not carried — JSON is machine-consumed and the source
// field already lets a caller derive it.
func FormatJSON(r Report) (string, error) {
	out := jsonReport{
		RepoRoot: jsonRepoRoot{
			Path:   r.RepoRoot.Path,
			Source: r.RepoRoot.Source.String(),
		},
		RepoScope: jsonRepoScope{
			Found: r.RepoScope.Found,
			Path:  r.RepoScope.Path,
		},
		RealmScope: jsonRealmScope{
			Position:  r.RealmScope.Position.String(),
			RealmRoot: r.RealmScope.RealmRoot,
			Source:    r.RealmScope.Source.String(),
		},
		CodeIndex: jsonCodeIndex{
			Scope:     r.CodeIndex.Scope.String(),
			RealmRoot: r.CodeIndex.RealmRoot,
			Members:   len(r.CodeIndex.Members),
		},
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("where json: %w", err)
	}
	return string(data), nil
}
