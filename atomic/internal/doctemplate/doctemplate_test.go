package doctemplate

import (
	"strings"
	"testing"
)

// Installed command artifacts cite these names verbatim, so a rename or
// removal here breaks them.
var wantNames = []string{
	"brief",
	"design-doc",
	"diagnose-context",
	"followups",
	"implementation-log",
	"session-report",
	"spec",
	"state",
}

func TestNamesMatchesContract(t *testing.T) {
	got := Names()
	if len(got) != len(wantNames) {
		t.Fatalf("Names() = %v, want %v", got, wantNames)
	}
	for i, name := range wantNames {
		if got[i] != name {
			t.Fatalf("Names()[%d] = %q, want %q (full: %v)", i, got[i], name, got)
		}
	}
}

func TestGetEveryRegisteredTemplate(t *testing.T) {
	for _, name := range wantNames {
		text, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("Get(%q) returned empty template", name)
		}
		// The opening guidance comment is what tells the filler what to do, so
		// it has to survive into the emitted text.
		if !strings.HasPrefix(text, "<!--") {
			t.Errorf("Get(%q) does not start with a guidance comment", name)
		}
		if !strings.Contains(text, "atomic template "+name) {
			t.Errorf("Get(%q) header does not name its emitting verb", name)
		}
	}
}

func TestGetUnknownNameListsValidNames(t *testing.T) {
	_, err := Get("nope")
	if err == nil {
		t.Fatal("Get(\"nope\") returned nil error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error does not echo the unknown name: %v", err)
	}
	if !strings.Contains(err.Error(), "design-doc") || !strings.Contains(err.Error(), "spec") {
		t.Errorf("error does not list valid names: %v", err)
	}
}
