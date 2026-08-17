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

// makeIndexDB writes a zero-byte file at the canonical db path with the given
// mtime. Not real SQLite — the check only stats it.
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

// The index is opt-in: a repo that never runs `atomic code index` must not see
// a WARN on every doctor run.
func TestCheckCodeIndexAbsent(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS when index absent (must never be WARN)", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

func TestCheckCodeIndexFresh(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-2 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (fresh index, detail: %s)", r.Severity, r.Detail)
	}
}

func TestCheckCodeIndexStale(t *testing.T) {
	root := t.TempDir()
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

// No environment state may turn the opt-in index into a hard requirement.
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

// makeRealmLayout writes a minimal realm at realmRoot and returns the path to
// a CLAUDE.md whose <wikis> block registers its wiki index.
func makeRealmLayout(t *testing.T, realmRoot string, members []realmMember) string {
	t.Helper()

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

	// The realm root is derived from this file's path.
	wikiDir := filepath.Join(realmRoot, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("makeRealmLayout mkdir wiki: %v", err)
	}
	wikiIndex := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(wikiIndex, []byte("# wiki\n"), 0o644); err != nil {
		t.Fatalf("makeRealmLayout write wiki/index.md: %v", err)
	}

	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	block := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", wikiIndex)
	if err := os.WriteFile(claudeMD, []byte(block), 0o644); err != nil {
		t.Fatalf("makeRealmLayout write CLAUDE.md: %v", err)
	}

	return claudeMD
}

// makeRealmDB writes a member db at <realmRoot>/.atomic/<key>.db.
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

func TestRunCheckCodeIndexRealmWith_AllFresh(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "beta", Path: "repos/beta"},
	}
	_ = makeRealmLayout(t, realmRoot, members)

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
	if strings.Contains(r.Detail, "stale: alpha") {
		t.Errorf("detail = %q, should not mention 'alpha' as stale", r.Detail)
	}
	if !strings.Contains(r.Detail, "1 fresh") {
		t.Errorf("detail = %q, want '1 fresh'", r.Detail)
	}
}

func TestRunCheckCodeIndexRealmWith_NotIndexedMember(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "baz", Path: "repos/baz"},
	}
	_ = makeRealmLayout(t, realmRoot, members)

	// baz deliberately gets no db file.
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
	if strings.Contains(r.Detail, "stale: alpha") || strings.Contains(r.Detail, "stale: delta") {
		t.Errorf("detail = %q, fresh members must not appear as stale", r.Detail)
	}
}

func TestRunCheckCodeIndexRealmWith_ExcludedMembersSkipped(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "excluded", Path: "repos/excluded", Exclude: true},
	}
	_ = makeRealmLayout(t, realmRoot, members)

	fresh := time.Now().Add(-1 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)
	// Stale, but excluded — must not raise the severity.
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

func TestRunCheckCodeIndexRealmWith_NoMembers(t *testing.T) {
	realmRoot := t.TempDir()
	_ = makeRealmLayout(t, realmRoot, nil)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (no members); detail: %s", r.Severity, r.Detail)
	}
}

func TestRunCheckCodeIndexRealmWith_NeverFail(t *testing.T) {
	realmRoot := t.TempDir()
	members := []realmMember{
		{Key: "alpha", Path: "repos/alpha"},
	}
	// alpha deliberately gets no db — the worst case.
	_ = makeRealmLayout(t, realmRoot, members)

	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, 7)
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = FAIL, realm check must never FAIL; detail: %s", r.Detail)
	}
}

func TestRunCheckCodeIndexWith_SingleRepoUnchanged_Absent(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("single-repo absent: severity = %v, want PASS", r.Severity)
	}
}

func TestRunCheckCodeIndexWith_SingleRepoUnchanged_Stale(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("single-repo stale: severity = %v, want WARN", r.Severity)
	}
}

func TestRunCheckCodeIndexWith_SingleRepoUnchanged_Fresh(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-1 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("single-repo fresh: severity = %v, want PASS", r.Severity)
	}
}

// ---- dispatcher tests ----
//
// These drive RunCheckCodeIndex so the scope-detection branch is exercised
// end-to-end, not just the two per-scope helpers.

// makeDispatcherFixture builds a realm with two fresh member dbs and member
// directories t.Chdir can enter; no member carries a local .atomic-index.
func makeDispatcherFixture(t *testing.T) (realmRoot, memberDir, claudeMD string) {
	t.Helper()
	realmRoot = t.TempDir()

	members := []realmMember{
		{Key: "alpha", Path: "members/alpha"},
		{Key: "beta", Path: "members/beta"},
	}
	claudeMD = makeRealmLayout(t, realmRoot, members)

	for _, m := range members {
		dir := filepath.Join(realmRoot, m.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("makeDispatcherFixture mkdir member %s: %v", m.Path, err)
		}
	}

	fresh := time.Now().Add(-1 * 24 * time.Hour)
	makeRealmDB(t, realmRoot, "alpha", fresh)
	makeRealmDB(t, realmRoot, "beta", fresh)

	memberDir = filepath.Join(realmRoot, "members/alpha")
	return realmRoot, memberDir, claudeMD
}

func TestCheckCodeIndex_RealmAllDispatch(t *testing.T) {
	realmRoot, _, claudeMD := makeDispatcherFixture(t)

	// cwd == realmRoot makes Resolve return ScopeRealmAll.
	t.Chdir(realmRoot)

	opts := doctor.Opts{
		ClaudeMDPath: claudeMD,
		StaleDays:    7,
	}
	r := doctor.RunCheckCodeIndex(opts)

	if r.Severity != doctor.PASS {
		t.Errorf("ScopeRealmAll: severity = %v, want PASS (both fresh); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "fresh") {
		t.Errorf("ScopeRealmAll: detail = %q, want aggregate detail containing 'fresh'", r.Detail)
	}
	// "not initialized" is the single-repo absent string; the aggregate never emits it.
	if strings.Contains(r.Detail, "not initialized") {
		t.Errorf("ScopeRealmAll: detail = %q, must not contain single-repo 'not initialized' string", r.Detail)
	}
}

// A member dir carries no local index, so a dispatcher that does not route
// ScopeRealmMember to the aggregate falsely reports "not initialized".
func TestCheckCodeIndex_RealmMemberDispatch(t *testing.T) {
	_, memberDir, claudeMD := makeDispatcherFixture(t)

	// No local .claude/.atomic-index here, so Resolve returns ScopeRealmMember.
	t.Chdir(memberDir)

	opts := doctor.Opts{
		ClaudeMDPath: claudeMD,
		StaleDays:    7,
	}
	r := doctor.RunCheckCodeIndex(opts)

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
