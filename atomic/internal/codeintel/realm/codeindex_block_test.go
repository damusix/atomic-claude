package realm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

func members(entries ...struct{ key, path string }) []realm.MemberEntry {
	out := make([]realm.MemberEntry, len(entries))
	for i, e := range entries {
		out[i] = realm.MemberEntry{Key: e.key, Path: e.path}
	}
	return out
}

func TestWriteCodeIndexBlock_AbsentCLAUDEMD(t *testing.T) {
	dir := t.TempDir()

	ms := members(
		struct{ key, path string }{"foo", "repos/foo"},
		struct{ key, path string }{"bar", "repos/bar"},
	)

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("WriteCodeIndexBlock: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "<code-index>") {
		t.Errorf("missing <code-index> open tag in:\n%s", content)
	}
	if !strings.Contains(content, "</code-index>") {
		t.Errorf("missing </code-index> close tag in:\n%s", content)
	}
	if !strings.Contains(content, `key="foo"`) || !strings.Contains(content, `path="repos/foo"`) {
		t.Errorf("missing foo member in:\n%s", content)
	}
	if !strings.Contains(content, `key="bar"`) || !strings.Contains(content, `path="repos/bar"`) {
		t.Errorf("missing bar member in:\n%s", content)
	}
	// A timestamp would make the block diff on every run.
	if strings.Contains(content, "generated=") {
		t.Errorf("block must not contain generated= timestamp; got:\n%s", content)
	}
}

func TestWriteCodeIndexBlock_SplicesIntoExisting(t *testing.T) {
	dir := t.TempDir()

	prefix := "# Realm CLAUDE.md\n\nSome context above.\n\n"
	suffix := "\n\n## Notes\n\nContent below the block.\n"
	existing := prefix + suffix

	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	ms := members(struct{ key, path string }{"alpha", "repos/alpha"})

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("WriteCodeIndexBlock: %v", err)
	}

	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "<code-index>") {
		t.Errorf("missing <code-index> in:\n%s", content)
	}
	if !strings.Contains(content, `key="alpha"`) {
		t.Errorf("missing alpha member in:\n%s", content)
	}
	if !strings.Contains(content, "# Realm CLAUDE.md") {
		t.Errorf("original heading lost in:\n%s", content)
	}
	if !strings.Contains(content, "## Notes") {
		t.Errorf("## Notes section lost in:\n%s", content)
	}
	if !strings.Contains(content, "Content below the block.") {
		t.Errorf("suffix content lost in:\n%s", content)
	}
}

func TestWriteCodeIndexBlock_Idempotent(t *testing.T) {
	dir := t.TempDir()

	ms := members(
		struct{ key, path string }{"x", "repos/x"},
		struct{ key, path string }{"y", "repos/y"},
	)

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("first WriteCodeIndexBlock: %v", err)
	}

	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	firstContent, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("ReadFile after first write: %v", err)
	}
	firstInfo, err := os.Stat(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("second WriteCodeIndexBlock: %v", err)
	}

	secondContent, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("ReadFile after second write: %v", err)
	}
	secondInfo, err := os.Stat(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(firstContent) != string(secondContent) {
		t.Errorf("content changed on idempotent re-run:\nbefore: %q\nafter:  %q", firstContent, secondContent)
	}
	// An unchanged mtime proves no write happened at all.
	if !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Logf("mtime changed from %v to %v (acceptable on some FS with low-res clock)", firstInfo.ModTime(), secondInfo.ModTime())
		// Advisory only: some filesystems have coarse mtime granularity.
	}
}

func TestWriteCodeIndexBlock_MembershipChange(t *testing.T) {
	dir := t.TempDir()

	ms1 := members(struct{ key, path string }{"foo", "repos/foo"})
	if err := realm.WriteCodeIndexBlock(dir, ms1); err != nil {
		t.Fatal(err)
	}

	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	existing, _ := os.ReadFile(claudeMDPath)
	manual := "\n## Manual notes\n\nKeep this.\n"
	if err := os.WriteFile(claudeMDPath, append(existing, []byte(manual)...), 0o644); err != nil {
		t.Fatal(err)
	}

	ms2 := members(
		struct{ key, path string }{"foo", "repos/foo"},
		struct{ key, path string }{"baz", "repos/baz"},
	)
	if err := realm.WriteCodeIndexBlock(dir, ms2); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(claudeMDPath)
	content := string(data)

	if !strings.Contains(content, `key="foo"`) {
		t.Errorf("missing foo after update:\n%s", content)
	}
	if !strings.Contains(content, `key="baz"`) {
		t.Errorf("missing baz after update:\n%s", content)
	}
	if !strings.Contains(content, "Keep this.") {
		t.Errorf("manual notes lost after update:\n%s", content)
	}
}

func TestWriteCodeIndexBlock_EmptyMembers(t *testing.T) {
	dir := t.TempDir()

	if err := realm.WriteCodeIndexBlock(dir, nil); err != nil {
		t.Fatalf("WriteCodeIndexBlock with nil members: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "<code-index>") {
		t.Errorf("missing <code-index> tag:\n%s", content)
	}
	if !strings.Contains(content, "</code-index>") {
		t.Errorf("missing </code-index> tag:\n%s", content)
	}
	if strings.Contains(content, "<member") {
		t.Errorf("unexpected <member> tag in empty block:\n%s", content)
	}
}

func TestWriteCodeIndexBlock_ExistingBlockReplacedInPlace(t *testing.T) {
	dir := t.TempDir()

	initial := `# Header

<code-index>
<member key="old" path="repos/old" />
</code-index>

## Trailing section

Content after.
`
	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	ms := members(struct{ key, path string }{"new", "repos/new"})
	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(claudeMDPath)
	content := string(data)

	if strings.Contains(content, `key="old"`) {
		t.Errorf("old member still present after replace:\n%s", content)
	}
	if !strings.Contains(content, `key="new"`) {
		t.Errorf("new member missing after replace:\n%s", content)
	}
	if !strings.Contains(content, "# Header") {
		t.Errorf("prefix lost:\n%s", content)
	}
	if !strings.Contains(content, "## Trailing section") {
		t.Errorf("suffix lost:\n%s", content)
	}
	if !strings.Contains(content, "Content after.") {
		t.Errorf("trailing content lost:\n%s", content)
	}
}
