package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureOutput swaps os.Stdout/os.Stderr for the duration of fn and returns
// what each captured; the dispatch functions under test write to the package
// vars directly, so redirecting them is the only way in. Pipes have a finite
// kernel buffer, so both ends are drained concurrently in goroutines started
// before fn runs — a writer that fills the buffer before fn returns would
// otherwise deadlock against an unread pipe.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr

	var outBytes, errBytes []byte
	done := make(chan struct{})
	go func() {
		outBytes, _ = io.ReadAll(rOut)
		close(done)
	}()
	errDone := make(chan struct{})
	go func() {
		errBytes, _ = io.ReadAll(rErr)
		close(errDone)
	}()

	fn()

	wOut.Close()
	wErr.Close()
	<-done
	<-errDone
	rOut.Close()
	rErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	return string(outBytes), string(errBytes)
}

// writeFixtureSession writes <home>/.claude/projects/<slug>/<id>.jsonl with
// one user row, then stamps its mtime so --since filtering is deterministic
// regardless of when the test runs.
func writeFixtureSession(t *testing.T, home, slug, id, text string, mtime time.Time) {
	t.Helper()

	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{
		"type":      "user",
		"timestamp": mtime.UTC().Format(time.RFC3339),
		"cwd":       "/repo/" + slug,
		"message":   map[string]any{"content": text},
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestRetroAction_NoArgsUsageError(t *testing.T) {
	home := t.TempDir()
	code := retroAction([]string{}, home, time.Now())
	if code != 2 {
		t.Errorf("retroAction(no args): got exit code %d, want 2", code)
	}
}

func TestRetroAction_UnknownVerbUsageError(t *testing.T) {
	home := t.TempDir()
	code := retroAction([]string{"bogus"}, home, time.Now())
	if code != 2 {
		t.Errorf("retroAction(bogus): got exit code %d, want 2", code)
	}
}

func TestRetroExtractAction_BadSinceUsageError(t *testing.T) {
	home := t.TempDir()
	code := retroAction([]string{"extract", "--since", "not-a-date"}, home, time.Now())
	if code != 2 {
		t.Errorf("retroAction(extract --since not-a-date): got exit code %d, want 2", code)
	}
}

func TestRetroExtractAction_ShardsZeroUsageError(t *testing.T) {
	home := t.TempDir()
	out := filepath.Join(home, "out.md")
	code := retroAction([]string{"extract", "--shards", "0", "-o", out}, home, time.Now())
	if code != 2 {
		t.Errorf("retroAction(extract --shards 0): got exit code %d, want 2", code)
	}
}

func TestRetroExtractAction_ShardsWithoutOutUsageError(t *testing.T) {
	home := t.TempDir()
	code := retroAction([]string{"extract", "--shards", "2"}, home, time.Now())
	if code != 2 {
		t.Errorf("retroAction(extract --shards 2, no -o): got exit code %d, want 2", code)
	}
}

func TestRetroExtractAction_WritesToOutFile(t *testing.T) {
	home := t.TempDir()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFixtureSession(t, home, "proj-a", "session-1", "hello world", since.AddDate(0, 0, 1))

	out := filepath.Join(home, "out.md")
	now := since.AddDate(0, 1, 0)
	code := retroAction([]string{"extract", "--since", "2026-01-01", "-o", out}, home, now)
	if code != 0 {
		t.Fatalf("retroAction(extract -o): got exit code %d, want 0", code)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("out.md not written: %v", err)
	}
	if !strings.Contains(string(body), "hello world") {
		t.Errorf("out.md missing session text; got:\n%s", string(body))
	}
}

func TestRetroExtractAction_NoOutPrintsStdout(t *testing.T) {
	home := t.TempDir()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFixtureSession(t, home, "proj-a", "session-1", "hello stdout", since.AddDate(0, 0, 1))

	now := since.AddDate(0, 1, 0)
	var code int
	stdout, _ := captureOutput(t, func() {
		code = retroAction([]string{"extract", "--since", "2026-01-01"}, home, now)
	})
	if code != 0 {
		t.Fatalf("retroAction(extract, no -o): got exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "hello stdout") {
		t.Errorf("stdout missing session text; got:\n%s", stdout)
	}
}

func TestRetroExtractAction_ShardsWritesNumberedFiles(t *testing.T) {
	home := t.TempDir()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFixtureSession(t, home, "proj-a", "session-1", "first session text", since.AddDate(0, 0, 1))
	writeFixtureSession(t, home, "proj-b", "session-2", "second session text", since.AddDate(0, 0, 2))

	out := filepath.Join(home, "history.md")
	now := since.AddDate(0, 1, 0)
	code := retroAction([]string{"extract", "--since", "2026-01-01", "--shards", "2", "-o", out}, home, now)
	if code != 0 {
		t.Fatalf("retroAction(extract --shards 2): got exit code %d, want 0", code)
	}

	f1 := filepath.Join(home, "history.1.md")
	f2 := filepath.Join(home, "history.2.md")
	if _, err := os.Stat(f1); err != nil {
		t.Errorf("history.1.md not written: %v", err)
	}
	if _, err := os.Stat(f2); err != nil {
		t.Errorf("history.2.md not written: %v", err)
	}
}

func TestRetroExtractAction_HeaderNamesSinceDateAndCounts(t *testing.T) {
	home := t.TempDir()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFixtureSession(t, home, "proj-a", "session-1", "hello world", since.AddDate(0, 0, 1))

	out := filepath.Join(home, "out.md")
	now := since.AddDate(0, 1, 0)
	code := retroAction([]string{"extract", "--since", "2026-01-01", "-o", out}, home, now)
	if code != 0 {
		t.Fatalf("retroAction(extract -o): got exit code %d, want 0", code)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("out.md not written: %v", err)
	}
	want := "retro extract — since 2026-01-01 — 1 sessions, 1 projects"
	if !strings.Contains(string(body), want) {
		t.Errorf("header missing %q; got:\n%s", want, string(body))
	}
}

func TestRetroExtractAction_ShardHeaderNamesShardIndex(t *testing.T) {
	home := t.TempDir()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFixtureSession(t, home, "proj-a", "session-1", "first session text", since.AddDate(0, 0, 1))
	writeFixtureSession(t, home, "proj-b", "session-2", "second session text", since.AddDate(0, 0, 2))

	out := filepath.Join(home, "history.md")
	now := since.AddDate(0, 1, 0)
	code := retroAction([]string{"extract", "--since", "2026-01-01", "--shards", "2", "-o", out}, home, now)
	if code != 0 {
		t.Fatalf("retroAction(extract --shards 2): got exit code %d, want 0", code)
	}

	body, err := os.ReadFile(filepath.Join(home, "history.1.md"))
	if err != nil {
		t.Fatalf("history.1.md not written: %v", err)
	}
	want := "retro extract — since 2026-01-01 — 2 sessions, 2 projects — shard 1/2"
	if !strings.Contains(string(body), want) {
		t.Errorf("shard header missing %q; got:\n%s", want, string(body))
	}
}

func TestRetroExtractAction_StderrSummaryNames30DayFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	var code int
	_, stderr := captureOutput(t, func() {
		code = retroAction([]string{"extract", "-o", filepath.Join(home, "out.md")}, home, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	})
	if code != 0 {
		t.Fatalf("retroAction(extract, no --since): got exit code %d, want 0", code)
	}
	if !strings.Contains(stderr, "no retro run log, 30-day default") {
		t.Errorf("stderr missing 30-day fallback message; got:\n%s", stderr)
	}
}

func TestRetroExtractAction_StderrSummaryNamesLastRun(t *testing.T) {
	home := t.TempDir()
	runsDir := filepath.Join(home, ".atomic", "retro-runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runFile := filepath.Join(runsDir, "2026-02-01-run.json")
	if err := os.WriteFile(runFile, []byte(`{"run_ts":"2026-02-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	var code int
	_, stderr := captureOutput(t, func() {
		code = retroAction([]string{"extract", "-o", filepath.Join(home, "out.md")}, home, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	})
	if code != 0 {
		t.Fatalf("retroAction(extract, no --since, retro-runs present): got exit code %d, want 0", code)
	}
	if !strings.Contains(stderr, "2026-02-01") || !strings.Contains(stderr, "last retro run") {
		t.Errorf("stderr missing last-run date message; got:\n%s", stderr)
	}
}
