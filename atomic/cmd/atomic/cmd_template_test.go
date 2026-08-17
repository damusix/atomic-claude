package main

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctemplate"
)

// TestTemplateAction_KnownNames verifies that templateAction exits 0 and
// writes non-empty text for each registered document-template name. Encodes
// the WHY: command artifacts instruct Claude to seed workflow documents from
// `atomic template <name>` — a broken embed path or missing template would
// silently hand back an empty skeleton and the improvised-structure problem
// the templates exist to prevent would return.
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

// TestTemplateAction_UnknownName verifies that templateAction exits 1 and
// writes to stderr for an unregistered template name — the fail-loud contract
// command artifacts rely on to stop rather than improvise structure.
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

// TestTemplateAction_NoArgs verifies that templateAction exits 1 with a usage
// message listing the valid names when called with no arguments.
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

// --- migrate helpers ---
