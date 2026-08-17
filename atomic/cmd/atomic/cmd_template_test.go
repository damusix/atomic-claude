package main

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctemplate"
)

// Command artifacts seed workflow documents from `atomic template <name>`; a
// broken embed path would hand back an empty skeleton and silently restore the
// improvised-structure problem the templates exist to prevent.
func TestTemplateAction_KnownNames(t *testing.T) {
	for _, name := range doctemplate.Names() {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			var errOut strings.Builder
			code := templateAction([]string{name}, &out, &errOut)
			if code != 0 {
				t.Fatalf("templateAction(%q) returned exit code %d, want 0; stderr: %s", name, code, errOut.String())
			}
			if strings.TrimSpace(out.String()) == "" {
				t.Errorf("templateAction(%q) wrote empty stdout", name)
			}
		})
	}
}

// The fail-loud contract artifacts rely on to stop rather than improvise.
func TestTemplateAction_UnknownName(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := templateAction([]string{"no-such-template"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("templateAction(\"no-such-template\") returned exit code 0, want non-zero")
	}
	if strings.TrimSpace(errOut.String()) == "" {
		t.Errorf("templateAction(\"no-such-template\") wrote nothing to stderr")
	}
	if out.String() != "" {
		t.Errorf("templateAction(\"no-such-template\") wrote unexpected stdout: %q", out.String())
	}
}

func TestTemplateAction_NoArgs(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := templateAction([]string{}, &out, &errOut)
	if code == 0 {
		t.Fatalf("templateAction with no args returned exit code 0, want non-zero")
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("no-args error message missing 'Usage:'; stderr: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "design-doc") {
		t.Errorf("no-args error message missing valid names; stderr: %q", errOut.String())
	}
}
