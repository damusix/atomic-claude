package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

const loaderBlockText = "<atomic>\n\n@AGENTS.md\n\n</atomic>\n"

func writeSteeringTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSteeringTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// Changing the mode in atomic.toml and re-running converges every pair at the
// working repository: the root guidance and the docs/wiki steering.
func TestConvergeSteeringModeConvertsRootAndWikiPairs(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	if _, err := wiki.InitRepoScope(root); err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, filepath.Join(root, "AGENTS.md"), "mine\n<atomic>\nguidance\n</atomic>\n")
	writeSteeringTestFile(t, filepath.Join(root, "CLAUDE.md"), loaderBlockText)
	writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"repo\"\n\n[steering]\nclaude = \"claude-md\"\n")

	var out strings.Builder
	if err := convergeSteeringMode(home, root, &out); err != nil {
		t.Fatalf("convergeSteeringMode: %v", err)
	}

	if got := readSteeringTestFile(t, filepath.Join(root, "AGENTS.md")); got != "mine\n" {
		t.Errorf("root AGENTS.md = %q, want only the user text", got)
	}
	rootClaude := readSteeringTestFile(t, filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(rootClaude, "guidance") || strings.Contains(rootClaude, "@AGENTS.md") {
		t.Errorf("root CLAUDE.md = %q, want the inline guidance and no loader", rootClaude)
	}
	if got := readSteeringTestFile(t, filepath.Join(root, "docs", "wiki", "AGENTS.md")); got != "" {
		t.Errorf("docs/wiki/AGENTS.md = %q, want the unmodified scaffold retired", got)
	}
	if got := readSteeringTestFile(t, filepath.Join(root, "docs", "wiki", "CLAUDE.md")); !strings.Contains(got, "Authoritative steering") {
		t.Errorf("docs/wiki/CLAUDE.md = %q, want the scaffold block", got)
	}
	if !strings.Contains(out.String(), "steering (claude-md)") {
		t.Errorf("no conversion notice:\n%s", out.String())
	}
}

// A realm converts the same way: its wiki pair and the realm-root pair.
func TestConvergeSteeringModeConvertsRealmPairs(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n\n[steering]\nclaude = \"agents-md\"\n")
	writeSteeringTestFile(t, filepath.Join(root, "wiki", "AGENTS.md"), "@index.md\n")
	writeSteeringTestFile(t, filepath.Join(root, "wiki", "CLAUDE.md"), loaderBlockText)
	writeSteeringTestFile(t, filepath.Join(root, "AGENTS.md"), "## Capture surfaces\n")
	writeSteeringTestFile(t, filepath.Join(root, "CLAUDE.md"), loaderBlockText)

	if err := convergeSteeringMode(home, root, io.Discard); err != nil {
		t.Fatalf("convergeSteeringMode: %v", err)
	}
	for _, dir := range []string{root, filepath.Join(root, "wiki")} {
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
			t.Errorf("%s/CLAUDE.md survives agents-md, hiding AGENTS.md from Claude", dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Errorf("%s/AGENTS.md lost: %v", dir, err)
		}
	}
}

// A pair beside the global Claude steering is never converted, even in the
// default loader mode, because that would write the pair into ~/.claude.
func TestConvergeSteeringModeSkipsTheClaudeNativeRoot(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, ".claude")
	writeSteeringTestFile(t, filepath.Join(native, "AGENTS.md"), "<atomic>\nx\n</atomic>\n")
	var out bytes.Buffer
	if err := convergeSteeringMode(home, native, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(native, "CLAUDE.md")); err == nil {
		t.Error("a CLAUDE.md loader was written into the Claude native root")
	}
	if !strings.Contains(out.String(), "native root") {
		t.Errorf("no skip notice:\n%s", out.String())
	}
}

func TestConvergeSteeringModeRejectsUnknownValue(t *testing.T) {
	root := t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(root), "[steering]\nclaude = \"nope\"\n")
	err := convergeSteeringMode(t.TempDir(), root, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `steering.claude "nope"`) {
		t.Errorf("err = %v, want the named enum error", err)
	}
}

