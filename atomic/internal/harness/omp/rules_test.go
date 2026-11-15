package omp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// writeCards runs the real card producer over stateRoot, so the fixture is the
// exact canonical card set the projection consumes.
func writeCards(t *testing.T, stateRoot, projectKey string, cards ...wiki.PointerCard) {
	t.Helper()
	if _, err := wiki.Refresh(stateRoot, projectKey, wiki.RefreshRepo, cards); err != nil {
		t.Fatalf("wiki.Refresh: %v", err)
	}
}

func pointerCard(domain string, include []string, body string) wiki.PointerCard {
	return wiki.PointerCard{Domain: domain, Include: include, Body: body}
}

// cardFile is where OMP reads one domain's card: directly under .omp/rules.
func cardFile(base, domain string) string {
	return filepath.Join(base, ".omp", "rules", "atomic-wiki-"+domain+".md")
}

// TestProjectShippedRulesHonestAndShipsBodies proves the projection ships the
// authored bodies while claiming nothing OMP 18.1.18 did not prove.
func TestProjectShippedRulesHonestAndShipsBodies(t *testing.T) {
	cat := fixtureCorpus(t)
	report, err := ProjectShippedRules(cat, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("ProjectShippedRules: %v", err)
	}
	if len(report.Rules) == 0 {
		t.Fatal("the fixture corpus carries no rule")
	}
	if report.Ordering != harness.RuleOrderingRecordID {
		t.Errorf("ordering = %q, want %q", report.Ordering, harness.RuleOrderingRecordID)
	}
	for i, r := range report.Rules {
		if i > 0 && report.Rules[i-1].RecordID >= r.RecordID {
			t.Errorf("rules are not in ascending identity order: %s then %s", report.Rules[i-1].RecordID, r.RecordID)
		}
		if got := managedfile.Digest(r.Bytes); got != r.SourceDigest {
			t.Errorf("%s: source digest %s does not match its bytes %s", r.RecordID, r.SourceDigest, got)
		}
		if r.Digest != artifacts.ProjectionDigest(r.Bytes) {
			t.Errorf("%s: projection digest does not match its bytes", r.RecordID)
		}
		if r.Tier != artifacts.EnforcementUnsupported {
			t.Errorf("%s: tier = %q; OMP proved no matched-body delivery", r.RecordID, r.Tier)
		}
		if len(r.Scope) != 0 {
			t.Errorf("%s: scope = %+v; no native scope field is proven", r.RecordID, r.Scope)
		}
		if r.Delivery != rules.RuntimeNone {
			t.Errorf("%s: delivery = %q, want %q", r.RecordID, r.Delivery, rules.RuntimeNone)
		}
	}
	var roles []string
	for _, gap := range report.Gaps {
		roles = append(roles, string(gap.Role))
	}
	for _, want := range []string{"static-scoped-rule", "context-return"} {
		if !contains(roles, want) {
			t.Errorf("gap %s missing from %v", want, roles)
		}
	}

	// Integration with package generation: the package carries the projection
	// byte for byte and claims the same tier and absence of scope.
	pkg, err := BuildPackage(cat, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}
	for _, r := range report.Rules {
		file, ok := findFile(pkg, r.Source)
		if !ok {
			t.Fatalf("package is missing rule %s", r.Source)
		}
		if string(file.Bytes) != string(r.Bytes) || file.Digest != r.Digest {
			t.Errorf("package rule %s does not match the projection bytes", r.Source)
		}
		if file.Tier != artifacts.EnforcementUnsupported || len(file.Scope) != 0 || file.Delivery != rules.RuntimeNone {
			t.Errorf("package rule %s claims tier %q scope %+v delivery %q", r.Source, file.Tier, file.Scope, file.Delivery)
		}
	}
}

