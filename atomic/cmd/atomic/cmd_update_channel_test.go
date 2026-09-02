package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// parseUpdateFlags mirrors runUpdate's flag surface so the precedence tests
// exercise the same fs.Visit-based "was it typed?" detection.
func parseUpdateFlags(t *testing.T, args []string) (*flag.FlagSet, string, bool) {
	t.Helper()
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.NewFile(0, os.DevNull))
	var channel string
	var pre bool
	fs.StringVar(&channel, "channel", "stable", "")
	fs.BoolVar(&pre, "pre", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs, channel, pre
}

// writeChannelConfig lays down a HOME whose config.toml pins update.channel.
func writeChannelConfig(t *testing.T, channel string) string {
	t.Helper()
	home := t.TempDir()
	cfg := config.Default()
	if channel != "" {
		if err := config.Set(cfg, "update.channel", channel); err != nil {
			t.Fatalf("set update.channel=%q: %v", channel, err)
		}
	}
	path := config.TOMLPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := config.WritePersist(path, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home
}

// --pre is a one-shot override, so a machine pinned to prerelease can still be
// asked for stable, and a stable machine can take one prerelease without its
// config changing underneath it.
func TestResolveUpdateChannelPrecedence(t *testing.T) {
	cases := []struct {
		name          string
		args          []string
		configChannel string
		want          string
	}{
		{"bare invocation defaults to stable", nil, "", selfupdate.ChannelStable},
		{"--pre wins over an unset config", []string{"--pre"}, "", selfupdate.ChannelPrerelease},
		{"config pins the channel", nil, selfupdate.ChannelPrerelease, selfupdate.ChannelPrerelease},
		{"--channel overrides a pinned config", []string{"--channel", "stable"}, selfupdate.ChannelPrerelease, selfupdate.ChannelStable},
		{"--pre agrees with a pinned config", []string{"--pre"}, selfupdate.ChannelPrerelease, selfupdate.ChannelPrerelease},
		{"--channel prerelease still works", []string{"--channel", "prerelease"}, "", selfupdate.ChannelPrerelease},
		{"--pre alongside a matching --channel", []string{"--pre", "--channel", "prerelease"}, "", selfupdate.ChannelPrerelease},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := writeChannelConfig(t, tc.configChannel)
			fs, channel, pre := parseUpdateFlags(t, tc.args)
			got, err := resolveUpdateChannel(home, fs, channel, pre)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("channel = %q, want %q", got, tc.want)
			}
		})
	}
}

// Silently preferring one over the other would install from a channel the user
// did not ask for.
func TestResolveUpdateChannelRejectsContradictoryFlags(t *testing.T) {
	home := writeChannelConfig(t, "")
	fs, channel, pre := parseUpdateFlags(t, []string{"--pre", "--channel", "stable"})
	if _, err := resolveUpdateChannel(home, fs, channel, pre); err == nil {
		t.Fatal("--pre with --channel stable: expected an error, got nil")
	}
}

// A typo like --channel beta must not quietly resolve to stable and update.
func TestResolveUpdateChannelRejectsUnknownChannel(t *testing.T) {
	home := writeChannelConfig(t, "")
	fs, channel, pre := parseUpdateFlags(t, []string{"--channel", "beta"})
	if _, err := resolveUpdateChannel(home, fs, channel, pre); err == nil {
		t.Fatal("--channel beta: expected an error, got nil")
	}
}

// An unreadable or unresolvable home must not block the update; it falls back
// to stable rather than erroring.
func TestResolveUpdateChannelWithoutHomeFallsBackToStable(t *testing.T) {
	fs, channel, pre := parseUpdateFlags(t, nil)
	got, err := resolveUpdateChannel("", fs, channel, pre)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != selfupdate.ChannelStable {
		t.Errorf("channel = %q, want %q", got, selfupdate.ChannelStable)
	}
}
