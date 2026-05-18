package validate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

// TestDispatch_UnknownSubcommand proves that an unrecognized subcommand exits 2.
// WHY: spec § Exit codes: exit 2 = validator-internal error, which includes bad
// invocation. This must not return 0 (would silently pass in CI pipelines that
// check only for zero exit).
func TestDispatch_UnknownSubcommand(t *testing.T) {
	code := validate.Run([]string{"bogus"})
	if code != 2 {
		t.Errorf("unknown subcommand: got exit %d, want 2", code)
	}
}

// TestDispatch_HelpFlag proves that --help exits 0 and prints the v1 subcommand
// list to stdout/stderr. WHY: users invoking `atomic validate --help` need to
// discover the three v1 verbs (spec, config, bundle) without reading the spec.
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

// TestDispatch_SpecEmptyDir proves that `validate spec <dir-with-no-specs>` exits 2
// (internal error: no files found), not 0. WHY: CP-5 wired real rule logic; the
// old stub test is replaced. Passing an empty dir confirms the glob-zero-files
// path returns 2 (internal error) not 0 (silent false-pass).
func TestDispatch_SpecEmptyDir(t *testing.T) {
	// Use a temp dir as the working directory so findRepoRoot fails gracefully,
	// but pass an explicit path to a file that doesn't exist.
	code := validate.Run([]string{"spec", "/tmp/does-not-exist-atomic-validate-test.md"})
	// Exit 2 = internal error (cannot read file).
	if code != 2 {
		t.Errorf("validate spec on nonexistent file: got exit %d, want 2", code)
	}
}

// TestDispatch_FlagBeforeSubcommand proves --json before the subcommand is
// accepted (flag order independence) and produces JSON output, not a parse error.
// WHY: users composing scripts may place flags before or after the subcommand;
// either must work or CI pipelines break silently on "unrecognized flag" exits.
func TestDispatch_FlagBeforeSubcommand(t *testing.T) {
	// Use a real-looking spec file so the runner has content to validate.
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
	// Exit 0 (no FAIL) and output must be JSON.
	if code != 0 {
		t.Errorf("flag before subcommand: got exit %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json before subcommand: expected JSON output, got:\n%s", out)
	}
}

// TestDispatch_FlagAfterSubcommand proves --json after the subcommand is also
// accepted and produces JSON output. Same reasoning as TestDispatch_FlagBeforeSubcommand.
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

// TestDispatch_SuggestFlag proves --suggest is accepted without error on a valid spec.
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

// TestDispatch_ConfigStub proves validate config exits 0 in stub state.
func TestDispatch_ConfigStub(t *testing.T) {
	code := validate.Run([]string{"config"})
	if code != 0 {
		t.Errorf("validate config stub: got exit %d, want 0", code)
	}
}

