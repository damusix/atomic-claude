package realmshare

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const fixtureRealmID = "1e762389-cd32-4e26-8abf-f50d86fb9c5a"

const portableManifest = `version = 1
id = "` + fixtureRealmID + `"

[[members]]
key = "service"
path = "repos/service"
exclude = true

[[buckets]]
name = "research"
path = "research"

[[mappings]]
source = "shared/wiki"
destination = "wiki"
kind = "directory"

[[mappings]]
source = "research"
destination = "research"
kind = "directory"

[[mappings]]
source = ".agents/realm.md"
destination = ".agents/realm.md"
kind = "instructions"

[[mappings]]
source = ".agents/skills/review"
destination = ".agents/skills/review"
kind = "skill"

[[mappings]]
source = ".agents/commands/triage.md"
destination = ".agents/commands/triage.md"
kind = "command"

[[mappings]]
source = ".agents/agents/reviewer.md"
destination = ".agents/agents/reviewer.md"
kind = "agent"

[[mappings]]
source = "guide.md"
destination = "guide.md"
kind = "file"
`

func TestLoadManifestPortableDefinition(t *testing.T) {
	for _, name := range []string{"publisher", "colleague"} {
		t.Run(name, func(t *testing.T) {
			clone, realm := manifestFixture(t)
			writeRealmshareFile(t, filepath.Join(clone, "shared", "wiki", "index.md"), name+" wiki revision")
			got, err := LoadManifest(clone, realm)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != fixtureRealmID || got.Version != 1 {
				t.Errorf("identity/version = %q/%d, want %q/1", got.ID, got.Version, fixtureRealmID)
			}
			if len(got.Members) != 1 || got.Members[0].Key != "service" || got.Members[0].Path != "repos/service" || !got.Members[0].Exclude {
				t.Errorf("member definitions = %+v", got.Members)
			}
			if len(got.Buckets) != 1 || got.Buckets[0].Name != "research" || got.Buckets[0].Path != "research" {
				t.Errorf("bucket definitions = %+v", got.Buckets)
			}
			want := map[string][2]string{
				"wiki":                       {"shared/wiki", "directory"},
				"research":                   {"research", "directory"},
				".agents/realm.md":           {".agents/realm.md", "instructions"},
				".agents/skills/review":      {".agents/skills/review", "skill"},
				".agents/commands/triage.md": {".agents/commands/triage.md", "command"},
				".agents/agents/reviewer.md": {".agents/agents/reviewer.md", "agent"},
				"guide.md":                   {"guide.md", "file"},
			}
			for _, mapping := range got.Mappings {
				expected, ok := want[mapping.Destination]
				if !ok || expected != [2]string{mapping.Source, string(mapping.Kind)} {
					t.Errorf("unexpected mapping: %+v", mapping)
				}
				delete(want, mapping.Destination)
			}
			if len(want) != 0 {
				t.Errorf("missing mappings: %v", want)
			}
		})
	}
}

func TestLoadManifestAllowsDeclaredLinks(t *testing.T) {
	clone, realm := manifestFixture(t)
	wiki := filepath.Join(clone, "shared", "wiki")
	symlinkRealmshare(t, "index.md", filepath.Join(wiki, "shortcut.md"))
	symlinkRealmshare(t, wiki, filepath.Join(realm, "wiki"))
	if _, err := LoadManifest(clone, realm); err != nil {
		t.Fatalf("declared links rejected: %v", err)
	}
}

func TestLoadManifestRejectsReservedDestinationState(t *testing.T) {
	clone, realm := manifestFixture(t)
	t.Setenv("ATOMIC_STATE_DIR", filepath.Join(realm, "local", "state"))
	definition := strings.Replace(portableManifest, `destination = "wiki"`, `destination = "local"`, 1)
	assertManifestRejected(t, clone, realm, definition)
}

