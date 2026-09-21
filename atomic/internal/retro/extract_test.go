package retro_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/retro"
)

// copyProjectsFixture copies testdata/projects into a fresh temp dir so tests
// can control mtime with os.Chtimes without mutating the checked-in fixture.
func copyProjectsFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", "projects")

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}

func mustExtract(t *testing.T, opts retro.Options) retro.Result {
	t.Helper()
	result, err := retro.Extract(opts)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return result
}

func TestExtract_RowDisposition(t *testing.T) {
	dir := copyProjectsFixture(t)
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	sess := result.Sessions[0]

	if sess.ProjectDir != "/Users/me/projects/foo" {
		t.Errorf("ProjectDir = %q, want cwd of first row", sess.ProjectDir)
	}

	wantFirst := time.Date(2026, 9, 6, 20, 44, 40, 0, time.UTC)
	wantLast := time.Date(2026, 9, 6, 20, 44, 52, 0, time.UTC)
	if !sess.First.Equal(wantFirst) {
		t.Errorf("First = %v, want %v (span over every user/assistant row)", sess.First, wantFirst)
	}
	if !sess.Last.Equal(wantLast) {
		t.Errorf("Last = %v, want %v (span over every user/assistant row)", sess.Last, wantLast)
	}

	wantKinds := []retro.Kind{
		retro.KindUser,    // system-reminder stripped, kept
		retro.KindCommand, // <command-name>
		retro.KindCommand, // <command-message> + <command-args>
		retro.KindUser,    // text-block array
		retro.KindSkill,
		retro.KindAgent,
	}
	if len(sess.Entries) != len(wantKinds) {
		t.Fatalf("got %d entries, want %d: %+v", len(sess.Entries), len(wantKinds), sess.Entries)
	}
	for i, e := range sess.Entries {
		if e.Kind != wantKinds[i] {
			t.Errorf("entry %d kind = %v, want %v (text %q)", i, e.Kind, wantKinds[i], e.Text)
		}
	}

	if sess.Entries[0].Text != "make the test pass without touching the fixture" {
		t.Errorf("system-reminder not stripped: %q", sess.Entries[0].Text)
	}
	if sess.Entries[1].Text != "/model" {
		t.Errorf("command-name line = %q, want /model", sess.Entries[1].Text)
	}
	if sess.Entries[2].Text != "/commit push" {
		t.Errorf("command-message line = %q, want /commit push", sess.Entries[2].Text)
	}
	if sess.Entries[3].Text != "no. I said without touching the fixture." {
		t.Errorf("text-block entry = %q", sess.Entries[3].Text)
	}
	if sess.Entries[4].Text != "atomic-tdd retro-extract" {
		t.Errorf("skill entry = %q, want %q", sess.Entries[4].Text, "atomic-tdd retro-extract")
	}
	if sess.Entries[5].Text != "atomic-implementer — Fix fixture-independent test" {
		t.Errorf("agent entry = %q", sess.Entries[5].Text)
	}
}

func TestExtract_MachineTagsDropped(t *testing.T) {
	tags := []string{
		"<local-command-stdout>",
		"<local-command-caveat>",
		"<task-notification>",
		"<bash-input>",
		"<bash-stdout>",
		"<bash-stderr>",
		"<agent-message from=\"peer\">",
		"<cross-session-message room=\"x\">",
	}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			dir := t.TempDir()
			projDir := filepath.Join(dir, "-Users-me-projects-tags")
			if err := os.MkdirAll(projDir, 0o755); err != nil {
				t.Fatal(err)
			}
			row := `{"type":"user","timestamp":"2026-09-06T21:00:00Z","message":{"content":"` + tag + `body</x>"}}` + "\n"
			path := filepath.Join(projDir, "session.jsonl")
			if err := os.WriteFile(path, []byte(row), 0o644); err != nil {
				t.Fatal(err)
			}
			setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

			result := mustExtract(t, retro.Options{
				ProjectsDir: dir,
				Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			})
			if len(result.Sessions) != 0 {
				t.Errorf("tag %q: expected row dropped, got session with %d entries", tag, len(result.Sessions[0].Entries))
			}
		})
	}
}

func TestExtract_SubagentsDirIgnored(t *testing.T) {
	dir := t.TempDir()
	slug := "-Users-me-projects-foo"
	nested := filepath.Join(dir, slug, "d730aa67-uuid", "subagents")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	row := `{"type":"user","timestamp":"2026-09-06T21:00:00Z","message":{"content":"typed by the orchestrator prompt, not the user"}}` + "\n"
	if err := os.WriteFile(filepath.Join(nested, "x.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if len(result.Sessions) != 0 || result.Stats.FilesScanned != 0 {
		t.Errorf("subagents/ file should be skipped as a non-direct child, got sessions=%d filesScanned=%d",
			len(result.Sessions), result.Stats.FilesScanned)
	}
}

func TestExtract_MtimeBoundary(t *testing.T) {
	dir := copyProjectsFixture(t)
	since := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	setAllMtimes(t, dir, since.Add(-time.Hour))
	result := mustExtract(t, retro.Options{ProjectsDir: dir, Since: since})
	if len(result.Sessions) != 0 {
		t.Errorf("file older than since boundary should be excluded, got %d sessions", len(result.Sessions))
	}

	setAllMtimes(t, dir, since)
	result = mustExtract(t, retro.Options{ProjectsDir: dir, Since: since})
	if len(result.Sessions) != 1 {
		t.Errorf("file at since boundary should be included, got %d sessions", len(result.Sessions))
	}
}

func TestExtract_ProjectGlob(t *testing.T) {
	dir := t.TempDir()
	for _, slug := range []string{"-Users-me-projects-foo", "-Users-me-projects-bar"} {
		projDir := filepath.Join(dir, slug)
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			t.Fatal(err)
		}
		row := `{"type":"user","timestamp":"2026-09-06T21:00:00Z","message":{"content":"hello from ` + slug + `"}}` + "\n"
		if err := os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(row), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		ProjectGlob: "*foo*",
	})
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session matching glob, got %d", len(result.Sessions))
	}
	if !strings.Contains(result.Sessions[0].Entries[0].Text, "-Users-me-projects-foo") {
		t.Errorf("glob kept the wrong project: %q", result.Sessions[0].Entries[0].Text)
	}
	if result.Stats.ProjectsScanned != 1 {
		t.Errorf("ProjectsScanned = %d, want 1 (glob should skip bar before reading it)", result.Stats.ProjectsScanned)
	}
}

