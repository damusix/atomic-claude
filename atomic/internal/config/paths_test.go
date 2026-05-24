package config

import (
	"path/filepath"
	"testing"
)

func TestPathHelpers(t *testing.T) {
	home := "/home/user/.claude"

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Dir", Dir(home), filepath.Join(home, ".atomic")},
		{"TOMLPath", TOMLPath(home), filepath.Join(home, ".atomic", "config.toml")},
		{"ResolvedPath", ResolvedPath(home), filepath.Join(home, ".atomic", "config.resolved.md")},
		{"BackupDir", BackupDir(home), filepath.Join(home, ".atomic", "backups")},
		{"ProposedCLAUDEMD", ProposedCLAUDEMD(home), filepath.Join(home, ".atomic", "proposed", "CLAUDE.md")},
		{"PreInstallDir", PreInstallDir(home), filepath.Join(home, ".atomic", "pre-install")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
