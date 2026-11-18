package rules

import (
	"fmt"
	"path/filepath"
)

// Bind creates a RuleInstance for one project. base must be absolute; it is
// symlink-resolved, so a checkout and a worktree share a base only on disk.
func Bind(record RuleRecord, projectKey, base string) (RuleInstance, error) {
	if projectKey == "" {
		return RuleInstance{}, fmt.Errorf("rules: bind %s: empty project key", record.ID)
	}
	if record.ID == "" {
		return RuleInstance{}, fmt.Errorf("rules: bind: record has no identity")
	}
	if !filepath.IsAbs(base) {
		return RuleInstance{}, fmt.Errorf("rules: bind %s: base %q is not absolute", record.ID, base)
	}
	resolved, err := resolveSymlinks(base)
	if err != nil {
		return RuleInstance{}, fmt.Errorf("rules: bind %s: resolve base %q: %w", record.ID, base, err)
	}
	return RuleInstance{
		Record:     record,
		ProjectKey: projectKey,
		Base:       resolved,
	}, nil
}

// BindAll binds every record to the same project key and base in record order.
func BindAll(records []RuleRecord, projectKey, base string) ([]RuleInstance, error) {
	instances := make([]RuleInstance, 0, len(records))
	for _, r := range records {
		inst, err := Bind(r, projectKey, base)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, nil
}
