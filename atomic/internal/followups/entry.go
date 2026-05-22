// Package followups manages per-entry follow-up files under
// .claude/project/followups/. Each entry is a frontmatter markdown file.
// This package handles parsing, rendering, and the CLOSED.md ledger.
package followups

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Severity is one of the three severity tiers.
type Severity string

const (
	SeverityRisk     Severity = "risk"
	SeverityNit      Severity = "nit"
	SeverityQuestion Severity = "question"
)

// Status is the entry lifecycle state.
type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

// Entry is a parsed follow-up entry.
type Entry struct {
	// Required fields
	ID       string
	Title    string
	Created  string // YYYY-MM-DD
	Origin   string // free-form, may be multi-line from block scalar
	Severity Severity
	ReviewBy string // YYYY-MM-DD
	Status   Status

	// Optional
	File string // path[:lines]

	// Body is the markdown content after the frontmatter.
	Body string
}

// entryFrontmatter mirrors the YAML shape for strict struct decode.
type entryFrontmatter struct {
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Created  string `yaml:"created"`
	Origin   string `yaml:"origin"`
	Severity string `yaml:"severity"`
	ReviewBy string `yaml:"review_by"`
	Status   string `yaml:"status"`
	File     string `yaml:"file"`
}

// ParseEntry parses a raw frontmatter markdown document into an Entry.
// Returns an error if any required field is missing or invalid.
func ParseEntry(raw string) (Entry, error) {
	const open = "---\n"
	if !strings.HasPrefix(raw, open) {
		return Entry{}, fmt.Errorf("followups: document has no frontmatter")
	}

	rest := raw[len(open):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return Entry{}, fmt.Errorf("followups: missing closing frontmatter delimiter")
	}
	yamlBlock := rest[:idx]
	tail := rest[idx+4:] // skip "\n---"
	if strings.HasPrefix(tail, "\n") {
		tail = tail[1:]
	}
	body := tail

	var fm entryFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return Entry{}, fmt.Errorf("followups: invalid YAML frontmatter: %w", err)
	}

	// Validate required fields.
	if fm.ID == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'id'")
	}
	if fm.Title == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'title'")
	}
	if fm.Created == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'created'")
	}
	if err := validateDate("created", fm.Created); err != nil {
		return Entry{}, err
	}
	if fm.Origin == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'origin'")
	}
	if fm.Severity == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'severity'")
	}
	if fm.ReviewBy == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'review_by'")
	}
	if err := validateDate("review_by", fm.ReviewBy); err != nil {
		return Entry{}, err
	}
	if fm.Status == "" {
		return Entry{}, fmt.Errorf("followups: missing required field 'status'")
	}

	// Validate enum fields.
	sev, err := parseSeverity(fm.Severity)
	if err != nil {
		return Entry{}, err
	}
	st, err := parseStatus(fm.Status)
	if err != nil {
		return Entry{}, err
	}

	// Trim trailing newline from block-scalar origin for clean display.
	origin := strings.TrimRight(fm.Origin, "\n")

	return Entry{
		ID:       fm.ID,
		Title:    fm.Title,
		Created:  fm.Created,
		Origin:   origin,
		Severity: sev,
		ReviewBy: fm.ReviewBy,
		Status:   st,
		File:     fm.File,
		Body:     body,
	}, nil
}

// validateDate checks that v is a valid YYYY-MM-DD date string.
func validateDate(field, v string) error {
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return fmt.Errorf("followups: field %q has invalid date %q: must be YYYY-MM-DD", field, v)
	}
	return nil
}

func parseSeverity(s string) (Severity, error) {
	switch Severity(s) {
	case SeverityRisk, SeverityNit, SeverityQuestion:
		return Severity(s), nil
	default:
		return "", fmt.Errorf("followups: invalid severity %q: must be risk, nit, or question", s)
	}
}

func parseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusOpen, StatusClosed:
		return Status(s), nil
	default:
		return "", fmt.Errorf("followups: invalid status %q: must be open or closed", s)
	}
}

// LoadEntriesWithErrors reads all *.md files (excluding INDEX.md and CLOSED.md)
// from dir, parses each as an Entry, and returns valid entries alongside a map
// of filename → error for files that failed to parse. The top-level error is
// non-nil only if dir cannot be read at all.
func LoadEntriesWithErrors(dir string) ([]Entry, map[string]error, error) {
	fis, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("followups: read dir %q: %w", dir, err)
	}

	var entries []Entry
	errs := map[string]error{}
	for _, fi := range fis {
		if fi.IsDir() {
			continue
		}
		name := fi.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "INDEX.md" || name == "CLOSED.md" {
			continue
		}

		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			errs[name] = readErr
			continue
		}
		e, parseErr := ParseEntry(string(raw))
		if parseErr != nil {
			errs[name] = parseErr
			continue
		}
		entries = append(entries, e)
	}
	return entries, errs, nil
}

// LoadEntries reads all *.md files (excluding INDEX.md and CLOSED.md) from dir,
// parses each as an Entry, and returns only the successfully-parsed results.
// Returns an error if dir does not exist or cannot be read. Files that fail to
// parse are silently skipped; use LoadEntriesWithErrors to surface per-file errors.
func LoadEntries(dir string) ([]Entry, error) {
	entries, _, err := LoadEntriesWithErrors(dir)
	return entries, err
}
