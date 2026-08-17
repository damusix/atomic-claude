package validate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

func TestDispatch_UnknownSubcommand(t *testing.T) {
	code := validate.Run([]string{"bogus"})
	if code != 2 {
		t.Errorf("unknown subcommand: got exit %d, want 2", code)
	}
}

func TestDispatch_HelpFlag(t *testing.T) {
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"--help"}, &buf)
	if code != 0 {
		t.Errorf("--help: got exit %d, want 0", code)
	}
	out := buf.String()
	for _, want := range []string{"spec", "config", "bundle"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q:\n%s", want, out)
		}
	}
}

func TestDispatch_SpecEmptyDir(t *testing.T) {
	code := validate.Run([]string{"spec", "/tmp/does-not-exist-atomic-validate-test.md"})
	if code != 2 {
		t.Errorf("validate spec on nonexistent file: got exit %d, want 2", code)
	}
}

func TestDispatch_FlagBeforeSubcommand(t *testing.T) {
	f := writeTempSpec(t, `# My Spec

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | test | foo.go | passes |

## Change log

<!-- empty -->
`)
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"--json", "spec", f}, &buf)
	out := buf.String()
	if code != 0 {
		t.Errorf("flag before subcommand: got exit %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json before subcommand: expected JSON output, got:\n%s", out)
	}
}

func TestDispatch_FlagAfterSubcommand(t *testing.T) {
	f := writeTempSpec(t, `# My Spec

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | test | foo.go | passes |

## Change log

<!-- empty -->
`)
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"spec", "--json", f}, &buf)
	out := buf.String()
	if code != 0 {
		t.Errorf("flag after subcommand: got exit %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json after subcommand: expected JSON output, got:\n%s", out)
	}
}

func TestDispatch_SuggestFlag(t *testing.T) {
	f := writeTempSpec(t, `# My Spec

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | test | foo.go | passes |

## Change log

<!-- empty -->
`)
	code := validate.Run([]string{"spec", "--suggest", f})
	if code != 0 {
		t.Errorf("--suggest flag on valid spec: got exit %d, want 0", code)
	}
}

// writeTempSpec writes content to a temp file and returns its path.
func writeTempSpec(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "spec-*.md")
	if err != nil {
		t.Fatalf("create temp spec: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp spec: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestDispatch_ConfigCleanRepo(t *testing.T) {
	code := validate.Run([]string{"config"})
	if code != 0 {
		t.Errorf("validate config stub: got exit %d, want 0", code)
	}
}