func TestExtract_LargeRowParsesAndMalformedRowCounted(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-Users-me-projects-big")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	largeText := strings.Repeat("x", 100*1024) // exceeds bufio.Scanner's 64 KiB default token limit
	lines := []string{
		`{"type":"user","timestamp":"2026-09-06T21:00:00Z","message":{"content":[{"type":"tool_result","content":"` + largeText + `"}]}}`,
		`{not valid json`,
		`{"type":"user","timestamp":"2026-09-06T21:00:01Z","message":{"content":"still parsing after the malformed row"}}`,
	}
	path := filepath.Join(projDir, "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	if result.Stats.UnparsableRows != 1 {
		t.Errorf("UnparsableRows = %d, want 1", result.Stats.UnparsableRows)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("expected the file to parse despite the >64 KiB row and the malformed row, got %d sessions", len(result.Sessions))
	}
	if len(result.Sessions[0].Entries) != 1 {
		t.Fatalf("expected 1 kept entry (the tool_result row drops), got %d", len(result.Sessions[0].Entries))
	}
	if result.Sessions[0].Entries[0].Text != "still parsing after the malformed row" {
		t.Errorf("unexpected surviving entry: %q", result.Sessions[0].Entries[0].Text)
	}
}

func TestExtract_ScannerErrorCounted(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-Users-me-projects-huge")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	huge := strings.Repeat("x", 17*1024*1024) // exceeds the 16 MiB scanner ceiling
	lines := []string{
		`{"type":"user","timestamp":"2026-09-06T21:00:00Z","message":{"content":"kept before the oversized row"}}`,
		`{"type":"user","timestamp":"2026-09-06T21:00:01Z","message":{"content":[{"type":"tool_result","content":"` + huge + `"}]}}`,
	}
	path := filepath.Join(projDir, "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	if result.Stats.UnparsableRows != 1 {
		t.Errorf("UnparsableRows = %d, want 1 (scanner.Err() past the ceiling)", result.Stats.UnparsableRows)
	}
	if len(result.Sessions) != 1 || len(result.Sessions[0].Entries) != 1 {
		t.Fatalf("expected the row before the oversized one to survive, got %+v", result.Sessions)
	}
}

func TestExtract_UnreadableProjectDirCounted(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-Users-me-projects-locked")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(projDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(projDir, 0o755) })

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if result.Stats.ReadErrors != 1 {
		t.Errorf("ReadErrors = %d, want 1 (unreadable project dir)", result.Stats.ReadErrors)
	}
}

func TestExtract_MissingTimestampDropped(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-Users-me-projects-notime")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	row := `{"type":"user","message":{"content":"typed text with no timestamp"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if len(result.Sessions) != 0 {
		t.Errorf("expected no session (entry with unparsable timestamp cannot be placed in time), got %d", len(result.Sessions))
	}
	if result.Stats.UnparsableRows != 1 {
		t.Errorf("UnparsableRows = %d, want 1", result.Stats.UnparsableRows)
	}
}

func TestExtract_ProjectDirFromFirstRowCarryingCWD(t *testing.T) {
	dir := t.TempDir()
	slug := "-Users-me-projects-nocwdfirst"
	projDir := filepath.Join(dir, slug)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"user","isMeta":true,"timestamp":"2026-09-06T21:00:00Z","message":{"content":"bookkeeping row with no cwd"}}`,
		`{"type":"user","timestamp":"2026-09-06T21:00:01Z","cwd":"/Users/me/projects/real","message":{"content":"first real row"}}`,
	}
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	if got := result.Sessions[0].ProjectDir; got != "/Users/me/projects/real" {
		t.Errorf("ProjectDir = %q, want cwd of first row that carried one, not the slug", got)
	}
}

func TestExtract_ProjectDirFallsBackToSlugWhenNoRowCarriesCWD(t *testing.T) {
	dir := t.TempDir()
	slug := "-Users-me-projects-nocwdever"
	projDir := filepath.Join(dir, slug)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	row := `{"type":"user","timestamp":"2026-09-06T21:00:00Z","message":{"content":"no cwd anywhere in this session"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	setAllMtimes(t, dir, time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC))

	result := mustExtract(t, retro.Options{
		ProjectsDir: dir,
		Since:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	if got := result.Sessions[0].ProjectDir; got != slug {
		t.Errorf("ProjectDir = %q, want slug fallback %q", got, slug)
	}
}

func setAllMtimes(t *testing.T, dir string, mtime time.Time) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, mtime, mtime)
	})
	if err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}
