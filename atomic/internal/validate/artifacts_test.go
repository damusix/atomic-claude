package validate_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

func scanText(path, src string) []validate.Finding {
	return validate.ScanArtifactText(path, src)
}

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

func TestA1_CorrectFlagPasses(t *testing.T) {
	src := "Run `atomic code search --json` to get structured output."
	findings := scanText("test.md", src)
	if len(findings) != 0 {
		t.Errorf("expected no findings for valid flag, got: %v", findings)
	}
}

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

func TestA1_ProseIgnored(t *testing.T) {
	src := "atomic operations are great --format is mentioned in prose but not a citation"
	findings := scanText("test.md", src)
	if len(findings) != 0 {
		t.Errorf("expected no findings for prose text, got: %v", findings)
	}
}

func TestA1_UnresolvedCitationSkipped(t *testing.T) {
	src := "Try `atomic code bogus --json` for the new command."
	findings := scanText("test.md", src)
	if len(findings) != 0 {
		t.Errorf("expected no findings for unresolved path, got: %v", findings)
	}
}

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

func TestA1_MultiWordPathResolved(t *testing.T) {
	good := "`atomic claude install --dry-run`"
	if findings := scanText("test.md", good); len(findings) != 0 {
		t.Errorf("valid flag --dry-run: expected no findings, got %v", findings)
	}
	bad := "`atomic claude install --nope`"
	findings := scanText("test.md", bad)
	if len(findings) == 0 {
		t.Fatal("invalid flag --nope on claude install: expected FAIL, got none")
	}
	if !strings.Contains(findings[0].Message, "--nope") {
		t.Errorf("expected finding to name --nope; got: %q", findings[0].Message)
	}
}

func TestA1_Dispatch_ArtifactsSubcommand(t *testing.T) {
	f := writeTempSpec(t, "# My Spec\n\n## Summary\n\nNo atomic citations here.\n\n## Change log\n\n<!-- empty -->\n")
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"artifacts", f}, &buf)
	if code != 0 {
		t.Errorf("artifacts subcommand on clean file: got exit %d, want 0; output:\n%s", code, buf.String())
	}
}

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
