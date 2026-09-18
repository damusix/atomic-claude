package rules

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Matcher resolves exact repository-relative candidate paths against a set of
// instances that share one project's runtime base.
type Matcher struct {
	instances []RuleInstance
}

// NewMatcher orders instances by record identity then base, so matching is
// reproducible regardless of input order.
func NewMatcher(instances []RuleInstance) (*Matcher, error) {
	ordered := make([]RuleInstance, len(instances))
	copy(ordered, instances)
	for _, inst := range ordered {
		if inst.Record.ID == "" {
			return nil, fmt.Errorf("rules: matcher: instance has no record identity")
		}
		if !filepath.IsAbs(inst.Base) {
			return nil, fmt.Errorf("rules: matcher %s: base %q is not absolute", inst.Record.ID, inst.Base)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Record.ID != ordered[j].Record.ID {
			return ordered[i].Record.ID < ordered[j].Record.ID
		}
		return ordered[i].Base < ordered[j].Base
	})
	return &Matcher{instances: ordered}, nil
}

// Instances returns the matcher's ordered bindings.
func (m *Matcher) Instances() []RuleInstance {
	out := make([]RuleInstance, len(m.instances))
	copy(out, m.instances)
	return out
}

// Match returns every overlapping record for candidate, in stable Record.ID
// order. candidate is an exact repository-relative path: absolute and
// parent-escaping inputs are errors, and a candidate whose resolved location
// falls outside an instance base does not match that instance.
func (m *Matcher) Match(candidate string) ([]Match, error) {
	rel, err := normalizeCandidate(candidate)
	if err != nil {
		return nil, err
	}
	var matches []Match
	for _, inst := range m.instances {
		if !contained(inst.Base, rel) {
			continue
		}
		for _, glob := range inst.Record.Include {
			if doublestar.MatchUnvalidated(glob, rel) {
				matches = append(matches, Match{Record: inst.Record, Base: inst.Base, Glob: glob})
				break
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Record.ID != matches[j].Record.ID {
			return matches[i].Record.ID < matches[j].Record.ID
		}
		return matches[i].Base < matches[j].Base
	})
	return dedupe(matches), nil
}

// normalizeCandidate accepts only an exact repository-relative path.
func normalizeCandidate(candidate string) (string, error) {
	if candidate == "" {
		return "", fmt.Errorf("rules: match: empty candidate path")
	}
	c := strings.ReplaceAll(candidate, `\`, "/")
	if strings.HasPrefix(c, "/") || strings.HasPrefix(c, "~") {
		return "", fmt.Errorf("rules: match: candidate %q is absolute", candidate)
	}
	if len(c) >= 2 && c[1] == ':' {
		return "", fmt.Errorf("rules: match: candidate %q is absolute", candidate)
	}
	for _, seg := range strings.Split(c, "/") {
		if seg == ".." {
			return "", fmt.Errorf("rules: match: candidate %q escapes its base", candidate)
		}
	}
	clean := path.Clean(c)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("rules: match: candidate %q is not a file path", candidate)
	}
	return clean, nil
}

// contained reports whether candidate resolves inside base. base is already
// symlink-resolved, so a candidate that traverses an escaping symlink resolves
// outside and is rejected even though it is lexically under base.
func contained(base, candidate string) bool {
	joined := filepath.Join(base, filepath.FromSlash(candidate))
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// The leaf may not exist; resolving the parent still catches a symlink
		// component that leaves the base.
		parent, name := filepath.Split(joined)
		if r, parentErr := filepath.EvalSymlinks(filepath.Clean(parent)); parentErr == nil {
			resolved = filepath.Join(r, name)
		} else {
			resolved = filepath.Clean(joined)
		}
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dedupe removes repeated (record, base) pairs produced by overlapping
// instances while preserving the first stable occurrence.
func dedupe(matches []Match) []Match {
	seen := make(map[string]bool, len(matches))
	out := matches[:0]
	for _, m := range matches {
		key := m.Record.ID + "\x00" + m.Base
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}
