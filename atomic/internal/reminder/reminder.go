// Package reminder manages frontmatter markdown reminders under
// .claude/.scratchpad/reminders/. There is no scheduling state: a reminder is
// pending while its file exists and done once deleted.
package reminder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/ids"
)

const slugMaxLen = 50

var validTransports = map[string]bool{
	"cron":    true,
	"routine": true,
	"none":    true,
}

func remindersDir(repoRoot string) string {
	return config.RemindersDir(repoRoot)
}

// Option configures optional fields on a reminder.
type Option func(*addOpts) error

type addOpts struct {
	due       string
	transport string
}

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
func Add(repoRoot, body string, opts ...Option) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("reminder: body must not be empty")
	}

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

	firstLine := firstNonEmpty(body)
	slug := ids.Slug(firstLine)
	if len(slug) > slugMaxLen {
		slug = slug[:slugMaxLen]
		slug = strings.TrimRight(slug, "-")
	}

	// The first attempt uses the bare slug; later ones disambiguate with the id.
	for attempt := 0; attempt < 3; attempt++ {
		id, err := ids.ShortID("r")
		if err != nil {
			return "", fmt.Errorf("reminder: generate id: %w", err)
		}

		var filename string
		if attempt == 0 {
			filename = today + "-" + slug + ".md"
		} else {
			filename = today + "-" + slug + "-" + id + ".md"
		}

		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			continue
		}

		meta := map[string]any{
			"id":      id,
			"created": today,
		}
		if o.due != "" {
			meta["due"] = o.due
		}
		if o.transport != "" {
			meta["transport"] = o.transport
		}
		kvs := orderedKVs(meta)

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
func SetDue(repoRoot, id, iso string) error {
	if _, err := time.Parse(time.RFC3339, iso); err != nil {
		return fmt.Errorf("reminder: invalid due timestamp %q: must be RFC3339", iso)
	}

	path, meta, body, err := findByID(repoRoot, id)
	if err != nil {
		return err
	}

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

// orderedKVs emits id, created, due, transport first, then unknown keys sorted.
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

type Row struct {
	ID        string
	Created   string
	Due       string // empty when absent (legacy reminders)
	Transport string // empty when absent (legacy reminders)
	Preview   string // first non-empty body line (raw, not truncated)
}

// List returns all reminders sorted by created ascending then id ascending.
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
func Show(repoRoot, id string) (string, error) {
	_, _, body, err := findByID(repoRoot, id)
	if err != nil {
		return "", err
	}
	return body, nil
}

// Rm deletes the reminder file with the given id.
func Rm(repoRoot, id string) error {
	path, _, _, err := findByID(repoRoot, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("reminder rm %q: %w", id, err)
	}
	return nil
}

// findByID scans for the file whose frontmatter id matches; ids are not encoded
// in the filename except on slug collision.
func findByID(repoRoot, id string) (path string, meta map[string]any, body string, err error) {
	dir := remindersDir(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, "", fmt.Errorf("reminder: no reminder with id %q", id)
		}
		return "", nil, "", fmt.Errorf("reminder: %w", err)
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
		m, b, err := frontmatter.Parse(string(raw))
		if err != nil {
			continue
		}
		if fid, _ := m["id"].(string); fid == id {
			return p, m, b, nil
		}
	}

	return "", nil, "", fmt.Errorf("reminder: no reminder with id %q", id)
}

func firstNonEmpty(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}
