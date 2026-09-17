package harness

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Selector names one target instance explicitly. Kind is required; Instance is
// an exact instance ID or native root. Leaving Instance empty is a request for
// the only match, which is refused when more than one candidate exists.
type Selector struct {
	Kind     Kind
	Instance string
}

// Select returns the single instance sel names. Zero matches is ErrNoInstance;
// more than one match without an exact Instance is an AmbiguousSelectionError
// naming every candidate, so the operator chooses instead of the tool guessing.
func Select(instances []Instance, sel Selector) (Instance, error) {
	candidates := make([]Instance, 0, len(instances))
	for _, inst := range instances {
		if inst.Kind != sel.Kind {
			continue
		}
		if sel.Instance != "" && !instanceMatches(inst, sel.Instance) {
			continue
		}
		candidates = append(candidates, inst)
	}
	switch len(candidates) {
	case 0:
		if sel.Instance != "" {
			return Instance{}, fmt.Errorf("harness: no %s instance matches %q: %w", sel.Kind, sel.Instance, ErrNoInstance)
		}
		return Instance{}, fmt.Errorf("harness: no %s instance discovered: %w", sel.Kind, ErrNoInstance)
	case 1:
		return candidates[0], nil
	default:
		return Instance{}, &AmbiguousSelectionError{Kind: sel.Kind, Instances: candidates}
	}
}

// instanceMatches reports whether selector names inst. Both the instance ID and
// the native root are accepted, in either spelling, so a caller that typed the
// path it saw on disk is not punished for a relative "~".
func instanceMatches(inst Instance, selector string) bool {
	want := filepath.Clean(selector)
	return selector == inst.ID || selector == inst.NativeRoot ||
		want == filepath.Clean(inst.ID) || want == filepath.Clean(inst.NativeRoot)
}

// AmbiguousSelectionError reports a selector that matched more than one
// instance. Ambiguity is refused, never auto-resolved: an install that guesses
// between two native roots can write into the wrong one.
type AmbiguousSelectionError struct {
	Kind      Kind
	Instances []Instance
}

func (e *AmbiguousSelectionError) Error() string {
	roots := make([]string, 0, len(e.Instances))
	for _, inst := range e.Instances {
		roots = append(roots, inst.NativeRoot)
	}
	sort.Strings(roots)
	return fmt.Sprintf("harness: %s has %d instances (%s); pass --instance <native-root>, or run `atomic harness adopt %s --instance <native-root>`",
		e.Kind, len(e.Instances), strings.Join(roots, ", "), e.Kind)
}

// Is reports the shared selector-refusal sentinel for both no-match and
// ambiguous selections, so a caller can branch on one condition.
func (e *AmbiguousSelectionError) Is(target error) bool { return target == ErrAmbiguousSelection }
