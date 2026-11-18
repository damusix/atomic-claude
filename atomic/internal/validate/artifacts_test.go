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

// A citation of a verb the binary does not register errors at runtime, the
// same defect class as an unknown flag.
func TestA1_UnknownVerbPathFails(t *testing.T) {
	cases := map[string]string{
		"Try `atomic code bogus --json` for the new command.": "`atomic code bogus`",
		"Run `atomic wiki stamp <path>` after editing.":       "`atomic wiki stamp`",
	}
	for src, path := range cases {
		findings := scanText("test.md", src)
		if len(findings) != 1 {
			t.Errorf("%q: expected 1 finding, got %v", src, findings)
			continue
		}
		got := findings[0]
		if got.Severity != "FAIL" || got.Rule != "A1" {
			t.Errorf("%q: got %s %s, want FAIL A1", src, got.Severity, got.Rule)
		}
		if !strings.Contains(got.Message, "unknown command "+path) {
			t.Errorf("%q: message should name %s; got %q", src, path, got.Message)
		}
	}
}

func TestA1_UnknownVerbListsKnownSubcommands(t *testing.T) {
	findings := scanText("test.md", "`atomic hooks bogus`")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %v", findings)
	}
	msg := findings[0].Message
	if !strings.Contains(msg, "known subcommands of `atomic hooks`:") || !strings.Contains(msg, "install") {
		t.Errorf("message should list the subcommands of `atomic hooks`; got %q", msg)
	}
}

// Real citation shapes from the artifact corpus that name a namespace, a set
// of alternatives, or a placeholder instead of one full command; none may FAIL.
func TestA1_NonCommandCitationShapesPass(t *testing.T) {
	cases := []string{
		"`atomic code`",
		"`atomic wiki bucket`",
		"`atomic bus gateway`",
		"`atomic code index|sync`",
		"`atomic code index|sync --nope`",
		"`atomic claude install/update`",
		"`atomic bus gateway [enroll|revoke]`",
		"`atomic followups <verb>`",
		"`atomic template <name>`",
		"`atomic wiki bucket NAME`",
		"`atomic code …`",
		"`atomic code \"query\"`",
	}
	for _, c := range cases {
		if findings := scanText("test.md", c); len(findings) != 0 {
			t.Errorf("case %q: expected no findings, got %v", c, findings)
		}
	}
}

// Sentence punctuation is not part of a verb, and the word carrying it ends
// the path, so what follows is never judged as a subcommand.
func TestA1_TrailingPunctuationEndsPath(t *testing.T) {
	cases := []string{
		"`atomic wiki bucket,`",
		"```bash\natomic wiki bucket; echo done\n```\n",
	}
	for _, c := range cases {
		if findings := scanText("test.md", c); len(findings) != 0 {
			t.Errorf("case %q: expected no findings, got %v", c, findings)
		}
	}
}

// Tokens past a shell boundary belong to another command, a redirect target,
// or a comment, so neither their flags nor their words are judged.
func TestA1_CitationEndsAtShellBoundary(t *testing.T) {
	cases := []string{
		"`atomic code affected --json $(git diff --name-only main...HEAD)`",
		"`atomic code search foo --json | jq -r .x`",
		"`atomic code search foo --json 2>&1 --nope`",
		"`atomic code search foo --json && grep --nope x`",
		"`atomic bus gateway 2>/dev/null`",
		"`atomic code >out.txt`",
		"`atomic code search foo <<EOF --nope`",
		"```bash\natomic bus gateway \\\n  --host x\n```\n",
		"```bash\natomic code   # comment\n```\n",
	}
	for _, c := range cases {
		if findings := scanText("test.md", c); len(findings) != 0 {
			t.Errorf("case %q: expected no findings, got %v", c, findings)
		}
	}
}

func TestA1_FlagAfterAnglePlaceholderChecked(t *testing.T) {
	for _, src := range []string{
		"`atomic code search <query> --nope`",
		"`atomic followups add <short title> --nope`",
	} {
		findings := scanText("test.md", src)
		if len(findings) != 1 || !strings.Contains(findings[0].Message, "--nope") {
			t.Errorf("%q: expected one --nope finding, got %v", src, findings)
		}
	}
}

// Mermaid fence lines are diagram labels; the same line in any other fence is
// read as a citation.
func TestA1_MermaidFenceSkipped(t *testing.T) {
	line := "    A[any atomic bus verb] --> B\n"
	if findings := scanText("test.md", "```mermaid\n"+line+"```\n"); len(findings) != 0 {
		t.Errorf("mermaid fence: expected no findings, got %v", findings)
	}
	if findings := scanText("test.md", "```text\n"+line+"```\n"); len(findings) != 1 {
		t.Errorf("text fence: expected 1 finding, got %v", findings)
	}
}

func TestA1_DoubleDashEndsFlagChecking(t *testing.T) {
	for _, src := range []string{
		"```bash\natomic code search foo -- --not-a-flag\n```\n",
		"`atomic docs index -- -drafts`",
	} {
		if findings := scanText("test.md", src); len(findings) != 0 {
			t.Errorf("%q: expected no findings past --, got %v", src, findings)
		}
	}
}

func TestA1_FlagBeforeBoundaryChecked(t *testing.T) {
	findings := scanText("test.md", "`atomic code search --format json | jq .`")
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "--format") {
		t.Errorf("expected one --format finding, got %v", findings)
	}
}

func TestA1_IndentedCodeBlocksScanned(t *testing.T) {
	cases := map[string]int{
		"1. Run:\n\n    ```bash\n    atomic code search --format json\n    ```\n": 4,
		"- Run:\n\n  ```bash\n  atomic code search --format json\n  ```\n":        4,
		"Example:\n\n    atomic code search --format json\n":                      3,
	}
	for src, line := range cases {
		findings := scanText("test.md", src)
		if len(findings) != 1 {
			t.Errorf("%q: expected 1 finding, got %v", src, findings)
			continue
		}
		if !strings.Contains(findings[0].Message, "--format") {
			t.Errorf("%q: expected --format finding, got %q", src, findings[0].Message)
		}
		if findings[0].Line != line {
			t.Errorf("%q: line got %d, want %d", src, findings[0].Line, line)
		}
	}
	good := "1. Run:\n\n    ```bash\n    atomic code search --json\n    ```\n"
	if findings := scanText("test.md", good); len(findings) != 0 {
		t.Errorf("valid flag in list fence: expected no findings, got %v", findings)
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
