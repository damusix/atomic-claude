package harness

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func TestSelectRefusesAmbiguousInstances(t *testing.T) {
	first := Instance{Kind: KindClaude, ID: "/home/u/.claude", NativeRoot: "/home/u/.claude"}
	second := Instance{Kind: KindClaude, ID: "/tmp/isolated", NativeRoot: "/tmp/isolated"}
	other := Instance{Kind: KindOMP, ID: "/home/u/.omp/agent", NativeRoot: "/home/u/.omp/agent"}
	all := []Instance{first, second, other}

	if _, err := Select(all, Selector{Kind: KindClaude}); !errors.Is(err, ErrAmbiguousSelection) {
		t.Fatalf("ambiguous selection error = %v, want ErrAmbiguousSelection", err)
	} else {
		var ambiguous *AmbiguousSelectionError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("error type = %T, want *AmbiguousSelectionError", err)
		}
		if len(ambiguous.Instances) != 2 {
			t.Errorf("ambiguous instances = %d, want 2", len(ambiguous.Instances))
		}
		msg := err.Error()
		for _, want := range []string{first.NativeRoot, second.NativeRoot, "atomic harness adopt claude --instance"} {
			if !strings.Contains(msg, want) {
				t.Errorf("guidance %q missing from %q", want, msg)
			}
		}
	}

	got, err := Select(all, Selector{Kind: KindClaude, Instance: second.NativeRoot})
	if err != nil {
		t.Fatalf("explicit selection: %v", err)
	}
	if got != second {
		t.Errorf("explicit selection = %+v, want %+v", got, second)
	}

	if _, err := Select(all, Selector{Kind: KindClaude, Instance: "/nowhere"}); !errors.Is(err, ErrNoInstance) {
		t.Errorf("unmatched selector error = %v, want ErrNoInstance", err)
	}
	if _, err := Select(all[:1], Selector{Kind: KindCodex}); !errors.Is(err, ErrNoInstance) {
		t.Errorf("missing kind error = %v, want ErrNoInstance", err)
	}
}

// fakeAdapter is a registry fixture: it discovers what a test tells it to and
// reports the Claude capability record.
type fakeAdapter struct {
	kind      Kind
	instances []Instance
	discover  func(home string) ([]Instance, error)
}

func (f *fakeAdapter) Kind() Kind { return f.kind }

func (f *fakeAdapter) Discover(home string) ([]Instance, error) {
	if f.discover != nil {
		return f.discover(home)
	}
	return f.instances, nil
}

func (f *fakeAdapter) Capabilities() CapabilityMatrix { return ClaudeCapabilities() }

func (f *fakeAdapter) Lifecycle(string) Lifecycle { return Lifecycle{} }

func TestRegistryRefusesDuplicateAndUnknownAdapters(t *testing.T) {
	if _, err := NewRegistry(&fakeAdapter{kind: KindClaude}); err != nil {
		t.Fatalf("single adapter: %v", err)
	}
	if _, err := NewRegistry(&fakeAdapter{kind: KindClaude}, &fakeAdapter{kind: KindClaude}); err == nil {
		t.Error("duplicate adapter accepted")
	}
	if _, err := NewRegistry(&fakeAdapter{kind: "vim"}); err == nil {
		t.Error("unknown adapter kind accepted")
	}
}

func TestRegistryDiscoverIsReadOnlyAcrossKinds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claude := &fakeAdapter{kind: KindClaude, instances: []Instance{{Kind: KindClaude, ID: "/a", NativeRoot: "/a"}}}
	omp := &fakeAdapter{kind: KindOMP, instances: []Instance{{Kind: KindOMP, ID: "/b", NativeRoot: "/b"}}}
	reg, err := NewRegistry(claude, omp)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	instances, err := reg.Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(instances) != 2 || instances[0].Kind != KindClaude || instances[1].Kind != KindOMP {
		t.Fatalf("discover order = %+v", instances)
	}
	if _, err := reg.Select(home, Selector{Kind: KindClaude}); err != nil {
		t.Fatalf("select: %v", err)
	}
	adapter, err := reg.Adapter(KindOMP)
	if err != nil || adapter != Adapter(omp) {
		t.Errorf("adapter lookup = %v, %v", adapter, err)
	}
	if _, err := reg.Adapter(KindCodex); err == nil {
		t.Error("lookup of an unregistered kind succeeded")
	}
	assertNoInstanceState(t, home)
}

