package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// makeOldSignalsLayout writes:
//
//	.claude/project/signals.md     (router with an @-ref line)
//	.claude/project/signals/dom.md (domain file)
//	CLAUDE.md                      (contains @.claude/project/signals.md)
func makeOldSignalsLayout(t *testing.T, root string) {
	t.Helper()
	mkfile := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mkfile(".claude/project/signals.md", "# signals router\n")
	mkfile(".claude/project/signals/dom.md", "# dom\ndom content\n")
	mkfile("CLAUDE.md", "@.claude/project/signals.md\n")
}

func TestMigrateSchemaToSemver(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, ""},
		{1, "1.0.0"},
		{2, "2.0.0"},
	}
	for _, tc := range cases {
		if got := schemaToSemver(tc.n); got != tc.want {
			t.Errorf("schemaToSemver(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestMigrateSemverToSchema(t *testing.T) {
	cases := []struct {
		v    string
		want int
	}{
		{"", 0},
		{"0.0.0", 0},
		{"1.0.0", 1},
		{"2.3.4", 2},
	}
	for _, tc := range cases {
		if got := semverToSchema(tc.v); got != tc.want {
			t.Errorf("semverToSchema(%q) = %d, want %d", tc.v, got, tc.want)
		}
	}
}

func TestScopedMigrations(t *testing.T) {
	reg := []migrate.Migration{
		{TargetVersion: "1.0.0", Scope: "install"},
		{TargetVersion: "2.0.0", Scope: "repo"},
		{TargetVersion: "3.0.0", Scope: "install"},
	}
	install := scopedMigrations("install", reg)
	if len(install) != 2 {
		t.Errorf("install scope: got %d, want 2", len(install))
	}
	repo := scopedMigrations("repo", reg)
	if len(repo) != 1 {
		t.Errorf("repo scope: got %d, want 1", len(repo))
	}
	none := scopedMigrations("other", reg)
	if len(none) != 0 {
		t.Errorf("unknown scope: got %d, want 0", len(none))
	}
}

func TestMigrateRepoActionOldLayout(t *testing.T) {
	root := t.TempDir()
	makeOldSignalsLayout(t, root)

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("migrateRepoAction: %v", err)
	}

	indexPath := filepath.Join(root, "docs", "wiki", "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "<wiki-schema>1</wiki-schema>") {
		t.Errorf("index.md missing <wiki-schema>1</wiki-schema>:\n%s", content)
	}

	if got := migrate.ReadWikiSchema(root); got != 1 {
		t.Errorf("ReadWikiSchema after migration: got %d, want 1", got)
	}

	claudeData, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if strings.Contains(string(claudeData), "@.claude/project/signals.md") {
		t.Errorf("CLAUDE.md still has old @-ref:\n%s", claudeData)
	}
	if !strings.Contains(string(claudeData), "@docs/wiki/index.md") {
		t.Errorf("CLAUDE.md missing new @-ref:\n%s", claudeData)
	}
}

// The second call is a no-op: the schema is already at 1.
func TestMigrateRepoActionIdempotent(t *testing.T) {
	root := t.TempDir()
	makeOldSignalsLayout(t, root)

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("first migrateRepoAction: %v", err)
	}

	indexPath := filepath.Join(root, "docs", "wiki", "index.md")
	after1, _ := os.ReadFile(indexPath)

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("second migrateRepoAction: %v", err)
	}
	after2, _ := os.ReadFile(indexPath)
	if string(after1) != string(after2) {
		t.Errorf("index.md was modified on idempotent re-run")
	}
}

func TestMigrateRepoActionNoSignals(t *testing.T) {
	root := t.TempDir()

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("migrateRepoAction on empty repo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "index.md")); err == nil {
		t.Error("docs/wiki/index.md should not exist for a no-signals repo")
	}
}

func withRealmConfirmStub(result bool, err error, f func()) {
	orig := realmConfirmFn
	realmConfirmFn = func(_, _ string, _ bool) (bool, error) { return result, err }
	defer func() { realmConfirmFn = orig }()
	f()
}

