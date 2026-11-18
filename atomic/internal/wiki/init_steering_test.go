package wiki

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

var (
	loaderDoc    = string(bundlespec.ScopeSteering.LoaderDocument())
	scaffoldDoc  = string(managedfile.BracketedBlockDocument([]byte(realmSteeringScaffold)))
	scaffoldText = realmSteeringScaffold
)

func steeringFile(t *testing.T, dir, name string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

func putSteering(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func apply(t *testing.T, dir string, mode bundlespec.SteeringMode) []string {
	t.Helper()
	_, notes, err := initSteeringPair(dir, scaffoldText, mode)
	if err != nil {
		t.Fatalf("%s: %v", mode, err)
	}
	return notes
}

func expectPair(t *testing.T, dir, wantAgents, wantClaude string) {
	t.Helper()
	agents, hasAgents := steeringFile(t, dir, "AGENTS.md")
	claude, hasClaude := steeringFile(t, dir, "CLAUDE.md")
	if agents != wantAgents || hasAgents != (wantAgents != "") {
		t.Errorf("AGENTS.md = %q (present %v), want %q", agents, hasAgents, wantAgents)
	}
	if claude != wantClaude || hasClaude != (wantClaude != "") {
		t.Errorf("CLAUDE.md = %q (present %v), want %q", claude, hasClaude, wantClaude)
	}
}

func TestInitSteeringPair_WritesTheShapeOfEachMode(t *testing.T) {
	for mode, want := range map[bundlespec.SteeringMode][2]string{
		bundlespec.SteeringLoader:   {scaffoldText, loaderDoc},
		bundlespec.SteeringAgentsMD: {scaffoldText, ""},
		bundlespec.SteeringClaudeMD: {"", scaffoldDoc},
	} {
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			apply(t, dir, mode)
			expectPair(t, dir, want[0], want[1])
		})
	}
}

// Every conversion is reversible: the owned inline block in CLAUDE.md is
// recognised, so a round trip lands back on the loader pair byte-for-byte.
func TestInitSteeringPair_ConversionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	apply(t, dir, bundlespec.SteeringLoader)
	for _, mode := range []bundlespec.SteeringMode{
		bundlespec.SteeringClaudeMD, bundlespec.SteeringAgentsMD,
		bundlespec.SteeringClaudeMD, bundlespec.SteeringLoader,
	} {
		apply(t, dir, mode)
	}
	expectPair(t, dir, scaffoldText, loaderDoc)
}

// User text beside Atomic's block survives every conversion byte-for-byte, and
// the scaffold moves between files only as Atomic's own block.
func TestInitSteeringPair_KeepsUserTextInClaudeMD(t *testing.T) {
	for _, userText := range []string{"# mine\n\nkeep this\n", "ends in a blank line\n\n"} {
		dir := t.TempDir()
		putSteering(t, dir, "CLAUDE.md", string(managedfile.AppendBlock([]byte(userText), []byte(loaderDoc))))
		putSteering(t, dir, "AGENTS.md", scaffoldText)

		apply(t, dir, bundlespec.SteeringClaudeMD)
		claude, _ := steeringFile(t, dir, "CLAUDE.md")
		if !strings.HasPrefix(claude, userText) || !strings.Contains(claude, scaffoldDoc) || strings.Contains(claude, "@AGENTS.md") {
			t.Fatalf("claude-md CLAUDE.md = %q", claude)
		}
		if _, ok := steeringFile(t, dir, "AGENTS.md"); ok {
			t.Error("the unmodified scaffold AGENTS.md was not retired")
		}

		apply(t, dir, bundlespec.SteeringAgentsMD)
		if claude, _ := steeringFile(t, dir, "CLAUDE.md"); claude != userText {
			t.Errorf("agents-md CLAUDE.md = %q, want exactly the user text %q", claude, userText)
		}
		if agents, _ := steeringFile(t, dir, "AGENTS.md"); agents != scaffoldText {
			t.Errorf("agents-md AGENTS.md = %q, want the scaffold restored", agents)
		}
	}
}

// A CLAUDE.md with no Atomic block is the author's own install: no mode touches it.
func TestInitSteeringPair_LeavesAnAuthorsClaudeMDAlone(t *testing.T) {
	dir := t.TempDir()
	putSteering(t, dir, "CLAUDE.md", "# mine\n")
	for _, mode := range bundlespec.SteeringModes {
		apply(t, dir, mode)
		if claude, _ := steeringFile(t, dir, "CLAUDE.md"); claude != "# mine\n" {
			t.Fatalf("%s rewrote the user's CLAUDE.md to %q", mode, claude)
		}
	}
}

// An edited AGENTS.md holds user text with no managed block, which Atomic never
// copies between files: claude-md leaves both files in loader shape and says so.
func TestInitSteeringPair_ClaudeMDLeavesEditedAgentsInLoaderShape(t *testing.T) {
	dir := t.TempDir()
	putSteering(t, dir, "AGENTS.md", "# my edits\n")
	putSteering(t, dir, "CLAUDE.md", loaderDoc)

	notes := apply(t, dir, bundlespec.SteeringClaudeMD)
	expectPair(t, dir, "# my edits\n", loaderDoc)
	if len(notes) != 1 || !strings.Contains(notes[0], "AGENTS.md") {
		t.Errorf("notes = %v, want one naming AGENTS.md", notes)
	}
}

