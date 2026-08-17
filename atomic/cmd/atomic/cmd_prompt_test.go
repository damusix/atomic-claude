package main

import (
	"strings"
	"testing"
)

// TestPromptAction_KnownNames verifies that promptAction exits 0 and writes
// non-empty text for each registered brief name. Encodes the WHY: the embed
// + dispatch chain must be end-to-end verified; a broken embed path or a
// typo in the name table would silently produce empty output.
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

// TestPromptAction_UnknownName verifies that promptAction exits 1 and writes
// to stderr for an unregistered brief name. Encodes the WHY: a non-zero exit
// on bad input is the contract consumers (validate artifacts, CI) rely on to
// catch stale citations before they reach production.
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

// TestPromptAction_NoArgs verifies that promptAction exits 1 with a usage
// message when called with no arguments.
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

// --- atomic template dispatch ---
