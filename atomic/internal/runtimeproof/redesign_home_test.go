package runtimeproof

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// redesignHome is one isolated HOME, CODEX_HOME, and git repository that the
// built atomic binary installs into before a real OMP or Codex launch.
type redesignHome struct {
	atomic                      string
	root, home, codexHome, repo string
}

// newRedesignHome skips the row loudly when the built binary is unavailable;
// the caller checks for the harness binary itself.
func newRedesignHome(t *testing.T, ev ScenarioEvidence, repoFiles map[string]string) *redesignHome {
	t.Helper()
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real harness launch is skipped")
	}
	atomicBin, err := atomicBinary(t)
	if err != nil {
		recordScenarioSkip(t, ev, "atomic binary could not be built: "+err.Error())
	}
	root := isolatedHome(t)
	h := &redesignHome{
		atomic: atomicBin, root: root,
		home:      filepath.Join(root, "home"),
		codexHome: filepath.Join(root, "codex-home"),
		repo:      filepath.Join(root, "repo"),
	}
	writeTree(t, h.home, map[string]string{".atomic/profile.md": "# Runtimeproof profile\n"})
	if err := os.MkdirAll(h.codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, h.repo, repoFiles)
	guidanceGit(t, h.repo, "init", "-q", "-b", "main")
	return h
}

// env is the parent environment with every home, credential, and harness
// session marker replaced, so a binary run here sees only the isolated roots.
func (h *redesignHome) env() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch {
		case key == "HOME", key == "CODEX_HOME", key == "OMP_PROFILE", key == "CLAUDECODE",
			strings.HasPrefix(key, "PI_"), strings.HasPrefix(key, "CLAUDE_"), strings.HasPrefix(key, "ATOMIC_"),
			strings.HasPrefix(key, "ANTHROPIC_"), strings.HasPrefix(key, "OPENAI_"),
			carriesCredential(key), slices.Contains(harness.MarkerVars, key):
			continue
		}
		env = append(env, entry)
	}
	return append(env, "HOME="+h.home, "CODEX_HOME="+h.codexHome, "OMP_NO_UPDATE_CHECK=1", h.path())
}

// path puts the built binary first, because the installed hooks invoke atomic
// by name.
func (h *redesignHome) path() string {
	return "PATH=" + filepath.Dir(h.atomic) + string(os.PathListSeparator) + os.Getenv("PATH")
}

// launchOverrides win over launchEnv's entries on a duplicate key, so the real
// atomic replaces the stub and the parent session's harness markers are blanked.
func (h *redesignHome) launchOverrides() []string {
	env := []string{h.path(), "CODEX_HOME=" + h.codexHome}
	for _, name := range harness.MarkerVars {
		env = append(env, name+"=")
	}
	return env
}

func (h *redesignHome) run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(h.atomic, args...)
	cmd.Dir = dir
	cmd.Env = h.env()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *redesignHome) mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := h.run(t, dir, args...)
	if err != nil {
		t.Fatalf("atomic %v: %v\n%s", args, err, out)
	}
	return out
}

// version runs `<binary> --version` against the isolated home, never the
// developer's.
func (h *redesignHome) version(t *testing.T, binary string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.Dir = h.repo
	cmd.Env = h.env()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s --version: %v", binary, err)
	}
	return strings.TrimSpace(string(out))
}

// jsonStrings returns every string value in the JSON lines of data, decoded, so
// a check matches the text a harness sent rather than its escaped encoding.
func jsonStrings(t *testing.T, data string) []string {
	t.Helper()
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	for _, line := range strings.Split(data, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("captured line is not JSON: %v", err)
		}
		walk(v)
	}
	return out
}

func anyContains(values []string, want string) bool {
	for _, v := range values {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}

func frontmatter(doc string) string {
	front, _, _ := strings.Cut(strings.TrimPrefix(doc, "---\n"), "\n---")
	return "\n" + front + "\n"
}
