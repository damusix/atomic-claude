package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeIndexDB creates the minimal directory structure and a zero-byte DB file
// at the canonical path (<root>/.claude/.atomic-index/atomic.db) with the
// given mtime. It does NOT open SQLite — the doctor check only stat-inspects
// the file, never reads it.
func makeIndexDB(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("makeIndexDB mkdir: %v", err)
	}
	dbPath := filepath.Join(dir, "atomic.db")
	if err := os.WriteFile(dbPath, []byte{}, 0o644); err != nil {
		t.Fatalf("makeIndexDB write: %v", err)
	}
	if err := os.Chtimes(dbPath, mtime, mtime); err != nil {
		t.Fatalf("makeIndexDB chtimes: %v", err)
	}
}

// TestCheckCodeIndexAbsent is the key spec assertion: absence of the DB must
// produce PASS (informational), never WARN.
//
// The index is opt-in; millions of repos that never run `atomic code index`
// must not see a persistent WARN on every `atomic doctor` run.
func TestCheckCodeIndexAbsent(t *testing.T) {
	root := t.TempDir()
	// No index directory, no DB file.
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS when index absent (must never be WARN)", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckCodeIndexFresh verifies PASS when the DB exists and is within the
// staleness threshold.
func TestCheckCodeIndexFresh(t *testing.T) {
	root := t.TempDir()
	// mtime = 2 days ago, threshold = 7
	mtime := time.Now().Add(-2 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (fresh index, detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckCodeIndexStale verifies WARN when the DB exists but is older than
// the staleness threshold.
func TestCheckCodeIndexStale(t *testing.T) {
	root := t.TempDir()
	// mtime = 10 days ago, threshold = 7
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (stale index)", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckCodeIndexNeverFail asserts the check never produces FAIL regardless
// of how broken the environment is. The code index is opt-in and optional; it
// must never be a hard installation requirement.
func TestCheckCodeIndexNeverFail(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name:  "absent",
			setup: func(t *testing.T, root string) {},
		},
		{
			name: "stale",
			setup: func(t *testing.T, root string) {
				mtime := time.Now().Add(-30 * 24 * time.Hour)
				makeIndexDB(t, root, mtime)
			},
		},
		{
			name: "fresh",
			setup: func(t *testing.T, root string) {
				mtime := time.Now().Add(-1 * 24 * time.Hour)
				makeIndexDB(t, root, mtime)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			r := doctor.RunCheckCodeIndexWith(root, 7)
			if r.Severity == doctor.FAIL {
				t.Errorf("severity = FAIL, want PASS or WARN (code index check must never FAIL)")
			}
		})
	}
}

// TestCheckCodeIndexStaleDaysRespected verifies the threshold is honoured.
// A 3-day-old DB is PASS at threshold=7 but WARN at threshold=2.
func TestCheckCodeIndexStaleDaysRespected(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r7 := doctor.RunCheckCodeIndexWith(root, 7)
	if r7.Severity != doctor.PASS {
		t.Errorf("threshold=7: severity = %v, want PASS", r7.Severity)
	}

	r2 := doctor.RunCheckCodeIndexWith(root, 2)
	if r2.Severity != doctor.WARN {
		t.Errorf("threshold=2: severity = %v, want WARN", r2.Severity)
	}
}

// ---- realm-aware tests ----

// makeRealmLayout creates a minimal realm at realmRoot:
//   - <realmRoot>/.atomic/code.toml with the given members
//   - <realmRoot>/wiki/index.md (placeholder; just needs to exist for path derivation)
//
// Returns the path to a CLAUDE.md that registers the wiki index.
func makeRealmLayout(t *testing.T, realmRoot string, members []realmMember) string {
	t.Helper()

	// Write .atomic/code.toml.
	atomicDir := filepath.Join(realmRoot, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("makeRealmLayout mkdir .atomic: %v", err)
	}
	var tomlLines []string
	for _, m := range members {
		tomlLines = append(tomlLines,
			fmt.Sprintf("[[member]]\nkey = %q\npath = %q\nexclude = %v\n",
				m.Key, m.Path, m.Exclude),
		)
	}
	tomlContent := strings.Join(tomlLines, "\n")
	if err := os.WriteFile(filepath.Join(atomicDir, "code.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("makeRealmLayout write code.toml: %v", err)
	}

	// Write wiki/index.md so the realm root can be derived.
	wikiDir := filepath.Join(realmRoot, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("makeRealmLayout mkdir wiki: %v", err)
	}
	wikiIndex := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(wikiIndex, []byte("# wiki\n"), 0o644); err != nil {
		t.Fatalf("makeRealmLayout write wiki/index.md: %v", err)
	}

	// Write a CLAUDE.md that registers the wiki index.
	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	block := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", wikiIndex)
	if err := os.WriteFile(claudeMD, []byte(block), 0o644); err != nil {
		t.Fatalf("makeRealmLayout write CLAUDE.md: %v", err)
	}

	return claudeMD
}

// makeRealmDB creates a member db at <realmRoot>/.atomic/<key>.db with the given mtime.
func makeRealmDB(t *testing.T, realmRoot, key string, mtime time.Time) {
	t.Helper()
	atomicDir := filepath.Join(realmRoot, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("makeRealmDB mkdir: %v", err)
	}
	dbPath := filepath.Join(atomicDir, key+".db")
	if err := os.WriteFile(dbPath, []byte{}, 0o644); err != nil {
		t.Fatalf("makeRealmDB write: %v", err)
	}
	if err := os.Chtimes(dbPath, mtime, mtime); err != nil {
		t.Fatalf("makeRealmDB chtimes: %v", err)
	}
}

type realmMember struct {
	Key     string
	Path    string
	Exclude bool
}

// TestRunCheckCodeIndexRealmWith_AllFresh verifies PASS with "N fresh" detail
// when all member dbs are within the staleness threshold.
func TestRunCheckCodeIndexRealmWith_AllFresh(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "beta", Path: "repos/beta"},
	}
	_ = makeRealmLayout(t, realmRoot, members)

	// Both dbs fresh (1 day old, threshold 7).
	mtime := time.Now().Add(-1 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", mtime)
	makeRealmDB(t, realmRoot, "beta", mtime)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (all fresh); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "2 fresh") {
		t.Errorf("detail = %q, want '2 fresh'", r.Detail)
	}
}

// TestRunCheckCodeIndexRealmWith_StaleMember verifies WARN and names the stale member.
func TestRunCheckCodeIndexRealmWith_StaleMember(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "beta", Path: "repos/beta"},
	}
	_ = makeRealmLayout(t, realmRoot, members)

	fresh := time.Now().Add(-1 * 24 * time.Hour)
	stale := time.Now().Add(-10 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)
	makeRealmDB(t, realmRoot, "beta", stale)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (stale member); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "beta") {
		t.Errorf("detail = %q, want mention of 'beta'", r.Detail)
	}
	// alpha is fresh — should not appear in actionable list.
	if strings.Contains(r.Detail, "stale: alpha") {
		t.Errorf("detail = %q, should not mention 'alpha' as stale", r.Detail)
	}
	if !strings.Contains(r.Detail, "1 fresh") {
		t.Errorf("detail = %q, want '1 fresh'", r.Detail)
	}
}

