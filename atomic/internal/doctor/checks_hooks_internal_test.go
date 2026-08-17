package doctor

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// Exercises the production checkHooks resolver rather than the
// RunCheckHooksWith seam, which bypasses the scope-root derivation entirely
// and so cannot catch a doubled ".claude" segment.
func TestCheckHooks_ResolvesHomeScopeRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := hooks.Install(home, home); err != nil {
		t.Fatalf("Install: %v", err)
	}

	r := checkHooks(Opts{})
	if r.Severity != PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}
