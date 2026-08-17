package cliusage_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
)

func TestCommandsNotEmpty(t *testing.T) {
	cmds := cliusage.Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands() returned empty slice")
	}
	for i, c := range cmds {
		if len(c.Path) == 0 {
			t.Errorf("command[%d] has empty Path", i)
		}
		if c.Description == "" {
			t.Errorf("command[%d] %v has empty Description", i, c.Path)
		}
	}
}

func TestLookupByPath(t *testing.T) {
	got := cliusage.LookupByPath([]string{"code", "search"})
	if got == nil {
		t.Fatal("LookupByPath([code search]) returned nil")
	}
	if !containsFlag(got.Flags, "--json") {
		t.Errorf("code search: expected --json flag, got %v", got.Flags)
	}

	got2 := cliusage.LookupByPath([]string{"doctor"})
	if got2 == nil {
		t.Fatal("LookupByPath([doctor]) returned nil")
	}
	if !containsFlag(got2.Flags, "--fix") {
		t.Errorf("doctor: expected --fix flag, got %v", got2.Flags)
	}

	if cliusage.LookupByPath([]string{"nonexistent", "verb"}) != nil {
		t.Error("LookupByPath([nonexistent verb]) should return nil")
	}
}

func TestTopLevelVerbs(t *testing.T) {
	verbs := cliusage.TopLevelVerbs()
	required := []string{"code", "signals", "validate", "wiki", "followups", "claude", "config", "docs", "doctor", "update", "profile", "hooks", "reminder", "docker", "prompt"}
	for _, v := range required {
		if !verbs[v] {
			t.Errorf("TopLevelVerbs missing %q", v)
		}
	}
}

// The fan-out code verbs carry --only/--exclude; --db is rejected everywhere.
func TestCodeFanOutVerbs_HaveOnlyExclude(t *testing.T) {
	fanOutVerbs := [][]string{
		{"code", "index"},
		{"code", "search"},
		{"code", "callers"},
		{"code", "callees"},
		{"code", "impact"},
		{"code", "explore"},
	}

	for _, path := range fanOutVerbs {
		cmd := cliusage.LookupByPath(path)
		if cmd == nil {
			t.Errorf("LookupByPath(%v) returned nil", path)
			continue
		}
		if !containsFlag(cmd.Flags, "--only") {
			t.Errorf("%v: missing --only flag; got %v", path, cmd.Flags)
		}
		if !containsFlag(cmd.Flags, "--exclude") {
			t.Errorf("%v: missing --exclude flag; got %v", path, cmd.Flags)
		}
		if containsFlag(cmd.Flags, "--db") {
			t.Errorf("%v: must NOT have --db flag (rejected in spec); got %v", path, cmd.Flags)
		}
	}
}

// The counterpart: verbs that never fan out across realm members must not
// advertise --only/--exclude.
func TestCodeNonFanOutVerbs_NoOnlyExclude(t *testing.T) {
	nonFanOutVerbs := [][]string{
		{"code", "sync"},
		{"code", "status"},
		{"code", "node"},
		{"code", "files"},
		{"code", "affected"},
	}

	for _, path := range nonFanOutVerbs {
		cmd := cliusage.LookupByPath(path)
		if cmd == nil {
			t.Errorf("LookupByPath(%v) returned nil", path)
			continue
		}
		if containsFlag(cmd.Flags, "--only") {
			t.Errorf("%v: should NOT have --only (non-fan-out verb); got %v", path, cmd.Flags)
		}
		if containsFlag(cmd.Flags, "--exclude") {
			t.Errorf("%v: should NOT have --exclude (non-fan-out verb); got %v", path, cmd.Flags)
		}
	}
}

// The internal background-invocation marker must never reach the public
// surface, only --force alongside the pre-existing flags.
func TestUpdate_HasForceFlag(t *testing.T) {
	cmd := cliusage.LookupByPath([]string{"update"})
	if cmd == nil {
		t.Fatal("LookupByPath([update]) returned nil")
	}
	if !containsFlag(cmd.Flags, "--force") {
		t.Errorf("update: expected --force flag, got %v", cmd.Flags)
	}
	for _, existing := range []string{"--check", "--channel", "--no-doctor", "--skip-claude-update"} {
		if !containsFlag(cmd.Flags, existing) {
			t.Errorf("update: expected pre-existing flag %q preserved, got %v", existing, cmd.Flags)
		}
	}
	if containsFlag(cmd.Flags, "--__background-check") {
		t.Error("update: --__background-check is an internal marker, must not appear in cliusage")
	}
}

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}