// assertNoInstanceState fails when discovery left enrollment, ledger, journal, or
// adoption state behind. Discovery is read-only, so a scan that creates any of
// these has already enrolled something.
func assertNoInstanceState(t *testing.T, home string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(home, ".atomic"),
		filepath.Join(home, ".atomic", "install"),
		filepath.Join(home, ".atomic", "install", "ledger.json"),
		filepath.Join(home, ".atomic", "pre-install"),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("discovery created %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stat %s: %v", path, err)
		}
	}
}

func TestCapabilityMatrixReflectsCP0Rows(t *testing.T) {
	claude := ClaudeCapabilities()
	if claude.Harness != KindClaude || claude.Version != "2.1.273" {
		t.Errorf("claude matrix = %+v", claude)
	}
	if claude.Root.Env != "CLAUDE_CONFIG_DIR" || claude.Root.DefaultDir != ".claude" {
		t.Errorf("claude root = %+v", claude.Root)
	}
	assertCapability(t, claude, RoleStaticScope, StatusUnsupported, "rules/")
	assertCapability(t, claude, RoleSessionBaseline, StatusPartial, "SessionStart")
	assertCapability(t, claude, RolePreOperationTargets, StatusUnsupported, "")
	assertCapability(t, claude, RoleContextReturn, StatusUnsupported, "")
	assertCapability(t, claude, RoleDeterministicDeny, StatusUnsupported, "")

	omp := OMPCapabilities()
	if omp.Harness != KindOMP || omp.Version != "18.1.18" {
		t.Errorf("omp matrix = %+v", omp)
	}
	if omp.Root.DefaultDir != ".omp/agent" {
		t.Errorf("omp root = %+v", omp.Root)
	}
	assertCapability(t, omp, RoleStaticScope, StatusUnsupported, "rules/")
	assertCapability(t, omp, RoleSessionBaseline, StatusSupported, "before_agent_start")
	assertCapability(t, omp, RolePreOperationTargets, StatusSupported, "tool_call")
	assertCapability(t, omp, RoleContextReturn, StatusUnsupported, "")
	assertCapability(t, omp, RoleDeterministicDeny, StatusSupported, "tool_call block result")
	// OMP proved the deny result for one exact intercepted command. That is a
	// supported row carrying a coverage caveat, the same shape as the
	// pre-operation row, not a partial row: an adapter may rely on it.
	if !omp.Supports(RoleDeterministicDeny) {
		t.Error("omp deterministic-deny is proven but does not report support")
	}
	for _, role := range []Role{RolePreOperationTargets, RoleDeterministicDeny} {
		if row := omp.Capability(role); row.Limitation == "" {
			t.Errorf("omp %s is supported without stating what is not covered", role)
		}
	}

	codex := CodexCapabilities()
	if codex.Harness != KindCodex || codex.Version != "0.147.0" {
		t.Errorf("codex matrix = %+v", codex)
	}
	if codex.Root.Env != "CODEX_HOME" || codex.Root.DefaultDir != "" {
		t.Errorf("codex root = %+v", codex.Root)
	}
	for _, role := range []Role{RoleStaticScope, RoleSessionBaseline, RolePreOperationTargets, RoleContextReturn, RoleDeterministicDeny} {
		assertCapability(t, codex, role, StatusUnsupported, "")
	}

	// No harness has proven a native scoped-rule surface, so nothing may claim
	// static-scope parity.
	for _, m := range []CapabilityMatrix{claude, omp, codex} {
		if m.Supports(RoleStaticScope) {
			t.Errorf("%s claims static-scope support", m.Harness)
		}
	}

	// A role with no row is unsupported, never absent-and-therefore-fine.
	missing := claude.Capability(Role("runtime-proof"))
	if missing.Status != StatusUnsupported || missing.Role != Role("runtime-proof") {
		t.Errorf("missing row = %+v, want unsupported", missing)
	}

	roles := claude.Roles()
	if len(roles) != 5 {
		t.Fatalf("claude rows = %d, want 5", len(roles))
	}
	if !sort.SliceIsSorted(roles, func(i, j int) bool { return roles[i].Role < roles[j].Role }) {
		t.Errorf("roles not in stable order: %+v", roles)
	}
	for _, row := range roles {
		if row.Evidence == "" {
			t.Errorf("row %s has no evidence reference", row.Role)
		}
	}
}