// TestProjectShippedRulesMissingRowsStayUnsupported proves a missing
// rule-delivery row records unsupported and never fails the install path.
func TestProjectShippedRulesMissingRowsStayUnsupported(t *testing.T) {
	cat := fixtureCorpus(t)
	rowless := harness.CapabilityMatrix{Harness: harness.KindOMP, Version: "0.0.0"}
	report, err := ProjectShippedRules(cat, rowless)
	if err != nil {
		t.Fatalf("ProjectShippedRules with a rowless record: %v", err)
	}
	if len(report.Rules) == 0 {
		t.Fatal("a rowless record dropped the shipped rule corpus")
	}
	for _, r := range report.Rules {
		if r.Tier != artifacts.EnforcementUnsupported || len(r.Scope) != 0 || r.Delivery != rules.RuntimeNone {
			t.Errorf("%s claims tier %q scope %+v delivery %q from a rowless record", r.RecordID, r.Tier, r.Scope, r.Delivery)
		}
	}
	if len(report.Gaps) != 5 {
		t.Errorf("gaps = %d, want all five rule-delivery roles", len(report.Gaps))
	}
	if _, err := BuildPackage(cat, rowless, nil); err != nil {
		t.Fatalf("package generation failed on a rowless record: %v", err)
	}
}

// TestLoadProjectCardsShipsBodiesWithoutScopeClaims proves cards ship their
// canonical bytes with no native scope claim.
func TestLoadProjectCardsShipsBodiesWithoutScopeClaims(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key",
		pointerCard("typescript", []string{"**/*.ts", "**/*.tsx"}, "# TypeScript domain\n\nbody\n"),
		pointerCard("python", []string{"**/*.py"}, "# Python domain\n\nbody\n"),
	)

	cards, err := LoadProjectCards(stateRoot, "repo-key", base, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("LoadProjectCards: %v", err)
	}
	if cards.Dir != filepath.Join(cards.Base, ".omp", "rules") {
		t.Errorf("native dir = %q, want the repository-relative OMP project rule directory under %q", cards.Dir, cards.Base)
	}
	if len(cards.Cards) != 2 {
		t.Fatalf("cards = %+v, want two", cards.Cards)
	}
	if cards.Cards[0].Name != "atomic-wiki-python.md" || cards.Cards[1].Name != "atomic-wiki-typescript.md" {
		t.Errorf("cards are not flat atomic-wiki-<domain>.md files in identity order: %+v", cards.Cards)
	}
	for _, card := range cards.Cards {
		if card.Tier != artifacts.EnforcementUnsupported || len(card.Scope) != 0 || card.Delivery != rules.RuntimeNone {
			t.Errorf("card %s claims tier %q scope %+v delivery %q", card.RecordID, card.Tier, card.Scope, card.Delivery)
		}
		if !strings.HasPrefix(card.RecordID, "wiki:repo-key:") {
			t.Errorf("card identity %q is not project-qualified", card.RecordID)
		}
		if !strings.Contains(string(card.Bytes), "globs:") {
			t.Errorf("card %s lost its scope metadata:\n%s", card.RecordID, card.Bytes)
		}
	}
}

