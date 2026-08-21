// Package scratchpad owns creation, lookup, listing, and archival of one
// slug-keyed bundle per unit of work — the state `atomic-plan`,
// `subagent-implementation`, `quick-fix`, `subagent-diagnose`, and
// `challenge-swarm` each carry across their own run.
package scratchpad

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctemplate"
	"github.com/pelletier/go-toml/v2"
)

// Meta is meta.toml's contract. No version field: go-toml/v2 ignores unknown
// keys on decode by default, which is the tolerance a fixture predating a
// field addition relies on.
type Meta struct {
	Slug        string   `toml:"slug"`
	Purposes    []string `toml:"purposes"`
	Created     string   `toml:"created"`
	Updated     string   `toml:"updated"`
	Status      string   `toml:"status"`
	Description string   `toml:"description,omitempty"`

	// extra carries keys Load found but this Meta doesn't model, so Save can
	// write them back rather than dropping them on a rewrite.
	extra map[string]any
}

// metaKnownKeys are the toml keys Meta models directly; anything else read
// by Load is stashed in Meta.extra and replayed by Save.
var metaKnownKeys = map[string]struct{}{
	"slug": {}, "purposes": {}, "created": {}, "updated": {}, "status": {}, "description": {},
}

// Bundle is a scratchpad root plus its parsed meta.toml.
type Bundle struct {
	Root string
	Meta *Meta
}

const metaFile = "meta.toml"

// purposeFiles maps each purpose to the bundle-relative markdown files it
// seeds, keyed to the doctemplate name that fills them.
var purposeFiles = map[string]map[string]string{
	"plan":      {"BRIEF.md": "brief", "STATE.md": "state", "FOLLOWUPS.md": "followups"},
	"implement": {"BRIEF.md": "brief", "STATE.md": "state", "FOLLOWUPS.md": "followups"},
	"fix":       {"BRIEF.md": "brief", "STATE.md": "state", "FOLLOWUPS.md": "followups"},
	"diagnose":  {"BRIEF.md": "brief", "STATE.md": "state", "FOLLOWUPS.md": "followups", "CONTEXT.md": "diagnose-context"},
	"review":    {},
}

// purposeDirs maps each purpose to the bundle-relative directories it seeds.
var purposeDirs = map[string][]string{
	"review": {"lenses", "findings"},
}

// seedFor returns the purpose matrix's file set (bundle-relative name ->
// doctemplate name) and directory set for purpose. The second return reports
// whether purpose is recognized at all.
func seedFor(purpose string) (files map[string]string, dirs []string, ok bool) {
	files, ok = purposeFiles[purpose]
	if !ok {
		return nil, nil, false
	}
	return files, purposeDirs[purpose], true
}

// Load reads meta.toml from bundleRoot, tolerating unrecognized keys: they
// are stashed on the returned Meta and replayed by Save rather than dropped.
func Load(bundleRoot string) (*Meta, error) {
	path := filepath.Join(bundleRoot, metaFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("scratchpad: parse %s: %w", path, err)
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("scratchpad: parse %s: %w", path, err)
	}
	for k, v := range raw {
		if _, known := metaKnownKeys[k]; known {
			continue
		}
		if m.extra == nil {
			m.extra = map[string]any{}
		}
		m.extra[k] = v
	}
	if m.Slug == "" {
		return nil, fmt.Errorf("scratchpad: %s: missing slug", path)
	}
	return &m, nil
}

// Save writes meta.toml under bundleRoot, stamping Updated to now and
// replaying any keys Load found that Meta doesn't model.
func Save(bundleRoot string, m *Meta) error {
	m.Updated = time.Now().UTC().Format(time.RFC3339)
	doc := map[string]any{
		"slug":     m.Slug,
		"purposes": m.Purposes,
		"created":  m.Created,
		"updated":  m.Updated,
		"status":   m.Status,
	}
	if m.Description != "" {
		doc["description"] = m.Description
	}
	for k, v := range m.extra {
		doc[k] = v
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("scratchpad: encode meta.toml: %w", err)
	}
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return fmt.Errorf("scratchpad: create bundle dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, metaFile), data, 0o644); err != nil {
		return fmt.Errorf("scratchpad: write meta.toml: %w", err)
	}
	return nil
}

// BundleRoot returns <scratchpad-root>/<slug>.
func BundleRoot(root, slug string) string {
	return filepath.Join(config.ScratchpadDir(root), slug)
}

// New additively creates or extends slug's bundle under root for purpose:
// missing purpose files/dirs are seeded, existing ones are left untouched,
// purpose is appended to Meta.Purposes if not already present, and the
// "plan" purpose also seeds docs/design/<slug>.md and docs/spec/<slug>.md
// outside the bundle. extended reports whether a bundle already existed for
// slug before this call.
func New(root, slug, purpose string) (b *Bundle, extended bool, err error) {
	files, dirs, ok := seedFor(purpose)
	if !ok {
		return nil, false, fmt.Errorf("scratchpad: unknown purpose %q", purpose)
	}

	bundleRoot := BundleRoot(root, slug)
	meta, loadErr := Load(bundleRoot)
	switch {
	case loadErr == nil:
		extended = true
	case os.IsNotExist(loadErr):
		now := time.Now().UTC().Format(time.RFC3339)
		meta = &Meta{Slug: slug, Created: now, Status: "active"}
	default:
		return nil, false, loadErr
	}

	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return nil, false, fmt.Errorf("scratchpad: create bundle dir: %w", err)
	}

	for name, tmpl := range files {
		path := filepath.Join(bundleRoot, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		content, err := doctemplate.Get(tmpl)
		if err != nil {
			return nil, false, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, false, fmt.Errorf("scratchpad: seed %s: %w", name, err)
		}
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(bundleRoot, dir), 0o755); err != nil {
			return nil, false, fmt.Errorf("scratchpad: seed %s/: %w", dir, err)
		}
	}

	if purpose == "plan" {
		if err := seedDoc(root, "design", "design-doc", slug); err != nil {
			return nil, false, err
		}
		if err := seedDoc(root, "spec", "spec", slug); err != nil {
			return nil, false, err
		}
	}

	if !hasPurpose(meta.Purposes, purpose) {
		meta.Purposes = append(meta.Purposes, purpose)
	}
	if err := Save(bundleRoot, meta); err != nil {
		return nil, false, err
	}

	return &Bundle{Root: bundleRoot, Meta: meta}, extended, nil
}

// seedDoc writes docs/<kind>/<slug>.md from the docTemplate template, unless
// it already exists.
func seedDoc(root, kind, docTemplate, slug string) error {
	path := filepath.Join(root, "docs", kind, slug+".md")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	content, err := doctemplate.Get(docTemplate)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("scratchpad: create docs/%s dir: %w", kind, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("scratchpad: seed docs/%s/%s.md: %w", kind, slug, err)
	}
	return nil
}

func hasPurpose(purposes []string, purpose string) bool {
	for _, p := range purposes {
		if p == purpose {
			return true
		}
	}
	return false
}
