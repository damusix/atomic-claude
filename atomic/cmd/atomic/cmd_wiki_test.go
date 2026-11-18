package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// The <wikis> projection follows enrollment, never detection, and never
// creates Claude steering on a home that enrolls no Claude target.
func TestWikiSteeringPaths(t *testing.T) {
	type target struct{ harness, root string }
	tests := []struct {
		name         string
		targets      []target
		legacyClaude bool
		want         func(home string) []string
	}{
		{
			name:    "omp only",
			targets: []target{{"omp", ".omp/agent"}},
			want:    func(h string) []string { return []string{filepath.Join(h, ".omp/agent/AGENTS.md")} },
		},
		{
			name:    "claude at a custom root and omp",
			targets: []target{{"claude", "custom-claude"}, {"omp", ".omp/agent"}},
			want: func(h string) []string {
				return []string{filepath.Join(h, "custom-claude/CLAUDE.md"), filepath.Join(h, ".omp/agent/AGENTS.md")}
			},
		},
		{
			name:    "codex projects into its global AGENTS.md",
			targets: []target{{"codex", "codex-home"}},
			want:    func(h string) []string { return []string{filepath.Join(h, "codex-home/AGENTS.md")} },
		},
		{
			name:         "unadopted claude install keeps its existing file",
			legacyClaude: true,
			want:         func(h string) []string { return []string{filepath.Join(h, ".claude/CLAUDE.md")} },
		},
		{
			name:         "an enrolled home ignores an unenrolled claude file",
			targets:      []target{{"omp", ".omp/agent"}},
			legacyClaude: true,
			want:         func(h string) []string { return []string{filepath.Join(h, ".omp/agent/AGENTS.md")} },
		},
		{
			name: "empty home projects nowhere",
			want: func(string) []string { return nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			ledger := &installstate.Ledger{}
			for _, tg := range tt.targets {
				root := filepath.Join(home, tg.root)
				ledger.UpsertTarget(installstate.TargetRecord{Harness: tg.harness, Instance: root, NativeRoot: root})
			}
			if err := ledger.Save(config.LedgerPath(home)); err != nil {
				t.Fatal(err)
			}
			if tt.legacyClaude {
				legacy := filepath.Join(home, ".claude", "CLAUDE.md")
				if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(legacy, []byte("# mine\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := wikiSteeringPaths(home)
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(home); !reflect.DeepEqual(got, want) {
				t.Errorf("wikiSteeringPaths = %v, want %v", got, want)
			}
		})
	}
}

// An unadopted Claude install under CLAUDE_CONFIG_DIR keeps its <wikis> target.
func TestWikiSteeringPathsFollowsClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-claude")
	t.Setenv(config.ClaudeConfigDirEnv, root)
	steering := filepath.Join(root, "CLAUDE.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(steering, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := wikiSteeringPaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{steering}; !reflect.DeepEqual(got, want) {
		t.Errorf("wikiSteeringPaths = %v, want %v", got, want)
	}
}