func assertCapability(t *testing.T, m CapabilityMatrix, role Role, want CapabilityStatus, wantNative string) {
	t.Helper()
	row := m.Capability(role)
	if row.Status != want {
		t.Errorf("%s %s status = %s, want %s", m.Harness, role, row.Status, want)
	}
	if row.Native != wantNative {
		t.Errorf("%s %s native = %q, want %q", m.Harness, role, row.Native, wantNative)
	}
}

func TestLifecycleUnwiredHooksReportUnsupported(t *testing.T) {
	target := Target{Kind: KindClaude, Instance: "/home/u/.claude", NativeRoot: "/home/u/.claude"}
	empty := Lifecycle{}

	if _, err := empty.Project(target, PlanRequest{Generation: "gen-1"}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("project error = %v, want ErrUnsupported", err)
	}
	if _, err := empty.Converge(target, Plan{Target: target}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("converge error = %v, want ErrUnsupported", err)
	}
	if _, err := empty.Verify(target); !errors.Is(err, ErrUnsupported) {
		t.Errorf("verify error = %v, want ErrUnsupported", err)
	}
	if _, err := empty.Remove(target); !errors.Is(err, ErrUnsupported) {
		t.Errorf("remove error = %v, want ErrUnsupported", err)
	}

	wired := Lifecycle{
		ProjectFn: func(t Target, req PlanRequest) (Plan, error) {
			return Plan{Target: t}, nil
		},
	}
	if _, err := wired.Project(target, PlanRequest{}); err != nil {
		t.Errorf("wired project: %v", err)
	}
}

func TestRemoveTargetRetainsSharedInfrastructure(t *testing.T) {
	owner := Target{Kind: KindOMP, Instance: "/home/u/.omp/agent"}
	other := Target{Kind: KindOMP, Instance: "/home/u/.omp/work"}
	shared := Resource{
		ID:        "/home/u/.atomic/packages/omp/atomic",
		Kind:      managedfile.KindTree,
		Path:      "/home/u/.atomic/packages/omp/atomic",
		Owner:     owner.Key(),
		Consumers: []string{owner.Key(), other.Key()},
		VisibleTo: []string{"/home/u/.omp/other-profile"},
	}
	solo := Resource{
		ID:        "/home/u/.omp/agent/AGENTS.md",
		Kind:      managedfile.KindBlock,
		Path:      "/home/u/.omp/agent/AGENTS.md",
		Owner:     owner.Key(),
		Consumers: []string{owner.Key()},
		VisibleTo: []string{"/home/u/.omp/unenrolled-profile"},
	}

	if err := shared.Removable(owner.Key()); !errors.Is(err, ErrSharedResource) {
		t.Errorf("shared resource removable = %v, want ErrSharedResource", err)
	}
	if err := solo.Removable(owner.Key()); err != nil {
		t.Errorf("sole-consumer resource removable = %v, want nil", err)
	}

	plan, err := RemoveTarget(owner, []Resource{shared, solo})
	if err != nil {
		t.Fatalf("remove plan: %v", err)
	}
	if len(plan.Removed) != 1 || plan.Removed[0] != solo.ID {
		t.Errorf("removed = %v, want [%s]", plan.Removed, solo.ID)
	}
	if len(plan.Retained) != 1 || plan.Retained[0] != shared.ID {
		t.Errorf("retained = %v, want [%s] (visibility is not a dependency)", plan.Retained, shared.ID)
	}
}

func TestParseKindAndTargetKey(t *testing.T) {
	for _, name := range []string{"claude", " Claude ", "OMP", "codex"} {
		if _, err := ParseKind(name); err != nil {
			t.Errorf("ParseKind(%q): %v", name, err)
		}
	}
	if _, err := ParseKind("vim"); err == nil {
		t.Error("unknown kind accepted")
	}

	target := Target{Kind: KindClaude, Instance: "/home/u/.claude"}
	if target.Key() != "claude:/home/u/.claude" {
		t.Errorf("key = %q", target.Key())
	}
	parsed, err := ParseKey(target.Key())
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if parsed.Kind != target.Kind || parsed.Instance != target.Instance {
		t.Errorf("parsed = %+v, want identity of %+v", parsed, target)
	}
	if _, err := ParseKey("claude"); err == nil {
		t.Error("key without an instance accepted")
	}
	if _, err := ParseKey("vim:/x"); err == nil {
		t.Error("key with an unknown kind accepted")
	}
}