// OMP lists a rule only when its frontmatter carries `description`, and shows
// `globs` as the scope; a `paths:` key is ignored.
func TestProjectCardCarriesTheDescriptionAndGlobsOMPReads(t *testing.T) {
	data := []byte("---\n# generated by /refresh-wiki\npaths:\n  - \"src/**\"\n  - \"docs/*.md\"\n---\n\nDomain: signals. Scan and infer \"project\" context.\n\nMap:\n  - docs/wiki/signals.md\n")
	record, err := rules.ParseWiki("repo-key", "rules/wiki/signals.md", data)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := BuildProjectCards([]harness.RuleSource{{Record: record, Bytes: data}}, "repo-key", t.TempDir(), harness.OMPCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	kvs, body, err := frontmatter.ParseOrdered(string(cards.Cards[0].Bytes))
	if err != nil {
		t.Fatalf("projected card frontmatter: %v", err)
	}
	got := map[string]any{}
	for _, kv := range kvs {
		got[kv.Key] = kv.Value
	}
	if got["description"] != "Domain: signals. Scan and infer \"project\" context." {
		t.Errorf("description = %q, want the card's first body line", got["description"])
	}
	if globs, _ := got["globs"].([]any); len(globs) != 2 || globs[0] != "src/**" || globs[1] != "docs/*.md" {
		t.Errorf("globs = %v, want the card's paths", got["globs"])
	}
	if _, ok := got["paths"]; ok {
		t.Errorf("projected card keeps a paths key OMP does not read: %v", got)
	}
	if !strings.Contains(body, "Map:\n  - docs/wiki/signals.md") {
		t.Errorf("projected card lost the canonical body:\n%s", body)
	}
	if !strings.Contains(string(cards.Cards[0].Bytes), cardMarker) {
		t.Errorf("projected card carries no generated marker:\n%s", cards.Cards[0].Bytes)
	}
}

// A card with no prose line still needs a description or OMP never lists it.
func TestProjectCardDescriptionFallsBackToTheHeadingThenTheDomain(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"heading", "# TypeScript domain\n\nbody\n", "TypeScript domain"},
		{"empty body", "", "Atomic wiki card for typescript"},
		{"html characters stay literal", "Domain: a <b> & c.\n", "Domain: a <b> & c."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("---\npaths:\n  - \"**/*.ts\"\n---\n\n" + tc.body)
			record, err := rules.ParseWiki("repo-key", "rules/wiki/typescript.md", data)
			if err != nil {
				t.Fatal(err)
			}
			cards, err := BuildProjectCards([]harness.RuleSource{{Record: record, Bytes: data}}, "repo-key", t.TempDir(), harness.OMPCapabilities())
			if err != nil {
				t.Fatal(err)
			}
			kvs, _, err := frontmatter.ParseOrdered(string(cards.Cards[0].Bytes))
			if err != nil {
				t.Fatal(err)
			}
			for _, kv := range kvs {
				if kv.Key == "description" && kv.Value != tc.want {
					t.Errorf("description = %q, want %q", kv.Value, tc.want)
				}
			}
		})
	}
}

// The flat file name embeds the domain, so a slash would escape the rule directory.
func TestBuildProjectCardsRefusesADomainWithASlash(t *testing.T) {
	src := wikiCardSource(t, "one", "ab")
	src.Record.Source = "rules/wiki/a/b.md"
	_, err := BuildProjectCards([]harness.RuleSource{src}, "one", t.TempDir(), harness.OMPCapabilities())
	if err == nil || !strings.Contains(err.Error(), "no usable domain") {
		t.Fatalf("error = %v, want a no-usable-domain refusal", err)
	}
}

