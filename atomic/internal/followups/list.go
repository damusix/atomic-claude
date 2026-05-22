package followups

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ListOpts controls ListEntries behavior.
type ListOpts struct {
	StaleOnly bool
	Today     time.Time
}

// ListEntry extends Entry with computed display fields.
type ListEntry struct {
	Entry
	AgeInDays int
	IsStale   bool
}

// ListEntries loads entries from dir and optionally filters to stale-only.
// Entries are sorted by severity (risk → nit → question), then by id.
func ListEntries(dir string, opts ListOpts) ([]ListEntry, error) {
	today := opts.Today
	if today.IsZero() {
		today = time.Now().UTC()
	}

	entries, err := LoadEntries(dir)
	if err != nil {
		return nil, fmt.Errorf("followups list: %w", err)
	}

	var result []ListEntry
	for _, e := range entries {
		stale := isStale(e, today)
		if opts.StaleOnly && !stale {
			continue
		}
		result = append(result, ListEntry{
			Entry:     e,
			AgeInDays: ageInDays(e.Created, today),
			IsStale:   stale,
		})
	}

	// Sort: severity order risk → nit → question, then id.
	sevOrder := map[Severity]int{
		SeverityRisk:     0,
		SeverityNit:      1,
		SeverityQuestion: 2,
	}
	sort.Slice(result, func(i, j int) bool {
		si, sj := sevOrder[result[i].Severity], sevOrder[result[j].Severity]
		if si != sj {
			return si < sj
		}
		return result[i].ID < result[j].ID
	})

	return result, nil
}

// FormatListHuman returns a human-readable grouped listing of entries.
func FormatListHuman(entries []ListEntry, today time.Time) string {
	var sb strings.Builder

	staleCount := 0
	for _, e := range entries {
		if e.IsStale {
			staleCount++
		}
	}
	sb.WriteString(fmt.Sprintf("Open: %d  •  Stale: %d\n\n", len(entries), staleCount))

	buckets := []struct {
		label string
		sev   Severity
	}{
		{"🟡 risks", SeverityRisk},
		{"🔵 nits", SeverityNit},
		{"❓ questions", SeverityQuestion},
	}

	for _, bucket := range buckets {
		var bEntries []ListEntry
		for _, e := range entries {
			if e.Severity == bucket.sev {
				bEntries = append(bEntries, e)
			}
		}
		sb.WriteString(fmt.Sprintf("## %s (%d)\n\n", bucket.label, len(bEntries)))
		if len(bEntries) == 0 {
			sb.WriteString("(none)\n\n")
			continue
		}
		for _, e := range bEntries {
			staleTag := ""
			if e.IsStale {
				staleTag = " **stale**"
			}
			sb.WriteString(fmt.Sprintf("- %s — %s (%dd%s)\n", e.ID, e.Title, e.AgeInDays, staleTag))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// jsonEntry is the JSON serialization shape for a list entry.
type jsonEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Created   string `json:"created"`
	ReviewBy  string `json:"review_by"`
	AgeInDays int    `json:"age_in_days"`
	Stale     bool   `json:"stale"`
	File      string `json:"file,omitempty"`
	Origin    string `json:"origin,omitempty"`
}

// FormatListJSON returns a JSON array of entries.
func FormatListJSON(entries []ListEntry) (string, error) {
	out := make([]jsonEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, jsonEntry{
			ID:        e.ID,
			Title:     e.Title,
			Severity:  string(e.Severity),
			Created:   e.Created,
			ReviewBy:  e.ReviewBy,
			AgeInDays: e.AgeInDays,
			Stale:     e.IsStale,
			File:      e.File,
			Origin:    e.Origin,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("followups list json: %w", err)
	}
	return string(data), nil
}