// An older install, or an emptied file, left CLAUDE.md holding no loader; loader
// mode rewrites it into the pair rather than treating it as the author's.
func TestInitSteeringPair_LoaderRewritesLegacyOrBlankClaudeMD(t *testing.T) {
	for name, legacy := range map[string]string{"legacy scaffold": scaffoldText, "whitespace": " \n\n"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			putSteering(t, dir, "CLAUDE.md", legacy)
			apply(t, dir, bundlespec.SteeringLoader)
			expectPair(t, dir, scaffoldText, loaderDoc)
		})
	}
}

// The realm root pair carries no scaffold; its AGENTS.md holds capture sections,
// so claude-md leaves it and the loader in place and says so.
func TestConvertSteeringPair_RealmRootAndWiki(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	wikiDir := filepath.Join(root, "wiki")
	putSteering(t, wikiDir, "AGENTS.md", realmSteeringScaffold)
	putSteering(t, wikiDir, "CLAUDE.md", loaderDoc)
	putSteering(t, root, "AGENTS.md", "## Capture surfaces\n")
	putSteering(t, root, "CLAUDE.md", loaderDoc)

	_, notes, err := ConvertSteeringPair(root, "realm", bundlespec.SteeringClaudeMD)
	if err != nil {
		t.Fatal(err)
	}
	expectPair(t, wikiDir, "", scaffoldDoc)
	if len(notes) != 1 || !strings.Contains(notes[0], root) {
		t.Errorf("notes = %v, want one for the realm root pair, whose capture section is user text", notes)
	}
	expectPair(t, root, "## Capture surfaces\n", loaderDoc)

	if _, _, err := ConvertSteeringPair(root, "realm", bundlespec.SteeringAgentsMD); err != nil {
		t.Fatal(err)
	}
	expectPair(t, wikiDir, realmSteeringScaffold, "")
	expectPair(t, root, "## Capture surfaces\n", "")
}

// Under loader the scaffold-less root pair creates no CLAUDE.md beside a plain
// AGENTS.md and leaves an owned loader byte-identical.
func TestConvertSteeringPair_RootLoaderModeCreatesNothing(t *testing.T) {
	for name, claude := range map[string]string{"no CLAUDE.md": "", "owned loader": loaderDoc} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			putSteering(t, root, "AGENTS.md", "# Project\n")
			if claude != "" {
				putSteering(t, root, "CLAUDE.md", claude)
			}
			changed, _, err := ConvertSteeringPair(root, "repo", bundlespec.SteeringLoader)
			if err != nil || len(changed) != 0 {
				t.Errorf("changed = %v, %v; want nothing", changed, err)
			}
			expectPair(t, root, "# Project\n", claude)
		})
	}
}

// An @-ref-only AGENTS.md (atomic-wiki references/repo.md Step 8) carries no
// <atomic> block; the loader beside it is still Atomic's and agents-md retires it.
func TestConvertSteeringPair_RepoRootLoaderBesideBlocklessAgents(t *testing.T) {
	root := t.TempDir()
	putSteering(t, root, "AGENTS.md", "# Project\n")
	putSteering(t, root, "CLAUDE.md", loaderDoc)

	changed, _, err := ConvertSteeringPair(root, "repo", bundlespec.SteeringAgentsMD)
	if err != nil {
		t.Fatal(err)
	}
	expectPair(t, root, "# Project\n", "")
	if len(changed) != 1 {
		t.Errorf("changed = %v, want the removed loader", changed)
	}
}

func TestConvertSteeringPair_SkipsAWikiWithNoPair(t *testing.T) {
	root := t.TempDir()
	changed, _, err := ConvertSteeringPair(root, "repo", bundlespec.SteeringClaudeMD)
	if err != nil || len(changed) != 0 {
		t.Errorf("ConvertSteeringPair = %v, %v; want nothing", changed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); err == nil {
		t.Error("conversion created docs/ for a repository that had no wiki pair")
	}
}

func TestCaptureSurfacesFile_FallsBackToTheModesFile(t *testing.T) {
	root := t.TempDir()
	if got := filepath.Base(captureSurfacesFile(root, bundlespec.SteeringClaudeMD)); got != "CLAUDE.md" {
		t.Errorf("claude-md fallback = %s, want CLAUDE.md", got)
	}
	if got := filepath.Base(captureSurfacesFile(root, bundlespec.SteeringLoader)); got != "AGENTS.md" {
		t.Errorf("loader fallback = %s, want AGENTS.md", got)
	}
}

// An unreadable steering config fails the writer rather than scaffolding loader.
func TestInitScopePair_FailsOnUnknownSteeringValue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atomic", "atomic.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[steering]\nclaude = \"nope\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, init := range map[string]func(string) ([]string, error){"repo": InitRepoScope, "realm": InitRealmScope} {
		if _, err := init(root); err == nil || !strings.Contains(err.Error(), `steering.claude "nope"`) {
			t.Errorf("%s: err = %v, want the named enum error", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); err == nil {
		t.Error("a steering pair was scaffolded despite the bad value")
	}
}

// Scan only creates a realm wiki pair, so a malformed atomic.toml cannot make
// it convert a claude-md pair to loader.
func TestWikiScan_LeavesAnExistingRealmWikiPairAlone(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	path := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("scope = \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	putSteering(t, wikiDir, "CLAUDE.md", scaffoldDoc)

	if code := wikiAction([]string{"scan", "--root=" + root}, home, steering(), root, io.Discard); code != 0 {
		t.Fatalf("scan exited %d", code)
	}
	expectPair(t, wikiDir, "", scaffoldDoc)
}
