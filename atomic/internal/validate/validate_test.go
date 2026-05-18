package validate_test

import (
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

// TestDispatch_SpecStub proves that `validate spec` exits 0 in the stub
// (no rules wired yet). WHY: CP-1 only scaffolds dispatch; rule logic ships in
// CP-5. A non-zero exit here would block every downstream checkpoint from
// having a clean baseline.
func TestDispatch_SpecStub(t *testing.T) {
	code := validate.Run([]string{"spec"})
	if code != 0 {
		t.Errorf("validate spec stub: got exit %d, want 0", code)
	}
}

// TestDispatch_FlagBeforeSubcommand proves --json before the subcommand is
// accepted (flag order independence). WHY: users composing scripts may place
// flags before or after the subcommand; either must work or CI pipelines break
// silently on "unrecognized flag" exits.
func TestDispatch_FlagBeforeSubcommand(t *testing.T) {
	code := validate.Run([]string{"--json", "spec"})
	if code != 0 {
		t.Errorf("flag before subcommand: got exit %d, want 0", code)
	}
}

// TestDispatch_FlagAfterSubcommand proves --json after the subcommand is also
// accepted. Same reasoning as TestDispatch_FlagBeforeSubcommand.
func TestDispatch_FlagAfterSubcommand(t *testing.T) {
	code := validate.Run([]string{"spec", "--json"})
	if code != 0 {
		t.Errorf("flag after subcommand: got exit %d, want 0", code)
	}
}

// TestDispatch_SuggestFlag proves --suggest is accepted without error.
func TestDispatch_SuggestFlag(t *testing.T) {
	code := validate.Run([]string{"spec", "--suggest"})
	if code != 0 {
		t.Errorf("--suggest flag: got exit %d, want 0", code)
	}
}

// TestDispatch_ConfigStub proves validate config exits 0 in stub state.
func TestDispatch_ConfigStub(t *testing.T) {
	code := validate.Run([]string{"config"})
	if code != 0 {
		t.Errorf("validate config stub: got exit %d, want 0", code)
	}
}