// TestProjectCardsBindPerProjectKey proves record identity is stable across
// worktrees, and a symlinked base resolves to one native directory.
func TestProjectCardsBindPerProjectKey(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	worktreeA := t.TempDir()
	worktreeB := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a, err := LoadProjectCards(stateRoot, "repo-key", worktreeA, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	b, err := LoadProjectCards(stateRoot, "repo-key", worktreeB, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(a.Instances) != 1 || len(b.Instances) != 1 {
		t.Fatalf("instances = %d and %d, want one each", len(a.Instances), len(b.Instances))
	}
	if a.Instances[0].Record.ID != b.Instances[0].Record.ID || a.Instances[0].Record.SourceDigest != b.Instances[0].Record.SourceDigest {
		t.Errorf("two worktrees disagree on record identity: %+v vs %+v", a.Instances[0].Record, b.Instances[0].Record)
	}
	if a.Instances[0].Base != a.Base {
		t.Errorf("instance base %q does not match the projection base %q", a.Instances[0].Base, a.Base)
	}
	if a.Instances[0].Base == b.Instances[0].Base || a.Dir == b.Dir {
		t.Errorf("two worktrees share a base: %q vs %q", a.Instances[0].Base, b.Instances[0].Base)
	}

	link := filepath.Join(t.TempDir(), "worktree-link")
	if err := os.Symlink(worktreeA, link); err != nil {
		t.Fatal(err)
	}
	linked, err := LoadProjectCards(stateRoot, "repo-key", link, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("load through symlink: %v", err)
	}
	if linked.Base != a.Base || linked.Dir != a.Dir {
		t.Errorf("symlinked base resolved to %q, want %q", linked.Base, a.Base)
	}
}

// TestBuildProjectCardsRejectsCrossProjectCollision proves two projects' cards
// can never share one native directory.
func TestBuildProjectCardsRejectsCrossProjectCollision(t *testing.T) {
	_, err := BuildProjectCards([]harness.RuleSource{
		wikiCardSource(t, "one", "signals"),
		wikiCardSource(t, "two", "signals"),
	}, "one", t.TempDir(), harness.OMPCapabilities())
	if err == nil {
		t.Fatal("cross-project card collision was accepted")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("error = %v, want a collision refusal", err)
	}
}

// wikiCardSource parses one canonical pointer card into a projection source.
func wikiCardSource(t *testing.T, projectKey, domain string) harness.RuleSource {
	t.Helper()
	source := "rules/wiki/" + domain + ".md"
	data := []byte("---\npaths:\n  - \"**/*.ts\"\n---\n\n# " + domain + "\n")
	record, err := rules.ParseWiki(projectKey, source, data)
	if err != nil {
		t.Fatalf("ParseWiki: %v", err)
	}
	return harness.RuleSource{Record: record, Bytes: data}
}

// TestPublishProjectCardsWritesRecoverably proves publication journals before
// the native write and a rerun is a no-op.
func TestPublishProjectCardsWritesRecoverably(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-first",
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !result.Applied || result.Status != harness.StatusConverged {
		t.Fatalf("result = %+v, want an applied converged publication", result)
	}
	if result.JournalPath == "" {
		t.Fatalf("publication recorded no operation id")
	}
	// The completed operation is consumed: its journal and transaction tree are
	// removed once the rows are committed.
	if _, err := os.Stat(result.JournalPath); !os.IsNotExist(err) {
		t.Errorf("completed publication left its journal behind (stat err = %v)", err)
	}

	published := cardFile(base, "typescript")
	if !exists(published) {
		t.Fatalf("published card %s is missing", published)
	}
	if got := readFile(t, published); !strings.Contains(got, "# TS") {
		t.Errorf("published card body = %q", got)
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find(result.Target.Key(), filepath.Join(result.Dir, "atomic-wiki-typescript.md"))
	if !ok {
		t.Fatal("no ownership row for the card file")
	}
	if row.Generation != result.Generation || row.Tier != string(Tier) || row.Consumer != "repo-key" {
		t.Errorf("row = %+v", row)
	}
	if row.Applied.Kind != managedfile.KindFile {
		t.Errorf("row applied kind = %s, want file", row.Applied.Kind)
	}
	if row.Applied.Digest != managedfile.Digest([]byte(readFile(t, published))) {
		t.Errorf("row applied digest = %s, want the published bytes' digest", row.Applied.Digest)
	}

	// A rerun on the same generation writes nothing.
	before := readFile(t, published)
	rerun, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-second",
	})
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if rerun.Applied || rerun.JournalPath != "" {
		t.Errorf("rerun = %+v, want a no-op", rerun)
	}
	if got := readFile(t, published); got != before {
		t.Error("rerun rewrote the published card")
	}
	if _, err := os.Stat(config.TransactionDir(home, "cards-second")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no-op rerun created a transaction directory: %v", err)
	}
}

// TestPublishProjectCardsRecoversInterruptedLedgerCommit proves a crash between
// publication and ledger commit converges without rewriting bytes.
func TestPublishProjectCardsRecoversInterruptedLedgerCommit(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("signals", []string{"docs/**"}, "# Signals\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-first",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	published := cardFile(base, "signals")
	before := readFile(t, published)

	// Simulate a lost or interrupted ledger commit: the native tree is intact,
	// the ownership row is gone.
	if err := os.Remove(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-recover",
	})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if result.Applied {
		t.Error("recovery rewrote native bytes that were already applied")
	}
	if got := readFile(t, published); got != before {
		t.Error("recovery changed the published card")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(result.Target.Key(), filepath.Join(result.Dir, "atomic-wiki-signals.md")); !ok {
		t.Error("recovery did not re-record the ownership row")
	}
}

// TestProjectCardsFreshnessReportsStale proves a canonical card that moved
// ahead of the surface is reported stale before any mutation, then applied.
func TestProjectCardsFreshnessReportsStale(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS v1\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-first",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS v2\n"))
	assessment, gaps, err := a.ProjectRuleStatus(stateRoot, "repo-key", base)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if assessment.Fresh {
		t.Error("a moved canonical card reported fresh")
	}
	if len(assessment.Cards) != 1 || assessment.Cards[0].State != CardStale {
		t.Fatalf("cards = %+v, want one stale card", assessment.Cards)
	}
	if len(gaps) == 0 {
		t.Error("status reports no capability gap")
	}

	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-refresh",
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !result.Applied || !result.Fresh {
		t.Errorf("refresh result = %+v, want an applied fresh publication", result)
	}
	if got := readFile(t, cardFile(base, "typescript")); !strings.Contains(got, "# TS v2") {
		t.Errorf("published card = %q, want the moved source", got)
	}
}