// TestRunCheckCodeIndexRealmWith_NotIndexedMember verifies WARN and names the absent member.
func TestRunCheckCodeIndexRealmWith_NotIndexedMember(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "baz", Path: "repos/baz"},
	}
	_ = makeRealmLayout(t, realmRoot, members)

	// alpha fresh, baz not indexed (no db file).
	fresh := time.Now().Add(-1 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (not-indexed member); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "baz") {
		t.Errorf("detail = %q, want mention of 'baz'", r.Detail)
	}
	if !strings.Contains(r.Detail, "1 fresh") {
		t.Errorf("detail = %q, want '1 fresh'", r.Detail)
	}
}

// TestRunCheckCodeIndexRealmWith_Mixed verifies the mixed case: fresh + stale + not-indexed.
// Worst severity = WARN; detail counts fresh, names stale + not-indexed.
func TestRunCheckCodeIndexRealmWith_Mixed(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"}, // fresh
		{Key: "beta", Path: "repos/beta"},   // stale
		{Key: "gamma", Path: "repos/gamma"}, // not indexed
		{Key: "delta", Path: "repos/delta"}, // fresh
	}
	_ = makeRealmLayout(t, realmRoot, members)

	fresh := time.Now().Add(-1 * 24 * time.Hour)
	stale := time.Now().Add(-20 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)
	makeRealmDB(t, realmRoot, "beta", stale)
	// gamma: no db (not indexed)
	makeRealmDB(t, realmRoot, "delta", fresh)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "2 fresh") {
		t.Errorf("detail = %q, want '2 fresh'", r.Detail)
	}
	if !strings.Contains(r.Detail, "beta") {
		t.Errorf("detail = %q, want 'beta' (stale)", r.Detail)
	}
	if !strings.Contains(r.Detail, "gamma") {
		t.Errorf("detail = %q, want 'gamma' (not indexed)", r.Detail)
	}
	// alpha and delta are fresh — must not appear as actionable.
	if strings.Contains(r.Detail, "stale: alpha") || strings.Contains(r.Detail, "stale: delta") {
		t.Errorf("detail = %q, fresh members must not appear as stale", r.Detail)
	}
}