func makeRealmWithMember(t *testing.T, setup func(memberRoot string)) (realmRoot, memberPath string) {
	t.Helper()
	realm := t.TempDir()
	member := filepath.Join(realm, "member-repo")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatalf("mkdir member: %v", err)
	}
	if setup != nil {
		setup(member)
	}
	return realm, member
}

// A step that captured home instead of <home>/.claude would silently corrupt
// install-scope migrations touching the Claude artifact tree.
func TestRunMigrateInstall_TwoRootSplit(t *testing.T) {
	home := t.TempDir()

	// Gives migrate.Run a "0.0.0" floor to migrate up from.
	cfgPath := config.TOMLPath(home)
	if err := config.WritePersist(cfgPath, config.Default()); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	origRegistry := migrate.Registry
	defer func() { migrate.Registry = origRegistry }()

	var capturedRoot string
	migrate.Registry = append(append([]migrate.Migration{}, origRegistry...), migrate.Migration{
		TargetVersion: "99.0.0",
		Scope:         "install",
		Up: func(ctx *migrate.Context) error {
			capturedRoot = ctx.Root
			return nil
		},
	})

	if err := runMigrateInstall(home); err != nil {
		t.Fatalf("runMigrateInstall: %v", err)
	}

	wantClaudeHome := filepath.Join(home, ".claude")
	if capturedRoot != wantClaudeHome {
		t.Errorf("migrate.Context.Root = %q, want %q", capturedRoot, wantClaudeHome)
	}

	// Must be <home>/.atomic/config.toml, not <home>/.claude/.atomic/config.toml.
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if cfg.Install.Version != "99.0.0" {
		t.Errorf("Install.Version = %q, want %q (config.toml under <home>/.atomic was not updated)", cfg.Install.Version, "99.0.0")
	}

	legacyCfgPath := config.TOMLPath(wantClaudeHome)
	if _, err := os.Stat(legacyCfgPath); !os.IsNotExist(err) {
		t.Errorf("expected no config.toml under <home>/.claude/.atomic, stat err = %v", err)
	}
}

// A non-TTY context must never auto-migrate.
func TestRunMigrateRealmNonInteractiveSkipsAll(t *testing.T) {
	realm, member := makeRealmWithMember(t, func(root string) {
		makeOldSignalsLayout(t, root)
	})

	withRealmConfirmStub(false, prompt.ErrNonInteractive, func() {
		if err := runMigrateRealm(realm); err != nil {
			t.Fatalf("runMigrateRealm: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(member, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("old signals.md should still exist (migration must have been skipped): %v", err)
	}
	if _, err := os.Stat(filepath.Join(member, "docs", "wiki", "index.md")); err == nil {
		t.Error("docs/wiki/index.md must not exist (migration must have been skipped)")
	}
}

// A member already at schema >= 1 is skipped without prompting.
func TestRunMigrateRealmSkipsAlreadyMigratedMember(t *testing.T) {
	realm, member := makeRealmWithMember(t, func(root string) {
		// <wiki-schema>1 marks this member fully migrated.
		p := filepath.Join(root, "docs", "wiki", "index.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir docs/wiki: %v", err)
		}
		content := "<wiki-type>repo</wiki-type>\n<wiki-schema>1</wiki-schema>\n# index\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write index.md: %v", err)
		}
	})
	_ = member

	prompted := false
	withRealmConfirmStub(false, nil, func() {
		orig := realmConfirmFn
		realmConfirmFn = func(_, _ string, _ bool) (bool, error) {
			prompted = true
			return false, nil
		}
		defer func() { realmConfirmFn = orig }()

		if err := runMigrateRealm(realm); err != nil {
			t.Fatalf("runMigrateRealm: %v", err)
		}
	})

	if prompted {
		t.Error("already-migrated member must be skipped without prompting")
	}
}

// ErrAborted skips that one member without aborting the realm loop.
func TestRunMigrateRealmAbortedSkipsMemberNotRealm(t *testing.T) {
	realm, member := makeRealmWithMember(t, func(root string) {
		makeOldSignalsLayout(t, root)
	})

	withRealmConfirmStub(false, prompt.ErrAborted, func() {
		if err := runMigrateRealm(realm); err != nil {
			t.Fatalf("runMigrateRealm returned error on ErrAborted: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(member, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("old signals.md should still exist (member was skipped): %v", err)
	}
}