// The realm <code-index> block belongs to Atomic, so a mode switch moves it to
// the file the mode makes Claude load and lets an emptied CLAUDE.md go.
func TestConvergeSteeringModeMovesTheCodeIndexBlock(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	const block = "<code-index>\n<member key=\"a\" path=\"repos/a\" />\n</code-index>"
	oldStub := "\n<!-- Realm CLAUDE.md — managed by `atomic code index`. Edit below this block freely. -->\n"
	writeSteeringTestFile(t, filepath.Join(root, "AGENTS.md"), "## Capture surfaces\n")
	writeSteeringTestFile(t, filepath.Join(root, "CLAUDE.md"), loaderBlockText+"\n"+block+"\n"+oldStub)
	cfg := func(mode string) {
		writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n\n[steering]\nclaude = \""+mode+"\"\n")
	}

	cfg("agents-md")
	if err := convergeSteeringMode(home, root, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Errorf("CLAUDE.md survives agents-md with %q, hiding AGENTS.md", readSteeringTestFile(t, filepath.Join(root, "CLAUDE.md")))
	}
	if got := readSteeringTestFile(t, filepath.Join(root, "AGENTS.md")); !strings.Contains(got, block) || !strings.Contains(got, "## Capture surfaces") {
		t.Errorf("AGENTS.md = %q, want the capture section and the code-index block", got)
	}
	if r := doctor.RunCheckSteeringWith(root, home); r.Severity != doctor.PASS {
		t.Errorf("doctor steering = %v: %s", r.Severity, r.Detail)
	}

	cfg("loader")
	if err := convergeSteeringMode(home, root, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := readSteeringTestFile(t, filepath.Join(root, "CLAUDE.md")); !strings.Contains(got, block) {
		t.Errorf("CLAUDE.md = %q, want the code-index block back", got)
	}
	if path := bundlespec.ScopeSteering.LoaderWithoutImport(root); path != "" {
		t.Errorf("%s does not import AGENTS.md under loader: %q", path, readSteeringTestFile(t, path))
	}
	if got := readSteeringTestFile(t, filepath.Join(root, "AGENTS.md")); strings.Contains(got, "<code-index>") || !strings.Contains(got, "## Capture surfaces") {
		t.Errorf("AGENTS.md = %q, want the capture section and no code-index block", got)
	}
}

// An invalid value names the key on stderr and skips conversion, but update
// still finishes its other steps and then exits 1; --skip-claude-update skips it.
func TestUpdateReportsInvalidSteeringModeAndFinishes(t *testing.T) {
	repo := t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(repo), "[steering]\nclaude = \"nope\"\n")
	writeSteeringTestFile(t, filepath.Join(repo, "AGENTS.md"), "<atomic>\nx\n</atomic>\n")
	migrated := false
	deps := stubUpdateDeps(t)
	deps.converge = func(string, io.Writer) error { return nil }
	deps.migrate = func(string) error { migrated = true; return nil }
	deps.repoRoot = func() (string, bool) { return repo, true }
	var errOut strings.Builder
	deps.errOut = &errOut

	if code := runUpdatePostSwap(t.TempDir(), true, false, deps); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "steering.claude") || !migrated {
		t.Errorf("stderr = %q, migrated = %v; want the key named and later steps run", errOut.String(), migrated)
	}

	errOut.Reset()
	if code := runUpdatePostSwap(t.TempDir(), true, true, deps); code != 0 {
		t.Errorf("--skip-claude-update exit = %d, want 0 with steering skipped", code)
	}
}

// A realm root need not be a Git repository for update to find it.
func TestCwdSteeringRootFindsANonGitRealm(t *testing.T) {
	realm, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, config.RepoConfigPath(realm), "scope = \"realm\"\n")
	t.Chdir(realm)
	if got, ok := cwdSteeringRoot("", false); !ok || got != realm {
		t.Errorf("cwdSteeringRoot = %q, %v; want %q", got, ok, realm)
	}
	if got, ok := cwdSteeringRoot("/repo", true); !ok || got != "/repo" {
		t.Errorf("cwdSteeringRoot with a repo = %q, %v", got, ok)
	}
}
