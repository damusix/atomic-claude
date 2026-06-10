package validate_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

// scanText is a test helper that calls ScanArtifactText, which is the pure seam
// exposed for testing (no disk access required).
func scanText(path, src string) []validate.Finding {
	return validate.ScanArtifactText(path, src)
}

// TestA1_WrongFlagFails proves that a known wrong flag (--format instead of
// --json) on a resolved command produces a FAIL finding naming the flag.
// WHY: this is the concrete bug that motivated the check — six agent files
// cited `atomic code search --format json`; the real flag is `--json`.
func TestA1_WrongFlagFails(t *testing.T) {
	src := "Run `atomic code search --format json` to search."
	findings := scanText("test.md", src)
	if len(findings) == 0 {
		t.Fatal("expected FAIL finding for --format, got none")
	}
	got := findings[0]
	if got.Severity != "FAIL" {
		t.Errorf("severity: got %q, want FAIL", got.Severity)
	}
	if got.Rule != "A1" {
		t.Errorf("rule: got %q, want A1", got.Rule)
	}
	if !strings.Contains(got.Message, "--format") {
		t.Errorf("message should name --format; got: %q", got.Message)
	}
}

// TestA1_CorrectFlagPasses proves that a known correct flag produces no findings.
// WHY: false positives would make the check unreliable as a CI gate.
func TestA1_CorrectFlagPasses(t *testing.T) {
	src := "Run `atomic code search --json` to get structured output."
	findings := scanText("test.md", src)
	if len(findings) != 0 {
		t.Errorf("expected no findings for valid flag, got: %v", findings)
	}
}

// TestA1_ArgEnumSubcommandPasses proves that positional tokens after the
// matched path are not treated as flags.
// WHY: `atomic validate spec --json` must pass — `spec` is positional, `--json` is real.
func TestA1_ArgEnumSubcommandPasses(t *testing.T) {
	cases := []string{
		"`atomic validate spec --json`",
		"`atomic followups add --kind plan`",
		"`atomic followups add --kind finding`",
	}
	for _, c := range cases {
		findings := scanText("test.md", c)
		if len(findings) != 0 {
			t.Errorf("case %q: expected no findings, got: %v", c, findings)
		}
	}
}

// TestA1_UniversalFlagPasses proves that universal flags (--help, -h, --version,
// -v, --repo, --no-update-check) never produce findings on any resolved command.
// WHY: every command accepts these; flagging them would be a false positive.
func TestA1_UniversalFlagPasses(t *testing.T) {
	cases := []string{
		"`atomic doctor --help`",
		"`atomic code search -h`",
		"`atomic validate --version`",
		"`atomic signals scan --repo /path`",
		"`atomic update --no-update-check`",
	}
	for _, c := range cases {
		findings := scanText("test.md", c)
		if len(findings) != 0 {
			t.Errorf("case %q: expected no findings for universal flag, got: %v", c, findings)
		}
	}
}

// TestA1_ProseIgnored proves that "atomic …" text outside code spans is not scanned.
// WHY: prose uses of "atomic" (e.g. "atomic operations", "atomic style") must not
// produce false positives — gating on code spans is the primary false-positive defense.
func TestA1_ProseIgnored(t *testing.T) {
	src := "atomic operations are great --format is mentioned in prose but not a citation"
	findings := scanText("test.md", src)
	if len(findings) != 0 {
		t.Errorf("expected no findings for prose text, got: %v", findings)
	}
}

// TestA1_UnresolvedCitationSkipped proves that an atomic citation with an unknown
// subcommand path produces no finding (accepted false-negative).
// WHY: we cannot attribute flags to an unknown command; emitting a finding would
// be a false positive if the path is merely undocumented.
func TestA1_UnresolvedCitationSkipped(t *testing.T) {
	src := "Try `atomic code bogus --json` for the new command."
	findings := scanText("test.md", src)
	if len(findings) != 0 {
		t.Errorf("expected no findings for unresolved path, got: %v", findings)
	}
}

// TestA1_FencedCodeBlockScanned proves that citations inside fenced code blocks
// are also checked, not just inline code spans.
// WHY: many artifact examples use fenced blocks for multi-line shell commands.
func TestA1_FencedCodeBlockScanned(t *testing.T) {
	src := "Example:\n\n```bash\natomic code search --format json\n```\n"
	findings := scanText("test.md", src)
	if len(findings) == 0 {
		t.Fatal("expected FAIL finding inside fenced block, got none")
	}
	if !strings.Contains(findings[0].Message, "--format") {
		t.Errorf("expected finding to name --format; got: %q", findings[0].Message)
	}
}

// TestA1_MultiWordPathResolved proves that multi-word paths like `claude install`
// resolve correctly and validate their flags.
// WHY: LookupByPath does an exact greedy match; single-token resolution would
// wrongly attribute `install` as a positional and emit no finding.
func TestA1_MultiWordPathResolved(t *testing.T) {
	// --dry-run is a valid flag for claude install
	good := "`atomic claude install --dry-run`"
	if findings := scanText("test.md", good); len(findings) != 0 {
		t.Errorf("valid flag --dry-run: expected no findings, got %v", findings)
	}
	// --nope is not a valid flag
	bad := "`atomic claude install --nope`"
	findings := scanText("test.md", bad)
	if len(findings) == 0 {
		t.Fatal("invalid flag --nope on claude install: expected FAIL, got none")
	}
	if !strings.Contains(findings[0].Message, "--nope") {
		t.Errorf("expected finding to name --nope; got: %q", findings[0].Message)
	}
}

// TestA1_Dispatch_ArtifactsSubcommand proves that `atomic validate artifacts`
// is a recognized subcommand (exit 0 when given a clean file, not exit 2).
// WHY: the subcommand must be wired in the dispatch switch or callers get "unknown subcommand".
func TestA1_Dispatch_ArtifactsSubcommand(t *testing.T) {
	f := writeTempSpec(t, "# My Spec\n\n## Summary\n\nNo atomic citations here.\n\n## Change log\n\n<!-- empty -->\n")
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"artifacts", f}, &buf)
	if code != 0 {
		t.Errorf("artifacts subcommand on clean file: got exit %d, want 0; output:\n%s", code, buf.String())
	}
}

// TestA1_Dispatch_JSONMode proves artifacts subcommand honors --json flag.
func TestA1_Dispatch_JSONMode(t *testing.T) {
	f := writeTempSpec(t, "# My Spec\n\nUse `atomic code search --format json` for wrong flag.\n\n## Change log\n\n<!-- empty -->\n")
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"artifacts", "--json", f}, &buf)
	out := buf.String()
	if code != 1 {
		t.Errorf("--json mode with FAIL: got exit %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json mode: expected JSON envelope, got:\n%s", out)
	}
}
