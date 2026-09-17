package harness

import (
	"fmt"
	"sort"
)

// Registry resolves one concrete adapter per harness kind and is the only place
// the command layer needs to know a concrete adapter. It holds no enrollment
// state: enrollment lives in the ledger, and an adapter's presence here never
// enrolls anything.
type Registry struct {
	adapters map[Kind]Adapter
}

// NewRegistry builds a registry over the given adapters, refusing a nil adapter
// or two adapters for one kind.
func NewRegistry(adapters ...Adapter) (*Registry, error) {
	r := &Registry{adapters: map[Kind]Adapter{}}
	for _, a := range adapters {
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds one adapter.
func (r *Registry) Register(a Adapter) error {
	if a == nil {
		return fmt.Errorf("harness: nil adapter")
	}
	kind := a.Kind()
	if !kind.Valid() {
		return fmt.Errorf("harness: adapter reports unknown kind %q", kind)
	}
	if _, ok := r.adapters[kind]; ok {
		return fmt.Errorf("harness: adapter for %s already registered", kind)
	}
	r.adapters[kind] = a
	return nil
}

// Adapter returns the adapter for kind.
func (r *Registry) Adapter(kind Kind) (Adapter, error) {
	a, ok := r.adapters[kind]
	if !ok {
		return nil, fmt.Errorf("harness: no adapter registered for %s", kind)
	}
	return a, nil
}

// Kinds returns the registered kinds in stable order.
func (r *Registry) Kinds() []Kind {
	out := make([]Kind, 0, len(r.adapters))
	for kind := range r.adapters {
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Discover reports every instance of every registered kind, read-only, in
// stable kind-then-root order. Nothing here consults or writes enrollment
// state.
func (r *Registry) Discover(home string) ([]Instance, error) {
	var out []Instance
	for _, kind := range r.Kinds() {
		a := r.adapters[kind]
		instances, err := a.Discover(home)
		if err != nil {
			return nil, fmt.Errorf("harness: discover %s: %w", kind, err)
		}
		out = append(out, instances...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].NativeRoot < out[j].NativeRoot
	})
	return out, nil
}

// Select discovers instances and returns the one sel names, refusing an
// ambiguous selection instead of guessing.
func (r *Registry) Select(home string, sel Selector) (Instance, error) {
	instances, err := r.Discover(home)
	if err != nil {
		return Instance{}, err
	}
	return Select(instances, sel)
}
