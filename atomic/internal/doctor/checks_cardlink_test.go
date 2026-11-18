package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// cardLinkRoot is a repository whose .claude/rules/atomic points at target,
// with .atomic/rules/wiki present so the expected target exists.
func cardLinkRoot(t *testing.T, target string) (root, link string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".atomic", "rules", "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(root, ".claude", "rules", "atomic")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return root, link
}

// A subdirectory session loads no card through the link until the user
// approves it, so doctor names that limit wherever the link exists.
func TestCheckCardLinkNamesTheSubdirectoryLimit(t *testing.T) {
	root, _ := cardLinkRoot(t, "../../.atomic/rules")
	got := doctor.RunCheckCardLinkWith(root)
	if got.Severity != doctor.PASS || !strings.Contains(got.Detail, "subdirectory") {
		t.Errorf("result = %+v, want a pass naming the subdirectory-session limit", got)
	}
}

// A link that points elsewhere, or at nothing, serves no card.
func TestCheckCardLinkWarnsOnAForeignOrDanglingLink(t *testing.T) {
	for name, target := range map[string]string{
		"foreign":  "../../elsewhere",
		"dangling": "../../.atomic/rules/missing",
	} {
		t.Run(name, func(t *testing.T) {
			root, link := cardLinkRoot(t, target)
			if name == "foreign" {
				if err := os.MkdirAll(filepath.Join(root, "elsewhere"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := doctor.RunCheckCardLinkWith(root)
			if got.Severity != doctor.WARN || !strings.Contains(got.Detail, link) {
				t.Errorf("result = %+v, want a warning naming %s", got, link)
			}
		})
	}
}

// `atomic repo init` compares the link's text, so an absolute link that still
// reaches the cards fails its next run as a conflict.
func TestCheckCardLinkWarnsOnANonRelativeTarget(t *testing.T) {
	root, link := cardLinkRoot(t, "placeholder")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".atomic", "rules"), link); err != nil {
		t.Fatal(err)
	}
	got := doctor.RunCheckCardLinkWith(root)
	if got.Severity != doctor.WARN || !strings.Contains(got.Detail, "../../.atomic/rules") {
		t.Errorf("result = %+v, want a warning naming the relative target ../../.atomic/rules", got)
	}
}

func TestCheckCardLinkIsQuietWithoutTheLink(t *testing.T) {
	got := doctor.RunCheckCardLinkWith(t.TempDir())
	if got.Severity != doctor.PASS || strings.Contains(got.Detail, "subdirectory") {
		t.Errorf("result = %+v, want a plain pass", got)
	}
}

// Outside a repository there is no root to look under, so nothing relative
// to the working directory is probed.
func TestCheckCardLinkWithoutARepository(t *testing.T) {
	got := doctor.RunCheckCardLinkWith("")
	if got.Severity != doctor.PASS || got.Detail != "no repository" {
		t.Errorf("result = %+v, want a no-repository pass", got)
	}
}

// The repair plan for a card-link warning tells the user what to do next,
// with no dangling separator when the warning carries no remediation.
func TestRepairPlanForACardLinkWarning(t *testing.T) {
	root, _ := cardLinkRoot(t, "../../elsewhere")
	foreign := doctor.RunCheckCardLinkWith(root)
	if !strings.Contains(foreign.Remediation, "atomic repo init") {
		t.Errorf("remediation = %q, want the retry verb", foreign.Remediation)
	}
	for name, r := range map[string]doctor.Result{
		"foreign link":   foreign,
		"no remediation": {Severity: doctor.WARN, Detail: "read failed"},
	} {
		t.Run(name, func(t *testing.T) {
			r.Name = "card-link"
			var out strings.Builder
			doctor.Repair([]doctor.Result{r}, doctor.Opts{}, nil, &out)
			if strings.Contains(out.String(), "; \n") || !strings.Contains(out.String(), "repair plan: cannot auto-fix") {
				t.Errorf("printed plan:\n%s", out.String())
			}
		})
	}
}

// The link the writer produces still reaches the state root of a repository
// reached through a symlinked one.
func TestCheckCardLinkPassesUnderAnUnresolvedRoot(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(real, ".atomic", "rules", "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(real, ".claude", "rules", "atomic")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", ".atomic", "rules"), link); err != nil {
		t.Fatal(err)
	}
	if got := doctor.RunCheckCardLinkWith(alias); got.Severity != doctor.PASS {
		t.Errorf("result = %+v, want a pass", got)
	}
}
