package rules

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const shippedSource = "rules/typescript/style.md"

func TestParseShipped_RecordShape(t *testing.T) {
	data := []byte("---\npaths:\n  - \"**/*.{ts,tsx}\"\n---\n\n# TypeScript\n\nBody.\n")
	rec, err := ParseShipped(shippedSource, data)
	if err != nil {
		t.Fatalf("ParseShipped: %v", err)
	}

	if rec.ID != "shipped:"+shippedSource {
		t.Errorf("ID = %q", rec.ID)
	}
	if rec.Class != ClassShipped || rec.Producer != ProducerShipped {
		t.Errorf("class/producer = %q/%q", rec.Class, rec.Producer)
	}
	if rec.Source != shippedSource {
		t.Errorf("Source = %q", rec.Source)
	}
	if rec.BaseKind != BaseRepositoryRoot {
		t.Errorf("BaseKind = %q", rec.BaseKind)
	}
	if len(rec.Include) != 1 || rec.Include[0] != "**/*.{ts,tsx}" {
		t.Errorf("Include = %v", rec.Include)
	}
	if strings.HasPrefix(string(rec.Body), "---") || !strings.Contains(string(rec.Body), "Body.") {
		t.Errorf("Body carries frontmatter or lost content: %q", rec.Body)
	}
	if rec.SourceDigest != managedfile.Digest(data) {
		t.Errorf("SourceDigest = %q, want canonical source digest", rec.SourceDigest)
	}
}

// paths: order is scope order, not an unordered set.
func TestParseShipped_PreservesPatternOrder(t *testing.T) {
	data := []byte("---\npaths:\n  - \"docs/spec/**/*.md\"\n  - \"docs/design/**/*.md\"\n---\nbody\n")
	rec, err := ParseShipped("rules/specs/spec-currency.md", data)
	if err != nil {
		t.Fatalf("ParseShipped: %v", err)
	}
	if got := strings.Join(rec.Include, ","); got != "docs/spec/**/*.md,docs/design/**/*.md" {
		t.Errorf("Include = %q", got)
	}
}

func TestParseShipped_NormalizesSeparators(t *testing.T) {
	rec, err := ParseShipped(shippedSource, []byte("---\npaths:\n  - \"src\\\\**\\\\*.ts\"\n---\nbody\n"))
	if err != nil {
		t.Fatalf("ParseShipped: %v", err)
	}
	if rec.Include[0] != "src/**/*.ts" {
		t.Errorf("Include = %q, want slash-normalized", rec.Include[0])
	}
}

func TestParseShipped_Rejects(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"unknown scope key", "---\npaths:\n  - \"**/*.ts\"\nglobs:\n  - x\n---\nbody\n", `unknown scope key "globs"`},
		{"missing paths", "---\n---\nbody\n", `missing "paths"`},
		{"no frontmatter", "# rule\n", `missing "paths"`},
		{"empty paths", "---\npaths: []\n---\nbody\n", "at least one pattern"},
		{"scalar paths", "---\npaths: \"**/*.ts\"\n---\nbody\n", "must be a list"},
		{"mapping element", "---\npaths:\n  - {pattern: \"**/*.ts\"}\n---\nbody\n", "is not a string"},
		{"absolute pattern", "---\npaths:\n  - \"/etc/**\"\n---\nbody\n", "absolute pattern"},
		{"home pattern", "---\npaths:\n  - \"~/x\"\n---\nbody\n", "absolute pattern"},
		{"drive pattern", "---\npaths:\n  - \"C:/x\"\n---\nbody\n", "absolute pattern"},
		{"parent escape", "---\npaths:\n  - \"../outside/**\"\n---\nbody\n", "parent-escaping pattern"},
		{"interior escape", "---\npaths:\n  - \"a/../../b\"\n---\nbody\n", "parent-escaping pattern"},
		{"brace parent escape", "---\npaths:\n  - \"{..,src}/y\"\n---\nbody\n", "resolves to"},
		{"brace dot segment", "---\npaths:\n  - \"{.,src}/y\"\n---\nbody\n", "resolves to"},
		{"class dot segment", "---\npaths:\n  - \"[.][.]/x\"\n---\nbody\n", "resolves to"},
		{"class parent segment", "---\npaths:\n  - \"a/[..]/x\"\n---\nbody\n", "resolves to"},
		{"malformed glob", "---\npaths:\n  - \"a[broken\"\n---\nbody\n", "malformed pattern"},
		{"unclosed frontmatter", "---\npaths:\n  - \"**/*.ts\"\nbody\n", "missing closing delimiter"},
		{"bad yaml", "---\npaths: [\n---\nbody\n", "invalid YAML"},
		{"duplicate key", "---\npaths:\n  - \"**/*.ts\"\npaths:\n  - \"**/*.js\"\n---\nbody\n", "duplicate frontmatter key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseShipped(shippedSource, []byte(tc.data))
			if err == nil {
				t.Fatalf("ParseShipped accepted invalid metadata")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), shippedSource) {
				t.Errorf("error %q does not name the source path", err)
			}
		})
	}
}