// TestProjectCardsRenameAndDelete proves a renamed domain replaces its old card
// and a deleted card set prunes the surface.
func TestProjectCardsRenameAndDelete(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key",
		pointerCard("signals", []string{"docs/**"}, "# Signals\n"),
		pointerCard("python", []string{"**/*.py"}, "# Python\n"),
	)

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-first",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	cardsDir := filepath.Join(base, ".omp", "rules")
	writeFile(t, filepath.Join(cardsDir, "mine.md"), "# mine\n")

	// Rename signals -> telemetry.
	writeCards(t, stateRoot, "repo-key",
		pointerCard("telemetry", []string{"docs/**"}, "# Telemetry\n"),
		pointerCard("python", []string{"**/*.py"}, "# Python\n"),
	)
	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-rename",
	})
	if err != nil {
		t.Fatalf("rename publish: %v", err)
	}
	if !contains(result.Removed, "atomic-wiki-signals.md") {
		t.Errorf("removed = %v, want atomic-wiki-signals.md", result.Removed)
	}
	if !exists(cardFile(base, "telemetry")) || exists(cardFile(base, "signals")) {
		t.Errorf("cards after rename: telemetry=%v signals=%v", exists(cardFile(base, "telemetry")), exists(cardFile(base, "signals")))
	}

	// Delete every card.
	writeCards(t, stateRoot, "repo-key")
	result, err = a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-delete",
	})
	if err != nil {
		t.Fatalf("delete publish: %v", err)
	}
	if !result.Applied || len(result.Cards) != 0 {
		t.Errorf("delete result = %+v, want an applied empty projection", result)
	}
	entries, err := os.ReadDir(cardsDir)
	if err != nil {
		t.Fatalf("read cards dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "mine.md" {
		t.Errorf("rules dir after deleting every card holds %v, want only the user's mine.md", entries)
	}
}

// TestPublishProjectCardsConflictPreservesBytes proves a surface changed
// outside Atomic's applied digest is refused before mutation and preserved.
func TestPublishProjectCardsConflictPreservesBytes(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	first, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-first",
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// A hand edit moves the file outside both the recorded and the projected digest.
	tampered := cardFile(base, "typescript")
	writeFile(t, tampered, "# hand edited\n")
	tamperedBytes := readFile(t, tampered)
	ledgerBefore := readFile(t, config.LedgerPath(home))

	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-conflict",
	})
	if err == nil {
		t.Fatal("a changed derivative was overwritten")
	}
	if result.Status != harness.StatusConflicted {
		t.Errorf("status = %q, want conflicted", result.Status)
	}
	if got := readFile(t, tampered); got != tamperedBytes {
		t.Error("the conflicting card was modified")
	}
	if got := readFile(t, config.LedgerPath(home)); got != ledgerBefore {
		t.Error("the conflict path changed the ledger")
	}
	if _, err := os.Stat(config.TransactionDir(home, "cards-conflict")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("conflict created a transaction directory: %v", err)
	}
	if first.Generation == "" {
		t.Error("first publication recorded no generation")
	}
}

