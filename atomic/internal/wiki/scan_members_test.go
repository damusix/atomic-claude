package wiki_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

func TestReadScanMembers_HappyPath(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	content := `# wiki index

<wiki-scan generated="2026-01-01" root="/realm">
  <repo path="repos/alpha" status="indexed"/>
  <repo path="repos/beta" status="summarized" summary="repos/beta.md"/>
  <repo path="repos/pending" status="pending"/>
</wiki-scan>
`
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	members, err := wiki.ReadScanMembers(indexPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d: %+v", len(members), members)
	}

	byPath := make(map[string]wiki.Member)
	for _, m := range members {
		byPath[m.Path] = m
	}

	if m, ok := byPath["repos/alpha"]; !ok {
		t.Error("missing repos/alpha")
	} else if m.Status != "indexed" {
		t.Errorf("expected status=indexed, got %q", m.Status)
	}

	if m, ok := byPath["repos/beta"]; !ok {
		t.Error("missing repos/beta")
	} else if m.Status != "summarized" {
		t.Errorf("expected status=summarized, got %q", m.Status)
	} else if m.SummaryPath != "repos/beta.md" {
		t.Errorf("expected SummaryPath=repos/beta.md, got %q", m.SummaryPath)
	}

	if m, ok := byPath["repos/pending"]; !ok {
		t.Error("missing repos/pending")
	} else if m.Status != "pending" {
		t.Errorf("expected status=pending, got %q", m.Status)
	}
}

func TestReadScanMembers_AbsentFile(t *testing.T) {
	dir := t.TempDir()
	members, err := wiki.ReadScanMembers(filepath.Join(dir, "missing.md"))
	if err != nil {
		t.Fatalf("absent file must not error, got: %v", err)
	}
	if members != nil {
		t.Fatalf("absent file must return nil, got: %v", members)
	}
}

func TestReadScanMembers_NoBlock(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	if err := os.WriteFile(indexPath, []byte("# no scan block here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	members, err := wiki.ReadScanMembers(indexPath)
	if err != nil {
		t.Fatalf("missing block must not error, got: %v", err)
	}
	if members != nil {
		t.Fatalf("missing block must return nil, got: %v", members)
	}
}
