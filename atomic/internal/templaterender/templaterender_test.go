package templaterender_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

func writePartials(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), templaterender.PartialsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// Byte-identical passthrough is what lets the mirror run every artifact through
// Expand without first checking for a directive.
func TestExpand_NoDirectiveIsByteIdentical(t *testing.T) {
	pool, err := templaterender.LoadPartials(writePartials(t, map[string]string{
		"greet.md": `{{ define "greet" }}hello{{ end }}`,
	}))
	if err != nil {
		t.Fatalf("LoadPartials: %v", err)
	}

	src := []byte("# A command\n\nNothing templated here.\n")
	got, err := templaterender.Expand(pool, "plain.md", src)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if string(got) != string(src) {
		t.Errorf("Expand rewrote a directive-free source:\n got %q\nwant %q", got, src)
	}
}

// The expanded text is what installs and what the manifest SHA covers.
func TestExpand_SubstitutesPartialBody(t *testing.T) {
	pool, err := templaterender.LoadPartials(writePartials(t, map[string]string{
		"flow.md": `{{ define "flow" }}STEP ONE{{ end }}`,
	}))
	if err != nil {
		t.Fatalf("LoadPartials: %v", err)
	}

	got, err := templaterender.Expand(pool, "cmd.md", []byte(`before {{ template "flow" . }} after`))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if want := "before STEP ONE after"; string(got) != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}

// A missing partial must fail loudly; emitting the artifact with a hole would
// ship a broken command.
func TestExpand_UnknownPartialErrors(t *testing.T) {
	pool, err := templaterender.LoadPartials(writePartials(t, nil))
	if err != nil {
		t.Fatalf("LoadPartials: %v", err)
	}

	if _, err := templaterender.Expand(pool, "cmd.md", []byte(`{{ template "nope" . }}`)); err == nil {
		t.Error("Expand succeeded on an undefined partial; want an error")
	}
}

// Each file parses into its own clone, so a define in one artifact cannot leak
// into the next one rendered from the same pool.
func TestExpand_DefinitionsDoNotLeakBetweenFiles(t *testing.T) {
	pool, err := templaterender.LoadPartials(writePartials(t, nil))
	if err != nil {
		t.Fatalf("LoadPartials: %v", err)
	}

	if _, err := templaterender.Expand(pool, "first.md", []byte(`{{ define "local" }}x{{ end }}ok`)); err != nil {
		t.Fatalf("Expand first: %v", err)
	}
	if _, err := templaterender.Expand(pool, "second.md", []byte(`{{ template "local" . }}`)); err == nil {
		t.Error("a define in one file was visible to the next; clones must isolate them")
	}
}

// A missing partials dir is not an error: a context tree that composes nothing
// still has to render.
func TestLoadPartials_AbsentDirIsEmptyPool(t *testing.T) {
	pool, err := templaterender.LoadPartials(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadPartials on absent dir: %v", err)
	}
	got, err := templaterender.Expand(pool, "plain.md", []byte("untouched"))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if string(got) != "untouched" {
		t.Errorf("Expand = %q, want %q", got, "untouched")
	}
}