func TestParseWiki_Identity(t *testing.T) {
	rec, err := ParseWiki("abc123", "rules/wiki/code-intel.md", []byte("---\npaths:\n  - \"atomic/internal/**\"\n---\nbody\n"))
	if err != nil {
		t.Fatalf("ParseWiki: %v", err)
	}
	if rec.ID != "wiki:abc123:code-intel" {
		t.Errorf("ID = %q", rec.ID)
	}
	if rec.Class != ClassWikiPointer || rec.Producer != ProducerWiki {
		t.Errorf("class/producer = %q/%q", rec.Class, rec.Producer)
	}
	if rec.NativeName() != "atomic-wiki/code-intel.md" {
		t.Errorf("NativeName = %q", rec.NativeName())
	}
}

func TestParseWiki_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		project string
		source  string
		want    string
	}{
		{"empty project key", "", "rules/wiki/card.md", "empty project key"},
		{"not under wiki", "k", "rules/other/card.md", "must be rules/wiki"},
		{"nested card", "k", "rules/wiki/sub/card.md", "must be rules/wiki"},
		{"non-markdown card", "k", "rules/wiki/card.txt", "must be rules/wiki"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseWiki(tc.project, tc.source, []byte("---\npaths:\n  - \"x/**\"\n---\nbody\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidate_Rejects(t *testing.T) {
	base := RuleRecord{
		ID:       "shipped:rules/a.md",
		Class:    ClassShipped,
		Producer: ProducerShipped,
		Source:   "rules/a.md",
		BaseKind: BaseRepositoryRoot,
		Include:  []string{"**/*.go"},
	}

	cases := []struct {
		name    string
		records []RuleRecord
		want    string
	}{
		{"identity collision", []RuleRecord{base, base}, "identity collision"},
		{
			"native-name collision",
			[]RuleRecord{
				base,
				{
					ID: "wiki:k:card", Class: ClassWikiPointer, Producer: ProducerWiki,
					Source: "rules/wiki/card.md", BaseKind: BaseRepositoryRoot, Include: []string{"**/*.md"},
				},
				{
					ID: "shipped:rules/atomic-wiki/card.md", Class: ClassShipped, Producer: ProducerShipped,
					Source: "rules/atomic-wiki/card.md", BaseKind: BaseRepositoryRoot, Include: []string{"**/*.py"},
				},
			},
			"native-name collision",
		},
		{"missing base kind", []RuleRecord{{ID: base.ID, Producer: ProducerShipped, Source: base.Source, Include: base.Include}}, "missing base kind"},
		{"no include", []RuleRecord{{ID: base.ID, Producer: ProducerShipped, Source: base.Source, BaseKind: BaseRepositoryRoot}}, "no include patterns"},
		{"no identity", []RuleRecord{{Producer: ProducerShipped, Source: "rules/a.md", BaseKind: BaseRepositoryRoot, Include: base.Include}}, "no identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.records)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidate_AcceptsDistinctRecords(t *testing.T) {
	records, err := LoadShipped("testdata/corpus/rules")
	if err != nil {
		t.Fatalf("LoadShipped: %v", err)
	}
	if err := Validate(records); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
