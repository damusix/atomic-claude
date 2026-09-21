package retro

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const truncateLimit = 800

// ShardFile is one size-balanced group of sessions, chronological within
// itself.
type ShardFile struct {
	Index    int // 1-based
	Count    int
	Sessions []Session
}

// Render turns sessions into one numbered markdown document. title is the
// document's first line, without the leading "# " or any shard suffix.
func Render(sessions []Session, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)

	for _, s := range sessions {
		fmt.Fprintf(&b, "## %s · %s\n", s.First.Format("2006-01-02"), s.ProjectDir)
		fmt.Fprintf(&b, "session: %s · first: %s · last: %s · %d entries\n\n",
			s.ID, s.First.Format(time.RFC3339), s.Last.Format(time.RFC3339), len(s.Entries))
		for _, e := range s.Entries {
			b.WriteString(renderEntry(e))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	body := strings.TrimRight(b.String(), "\n") + "\n"
	return number(body)
}

func renderEntry(e Entry) string {
	ts := e.Timestamp.Format("01-02 15:04")
	switch e.Kind {
	case KindCommand:
		return fmt.Sprintf("[%s] %s", ts, e.Text)
	case KindSkill:
		return fmt.Sprintf("[%s] skill: %s", ts, e.Text)
	case KindAgent:
		return fmt.Sprintf("[%s] agent: %s", ts, e.Text)
	default:
		return renderUserEntry(ts, e.Text)
	}
}

func renderUserEntry(ts, text string) string {
	lines := strings.Split(truncate(text, truncateLimit), "\n")
	out := make([]string, len(lines))
	out[0] = fmt.Sprintf("[%s] user: %s", ts, lines[0])
	for i := 1; i < len(lines); i++ {
		out[i] = "    " + lines[i]
	}
	return strings.Join(out, "\n")
}

// truncate cuts text to limit runes and appends a visible marker naming how
// many characters were cut, so a pasted log can't dominate a shard.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return fmt.Sprintf("%s…[+%d chars]", string(runes[:limit]), len(runes)-limit)
}

// number prefixes every physical line with its own line number, right-aligned
// to width 5, so `sed -n '<N>p' <file>` recovers the line marked N.
func number(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%5d | %s\n", i+1, line)
	}
	return b.String()
}

// Shard assigns sessions to K = min(n, len(sessions)) bins, largest session
// first into the lightest bin by rendered size, so no session splits across
// files and the byte size across shards is balanced. Each bin is returned in
// chronological order.
func Shard(sessions []Session, n int) []ShardFile {
	k := n
	if k < 1 {
		k = 1
	}
	if k > len(sessions) {
		k = len(sessions)
	}
	if k == 0 {
		return nil
	}

	sorted := append([]Session(nil), sessions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Size > sorted[j].Size })

	bins := make([][]Session, k)
	sizes := make([]int64, k)
	for _, s := range sorted {
		lightest := 0
		for i := 1; i < k; i++ {
			if sizes[i] < sizes[lightest] {
				lightest = i
			}
		}
		bins[lightest] = append(bins[lightest], s)
		sizes[lightest] += s.Size
	}

	result := make([]ShardFile, k)
	for i, bin := range bins {
		sort.Slice(bin, func(a, b int) bool { return bin[a].First.Before(bin[b].First) })
		result[i] = ShardFile{Index: i + 1, Count: k, Sessions: bin}
	}
	return result
}