// TestPublishProjectCardsConflictOnUnrecordedSurface proves bytes Atomic never
// recorded writing are preserved, not clobbered.
func TestPublishProjectCardsConflictOnUnrecordedSurface(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	foreign := cardFile(base, "typescript")
	writeFile(t, foreign, "# foreign\n")
	foreignBytes := readFile(t, foreign)

	a := newAdapter(t, map[string]string{})
	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-foreign",
	})
	if err == nil {
		t.Fatal("an unrecorded surface was overwritten")
	}
	if !strings.Contains(err.Error(), "atomic-wiki-typescript.md") {
		t.Errorf("error = %v, want the conflicting file named", err)
	}
	if result.Status != harness.StatusConflicted {
		t.Errorf("status = %q, want conflicted", result.Status)
	}
	if got := readFile(t, foreign); got != foreignBytes {
		t.Error("the unrecorded card was modified")
	}
	if exists(config.LedgerPath(home)) {
		t.Error("the refusal wrote a ledger")
	}
}

// TestPublishProjectCardsEmptyProjectIsNoOp proves a repository with no cards
// creates no native surface, journal, or ledger state.
func TestPublishProjectCardsEmptyProjectIsNoOp(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base := t.TempDir()

	a := newAdapter(t, map[string]string{})
	result, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-empty",
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if result.Applied || result.JournalPath != "" {
		t.Errorf("result = %+v, want a no-op", result)
	}
	if exists(filepath.Join(base, ".omp")) {
		t.Error("an empty projection created the .omp directory")
	}
	if exists(config.LedgerPath(home)) {
		t.Error("an empty projection wrote a ledger")
	}
}

// altDeviceRoot returns a writable directory on another filesystem, or "". macOS
// has no unprivileged alternate volume, so the test skips there.
func altDeviceRoot(base string) string {
	candidates := []string{"/dev/shm", "/run/shm"}
	if override := os.Getenv("ATOMIC_TEST_ALT_DEVICE"); override != "" {
		candidates = append([]string{override}, candidates...)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		if onDifferentDevice(base, candidate) {
			return candidate
		}
	}
	return ""
}

func onDifferentDevice(a, b string) bool {
	deviceOf := func(path string) (uint64, bool) {
		fi, err := os.Stat(path)
		if err != nil {
			return 0, false
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, false
		}
		return uint64(st.Dev), true
	}
	devA, okA := deviceOf(a)
	devB, okB := deviceOf(b)
	return okA && okB && devA != devB
}

