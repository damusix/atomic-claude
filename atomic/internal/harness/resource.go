package harness

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Resource is one physical native resource Atomic owns bytes in. It carries a
// single ownership record plus the two sets that must never be conflated:
// Consumers are enrolled targets that depend on it, while VisibleTo names
// unenrolled instances that can merely discover it. Visibility is not
// enrollment and never becomes ownership.
type Resource struct {
	ID   string           `json:"id"`
	Kind managedfile.Kind `json:"kind"`
	Path string           `json:"path,omitempty"`
	// Owner is the target key that applied the current bytes, empty when the
	// resource is unowned.
	Owner string `json:"owner,omitempty"`
	// Consumers are enrolled target keys that depend on this resource.
	Consumers []string `json:"consumers,omitempty"`
	// VisibleTo names unenrolled instances that can discover the resource.
	VisibleTo []string `json:"visible_to,omitempty"`
}

// Removable reports whether consumer may delete the resource. A resource
// another enrolled consumer still depends on is shared infrastructure and is
// never removable, regardless of who can see it.
func (r Resource) Removable(consumer string) error {
	var others []string
	for _, c := range r.Consumers {
		if c != consumer {
			others = append(others, c)
		}
	}
	if len(others) > 0 {
		return fmt.Errorf("harness: resource %s: %w (consumers: %s)", r.ID, ErrSharedResource, strings.Join(others, ", "))
	}
	return nil
}

// RemoveTarget plans one target's removal: resources it solely consumes are
// removed, resources another enrolled consumer still depends on are retained
// and reported. Visibility by unenrolled instances never blocks removal,
// because visibility is not a dependency.
func RemoveTarget(t Target, resources []Resource) (Removal, error) {
	plan := Removal{Target: t}
	for _, r := range resources {
		if err := r.Removable(t.Key()); err != nil {
			if errors.Is(err, ErrSharedResource) {
				plan.Retained = append(plan.Retained, r.ID)
				continue
			}
			return Removal{}, err
		}
		plan.Removed = append(plan.Removed, r.ID)
	}
	sort.Strings(plan.Removed)
	sort.Strings(plan.Retained)
	return plan, nil
}