func TestLoadManifestRejectsInvalidDefinitions(t *testing.T) {
	clone, realm := manifestFixture(t)
	absoluteSource := strconv.Quote(filepath.Join(clone, "shared", "wiki"))
	absoluteLocal := strconv.Quote(filepath.Join(realm, "research"))
	writeRealmshareFile(t, filepath.Join(filepath.Dir(clone), "outside", "index.md"), "outside")
	if runtime.GOOS != "windows" {
		writeRealmshareFile(t, filepath.Join(clone, "C:", "shared", "wiki", "index.md"), "outside")
	}
	for _, tc := range []struct {
		name, old, replacement string
	}{
		{"unsupported version", "version = 1", "version = 2"},
		{"missing version", "version = 1\n", ""},
		{"missing identity", `id = "` + fixtureRealmID + `"`, ""},
		{"unknown kind", `kind = "file"`, `kind = "hook"`},
		{"absolute source", `source = "shared/wiki"`, `source = ` + absoluteSource},
		{"absolute destination", `destination = "guide.md"`, `destination = ` + strconv.Quote(filepath.Join(realm, "guide.md"))},
		{"Windows absolute source", `source = "shared/wiki"`, `source = "C:/shared/wiki"`},
		{"escaping source", `source = "shared/wiki"`, `source = "../outside"`},
		{"escaping destination", `destination = "guide.md"`, `destination = "../outside/index.md"`},
		{"noncanonical path", `source = "shared/wiki"`, `source = "shared//wiki"`},
		{"case-alias destination", `destination = "guide.md"`, `destination = "WIKI/index.md"`},
		{"device destination", `destination = "guide.md"`, `destination = "notes/NUL.md"`},
		{"wrong artifact kind", `kind = "instructions"`, `kind = "file"`},
		{"absolute member", `path = "repos/service"`, `path = ` + absoluteLocal},
		{"escaping member", `path = "repos/service"`, `path = "../outside"`},
		{"absolute bucket", `path = "research"`, `path = ` + absoluteLocal},
		{"escaping bucket", `path = "research"`, `path = "../outside"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertManifestRejected(t, clone, realm, strings.Replace(portableManifest, tc.old, tc.replacement, 1))
		})
	}
}

func TestMappingArtifactIdentity(t *testing.T) {
	for _, tc := range []struct {
		kind                MappingKind
		source, destination string
		wantError           string
	}{
		{KindSkill, ".agents/skills/review", ".agents/skills/Review", "identity"},
		{KindCommand, ".agents/commands/review.md", ".agents/commands/other.md", "identity"},
		{KindAgent, ".agents/agents/review.md", ".agents/agents/other.md", "identity"},
		{KindFile, "shared/guide.md", "guide.md", ""},
		{KindDirectory, "shared/wiki", "wiki", ""},
		{KindInstructions, ".agents/realm.md", ".agents/other.md", "not a selectable"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			mapping := Mapping{Kind: tc.kind, Source: tc.source, Destination: tc.source}
			if err := mapping.validate(".atomic"); err != nil {
				t.Fatalf("canonical mapping rejected: %v", err)
			}
			mapping.Destination = tc.destination
			err := mapping.validate(".atomic")
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("ordinary alias rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) || !strings.Contains(err.Error(), tc.destination) {
				t.Fatalf("want %q diagnostic for destination %q, got %v", tc.wantError, tc.destination, err)
			}
			if tc.kind != KindInstructions && (!strings.Contains(err.Error(), tc.source) || !strings.Contains(err.Error(), string(tc.kind))) {
				t.Fatalf("identity diagnostic omits kind %q or source %q: %v", tc.kind, tc.source, err)
			}
		})
	}
}

func TestLoadManifestRejectsOverlappingMappings(t *testing.T) {
	clone, realm := manifestFixture(t)
	writeRealmshareFile(t, filepath.Join(clone, "notes", "a.md"), "notes")
	for _, tc := range []struct {
		name, source, destination, kind string
	}{
		{"nested source", "shared/wiki/index.md", "excerpt.md", "file"},
		{"duplicate source", "shared/wiki", "another-wiki", "directory"},
		{"nested destination", "notes", "wiki/notes", "directory"},
		{"duplicate destination", "notes", "wiki", "directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertManifestRejected(t, clone, realm, portableManifest+manifestMapping(tc.source, tc.destination, tc.kind))
		})
	}
}

func TestLoadManifestRejectsCaseAliasesWithinSelectedDirectory(t *testing.T) {
	clone, realm := manifestFixture(t)
	alias := filepath.Join(clone, "shared", "wiki", "Index.md")
	file, err := os.OpenFile(alias, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		t.Skip("temporary filesystem cannot store both index.md and Index.md")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(clone, realm); err == nil {
		t.Fatal("accepted index.md and Index.md in the same selected directory")
	}
}

func TestLoadManifestRejectsCaseAliasedSourcePrefixes(t *testing.T) {
	clone, realm := manifestFixture(t)
	writeRealmshareFile(t, filepath.Join(clone, "Docs", "a.md"), "a")
	original := filepath.Join(clone, "Docs", "b.md")
	writeRealmshareFile(t, original, "b")
	first := portableManifest + manifestMapping("Docs/a.md", "a.md", "file")
	manifest := filepath.Join(clone, "realm.toml")
	writeRealmshareFile(t, manifest, first+manifestMapping("Docs/b.md", "b.md", "file"))
	if _, err := LoadManifest(clone, realm); err != nil {
		t.Fatalf("independent file mappings with a common source prefix rejected: %v", err)
	}

	alias := filepath.Join(clone, "docs")
	if err := os.Mkdir(alias, 0o755); os.IsExist(err) {
		t.Skip("temporary filesystem cannot store both Docs and docs")
	} else if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, filepath.Join(alias, "b.md")); err != nil {
		t.Fatal(err)
	}
	writeRealmshareFile(t, manifest, first+manifestMapping("docs/b.md", "b.md", "file"))
	if _, err := LoadManifest(clone, realm); err == nil {
		t.Fatal("accepted independent source mappings Docs/a.md and docs/b.md")
	}
}

func TestLoadManifestRejectsCaseAliasedDestinationPrefixes(t *testing.T) {
	clone, realm := manifestFixture(t)
	writeRealmshareFile(t, filepath.Join(clone, "notes", "a.md"), "a")
	writeRealmshareFile(t, filepath.Join(clone, "notes", "b.md"), "b")
	first := portableManifest + manifestMapping("notes/a.md", "Docs/a.md", "file")
	manifest := filepath.Join(clone, "realm.toml")
	writeRealmshareFile(t, manifest, first+manifestMapping("notes/b.md", "Docs/b.md", "file"))
	if _, err := LoadManifest(clone, realm); err != nil {
		t.Fatalf("independent file mappings with a common destination prefix rejected: %v", err)
	}

	writeRealmshareFile(t, manifest, first+manifestMapping("notes/b.md", "docs/b.md", "file"))
	if _, err := LoadManifest(clone, realm); err == nil {
		t.Fatal("accepted independent destination mappings Docs/a.md and docs/b.md")
	}
}

func TestLoadManifestRejectsLocalOnlyContent(t *testing.T) {
	for _, path := range []string{
		"AGENTS.local.md", "CLAUDE.local.md", "AGENTS.md", "CLAUDE.md",
		".claude/settings.json", ".claude/settings.local.json", ".claude/hooks/start.py",
		".codex/config.toml", ".mcp.json", ".atomic/realm-share.toml",
	} {
		t.Run(path, func(t *testing.T) {
			for _, side := range []string{"source", "destination"} {
				t.Run(side, func(t *testing.T) {
					clone, realm := manifestFixture(t)
					writeRealmshareFile(t, filepath.Join(clone, "notes.md"), "notes")
					writeRealmshareFile(t, filepath.Join(clone, filepath.FromSlash(path)), "local-only data")
					source, destination := "notes.md", "notes.md"
					if side == "source" {
						source = path
					} else {
						destination = path
					}
					assertManifestRejected(t, clone, realm, portableManifest+manifestMapping(source, destination, "file"))
				})
			}
		})
	}
	t.Run("private file under selected directory", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		writeRealmshareFile(t, filepath.Join(clone, "shared", "wiki", "AGENTS.local.md"), "private instructions")
		assertManifestRejected(t, clone, realm, portableManifest)
	})
	for _, name := range []string{".claude/settings.json", ".codex/config.toml", ".mcp.json"} {
		t.Run(name+" under selected directory", func(t *testing.T) {
			clone, realm := manifestFixture(t)
			writeRealmshareFile(t, filepath.Join(clone, "shared", "wiki", filepath.FromSlash(name)), "harness configuration")
			assertManifestRejected(t, clone, realm, portableManifest)
		})
	}
	t.Run("embedded harness configuration", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		assertManifestRejected(t, clone, realm, portableManifest+"\n[permissions]\nallow = ['Bash(*)']\n")
	})
}

func TestLoadManifestRejectsUnsafeSymlinks(t *testing.T) {
	t.Run("selected source", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		outside := filepath.Join(filepath.Dir(clone), "outside.md")
		writeRealmshareFile(t, outside, "outside")
		symlinkRealmshare(t, "../outside.md", filepath.Join(clone, "notes.md"))
		assertManifestRejected(t, clone, realm, portableManifest+manifestMapping("notes.md", "notes.md", "file"))
	})
	t.Run("absolute link into the clone", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		wiki := filepath.Join(clone, "shared", "wiki")
		symlinkRealmshare(t, filepath.Join(wiki, "index.md"), filepath.Join(wiki, "shortcut.md"))
		assertManifestRejected(t, clone, realm, portableManifest)
	})
	t.Run("nested link to undeclared content", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		writeRealmshareFile(t, filepath.Join(clone, "unselected.md"), "unselected")
		symlinkRealmshare(t, "../../unselected.md", filepath.Join(clone, "shared", "wiki", "shortcut.md"))
		assertManifestRejected(t, clone, realm, portableManifest)
	})
	t.Run("dangling nested link", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		symlinkRealmshare(t, "missing.md", filepath.Join(clone, "shared", "wiki", "shortcut.md"))
		assertManifestRejected(t, clone, realm, portableManifest)
	})
	t.Run("link to another selected tree through an unsafe parent", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		wiki := filepath.Join(clone, "shared", "wiki")
		symlinkRealmshare(t, "../../research", filepath.Join(wiki, "research-link"))
		symlinkRealmshare(t, "research-link/index.md", filepath.Join(wiki, "shortcut.md"))
		assertManifestRejected(t, clone, realm, portableManifest)
	})
	t.Run("escaping destination parent", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		outside := t.TempDir()
		symlinkRealmshare(t, outside, filepath.Join(realm, "linked"))
		definition := strings.Replace(portableManifest, `destination = "wiki"`, `destination = "linked/wiki"`, 1)
		assertManifestRejected(t, clone, realm, definition)
	})
	t.Run("escaping member", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		if err := os.Mkdir(filepath.Join(realm, "repos"), 0o700); err != nil {
			t.Fatal(err)
		}
		symlinkRealmshare(t, t.TempDir(), filepath.Join(realm, "repos", "service"))
		assertManifestRejected(t, clone, realm, portableManifest)
	})
	t.Run("escaping bucket below a mapped destination", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		writeRealmshareFile(t, filepath.Join(clone, "research", "papers", "index.md"), "papers")
		if err := os.Mkdir(filepath.Join(realm, "research"), 0o700); err != nil {
			t.Fatal(err)
		}
		symlinkRealmshare(t, t.TempDir(), filepath.Join(realm, "research", "papers"))
		definition := strings.Replace(portableManifest, `path = "research"`, `path = "research/papers"`, 1)
		assertManifestRejected(t, clone, realm, definition)
	})
	t.Run("manifest file", func(t *testing.T) {
		clone, realm := manifestFixture(t)
		path := filepath.Join(clone, "realm.toml")
		outside := filepath.Join(filepath.Dir(clone), "realm.toml")
		if err := os.Rename(path, outside); err != nil {
			t.Fatal(err)
		}
		symlinkRealmshare(t, outside, path)
		if _, err := LoadManifest(clone, realm); err == nil {
			t.Fatal("loaded realm.toml through an out-of-clone symlink")
		}
	})
}

func TestLoadManifestRejectsReciprocalDirectorySymlinks(t *testing.T) {
	clone, realm := manifestFixture(t)
	wiki := filepath.Join(clone, "shared", "wiki")
	research := filepath.Join(clone, "research")
	symlinkRealmshare(t, "../../research", filepath.Join(wiki, "research-link"))
	if _, err := LoadManifest(clone, realm); err != nil {
		t.Fatalf("one-way relative directory symlink rejected: %v", err)
	}
	symlinkRealmshare(t, "../shared/wiki", filepath.Join(research, "wiki-link"))
	if _, err := LoadManifest(clone, realm); err == nil {
		t.Fatal("accepted reciprocal directory symlinks between shared/wiki and research")
	}
}

func manifestFixture(t *testing.T) (clone, realm string) {
	t.Helper()
	t.Setenv("ATOMIC_STATE_DIR", ".atomic")
	sandbox := t.TempDir()
	clone = filepath.Join(sandbox, "clone")
	realm = filepath.Join(sandbox, "realm")
	if err := os.Mkdir(realm, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifestFixture(t, clone, portableManifest)
	return clone, realm
}

func writeManifestFixture(t *testing.T, clone, definition string) {
	t.Helper()
	for _, name := range []string{
		"shared/wiki/index.md", "research/index.md", ".agents/realm.md", ".agents/skills/review/SKILL.md",
		".agents/commands/triage.md", ".agents/agents/reviewer.md", "guide.md",
	} {
		writeRealmshareFile(t, filepath.Join(clone, filepath.FromSlash(name)), "# Team content\n")
	}
	writeRealmshareFile(t, filepath.Join(clone, "realm.toml"), definition)
}

func manifestMapping(source, destination, kind string) string {
	return fmt.Sprintf("\n[[mappings]]\nsource = %q\ndestination = %q\nkind = %q\n", source, destination, kind)
}

func assertManifestRejected(t *testing.T, clone, realm, definition string) {
	t.Helper()
	writeRealmshareFile(t, filepath.Join(clone, "realm.toml"), definition)
	if _, err := LoadManifest(clone, realm); err == nil {
		t.Fatal("accepted an invalid shared definition")
	}
}

func writeRealmshareFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlinkRealmshare(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("native symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}
