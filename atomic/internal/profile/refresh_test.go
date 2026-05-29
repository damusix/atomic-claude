package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ParseDuration ---

func TestParseDuration_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"7d", 7},
		{"30d", 30},
		{"1d", 1},
		{"365d", 365},
	}
	for _, c := range cases {
		got, err := ParseDuration(c.in)
		if err != nil {
			t.Errorf("ParseDuration(%q) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseDuration(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	cases := []string{"7h", "1w", "abc", "", "7", "d", "7D", "0d", "-1d"}
	for _, in := range cases {
		_, err := ParseDuration(in)
		if err == nil {
			t.Errorf("ParseDuration(%q) expected error, got nil", in)
		}
	}
}

// --- ParseLastcheck ---

func TestParseLastcheck_Present(t *testing.T) {
	content := "## Environment\n<deterministic lastcheck=2026-05-21>\n- OS: darwin\n</deterministic>\n"
	got, ok := ParseLastcheck(content)
	if !ok {
		t.Fatal("ParseLastcheck: expected ok=true")
	}
	if got != "2026-05-21" {
		t.Errorf("ParseLastcheck = %q, want %q", got, "2026-05-21")
	}
}

func TestParseLastcheck_Absent(t *testing.T) {
	// v1-format file: <deterministic> without attribute
	content := "## Environment\n<deterministic>\n- OS: darwin\n</deterministic>\n"
	_, ok := ParseLastcheck(content)
	if ok {
		t.Error("ParseLastcheck: expected ok=false for v1-format content")
	}
}

func TestParseLastcheck_EmptyContent(t *testing.T) {
	_, ok := ParseLastcheck("")
	if ok {
		t.Error("ParseLastcheck: expected ok=false for empty content")
	}
}

// --- IsStale ---

func TestIsStale_FreshWithinWindow(t *testing.T) {
	// lastcheck 3 days ago, window 7 days → NOT stale
	if IsStale("2026-05-25", "2026-05-28", 7) {
		t.Error("IsStale: expected false when within 7d window")
	}
}

func TestIsStale_ExactlyAtBoundary(t *testing.T) {
	// lastcheck exactly 7 days ago → NOT stale (< window means fresh; = window means stale)
	if !IsStale("2026-05-21", "2026-05-28", 7) {
		t.Error("IsStale: expected true when exactly at 7d boundary")
	}
}

func TestIsStale_OlderThanWindow(t *testing.T) {
	// lastcheck 14 days ago, window 7 days → stale
	if !IsStale("2026-05-14", "2026-05-28", 7) {
		t.Error("IsStale: expected true when older than window")
	}
}

func TestIsStale_MalformedLastcheck(t *testing.T) {
	// Unparseable lastcheck → treat as stale
	if !IsStale("not-a-date", "2026-05-28", 7) {
		t.Error("IsStale: expected true for malformed lastcheck")
	}
}

func TestIsStale_MalformedToday(t *testing.T) {
	// Unparseable today → treat as stale (safe fallback)
	if !IsStale("2026-05-21", "bad-today", 7) {
		t.Error("IsStale: expected true for malformed today")
	}
}

// --- Refresh core ---

func TestRefresh_WritesNewFile(t *testing.T) {
	// File absent → creates stub + Environment section, stamps lastcheck.
	claudeHome := t.TempDir()
	date := "2026-05-28"

	wrote, err := Refresh(claudeHome, date)
	if err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if !wrote {
		t.Error("Refresh: expected wrote=true for new file")
	}

	profilePath := filepath.Join(claudeHome, ".atomic", "profile.md")
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("profile.md not written: %v", err)
	}

	got := string(content)
	if !strings.Contains(got, "## Environment") {
		t.Error("profile.md missing ## Environment section")
	}
	if !strings.Contains(got, "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("profile.md missing expected lastcheck stamp; got:\n%s", got)
	}
	if !strings.Contains(got, "## Identity") {
		t.Error("profile.md missing ## Identity stub section")
	}
}

func TestRefresh_RewritesExistingEnvironmentSection(t *testing.T) {
	// File exists with v1-format Environment section → section replaced, lastcheck stamped.
	claudeHome := t.TempDir()
	atomicDir := filepath.Join(claudeHome, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	existing := "# User profile\n\n## Identity\n<stable>\n- Name: ...\n</stable>\n\n## Environment\n<deterministic>\n- Git user.name: old\n- OS: linux\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	date := "2026-05-28"
	wrote, err := Refresh(claudeHome, date)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !wrote {
		t.Error("Refresh: expected wrote=true")
	}

	content, _ := os.ReadFile(profilePath)
	got := string(content)

	// Identity section must be preserved
	if !strings.Contains(got, "## Identity") {
		t.Error("Refresh: Identity section was clobbered")
	}
	// New Environment section with lastcheck
	if !strings.Contains(got, "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("Refresh: expected new lastcheck; got:\n%s", got)
	}
	// Old data should be gone
	if strings.Contains(got, "<deterministic>\n") {
		t.Error("Refresh: old v1 <deterministic> tag still present")
	}
}

func TestRefresh_IfStale_NoOpWhenFresh(t *testing.T) {
	// File has lastcheck=today → Refresh with --if-stale 7d should no-op.
	claudeHome := t.TempDir()
	atomicDir := filepath.Join(claudeHome, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	date := "2026-05-28"
	existing := "# User profile\n\n## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	// Get mtime before
	statBefore, _ := os.Stat(profilePath)

	wrote, err := RefreshIfStale(claudeHome, date, 7)
	if err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}
	if wrote {
		t.Error("RefreshIfStale: expected wrote=false when fresh")
	}

	// File must not have changed
	statAfter, _ := os.Stat(profilePath)
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Error("RefreshIfStale: file mtime changed even though it was fresh")
	}
	// Content must not have changed
	content, _ := os.ReadFile(profilePath)
	if string(content) != existing {
		t.Error("RefreshIfStale: file content changed even though it was fresh")
	}
}

func TestRefresh_IfStale_RefreshesWhenStale(t *testing.T) {
	// File has lastcheck=14 days ago, window=7d → should refresh.
	claudeHome := t.TempDir()
	atomicDir := filepath.Join(claudeHome, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	today := "2026-05-28"
	staleDate := "2026-05-14" // 14 days ago
	existing := "# User profile\n\n## Environment\n<deterministic lastcheck=" + staleDate + ">\n- OS: linux\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := RefreshIfStale(claudeHome, today, 7)
	if err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}
	if !wrote {
		t.Error("RefreshIfStale: expected wrote=true when stale")
	}

	content, _ := os.ReadFile(profilePath)
	if !strings.Contains(string(content), "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("RefreshIfStale: expected new lastcheck stamp; got:\n%s", string(content))
	}
}

func TestRefresh_IfStale_NoLastcheck_Refreshes(t *testing.T) {
	// v1-format file (no lastcheck attribute) → treated as infinitely stale → refreshes.
	claudeHome := t.TempDir()
	atomicDir := filepath.Join(claudeHome, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	today := "2026-05-28"
	existing := "# User profile\n\n## Environment\n<deterministic>\n- OS: linux\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := RefreshIfStale(claudeHome, today, 7)
	if err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}
	if !wrote {
		t.Error("RefreshIfStale: expected wrote=true for v1-format file (no lastcheck)")
	}
}
