package harness

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Two Claude roots hold rows with one root-relative id for different files; a
// status merge keyed by id alone would show them as one resource.
func TestResourcesKeepsSameIDInDifferentRootsApart(t *testing.T) {
	const id = "CLAUDE.md"
	ledger := &installstate.Ledger{Rows: []installstate.Row{
		{Target: "claude:/a", Resource: id, Applied: installstate.AppliedValue{Path: "/a/CLAUDE.md", Kind: managedfile.KindBlock}},
		{Target: "claude:/b", Resource: id, Applied: installstate.AppliedValue{Path: "/b/CLAUDE.md", Kind: managedfile.KindBlock}},
	}}

	got := Resources(ledger, nil, nil)
	if len(got) != 2 {
		t.Fatalf("resources = %+v, want one per path", got)
	}
	for _, r := range got {
		if len(r.Consumers) != 1 {
			t.Errorf("%s consumers = %v, want its own root's target only", r.Path, r.Consumers)
		}
	}
}
