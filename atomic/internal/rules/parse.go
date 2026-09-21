package rules

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// scopeKey is the only frontmatter key a scoped rule may carry. Any other key
// is unknown scope metadata and fails instead of being silently dropped.
const scopeKey = "paths"

// ParseShipped parses one authored context/rules source. source is the
// producer-relative slash path (e.g. "rules/typescript/style.md"); data is the
// raw file bytes. Every error names source.
func ParseShipped(source string, data []byte) (RuleRecord, error) {
	return parse(ProducerShipped, "", source, data)
}

// ParseWiki parses one repository-refresh pointer card. source is the
// producer-relative slash path under rules/wiki/; the card's domain is its file
// name without the .md suffix. Every error names source.
func ParseWiki(projectKey, source string, data []byte) (RuleRecord, error) {
	if projectKey == "" {
		return RuleRecord{}, fmt.Errorf("rules: parse %s: empty project key", source)
	}
	return parse(ProducerWiki, projectKey, source, data)
}

func parse(producer Producer, projectKey, source string, data []byte) (RuleRecord, error) {
	if source == "" {
		return RuleRecord{}, fmt.Errorf("rules: parse: empty source path")
	}
	kvs, body, err := frontmatter.ParseOrdered(string(data))
	if err != nil {
		return RuleRecord{}, fmt.Errorf("rules: parse %s: %w", source, err)
	}

	seen := make(map[string]bool, len(kvs))
	var patterns []any
	for _, kv := range kvs {
		if seen[kv.Key] {
			return RuleRecord{}, fmt.Errorf("rules: parse %s: duplicate frontmatter key %q", source, kv.Key)
		}
		seen[kv.Key] = true
		if kv.Key != scopeKey {
			return RuleRecord{}, fmt.Errorf("rules: parse %s: unknown scope key %q", source, kv.Key)
		}
		list, ok := kv.Value.([]any)
		if !ok {
			return RuleRecord{}, fmt.Errorf("rules: parse %s: %q must be a list", source, scopeKey)
		}
		patterns = list
	}

	if !seen[scopeKey] {
		return RuleRecord{}, fmt.Errorf("rules: parse %s: missing %q scope metadata", source, scopeKey)
	}
	if len(patterns) == 0 {
		return RuleRecord{}, fmt.Errorf("rules: parse %s: %q must list at least one pattern", source, scopeKey)
	}

	include := make([]string, 0, len(patterns))
	for i, raw := range patterns {
		pattern, ok := raw.(string)
		if !ok {
			return RuleRecord{}, fmt.Errorf("rules: parse %s: %s[%d] is not a string", source, scopeKey, i)
		}
		normalized, err := normalizePattern(pattern)
		if err != nil {
			return RuleRecord{}, fmt.Errorf("rules: parse %s: %s[%d]: %w", source, scopeKey, i, err)
		}
		include = append(include, normalized)
	}

	rec := RuleRecord{
		Class:        classFor(producer),
		Producer:     producer,
		Source:       source,
		BaseKind:     BaseRepositoryRoot,
		Include:      include,
		Body:         []byte(body),
		SourceDigest: managedfile.Digest(data),
	}
	rec.ID, err = recordID(producer, projectKey, source)
	if err != nil {
		return RuleRecord{}, err
	}
	return rec, nil
}

func classFor(producer Producer) Class {
	if producer == ProducerWiki {
		return ClassWikiPointer
	}
	return ClassShipped
}

// recordID builds the producer-qualified identity, validating producer-specific
// source shape so a malformed card cannot claim an identity.
func recordID(producer Producer, projectKey, source string) (string, error) {
	switch producer {
	case ProducerShipped:
		return string(ProducerShipped) + ":" + source, nil
	case ProducerWiki:
		rel := strings.TrimPrefix(source, "rules/wiki/")
		if rel == source || rel == "" || strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".md") {
			return "", fmt.Errorf("rules: parse %s: wiki card must be rules/wiki/<domain>.md", source)
		}
		return string(ProducerWiki) + ":" + projectKey + ":" + strings.TrimSuffix(rel, ".md"), nil
	default:
		return "", fmt.Errorf("rules: parse %s: unknown producer %q", source, producer)
	}
}