// TestRunCheckCodeIndexRealmWith_ExcludedMembersSkipped verifies that excluded members
// are not counted or listed.
func TestRunCheckCodeIndexRealmWith_ExcludedMembersSkipped(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "excluded", Path: "repos/excluded", Exclude: true},
	}
	_ = makeRealmLayout(t, realmRoot, members)

	fresh := time.Now().Add(-1 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)
	// excluded has a stale db — but since it's excluded it must not cause WARN.
	stale := time.Now().Add(-20 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "excluded", stale)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (excluded member ignored); detail: %s", r.Severity, r.Detail)
	}
	if strings.Contains(r.Detail, "excluded") {
		t.Errorf("detail = %q, excluded member must not appear", r.Detail)
	}
}

// TestRunCheckCodeIndexRealmWith_NoMembers verifies sensible PASS when realm config is empty.
func TestRunCheckCodeIndexRealmWith_NoMembers(t *testing.T) {
	realmRoot := t.TempDir()
	_ = makeRealmLayout(t, realmRoot, nil)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (no members); detail: %s", r.Severity, r.Detail)
	}
}

// TestRunCheckCodeIndexRealmWith_NeverFail asserts the realm check never produces FAIL.
func TestRunCheckCodeIndexRealmWith_NeverFail(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
	}
	_ = makeRealmLayout(t, realmRoot, members)
	// alpha not indexed and stale — worst possible case.

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = FAIL, realm check must never FAIL; detail: %s", r.Detail)
	}
}

// TestRunCheckCodeIndexWith_SingleRepoUnchanged_Absent verifies single-repo
// behavior (absent → PASS informational) is unchanged by the realm feature.
func TestRunCheckCodeIndexWith_SingleRepoUnchanged_Absent(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("single-repo absent: severity = %v, want PASS", r.Severity)
	}
}

// TestRunCheckCodeIndexWith_SingleRepoUnchanged_Stale verifies single-repo
// behavior (stale → WARN) is unchanged by the realm feature.
func TestRunCheckCodeIndexWith_SingleRepoUnchanged_Stale(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("single-repo stale: severity = %v, want WARN", r.Severity)
	}
}

// TestRunCheckCodeIndexWith_SingleRepoUnchanged_Fresh verifies single-repo
// behavior (fresh → PASS) is unchanged by the realm feature.
func TestRunCheckCodeIndexWith_SingleRepoUnchanged_Fresh(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-1 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("single-repo fresh: severity = %v, want PASS", r.Severity)
	}
}

// ---- dispatcher tests (Fix 1 + Fix 3) ----
//
// These tests drive RunCheckCodeIndex (the exported wrapper over the private
// checkCodeIndex dispatcher) so that the scope-detection branch — including the
// new ScopeRealmMember → aggregate routing — is exercised end-to-end.
//
// The realm fixture mirrors makeRealmLayout but also needs a member directory so
// t.Chdir can navigate into it.

