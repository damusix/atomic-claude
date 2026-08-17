package coldprompt_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/coldprompt"
)

func TestGet_KnownNames(t *testing.T) {
	cases := []string{"git-cleanup", "claude-merge", "implementer", "reviewer"}
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

func TestGet_UnknownName(t *testing.T) {
	_, err := coldprompt.Get("bogus-name")
	if err == nil {
		t.Fatal("Get(\"bogus-name\") returned nil error, want non-nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bogus-name") {
		t.Errorf("error message does not mention the unknown name: %q", msg)
	}
	if !strings.Contains(msg, "git-cleanup") {
		t.Errorf("error message does not list valid name %q: %q", "git-cleanup", msg)
	}
}

func TestNames(t *testing.T) {
	names := coldprompt.Names()
	if len(names) == 0 {
		t.Fatal("Names() returned empty slice")
	}
	want := map[string]bool{"git-cleanup": true, "claude-merge": true, "implementer": true, "reviewer": true}
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
