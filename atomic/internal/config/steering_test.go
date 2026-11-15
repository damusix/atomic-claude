package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

func writeSteeringFixture(t *testing.T, home, root, userToml, repoToml string) {
	t.Helper()
	if userToml != "" {
		if err := os.WriteFile(TOMLPath(home), []byte(userToml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if repoToml != "" {
		path := RepoConfigPath(root)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(repoToml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The repository decides its own layout, so a committed value outranks one
// person's user default; with neither set, today's loader shape stays.
func TestResolveSteeringMode_RepoOverUserOverLoader(t *testing.T) {
	cases := []struct {
		name, user, repo string
		want             bundlespec.SteeringMode
	}{
		{"neither", "", "", bundlespec.SteeringLoader},
		{"user only", "[steering]\nclaude = \"agents-md\"\n", "", bundlespec.SteeringAgentsMD},
		{"repo only", "", "[steering]\nclaude = \"claude-md\"\n", bundlespec.SteeringClaudeMD},
		{"repo beats user", "[steering]\nclaude = \"agents-md\"\n", "[steering]\nclaude = \"loader\"\n", bundlespec.SteeringLoader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			if err := os.MkdirAll(filepath.Dir(TOMLPath(home)), 0o755); err != nil {
				t.Fatal(err)
			}
			writeSteeringFixture(t, home, root, tc.user, tc.repo)
			got, err := ResolveSteeringMode(home, root)
			if err != nil {
				t.Fatalf("ResolveSteeringMode: %v", err)
			}
			if got != tc.want {
				t.Errorf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

// A typo must not silently fall back to loader, or a repository meant to be
// Claude-only would quietly ship the wrong files.
func TestResolveSteeringMode_UnknownValueFailsLoudly(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(TOMLPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}

	writeSteeringFixture(t, home, root, "", "[steering]\nclaude = \"claudemd\"\n")
	if _, err := ResolveSteeringMode(home, root); err == nil || !strings.Contains(err.Error(), `steering.claude "claudemd" is not one of: loader, agents-md, claude-md`) {
		t.Errorf("repo value: err = %v, want the named enum error", err)
	}

	if err := os.Remove(RepoConfigPath(root)); err != nil {
		t.Fatal(err)
	}
	writeSteeringFixture(t, home, root, "[steering]\nclaude = \"nope\"\n", "")
	if _, err := ResolveSteeringMode(home, root); err == nil || !strings.Contains(err.Error(), `steering.claude "nope"`) {
		t.Errorf("user value: err = %v, want the named enum error", err)
	}
}

func TestSteeringClaudeIsASettableUserKey(t *testing.T) {
	cfg := Default()
	if v, err := Get(cfg, "steering.claude"); err != nil || v != "loader" {
		t.Fatalf("Get default = %q, %v; want loader", v, err)
	}
	if err := Set(cfg, "steering.claude", "agents-md"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, _ := Get(cfg, "steering.claude"); v != "agents-md" {
		t.Errorf("Get after Set = %q", v)
	}
	if err := Set(cfg, "steering.claude", "bogus"); err == nil {
		t.Error("Set accepted an unknown mode")
	}
	if err := Unset(cfg, "steering.claude"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(cfg, "steering.claude"); v != "loader" {
		t.Errorf("Get after Unset = %q, want loader", v)
	}
}

// The repo config's steering table is a known key, so it earns no unknown-key warning.
func TestRepoConfigSteeringKeyIsKnown(t *testing.T) {
	root := t.TempDir()
	writeSteeringFixture(t, "", root, "", "[steering]\nclaude = \"loader\"\n")
	_, warns, err := LoadRepoConfig(RepoConfigPath(root))
	if err != nil || len(warns) != 0 {
		t.Errorf("LoadRepoConfig = warns %v, err %v; want none", warns, err)
	}
}