// makeDispatcherFixture builds a complete realm fixture and returns:
//   - realmRoot  — the realm root directory
//   - memberDir  — path to a member sub-directory (no local .atomic-index)
//   - claudeMD   — a CLAUDE.md whose <wikis> block points at realmRoot/wiki/index.md
//
// Two member dbs are created under <realmRoot>/.atomic/:
//   - "alpha" fresh (1 day old)
//   - "beta"  fresh (1 day old)
func makeDispatcherFixture(t *testing.T) (realmRoot, memberDir, claudeMD string) {
	t.Helper()
	realmRoot = t.TempDir()

	members := []realmMember{
		{Key: "alpha", Path: "members/alpha"},
		{Key: "beta", Path: "members/beta"},
	}
	claudeMD = makeRealmLayout(t, realmRoot, members)

	// Create the member directories (needed for t.Chdir).
	for _, m := range members {
		dir := filepath.Join(realmRoot, m.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("makeDispatcherFixture mkdir member %s: %v", m.Path, err)
		}
	}

	// Both dbs fresh (1 day old, threshold 7).
	fresh := time.Now().Add(-1 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)
	makeRealmDB(t, realmRoot, "beta", fresh)

	memberDir = filepath.Join(realmRoot, "members/alpha")
	return realmRoot, memberDir, claudeMD
}

// TestCheckCodeIndex_RealmAllDispatch verifies that RunCheckCodeIndex routes to
// the realm aggregate (ScopeRealmAll) when cwd == realmRoot.
func TestCheckCodeIndex_RealmAllDispatch(t *testing.T) {
	realmRoot, _, claudeMD := makeDispatcherFixture(t)

	// Chdir to the realm root so Resolve returns ScopeRealmAll.
	t.Chdir(realmRoot)

	opts := doctor.Opts{
		ClaudeMDPath: claudeMD,
		StaleDays:    7,
	}
	r := doctor.RunCheckCodeIndex(opts)

	// Both members are fresh → PASS with "2 fresh".
	if r.Severity != doctor.PASS {
		t.Errorf("ScopeRealmAll: severity = %v, want PASS (both fresh); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "fresh") {
		t.Errorf("ScopeRealmAll: detail = %q, want aggregate detail containing 'fresh'", r.Detail)
	}
	// The aggregate format never contains "not initialized" (that's single-repo absent).
	if strings.Contains(r.Detail, "not initialized") {
		t.Errorf("ScopeRealmAll: detail = %q, must not contain single-repo 'not initialized' string", r.Detail)
	}
}

// TestCheckCodeIndex_RealmMemberDispatch verifies that RunCheckCodeIndex routes to
// the realm aggregate (Fix 1) when cwd is inside a member directory (ScopeRealmMember).
// Prior to Fix 1 this branch fell through to the single-repo path and falsely
// reported "code index not initialized".
func TestCheckCodeIndex_RealmMemberDispatch(t *testing.T) {
	_, memberDir, claudeMD := makeDispatcherFixture(t)

	// Chdir into a member directory — no local .claude/.atomic-index here, so
	// Resolve must return ScopeRealmMember (not ScopeRepo).
	t.Chdir(memberDir)

	opts := doctor.Opts{
		ClaudeMDPath: claudeMD,
		StaleDays:    7,
	}
	r := doctor.RunCheckCodeIndex(opts)

	// Expect the realm aggregate result (PASS, "fresh"), NOT the single-repo
	// "code index not initialized" PASS that the broken path produced.
	if r.Severity != doctor.PASS {
		t.Errorf("ScopeRealmMember: severity = %v, want PASS (realm aggregate); detail: %s", r.Severity, r.Detail)
	}
	if strings.Contains(r.Detail, "not initialized") {
		t.Errorf("ScopeRealmMember: detail = %q, Fix 1 regression — still routing to single-repo path", r.Detail)
	}
	if !strings.Contains(r.Detail, "fresh") {
		t.Errorf("ScopeRealmMember: detail = %q, want realm aggregate detail containing 'fresh'", r.Detail)
	}
}
