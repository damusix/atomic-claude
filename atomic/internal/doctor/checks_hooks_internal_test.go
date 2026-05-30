package doctor

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// TestCheckHooks_ResolvesHomeScopeRoot is the regression guard for the doctor
// scope bug: checkHooks must pass $HOME (not ~/.claude) as the scope root, since
// hooks.IsInstalled appends ".claude/settings.json" to it. Passing ~/.claude
// doubled the segment (~/.claude/.claude/settings.json) and made a correctly
// installed hook report as missing.
//
// This exercises the production checkHooks resolver — the exported
// RunCheckHooksWith seam bypasses it, which is why the bug went uncaught.
func TestCheckHooks_ResolvesHomeScopeRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Install the hook at the user scope ($HOME), the same place checkHooks reads.
	if err := hooks.Install(home, home); err != nil {
		t.Fatalf("Install: %v", err)
	}

	r := checkHooks(Opts{})
	if r.Severity != PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}
