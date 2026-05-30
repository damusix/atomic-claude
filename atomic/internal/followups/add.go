package followups

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// kebabCaseRe validates kebab-case ids: lowercase letters, digits, hyphens.
var kebabCaseRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// AddOpts holds parameters for Add.
type AddOpts struct {
	ID       string
	Title    string
	Kind     string // "finding" (default) or "plan"
	Severity string // "risk", "nit", or "question"; optional when Kind=="plan"
	Origin   string
	File     string // optional path[:lines]
	Body     string // optional body content; empty = no body
	Today    time.Time

	// Dir is unused in Add (dir is passed as first arg); kept for test compatibility.
	Dir string
}

// Add creates a new entry file in dir with the given options.
// Returns the absolute path to the created file.
// Exit conditions: returns error on validation failure or id collision.
func Add(dir string, opts AddOpts) (string, error) {
	// Validate id.
	if opts.ID == "" {
		return "", fmt.Errorf("followups add: id must not be empty")
	}
	if !kebabCaseRe.MatchString(opts.ID) {
		return "", fmt.Errorf("followups add: id %q must be kebab-case (lowercase letters, digits, hyphens)", opts.ID)
	}

	// Validate title.
	if strings.TrimSpace(opts.Title) == "" {
		return "", fmt.Errorf("followups add: title must not be empty")
	}

	// Validate and default kind.
	knd, err := parseKind(opts.Kind)
	if err != nil {
		return "", fmt.Errorf("followups add: %w", err)
	}

	// Validate severity: required for findings, optional for plans.
	if opts.Severity != "" {
		if _, err := parseSeverity(opts.Severity); err != nil {
			return "", fmt.Errorf("followups add: %w", err)
		}
	} else if knd != KindPlan {
		return "", fmt.Errorf("followups add: missing required field 'severity'")
	}

	// Validate origin.
	if strings.TrimSpace(opts.Origin) == "" {
		return "", fmt.Errorf("followups add: origin must not be empty")
	}

	// Check for id collision.
	entryPath := filepath.Join(dir, opts.ID+".md")
	if _, err := os.Stat(entryPath); err == nil {
		return "", fmt.Errorf("followups add: id %q already exists at %s", opts.ID, entryPath)
	}

	today := opts.Today
	if today.IsZero() {
		today = time.Now().UTC()
	}

	created := today.Format("2006-01-02")
	reviewBy := today.AddDate(0, 0, 60).Format("2006-01-02")

	// Build frontmatter struct.
	type fm struct {
		ID       string `yaml:"id"`
		Title    string `yaml:"title"`
		Created  string `yaml:"created"`
		Origin   string `yaml:"origin"`
		Kind     string `yaml:"kind"`
		Severity string `yaml:"severity,omitempty"`
		ReviewBy string `yaml:"review_by"`
		Status   string `yaml:"status"`
		File     string `yaml:"file,omitempty"`
	}

	data := fm{
		ID:       opts.ID,
		Title:    opts.Title,
		Created:  created,
		Origin:   opts.Origin + "\n",
		Kind:     string(knd),
		Severity: opts.Severity,
		ReviewBy: reviewBy,
		Status:   string(StatusOpen),
		File:     opts.File,
	}

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("followups add: marshal frontmatter: %w", err)
	}

	body := strings.TrimRight(opts.Body, "\n")
	var bodySection string
	if body != "" {
		bodySection = "\n" + body + "\n"
	} else {
		bodySection = "\n"
	}

	content := "---\n" + string(yamlBytes) + "---\n" + bodySection

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("followups add: mkdir %q: %w", dir, err)
	}

	if err := os.WriteFile(entryPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("followups add: write %q: %w", entryPath, err)
	}

	return entryPath, nil
}
