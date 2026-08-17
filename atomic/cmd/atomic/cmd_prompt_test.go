package main

import (
	"strings"
	"testing"
)

// A broken embed path or a typo in the name table produces empty output
// silently, so the embed-plus-dispatch chain is checked end to end.
func TestPromptAction_KnownNames(t *testing.T) {
	names := []string{"git-cleanup", "claude-merge", "implementer", "reviewer"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			var errOut strings.Builder
			code := promptAction([]string{name}, &out, &errOut)
			if code != 0 {
				t.Fatalf("promptAction(%q) returned exit code %d, want 0; stderr: %s", name, code, errOut.String())
			}
			if strings.TrimSpace(out.String()) == "" {
				t.Errorf("promptAction(%q) wrote empty stdout", name)
			}
		})
	}
}

// `validate artifacts` and CI rely on the non-zero exit to catch stale
// citations before they ship.
func TestPromptAction_UnknownName(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := promptAction([]string{"no-such-brief"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("promptAction(\"no-such-brief\") returned exit code 0, want non-zero")
	}
	if strings.TrimSpace(errOut.String()) == "" {
		t.Errorf("promptAction(\"no-such-brief\") wrote nothing to stderr")
	}
	if out.String() != "" {
		t.Errorf("promptAction(\"no-such-brief\") wrote unexpected stdout: %q", out.String())
	}
}

func TestPromptAction_NoArgs(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := promptAction([]string{}, &out, &errOut)
	if code == 0 {
		t.Fatalf("promptAction with no args returned exit code 0, want non-zero")
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("no-args error message missing 'Usage:'; stderr: %q", errOut.String())
	}
}
