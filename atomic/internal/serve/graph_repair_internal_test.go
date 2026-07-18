package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRepairFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A relative link whose ../ run over-climbs its true target (authored against
// a different nesting depth) is repaired by probing the remainder against each
// ancestor of the page's directory — the edge resolves instead of breaking,
// and the rendered href agrees.
func TestOverClimbedRelativeLinkRepaired(t *testing.T) {
	root := t.TempDir()
	writeRepairFixture(t, root, "gui/docs/wiki/auth.md", "[machine](../../../src/core/auth.ts)\n[plan](../../../docs/plans/plan.md)")
	writeRepairFixture(t, root, "gui/src/core/auth.ts", "export {}")
	writeRepairFixture(t, root, "gui/docs/plans/plan.md", "# plan")

	g := BuildLinkGraph(root)
	out := g.Outbound("gui/docs/wiki/auth.md")
	if len(out) != 2 {
		t.Fatalf("outbound = %d, want 2 (%+v)", len(out), out)
	}
	byTarget := map[string]Edge{}
	for _, e := range out {
		byTarget[e.Target] = e
	}
	ts := byTarget["../../../src/core/auth.ts"]
	if ts.Broken || ts.ResolvedPath != "gui/src/core/auth.ts" || !ts.CodeFile {
		t.Errorf("code edge = %+v, want repaired codeFile gui/src/core/auth.ts", ts)
	}
	md := byTarget["../../../docs/plans/plan.md"]
	if md.Broken || md.ResolvedPath != "gui/docs/plans/plan.md" || md.CodeFile {
		t.Errorf("md edge = %+v, want repaired page gui/docs/plans/plan.md", md)
	}

	href, htmxPage, external := resolvePageHref(root, "gui/docs/wiki/auth.md", "../../../src/core/auth.ts")
	if href != "/file/gui/src/core/auth.ts" || htmxPage || external {
		t.Errorf("href = %q htmx=%v ext=%v, want /file/gui/src/core/auth.ts", href, htmxPage, external)
	}

	// A genuinely dead over-climb (no ancestor carries the remainder) stays broken.
	writeRepairFixture(t, root, "gui/docs/wiki/dead.md", "[x](../../../nope/missing.ts)")
	g2 := BuildLinkGraph(root)
	dead := g2.Outbound("gui/docs/wiki/dead.md")
	if len(dead) != 1 || !dead[0].Broken {
		t.Errorf("dead edge = %+v, want broken", dead)
	}
}