// normalizePattern converts separators to "/" and rejects metadata the parser
// cannot honor without a checkout: absolute and parent-escaping patterns, and
// malformed globs.
func normalizePattern(pattern string) (string, error) {
	p := strings.ReplaceAll(pattern, `\`, "/")
	if p == "" {
		return "", fmt.Errorf("empty pattern")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
		return "", fmt.Errorf("absolute pattern %q", pattern)
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", fmt.Errorf("absolute pattern %q", pattern)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("parent-escaping pattern %q", pattern)
		}
	}
	if hiddenDotSegment(p) {
		return "", fmt.Errorf("pattern %q contains a path segment that resolves to %q or %q", pattern, ".", "..")
	}
	if !doublestar.ValidatePattern(p) {
		return "", fmt.Errorf("malformed pattern %q", pattern)
	}
	return path.Clean(p), nil
}

// hiddenDotSegment reports whether any `/`-separated segment can resolve to `.`
// or `..` through a character class or a brace alternative. The literal-segment
// check above cannot see `{..,src}/y` or `[.][.]/x`, so a pattern that escapes
// through either would be compiled into the runtime module as a match the
// canonical matcher never intends.
func hiddenDotSegment(pattern string) bool {
	for _, seg := range strings.Split(pattern, "/") {
		if dotSegment(seg) {
			return true
		}
	}
	return false
}

// dotSegment reports whether one segment resolves to `.` or `..`: a literal dot
// or parent segment, a character class built only from dots (which is a literal
// `.` once expanded), or a brace alternative that itself qualifies.
func dotSegment(segment string) bool {
	if segment == "." || segment == ".." {
		return true
	}
	for i := 0; i < len(segment); i++ {
		switch segment[i] {
		case '[':
			end := strings.IndexByte(segment[i:], ']')
			if end < 0 {
				return false
			}
			if body := segment[i+1 : i+end]; body != "" && strings.Trim(body, ".") == "" {
				return true
			}
			i += end
		case '{':
			end := strings.IndexByte(segment[i:], '}')
			if end < 0 {
				return false
			}
			for _, alternative := range strings.Split(segment[i+1:i+end], ",") {
				if dotSegment(alternative) {
					return true
				}
			}
			i += end
		}
	}
	return false
}

// LoadShipped parses every authored .md under rulesDir into records, sorted by
// source. The wiki/ subtree belongs to the wiki producer and is skipped here.
// Malformed metadata and identity or native-name collisions fail the whole load
// rather than dropping a rule silently.
func LoadShipped(rulesDir string) ([]RuleRecord, error) {
	records, err := loadDir(rulesDir, func(rel string) bool {
		return rel == "wiki"
	}, func(rel string, data []byte) (RuleRecord, error) {
		return ParseShipped("rules/"+rel, data)
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// LoadWiki parses every pointer card under rulesDir/wiki into records for one
// project key, sorted by source. A repository with no generated cards yields no
// records and no error.
func LoadWiki(rulesDir, projectKey string) ([]RuleRecord, error) {
	wikiDir := filepath.Join(rulesDir, "wiki")
	if _, err := os.Stat(wikiDir); os.IsNotExist(err) {
		return nil, nil
	}
	records, err := loadDir(wikiDir, nil, func(rel string, data []byte) (RuleRecord, error) {
		return ParseWiki(projectKey, "rules/wiki/"+rel, data)
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func loadDir(dir string, skipDir func(rel string) bool, parse func(rel string, data []byte) (RuleRecord, error)) ([]RuleRecord, error) {
	var records []RuleRecord
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir != nil && skipDir(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rec, err := parse(rel, data)
		if err != nil {
			return err
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Source < records[j].Source })
	if err := Validate(records); err != nil {
		return nil, err
	}
	return records, nil
}

// Validate gates a combined rule set before any target mutation: identity
// collisions, native-name collisions, and records that violate the producer
// contract are all errors.
func Validate(records []RuleRecord) error {
	byID := make(map[string]string, len(records))
	byNative := make(map[string]string, len(records))
	for _, r := range records {
		if r.ID == "" || r.Source == "" {
			return fmt.Errorf("rules: record %q has no identity", r.Source)
		}
		if r.BaseKind == "" {
			return fmt.Errorf("rules: %s: missing base kind", r.ID)
		}
		if len(r.Include) == 0 {
			return fmt.Errorf("rules: %s: no include patterns", r.ID)
		}
		wantPrefix := string(r.Producer) + ":"
		if !strings.HasPrefix(r.ID, wantPrefix) || r.Producer == "" {
			return fmt.Errorf("rules: %s: identity does not match producer %q", r.ID, r.Producer)
		}
		if prev, ok := byID[r.ID]; ok {
			return fmt.Errorf("rules: identity collision %s (%s and %s)", r.ID, prev, r.Source)
		}
		byID[r.ID] = r.Source

		native := r.NativeName()
		if prev, ok := byNative[native]; ok {
			return fmt.Errorf("rules: native-name collision %q (%s and %s)", native, prev, r.ID)
		}
		byNative[native] = r.ID
	}
	return nil
}
