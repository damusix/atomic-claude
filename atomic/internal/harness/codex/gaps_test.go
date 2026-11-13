package codex

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// Every artifact kind Codex does not deliver must read unsupported wherever a
// plan is shown, and a kind it does deliver must not.
func TestPlanNamesEveryUndeliveredKind(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	plan := project(t, newAdapter(t, root), home, root)
	joined := strings.Join(plan.Unproven, "\n")
	for _, want := range []string{"command delivery: ", "steering @ imports: ", "output-style selection: ", "path-scoped rule bodies: ", "repository wiki pointer cards: "} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan unproven lacks %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "repository wiki pointer cards: edits only") {
		t.Errorf("the card gap does not name the PreToolUse hook's edits-only coverage:\n%s", joined)
	}
	if strings.Contains("\n"+joined, "\nskill") && harness.CodexCapabilities().Supports(harness.RoleSkillDiscovery) {
		t.Errorf("a proven skill surface is reported unsupported:\n%s", joined)
	}
}

func TestCodexKindRowsMatchTheProbe(t *testing.T) {
	m := harness.CodexCapabilities()
	if !m.Supports(harness.RoleSkillDiscovery) {
		t.Error("codex-cli 0.147.0 listed a registered plugin's skills; the skill-discovery row must be supported")
	}
	for _, role := range []harness.Role{harness.RoleCommandDelivery, harness.RoleSteeringImports, harness.RoleOutputStyleSelection, harness.RoleStaticScope} {
		if row := m.Capability(role); row.Status != harness.StatusUnsupported || row.Limitation == "" {
			t.Errorf("%s = %+v, want unsupported with the observation", role, row)
		}
	}
}
