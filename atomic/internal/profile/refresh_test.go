package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/workflow"
)

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
	// v1 format: <deterministic> with no attribute.
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

func TestIsStale_FreshWithinWindow(t *testing.T) {
	if IsStale("2026-05-25", "2026-05-28", 7) {
		t.Error("IsStale: expected false when within 7d window")
	}
}

func TestIsStale_ExactlyAtBoundary(t *testing.T) {
	// The window boundary itself counts as stale.
	if !IsStale("2026-05-21", "2026-05-28", 7) {
		t.Error("IsStale: expected true when exactly at 7d boundary")
	}
}

func TestIsStale_OlderThanWindow(t *testing.T) {
	if !IsStale("2026-05-14", "2026-05-28", 7) {
		t.Error("IsStale: expected true when older than window")
	}
}

func TestIsStale_MalformedLastcheck(t *testing.T) {
	if !IsStale("not-a-date", "2026-05-28", 7) {
		t.Error("IsStale: expected true for malformed lastcheck")
	}
}

func TestIsStale_MalformedToday(t *testing.T) {
	if !IsStale("2026-05-21", "bad-today", 7) {
		t.Error("IsStale: expected true for malformed today")
	}
}

func TestRefresh_WritesNewFile(t *testing.T) {
	home := t.TempDir()
	date := "2026-05-28"

	wrote, err := refresh(home, date, quiet(home))
	if err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if !wrote {
		t.Error("Refresh: expected wrote=true for new file")
	}

	profilePath := filepath.Join(home, ".atomic", "profile.md")
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
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	existing := "# User profile\n\n## Identity\n<stable>\n- Name: ...\n</stable>\n\n## Environment\n<deterministic>\n- Git user.name: old\n- OS: linux\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	date := "2026-05-28"
	wrote, err := refresh(home, date, quiet(home))
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !wrote {
		t.Error("Refresh: expected wrote=true")
	}

	content, _ := os.ReadFile(profilePath)
	got := string(content)

	if !strings.Contains(got, "## Identity") {
		t.Error("Refresh: Identity section was clobbered")
	}
	if !strings.Contains(got, "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("Refresh: expected new lastcheck; got:\n%s", got)
	}
	if strings.Contains(got, "<deterministic>\n") {
		t.Error("Refresh: old v1 <deterministic> tag still present")
	}
}

func TestRefresh_IfStale_NoOpWhenFresh(t *testing.T) {
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	date := "2026-05-28"
	existing := "# User profile\n\n## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	statBefore, _ := os.Stat(profilePath)

	wrote, err := refreshIfStale(home, date, 7, quiet(home))
	if err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}
	if wrote {
		t.Error("RefreshIfStale: expected wrote=false when fresh")
	}

	statAfter, _ := os.Stat(profilePath)
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Error("RefreshIfStale: file mtime changed even though it was fresh")
	}
	content, _ := os.ReadFile(profilePath)
	if string(content) != existing {
		t.Error("RefreshIfStale: file content changed even though it was fresh")
	}
}

func TestRefresh_IfStale_RefreshesWhenStale(t *testing.T) {
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
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

	wrote, err := refreshIfStale(home, today, 7, quiet(home))
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
	// No lastcheck attribute counts as infinitely stale.
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}

	today := "2026-05-28"
	existing := "# User profile\n\n## Environment\n<deterministic>\n- OS: linux\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := refreshIfStale(home, today, 7, quiet(home))
	if err != nil {
		t.Fatalf("RefreshIfStale: %v", err)
	}
	if !wrote {
		t.Error("RefreshIfStale: expected wrote=true for v1-format file (no lastcheck)")
	}
}

// `atomic workflow set` can write the profile while a refresh is probing; the
// refresh must not overwrite that value with the content it read first.
func TestRefresh_KeepsAProfileWriteThatLandsDuringTheProbes(t *testing.T) {
	home := t.TempDir()
	if _, err := refresh(home, "2026-05-01", quiet(home)); err != nil {
		t.Fatal(err)
	}
	path := config.ProfilePath(home)
	probe := probeFor(home)
	var wroteMidway bool
	probe.Getenv = func(string) string {
		if !wroteMidway {
			wroteMidway = true
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			content, err := workflow.SetValue(string(raw), workflow.LevelProfile,
				workflow.KeyIssueTrackerProject, "ACME", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return ""
	}
	opts, _ := quietOpts(probe)
	if _, err := refresh(home, "2026-05-28", opts); err != nil {
		t.Fatal(err)
	}
	got := readProfile(t, path)
	if !strings.Contains(got, "- issue-tracker-project: ACME") {
		t.Errorf("the concurrent set was lost:\n%s", got)
	}
	if !strings.Contains(got, "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("the refresh was lost:\n%s", got)
	}
}

func TestRefresh_WaitsForTheProfileLock(t *testing.T) {
	home := t.TempDir()
	opts := quiet(home)
	unlock, err := LockProfile(home)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() {
		_, err := refresh(home, "2026-05-28", opts)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("refresh finished while the lock was held")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never finished after the lock was released")
	}
}

func TestLockProfile_ASecondHolderWaitsForTheFirst(t *testing.T) {
	home := t.TempDir()
	unlock, err := LockProfile(home)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func())
	go func() {
		second, err := LockProfile(home)
		if err != nil {
			t.Error(err)
			second = func() {}
		}
		acquired <- second
	}()
	select {
	case <-acquired:
		t.Fatal("second holder acquired the lock while the first held it")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case second := <-acquired:
		second()
	case <-time.After(5 * time.Second):
		t.Fatal("second holder never acquired the released lock")
	}
}

func readProfile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
