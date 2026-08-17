// Package followups parses and renders the per-entry frontmatter markdown files
// under the project followups dir, plus the CLOSED.md ledger.
package followups

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Kind string

const (
	KindFinding Kind = "finding"
	KindPlan    Kind = "plan"
)

type Severity string

const (
	SeverityRisk     Severity = "risk"
	SeverityNit      Severity = "nit"
	SeverityQuestion Severity = "question"
)

type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

type Entry struct {
	ID       string
	Title    string
	Created  string // YYYY-MM-DD
	Origin   string // free-form, may be multi-line from block scalar
	Kind     Kind
	Severity Severity
	ReviewBy string // YYYY-MM-DD
	Status   Status

	File string // path[:lines]

	Body string
}

type entryFrontmatter struct {
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Created  string `yaml:"created"`
	Origin   string `yaml:"origin"`
	Kind     string `yaml:"kind"`
	Severity string `yaml:"severity"`
	ReviewBy string `yaml:"review_by"`
	Status   string `yaml:"status"`
	File     string `yaml:"file"`
}

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
	// A missing kind decodes as "finding", for entries predating the field.
	knd, err := parseKind(fm.Kind)
	if err != nil {
		return Entry{}, err
	}

	if fm.Severity == "" && knd != KindPlan {
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

	var sev Severity
	if fm.Severity != "" {
		sev, err = parseSeverity(fm.Severity)
		if err != nil {
			return Entry{}, err
		}
	}
	st, err := parseStatus(fm.Status)
	if err != nil {
		return Entry{}, err
	}

	// A block-scalar origin carries a trailing newline.
	origin := strings.TrimRight(fm.Origin, "\n")

	return Entry{
		ID:       fm.ID,
		Title:    fm.Title,
		Created:  fm.Created,
		Origin:   origin,
		Kind:     knd,
		Severity: sev,
		ReviewBy: fm.ReviewBy,
		Status:   st,
		File:     fm.File,
		Body:     body,
	}, nil
}

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

func parseKind(s string) (Kind, error) {
	if s == "" {
		return KindFinding, nil
	}
	switch Kind(s) {
	case KindFinding, KindPlan:
		return Kind(s), nil
	default:
		return "", fmt.Errorf("followups: invalid kind %q: must be finding or plan", s)
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

// LoadEntriesWithErrors returns the parsed entries plus a filename → error map
// for the ones that failed. The top-level error means dir itself is unreadable.
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

// LoadEntries silently skips unparseable files; LoadEntriesWithErrors surfaces
// them instead.
func LoadEntries(dir string) ([]Entry, error) {
	entries, _, err := LoadEntriesWithErrors(dir)
	return entries, err
}
