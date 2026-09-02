package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An unset channel resolves to stable rather than an empty string, so callers
// can use the resolved value directly without repeating the fallback.
func TestResolvedChannelDefaultsToStable(t *testing.T) {
	if got := Resolved(&Config{})["update.channel"]; got != "stable" {
		t.Errorf("Resolved(&Config{}) update.channel = %q, want \"stable\"", got)
	}
	if got := Resolved(Default())["update.channel"]; got != "stable" {
		t.Errorf("Resolved(Default()) update.channel = %q, want \"stable\"", got)
	}
}

func TestChannelSetGetUnsetRoundTrip(t *testing.T) {
	cfg := Default()

	if err := Set(cfg, "update.channel", "prerelease"); err != nil {
		t.Fatalf("Set(prerelease): %v", err)
	}
	got, err := Get(cfg, "update.channel")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "prerelease" {
		t.Errorf("after Set, update.channel = %q, want \"prerelease\"", got)
	}

	if err := Unset(cfg, "update.channel"); err != nil {
		t.Fatalf("Unset: %v", err)
	}
	got, err = Get(cfg, "update.channel")
	if err != nil {
		t.Fatalf("Get after Unset: %v", err)
	}
	if got != "stable" {
		t.Errorf("after Unset, update.channel = %q, want \"stable\"", got)
	}
}

// A rejected write is what stops a typo reaching the update path, where an
// unrecognized channel would silently behave as stable.
func TestChannelSetRejectsUnknownValue(t *testing.T) {
	cfg := Default()
	for _, v := range []string{"beta", "Stable", "PRERELEASE", "pre", ""} {
		if err := Set(cfg, "update.channel", v); err == nil {
			t.Errorf("Set(update.channel, %q): expected an error, got nil", v)
		}
	}
	for _, v := range []string{"stable", "prerelease"} {
		if err := Set(cfg, "update.channel", v); err != nil {
			t.Errorf("Set(update.channel, %q): unexpected error: %v", v, err)
		}
	}
}

// A hand-edited config carrying a bad channel must fail Validate rather than
// load silently.
func TestChannelValidateRejectsUnknownStoredValue(t *testing.T) {
	cfg := Default()
	cfg.Update.Channel = "beta"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate with update.channel=beta: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "update.channel") {
		t.Errorf("error should name the key, got: %v", err)
	}

	cfg.Update.Channel = ""
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate with an unset channel: unexpected error: %v", err)
	}
}

// An unset channel must not leave `channel = ""` on disk, matching how
// repl.idle_timeout is persisted.
func TestChannelPersistOmitsUnsetValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "update.channel", "prerelease"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(path, cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Update.Channel != "prerelease" {
		t.Errorf("round-tripped channel = %q, want \"prerelease\"", loaded.Update.Channel)
	}

	if err := Unset(loaded, "update.channel"); err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if err := WritePersist(path, loaded); err != nil {
		t.Fatalf("WritePersist after Unset: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "channel") {
		t.Errorf("expected no channel key for an unset value, got:\n%s", raw)
	}
}
