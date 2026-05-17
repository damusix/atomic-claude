// Package reminder manages reminder files under .claude/.scratchpad/reminders/.
// Each reminder is a frontmatter markdown file. The binary tracks no scheduling
// state — a reminder either exists (pending) or it doesn't (done = deleted).
package reminder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/ids"
)

const (
	remindersRelPath = ".claude/.scratchpad/reminders"
	slugMaxLen       = 50
)

// validTransports is the set of accepted transport kinds.
var validTransports = map[string]bool{
	"cron":    true,
	"routine": true,
	"none":    true,
}

// remindersDir returns the absolute path to the reminders directory.
func remindersDir(repoRoot string) string {
	return filepath.Join(repoRoot, remindersRelPath)
}

// Option configures optional fields on a reminder.
type Option func(*addOpts) error

type addOpts struct {
	due       string
	transport string
}

// WithDue sets the due field (RFC3339 / ISO-8601 timestamp).
// Returns an error if the value is not a valid RFC3339 timestamp.
func WithDue(iso string) Option {
	return func(o *addOpts) error {
		if _, err := time.Parse(time.RFC3339, iso); err != nil {
			return fmt.Errorf("reminder: invalid due timestamp %q: must be RFC3339", iso)
		}
		o.due = iso
		return nil
	}
}

// WithTransport sets the transport field. Accepted values: cron, routine, none.
func WithTransport(kind string) Option {
	return func(o *addOpts) error {
		if !validTransports[kind] {
			return fmt.Errorf("reminder: invalid transport %q: must be cron, routine, or none", kind)
		}
		o.transport = kind
		return nil
	}
}

// Add creates a new reminder file and returns the assigned id.
// Returns an error if body is empty/whitespace-only, or if any option is invalid.
func Add(repoRoot, body string, opts ...Option) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("reminder: body must not be empty")
	}

	// Apply options.
	o := &addOpts{}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return "", err
		}
	}

	dir := remindersDir(repoRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("reminder: create reminders dir: %w", err)
	}

	today := time.Now().UTC().Format("2006-01-02")

	// First non-empty line of body for the slug.
	firstLine := firstNonEmpty(body)
	slug := ids.Slug(firstLine)
	if len(slug) > slugMaxLen {
		slug = slug[:slugMaxLen]
		slug = strings.TrimRight(slug, "-")
	}

	// Attempt up to 3 times to find a non-colliding path.
	for attempt := 0; attempt < 3; attempt++ {
		id, err := ids.ShortID("r")
		if err != nil {
			return "", fmt.Errorf("reminder: generate id: %w", err)
		}

		var filename string
		if attempt == 0 {
			filename = today + "-" + slug + ".md"
		} else {
			// Append the id suffix on collision.
			filename = today + "-" + slug + "-" + id + ".md"
		}

		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			// File exists — retry with id suffix.
			continue
		}

		// Order: id, created, due (when set), transport (when set) — spec order.
		kvs := []frontmatter.KV{
			{Key: "id", Value: id},
			{Key: "created", Value: today},
		}
		if o.due != "" {
			kvs = append(kvs, frontmatter.KV{Key: "due", Value: o.due})
		}
		if o.transport != "" {
			kvs = append(kvs, frontmatter.KV{Key: "transport", Value: o.transport})
		}

		// Ensure body ends with a newline.
		content := strings.TrimRight(body, "\n") + "\n"
		doc, err := frontmatter.EmitOrdered(kvs, "\n"+content)
		if err != nil {
			return "", fmt.Errorf("reminder: emit: %w", err)
		}

		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			return "", fmt.Errorf("reminder: write file: %w", err)
		}
		return id, nil
	}

	return "", fmt.Errorf("reminder: could not find non-colliding path after 3 attempts")
}

