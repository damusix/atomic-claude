package wiki

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// Repository wiki init, the refresh pipeline's last binary step, re-renders
// the cards through Refresh and projects them; canonical cards stay unchanged.
func TestWikiInitRepoRefreshesAndProjectsCards(t *testing.T) {
	root := t.TempDir()
	stateRoot, err := config.RepositoryStateDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(stateRoot, "k", RefreshRepo, []PointerCard{{Domain: "core", Include: []string{"src/**"}, Body: "Domain: core.\n"}}); err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(stateRoot, "rules", "wiki", "core.md")
	before, err := os.ReadFile(card)
	if err != nil {
		t.Fatal(err)
	}

	var gotRoot, gotState string
	prev := ProjectRepoCards
	ProjectRepoCards = func(repoRoot, state string) error {
		gotRoot, gotState = repoRoot, state
		return nil
	}
	t.Cleanup(func() { ProjectRepoCards = prev })

	var out bytes.Buffer
	if code := wikiInitAction([]string{"--scope=repo", "--root=" + root}, root, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	after, err := os.ReadFile(card)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("canonical card changed:\n%s\n---\n%s", before, after)
	}
	if gotRoot != root || gotState != stateRoot {
		t.Errorf("projection got (%q, %q), want (%q, %q)", gotRoot, gotState, root, stateRoot)
	}
}

// A card Refresh cannot parse fails the verb rather than projecting it.
func TestWikiInitRepoRefusesAMalformedCard(t *testing.T) {
	root := t.TempDir()
	stateRoot, err := config.RepositoryStateDir(root)
	if err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(stateRoot, "rules", "wiki", "core.md")
	if err := os.MkdirAll(filepath.Dir(card), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(card, []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := ProjectRepoCards
	ProjectRepoCards = func(string, string) error {
		t.Error("a malformed card reached the projection")
		return nil
	}
	t.Cleanup(func() { ProjectRepoCards = prev })

	var out bytes.Buffer
	if code := wikiInitAction([]string{"--scope=repo", "--root=" + root}, root, &out); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(out.String(), "created "+filepath.Join(root, "docs", "wiki", "CLAUDE.md")) {
		t.Errorf("stdout = %q, want the files written before the failure", out.String())
	}
}

// A projection conflict is target state, not a failure of the canonical
// output, so the verb warns and exits 0.
func TestWikiInitRepoWarnsOnAProjectionError(t *testing.T) {
	root := t.TempDir()
	prev := ProjectRepoCards
	ProjectRepoCards = func(string, string) error { return errors.New("link conflict") }
	t.Cleanup(func() { ProjectRepoCards = prev })

	var out bytes.Buffer
	if code := wikiInitAction([]string{"--scope=repo", "--root=" + root}, root, &out); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(out.String(), "created ") {
		t.Errorf("stdout = %q, want the created files", out.String())
	}
}
