package runtimeproof

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
)

// ompPayload returns the provider requests the payload probe captured. OMP
// builds the request before any model answer, so no credential is needed.
func ompPayload(t *testing.T, home OMPHome, name, workDir, prompt string, args ...string) string {
	t.Helper()
	probe, err := filepath.Abs(filepath.Join("testdata", "omp-payload-probe.ts"))
	if err != nil {
		t.Fatalf("probe path: %v", err)
	}
	out := filepath.Join(t.TempDir(), name+".jsonl")
	launch, err := home.Launch(OMPLaunch{Name: name, WorkDir: workDir, Prompt: prompt, Log: home.LogPath("atomic-runtime-" + name + ".jsonl")})
	if err != nil {
		t.Fatalf("%s launch: %v", name, err)
	}
	launch.Args = append(append(args, "--extension", probe), launch.Args...)
	launch.Env = append(launch.Env, "ATOMIC_OMP_PAYLOAD_PROBE="+out)
	obs, err := Run(context.Background(), launch)
	if err != nil {
		t.Fatalf("%s Run: %v", name, err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("%s: no provider request was captured: %v\nstdout: %s\nstderr: %s", name, err, obs.Stdout, obs.Stderr)
	}
	return string(data)
}

// probeCorpus is the runtime fixture corpus plus one command and one skill
// whose markers no other fixture artifact carries.
func probeCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", "omp-corpus")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeTree(t, dir, map[string]string{filepath.ToSlash(rel): string(data)})
		return nil
	})
	if err != nil {
		t.Fatalf("copy fixture corpus: %v", err)
	}
	writeTree(t, dir, map[string]string{
		"context/commands/atomic-probe.md":     "---\ndescription: Probe command.\n---\n\nPROBE_COMMAND_EXPANDED args=[$ARGUMENTS]\n",
		"context/skills/atomic-probe/SKILL.md": "---\nname: atomic-probe\ndescription: PROBE_SKILL_LISTED probe skill.\n---\n\nPROBE_SKILL_BODY\n",
	})
	cat, err := artifacts.Load(dir)
	if err != nil {
		t.Fatalf("load probe corpus: %v", err)
	}
	return cat
}

// TestOMPNativeKindDelivery probes each artifact kind against the layout an
// install writes; record changes in docs/research/harness-capability-matrix.md.
func TestOMPNativeKindDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	if _, err := OMPCLI(); err != nil {
		t.Skipf("omp is unavailable: %v", err)
	}
	if version, err := OMPVersion(); err == nil {
		t.Logf("observed %s", version)
	}
	dir := t.TempDir()
	adapter := runtimeAdapter(t)
	cat := probeCorpus(t)
	adapter.Corpus = func() (*artifacts.Catalog, error) { return cat, nil }
	home, _ := prepareOMPHome(t, filepath.Join(dir, "home"), OMPHomeRequest{Adapter: adapter})
	writeTree(t, home.Home, map[string]string{".atomic/profile.md": "PROFILE_IMPORT_MARKER\n"})
	writeTree(t, home.AgentRoot, map[string]string{
		"rules/atomic-probe-rule.md": "---\ndescription: PROBE_RULE_DESC\nglobs:\n  - \"src/**\"\n---\nPROBE_RULE_BODY\n",
	})
	direct := filepath.Join(dir, "direct")
	writeTree(t, direct, map[string]string{
		"AGENTS.md":          "# repo\n\n@docs/wiki/index.md\n",
		"docs/wiki/index.md": "WIKI_DIRECT_MARKER\n",
		"src/a.ts":           "export const a = 1;\n",
	})

	payload := ompPayload(t, home, "omp-kinds", direct, "/atomic-probe alpha beta", "--tools", "read")
	checks := []struct{ kind, want string }{
		{"command expansion with arguments", "PROBE_COMMAND_EXPANDED args=[alpha beta]"},
		{"skill name", "atomic-probe"},
		{"skill description", "PROBE_SKILL_LISTED"},
		{"profile @ import", "PROFILE_IMPORT_MARKER"},
		{"repository AGENTS.md @ import", "WIKI_DIRECT_MARKER"},
		{"globs rule listing", "PROBE_RULE_DESC"},
	}
	for _, c := range checks {
		if !strings.Contains(payload, c.want) {
			t.Errorf("%s: %q did not reach the provider request", c.kind, c.want)
		}
	}
	if strings.Contains(payload, "PROBE_SKILL_BODY") {
		t.Error("a skill body reached the opening request; skill discovery is a listing, revisit the skill-discovery row")
	}
	if strings.Contains(payload, "PROBE_RULE_BODY") {
		t.Error("a globs rule body reached the opening request; revisit the static-scoped-rule row")
	}

	// The router reaches the model only through the CLAUDE.md loader once the
	// repository AGENTS.md is disabled as a context file.
	loader := filepath.Join(dir, "loader")
	writeTree(t, loader, map[string]string{
		"AGENTS.md":          "# repo\n\n@docs/wiki/index.md\n",
		"CLAUDE.md":          "@AGENTS.md\n",
		"docs/wiki/index.md": "WIKI_LOADER_MARKER\n",
		"overlay.yml":        "disabledExtensions:\n  - context-file:project:AGENTS.md\n",
	})
	loaded := ompPayload(t, home, "omp-claude-loader", loader, "Reply OK.", "--no-tools", "--config", filepath.Join(loader, "overlay.yml"))
	if !strings.Contains(loaded, "WIKI_LOADER_MARKER") {
		t.Error("the router did not reach the request through CLAUDE.md -> @AGENTS.md")
	}
	if !strings.Contains(loaded, filepath.Join(loader, "CLAUDE.md")) {
		t.Error("the request does not name the CLAUDE.md loader as the context file")
	}

	// A missing import target stays in the request as its literal token.
	if err := os.Remove(filepath.Join(home.Home, ".atomic", "profile.md")); err != nil {
		t.Fatal(err)
	}
	missing := ompPayload(t, home, "omp-missing-import", direct, "Reply OK.", "--no-tools")
	if strings.Contains(missing, "PROFILE_IMPORT_MARKER") || !strings.Contains(missing, "@~/.atomic/profile.md") {
		t.Error("a missing profile import did not stay as the literal @~/.atomic/profile.md token")
	}
}
