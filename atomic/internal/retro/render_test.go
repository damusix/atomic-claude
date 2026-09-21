package retro_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/retro"
)

func sedLine(t *testing.T, doc string, n int) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(doc, "\n"), "\n")
	if n < 1 || n > len(lines) {
		t.Fatalf("line %d out of range (doc has %d lines)", n, len(lines))
	}
	return lines[n-1]
}

func makeSession(id, dir string, first time.Time, entries ...retro.Entry) retro.Session {
	last := first
	for _, e := range entries {
		if e.Timestamp.After(last) {
			last = e.Timestamp
		}
	}
	var size int64
	for _, e := range entries {
		size += int64(len(e.Text))
	}
	return retro.Session{
		ID:         id,
		ProjectDir: dir,
		First:      first,
		Last:       last,
		Entries:    entries,
		Size:       size,
	}
}

func TestRender_NumberingRoundTrips(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	sess := makeSession("session-a", "/Users/me/projects/foo", ts,
		retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: "hello"},
		retro.Entry{Kind: retro.KindSkill, Timestamp: ts, Text: "atomic-tdd retro-extract"},
	)

	doc := retro.Render([]retro.Session{sess}, "retro extract — since 2026-08-19 — 1 sessions, 1 projects")

	for i, line := range strings.Split(strings.TrimRight(doc, "\n"), "\n") {
		n := i + 1
		want := fmt.Sprintf("%5d | ", n)
		if !strings.HasPrefix(line, want) {
			t.Fatalf("line %d prefix = %q, want %q", n, line[:min(len(line), len(want))], want)
		}
	}

	found := false
	for i := range strings.Split(strings.TrimRight(doc, "\n"), "\n") {
		n := i + 1
		if strings.Contains(sedLine(t, doc, n), "skill: atomic-tdd retro-extract") {
			found = true
		}
	}
	if !found {
		t.Errorf("skill entry not found in rendered doc:\n%s", doc)
	}
}

func TestRender_Truncation(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	long := strings.Repeat("a", 850)
	sess := makeSession("session-a", "/Users/me/projects/foo", ts,
		retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: long},
	)

	doc := retro.Render([]retro.Session{sess}, "title")

	if !strings.Contains(doc, "…[+50 chars]") {
		t.Errorf("expected truncation marker for 50 cut chars, doc:\n%s", doc)
	}
	if strings.Contains(doc, strings.Repeat("a", 801)) {
		t.Errorf("text was not cut at the 800-rune boundary")
	}
}

func TestRender_TruncationRuneSafety(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	// multi-byte rune repeated past the limit; a byte-based cut would split it.
	long := strings.Repeat("é", 850)
	sess := makeSession("session-a", "/Users/me/projects/foo", ts,
		retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: long},
	)

	doc := retro.Render([]retro.Session{sess}, "title")
	if !strings.Contains(doc, strings.Repeat("é", 800)+"…[+50 chars]") {
		t.Errorf("rune-unsafe truncation, doc:\n%s", doc)
	}
}

func TestRender_MultilineIndentation(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	sess := makeSession("session-a", "/Users/me/projects/foo", ts,
		retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: "first line\nsecond line\nthird line"},
	)

	doc := retro.Render([]retro.Session{sess}, "title")
	if !strings.Contains(doc, "user: first line") {
		t.Errorf("first line missing user: label, doc:\n%s", doc)
	}
	if !strings.Contains(doc, "|     second line") {
		t.Errorf("continuation line not indented 4 spaces, doc:\n%s", doc)
	}
}

func TestShard_NeverSplitsSessionAndCapsAtSessionCount(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	sessions := []retro.Session{
		makeSession("a", "/foo", ts, retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: strings.Repeat("x", 500)}),
		makeSession("b", "/foo", ts.Add(time.Minute), retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: strings.Repeat("x", 10)}),
	}

	shards := retro.Shard(sessions, 5)
	if len(shards) != 2 {
		t.Fatalf("K should cap at session count 2, got %d shards", len(shards))
	}
	total := 0
	for _, s := range shards {
		total += len(s.Sessions)
		if s.Count != 2 {
			t.Errorf("shard.Count = %d, want 2", s.Count)
		}
	}
	if total != 2 {
		t.Errorf("total sessions across shards = %d, want 2 (no split, no drop)", total)
	}
}

func TestShard_BalancesSize(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	big := makeSession("big", "/foo", ts, retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: strings.Repeat("x", 1000)})
	small1 := makeSession("s1", "/foo", ts.Add(time.Minute), retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: strings.Repeat("x", 10)})
	small2 := makeSession("s2", "/foo", ts.Add(2*time.Minute), retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: strings.Repeat("x", 10)})

	shards := retro.Shard([]retro.Session{big, small1, small2}, 2)
	if len(shards) != 2 {
		t.Fatalf("expected 2 shards, got %d", len(shards))
	}

	var bigShard retro.ShardFile
	for _, s := range shards {
		for _, sess := range s.Sessions {
			if sess.ID == "big" {
				bigShard = s
			}
		}
	}
	if len(bigShard.Sessions) != 1 {
		t.Errorf("the big session's shard should hold only it, balancing the two small ones together; got %d sessions in that shard", len(bigShard.Sessions))
	}
}

func TestShard_ChronologicalWithinShard(t *testing.T) {
	ts := time.Date(2026, 9, 6, 20, 45, 0, 0, time.UTC)
	first := makeSession("first", "/foo", ts, retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: "a"})
	second := makeSession("second", "/foo", ts.Add(time.Hour), retro.Entry{Kind: retro.KindUser, Timestamp: ts, Text: "b"})

	// Pass out of order; Shard must sort each bin by First.
	shards := retro.Shard([]retro.Session{second, first}, 1)
	if len(shards) != 1 || len(shards[0].Sessions) != 2 {
		t.Fatalf("expected 1 shard with 2 sessions, got %+v", shards)
	}
	if shards[0].Sessions[0].ID != "first" || shards[0].Sessions[1].ID != "second" {
		t.Errorf("shard not chronological: %s, %s", shards[0].Sessions[0].ID, shards[0].Sessions[1].ID)
	}
}