// TestPublishProjectCardsAcrossFilesystems guards a card surface on a different
// filesystem than the HOME-rooted stage: no half-copied tree, no residue.
func TestPublishProjectCardsAcrossFilesystems(t *testing.T) {
	home := newHome(t)
	alt := altDeviceRoot(home)
	if alt == "" {
		t.Skip("no second writable filesystem available")
	}
	stateRoot := filepath.Join(t.TempDir(), ".claude")
	base, err := os.MkdirTemp(alt, "atomic-omp-crossdev-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	first, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-crossdev-first",
	})
	if err != nil {
		t.Fatalf("cross-filesystem publish: %v", err)
	}
	if !first.Applied || first.Status != harness.StatusConverged {
		t.Fatalf("result = %+v, want an applied converged publication", first)
	}
	published := cardFile(base, "typescript")
	if got := readFile(t, published); !strings.Contains(got, "# TS") {
		t.Errorf("published card body = %q", got)
	}
	if _, err := os.Stat(first.JournalPath); !os.IsNotExist(err) {
		t.Errorf("completed cross-filesystem publication left its journal behind (stat err = %v)", err)
	}
	if _, err := os.Stat(config.TransactionDir(home, "cards-crossdev-first")); !os.IsNotExist(err) {
		t.Errorf("completed cross-filesystem publication left its transaction tree behind (stat err = %v)", err)
	}
	// The copy is staged beside the destination and renamed into place, so a
	// successful publication leaves no staging residue on that filesystem.
	entries, err := os.ReadDir(filepath.Dir(published))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "atomic-wiki-typescript.md" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("rules dir holds %v, want only the published card", names)
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find(first.Target.Key(), filepath.Join(first.Dir, "atomic-wiki-typescript.md"))
	if !ok {
		t.Fatal("no ownership row for the cross-filesystem card")
	}
	if row.Applied.Digest != managedfile.Digest([]byte(readFile(t, published))) {
		t.Errorf("row applied digest = %s, want the published bytes' digest", row.Applied.Digest)
	}

	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS v2\n"))
	second, err := a.PublishProjectCards(CardPublishRequest{
		Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base, OperationID: "cards-crossdev-second",
	})
	if err != nil {
		t.Fatalf("cross-filesystem replacement: %v", err)
	}
	if !second.Applied || second.Status != harness.StatusConverged {
		t.Fatalf("replacement result = %+v", second)
	}
	if got := readFile(t, published); !strings.Contains(got, "# TS v2") {
		t.Errorf("replaced card body = %q", got)
	}
	if _, err := os.Stat(second.JournalPath); !os.IsNotExist(err) {
		t.Errorf("completed cross-filesystem replacement left its journal behind (stat err = %v)", err)
	}
	ledger, err = installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok = ledger.Find(second.Target.Key(), filepath.Join(second.Dir, "atomic-wiki-typescript.md"))
	if !ok {
		t.Fatal("replacement dropped the ownership row")
	}
	if want := managedfile.Digest([]byte(readFile(t, published))); row.Applied.Digest != want {
		t.Errorf("replacement row digest = %s, want %s", row.Applied.Digest, want)
	}
}

// A generated card no longer projected is kept, and reported, when its bytes
// differ from what the ledger recorded.
func TestPublishProjectCardsKeepsAHandEditedDroppedCard(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base := t.TempDir()
	writeCards(t, stateRoot, "repo-key",
		pointerCard("signals", []string{"docs/**"}, "# Signals\n"),
		pointerCard("python", []string{"**/*.py"}, "# Python\n"),
	)
	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	dropped := cardFile(base, "signals")
	edited := readFile(t, dropped) + "hand edit\n"
	writeFile(t, dropped, edited)
	writeCards(t, stateRoot, "repo-key", pointerCard("python", []string{"**/*.py"}, "# Python\n"))

	result, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := readFile(t, dropped); got != edited {
		t.Error("the hand-edited dropped card was removed or changed")
	}
	if len(result.Kept) != 1 || filepath.Base(result.Kept[0]) != "atomic-wiki-signals.md" {
		t.Errorf("kept = %v, want the dropped card reported", result.Kept)
	}
	if result.Fresh {
		t.Error("a publication that kept a dropped card reported fresh")
	}
}

// OMP's rule directory holds the repository's own rules, so publication leaves
// every file it did not write alone.
func TestPublishProjectCardsLeavesForeignRulesAlone(t *testing.T) {
	home := newHome(t)
	stateRoot := filepath.Join(t.TempDir(), ".atomic")
	base := t.TempDir()
	mine := filepath.Join(base, ".omp", "rules", "mine.md")
	writeFile(t, mine, "---\ndescription: mine\n---\n")
	writeCards(t, stateRoot, "repo-key", pointerCard("typescript", []string{"**/*.ts"}, "# TS\n"))

	a := newAdapter(t, map[string]string{})
	if _, err := a.PublishProjectCards(CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "repo-key", Base: base}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := readFile(t, mine); got != "---\ndescription: mine\n---\n" {
		t.Errorf("the repository's own rule changed: %q", got)
	}
}