// SetDue rewrites the due: field of an existing reminder in place.
// All other frontmatter fields and the body are left unchanged.
// Returns an error if the id is not found or iso is malformed.
func SetDue(repoRoot, id, iso string) error {
	if _, err := time.Parse(time.RFC3339, iso); err != nil {
		return fmt.Errorf("reminder: invalid due timestamp %q: must be RFC3339", iso)
	}

	path, body, err := findByID(repoRoot, id)
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reminder set-due: read %q: %w", path, err)
	}
	meta, _, err := frontmatter.Parse(string(raw))
	if err != nil {
		return fmt.Errorf("reminder set-due: parse %q: %w", path, err)
	}

	// Rebuild kvs in spec order: id, created, due, transport.
	// Preserve any fields present; set/update due.
	meta["due"] = iso

	kvs := orderedKVs(meta)

	content := strings.TrimRight(body, "\n") + "\n"
	doc, err := frontmatter.EmitOrdered(kvs, "\n"+content)
	if err != nil {
		return fmt.Errorf("reminder set-due: emit: %w", err)
	}

	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		return fmt.Errorf("reminder set-due: write: %w", err)
	}
	return nil
}

// orderedKVs returns frontmatter key-value pairs in spec order:
// id, created, due, transport. Unknown keys are appended sorted after.
func orderedKVs(meta map[string]any) []frontmatter.KV {
	order := []string{"id", "created", "due", "transport"}
	seen := map[string]bool{}
	var kvs []frontmatter.KV
	for _, k := range order {
		if v, ok := meta[k]; ok {
			kvs = append(kvs, frontmatter.KV{Key: k, Value: v})
			seen[k] = true
		}
	}
	// Append any remaining keys in sorted order.
	var extra []string
	for k := range meta {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		kvs = append(kvs, frontmatter.KV{Key: k, Value: meta[k]})
	}
	return kvs
}

// Row is one entry in the reminder list.
type Row struct {
	ID        string
	Created   string
	Due       string // empty when absent (legacy reminders)
	Transport string // empty when absent (legacy reminders)
	Preview   string // first non-empty body line (raw, not truncated)
}

// List returns all reminders sorted by created ascending then id ascending.
// Returns empty slice (not error) when the directory is absent or empty.
func List(repoRoot string) ([]Row, error) {
	dir := remindersDir(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reminder list: %w", err)
	}

	var rows []Row
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		meta, body, err := frontmatter.Parse(string(raw))
		if err != nil {
			continue
		}
		id, _ := meta["id"].(string)
		created, _ := meta["created"].(string)
		due, _ := meta["due"].(string)
		transport, _ := meta["transport"].(string)
		preview := firstNonEmpty(body)
		rows = append(rows, Row{
			ID:        id,
			Created:   created,
			Due:       due,
			Transport: transport,
			Preview:   preview,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Created != rows[j].Created {
			return rows[i].Created < rows[j].Created
		}
		return rows[i].ID < rows[j].ID
	})

	return rows, nil
}

// Show returns the body (frontmatter stripped) of the reminder with the given id.
// Returns an error if no matching reminder is found.
func Show(repoRoot, id string) (string, error) {
	_, body, err := findByID(repoRoot, id)
	if err != nil {
		return "", err
	}
	return body, nil
}

// Rm deletes the reminder file with the given id.
// Returns an error if no matching reminder is found.
func Rm(repoRoot, id string) error {
	path, _, err := findByID(repoRoot, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("reminder rm %q: %w", id, err)
	}
	return nil
}

// findByID scans the reminders directory for a file whose frontmatter id
// matches the given id. Returns the file path and body on success.
func findByID(repoRoot, id string) (path, body string, err error) {
	dir := remindersDir(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("reminder: no reminder with id %q", id)
		}
		return "", "", fmt.Errorf("reminder: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		meta, b, err := frontmatter.Parse(string(raw))
		if err != nil {
			continue
		}
		if fid, _ := meta["id"].(string); fid == id {
			return p, b, nil
		}
	}

	return "", "", fmt.Errorf("reminder: no reminder with id %q", id)
}

// firstNonEmpty returns the first non-empty (after trimming) line of s.
func firstNonEmpty(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// truncate shortens s to at most maxLen runes, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}
