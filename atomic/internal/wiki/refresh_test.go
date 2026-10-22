package wiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

func card(domain string, include ...string) PointerCard {
	return PointerCard{
		Domain:  domain,
		Include: include,
		Body:    "Domain: " + domain + ".\n\nMap:\n  - docs/wiki/" + domain + ".md\n",
	}
}

// A repository refresh publishes one card per domain under the selected state
// root and yields the same records a later load reads back.
func TestRefresh_RepoPublishesWikiRecords(t *testing.T) {
	root := t.TempDir()

	records, err := Refresh(root, "proj", RefreshRepo, []PointerCard{
		card("signals", "atomic/internal/signals/**", "context/agents/atomic-wiki-inferrer.md"),
		card("wiki", "atomic/internal/wiki/**"),
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}

	byID := map[string]rules.RuleRecord{}
	for _, r := range records {
		byID[r.ID] = r
	}
	signals, ok := byID["wiki:proj:signals"]
	if !ok {
		t.Fatalf("missing wiki:proj:signals in %+v", byID)
	}
	if signals.Producer != rules.ProducerWiki {
		t.Errorf("producer = %q, want %q", signals.Producer, rules.ProducerWiki)
	}
	if signals.Class != rules.ClassWikiPointer {
		t.Errorf("class = %q, want %q", signals.Class, rules.ClassWikiPointer)
	}
	if got, want := signals.Include, []string{"atomic/internal/signals/**", "context/agents/atomic-wiki-inferrer.md"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("include = %v, want %v", got, want)
	}

	for _, domain := range []string{"signals", "wiki"} {
		if _, err := os.Stat(filepath.Join(root, "rules", "wiki", domain+".md")); err != nil {
			t.Errorf("card %s not written: %v", domain, err)
		}
	}

	reloaded, err := rules.LoadWiki(filepath.Join(root, "rules"), "proj")
	if err != nil {
		t.Fatalf("LoadWiki: %v", err)
	}
	if len(reloaded) != len(records) {
		t.Fatalf("reloaded = %d records, want %d", len(reloaded), len(records))
	}
	for i := range records {
		if reloaded[i].ID != records[i].ID || reloaded[i].SourceDigest != records[i].SourceDigest {
			t.Errorf("record %d = %+v, want %+v", i, reloaded[i], records[i])
		}
	}
}

// Realm refresh spans multiple repository bases: it produces no scoped pointer
// rules and writes nothing, even when handed a card set.
func TestRefresh_RealmWritesNoCards(t *testing.T) {
	root := t.TempDir()

	records, err := Refresh(root, "proj", RefreshRealm, []PointerCard{card("signals", "atomic/internal/signals/**")})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if records != nil {
		t.Fatalf("records = %+v, want nil", records)
	}
	if _, err := os.Stat(filepath.Join(root, "rules")); !os.IsNotExist(err) {
		t.Fatalf("realm refresh wrote under rules/: stat err = %v", err)
	}
}

// Cards are pipeline-owned and regenerated every refresh, so a domain absent
// from the current router table loses its card.
func TestRefresh_PrunesRemovedDomain(t *testing.T) {
	root := t.TempDir()
	if _, err := Refresh(root, "proj", RefreshRepo, []PointerCard{card("old", "a/**"), card("kept", "b/**")}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	records, err := Refresh(root, "proj", RefreshRepo, []PointerCard{card("kept", "b/**")})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(records) != 1 || records[0].ID != "wiki:proj:kept" {
		t.Fatalf("records = %+v, want only wiki:proj:kept", records)
	}
	if _, err := os.Stat(filepath.Join(root, "rules", "wiki", "old.md")); !os.IsNotExist(err) {
		t.Fatalf("stale card survived: stat err = %v", err)
	}
}

// A card the parser rejects is refused before it reaches the filesystem, and
// the refusal leaves existing cards untouched.
func TestRefresh_RejectsMalformedCard(t *testing.T) {
	root := t.TempDir()
	if _, err := Refresh(root, "proj", RefreshRepo, []PointerCard{card("kept", "a/**")}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if _, err := Refresh(root, "proj", RefreshRepo, []PointerCard{card("kept", "a/**"), {Domain: "empty", Body: "body\n"}}); err == nil {
		t.Fatal("Refresh accepted a card with no include globs")
	}
	if _, err := os.Stat(filepath.Join(root, "rules", "wiki", "empty.md")); !os.IsNotExist(err) {
		t.Fatalf("malformed card was written: stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "rules", "wiki", "kept.md")); err != nil {
		t.Fatalf("rejected refresh pruned an existing card: %v", err)
	}
}

// The cards land under whatever state root the caller selected, not a
// hardcoded .claude.
func TestRefresh_UsesSelectedStateRoot(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "custom-state")

	if _, err := Refresh(stateRoot, "proj", RefreshRepo, []PointerCard{card("wiki", "atomic/internal/wiki/**")}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "rules", "wiki", "wiki.md")); err != nil {
		t.Fatalf("card not written under selected state root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "rules")); !os.IsNotExist(err) {
		t.Fatalf("refresh wrote outside the selected state root: stat err = %v", err)
	}
}

func TestRefresh_UnknownScope(t *testing.T) {
	if _, err := Refresh(t.TempDir(), "proj", RefreshScope("bogus"), nil); err == nil {
		t.Fatal("Refresh accepted an unknown scope")
	}
}
