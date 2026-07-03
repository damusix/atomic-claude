package where

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatHuman renders a Report as the plain-text default output. This is a
// descriptive report, not a health check — no PASS/WARN/FAIL severity.
func FormatHuman(r Report) string {
	var sb strings.Builder

	sb.WriteString("repo-scope wiki:  ")
	if r.RepoScope.Found {
		fmt.Fprintf(&sb, "found — %s\n", r.RepoScope.Path)
	} else {
		sb.WriteString("not found\n")
	}

	sb.WriteString("realm-scope:      ")
	switch r.RealmScope.Position {
	case RealmRoot:
		fmt.Fprintf(&sb, "root — %s\n", r.RealmScope.RealmRoot)
	case RealmMember:
		fmt.Fprintf(&sb, "member — realm root %s\n", r.RealmScope.RealmRoot)
	case RealmOrphaned:
		fmt.Fprintf(&sb, "orphaned — under realm root %s, not a registered member\n", r.RealmScope.RealmRoot)
	default:
		sb.WriteString("none\n")
	}

	sb.WriteString("code-index scope: ")
	sb.WriteString(r.CodeIndex.Scope.String())
	if r.CodeIndex.RealmRoot != "" {
		fmt.Fprintf(&sb, " — realm root %s", r.CodeIndex.RealmRoot)
	}
	sb.WriteString("\n")

	return sb.String()
}

// jsonReport is the JSON serialization shape for Report.
type jsonReport struct {
	RepoScope  jsonRepoScope  `json:"repo_scope"`
	RealmScope jsonRealmScope `json:"realm_scope"`
	CodeIndex  jsonCodeIndex  `json:"code_index"`
}

type jsonRepoScope struct {
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`
}

type jsonRealmScope struct {
	Position  string `json:"position"`
	RealmRoot string `json:"realm_root,omitempty"`
}

type jsonCodeIndex struct {
	Scope     string `json:"scope"`
	RealmRoot string `json:"realm_root,omitempty"`
	Members   int    `json:"members,omitempty"`
}

// FormatJSON renders a Report as indented JSON.
func FormatJSON(r Report) (string, error) {
	out := jsonReport{
		RepoScope: jsonRepoScope{
			Found: r.RepoScope.Found,
			Path:  r.RepoScope.Path,
		},
		RealmScope: jsonRealmScope{
			Position:  r.RealmScope.Position.String(),
			RealmRoot: r.RealmScope.RealmRoot,
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
