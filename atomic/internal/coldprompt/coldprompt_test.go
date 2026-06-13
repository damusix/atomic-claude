package coldprompt_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/coldprompt"
)

// TestGet_KnownNames verifies that Get returns non-empty brief text for
// each documented cold-op name. The briefs are embedded at build time — a
// non-empty result proves the embed succeeded and the file has content.
func TestGet_KnownNames(t *testing.T) {
	cases := []string{"git-cleanup", "claude-merge"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := coldprompt.Get(name)
			if err != nil {
				t.Fatalf("Get(%q) returned unexpected error: %v", name, err)
			}
			if strings.TrimSpace(got) == "" {
				t.Errorf("Get(%q) returned empty string", name)
			}
		})
	}
}

// TestGet_UnknownName verifies that Get returns a non-nil error for an
// unregistered name and that the error message lists the valid names.
// This prevents silent bad citations from passing undetected.
func TestGet_UnknownName(t *testing.T) {
	_, err := coldprompt.Get("bogus-name")
	if err == nil {
		t.Fatal("Get(\"bogus-name\") returned nil error, want non-nil")
	}
	msg := err.Error()
	// Error must mention the unknown name.
	if !strings.Contains(msg, "bogus-name") {
		t.Errorf("error message does not mention the unknown name: %q", msg)
	}
	// Error must list at least one valid name so the caller can correct it.
	if !strings.Contains(msg, "git-cleanup") {
		t.Errorf("error message does not list valid name %q: %q", "git-cleanup", msg)
	}
}

// TestNames verifies Names() returns the exact set of registered names.
func TestNames(t *testing.T) {
	names := coldprompt.Names()
	if len(names) == 0 {
		t.Fatal("Names() returned empty slice")
	}
	want := map[string]bool{"git-cleanup": true, "claude-merge": true}
	got := make(map[string]bool, len(names))
	for _, n := range names {
		got[n] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("Names() missing %q", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("Names() contains unexpected name %q", k)
		}
	}
}
