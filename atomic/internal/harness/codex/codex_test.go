package codex

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

var update = flag.Bool("update", false, "regenerate golden testdata fixtures")

// fixtureCorpus is the small canonical corpus the artifacts package owns; using
// it keeps the package goldens readable and independent of the shipped corpus.
func fixtureCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	cat, err := artifacts.Load(filepath.Join("..", "..", "artifacts", "testdata", "repo"))
	if err != nil {
		t.Fatalf("load fixture corpus: %v", err)
	}
	return cat
}

// newAdapter returns an adapter over the fixture corpus and a fixed root, so no
// test depends on an installed Codex binary or the process environment.
func newAdapter(t *testing.T, root string) *Adapter {
	t.Helper()
	cat := fixtureCorpus(t)
	return &Adapter{
		ResolveHome: func(string) (string, error) { return root, nil },
		Corpus:      func() (*artifacts.Catalog, error) { return cat, nil },
	}
}

func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// checkGolden compares bytes against a committed golden, or rewrites it under
// -update.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run go test ./internal/harness/codex -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestAdapterReportsCodexKindAndCP0Capabilities(t *testing.T) {
	a := New()
	if a.Kind() != harness.KindCodex {
		t.Errorf("kind = %s", a.Kind())
	}
	caps := a.Capabilities()
	if caps.Root.Env != HomeEnv {
		t.Errorf("adapter root env %q disagrees with the CP0 row %q", HomeEnv, caps.Root.Env)
	}
	if caps.Root.DefaultDir != "" {
		t.Errorf("CP0 row claims a default root %q; the default was never exercised", caps.Root.DefaultDir)
	}
	for _, role := range []harness.Role{
		harness.RoleStaticScope,
		harness.RoleSessionBaseline,
		harness.RolePreOperationTargets,
		harness.RoleContextReturn,
		harness.RoleDeterministicDeny,
	} {
		if caps.Supports(role) {
			t.Errorf("role %s reads as supported; Codex 0.147.0 proved no hook event", role)
		}
	}
}

func TestDiscoverResolvesConfiguredRoot(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HomeEnv, root)

	instances, err := New().Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %+v, want exactly the configured CODEX_HOME", instances)
	}
	if instances[0].NativeRoot != root || !instances[0].Exists {
		t.Errorf("instance = %+v, want the existing configured root", instances[0])
	}
	for _, path := range []string{
		filepath.Join(home, ".atomic"),
		config.LedgerPath(home),
		config.PackageRoot(home, "codex"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("discovery created %s", path)
		}
	}
}

// TestDiscoverReportsUnprovenDefault proves an unset CODEX_HOME is an explicit
// unsupported answer, never a guess at the default root CP0 never exercised.
func TestDiscoverReportsUnprovenDefault(t *testing.T) {
	home := newHome(t)
	t.Setenv(HomeEnv, "")

	_, err := New().Discover(home)
	if err == nil {
		t.Fatal("discovery accepted an unset CODEX_HOME")
	}
	if !errors.Is(err, harness.ErrUnsupported) {
		t.Errorf("error %v does not report the surface unsupported", err)
	}
	if !strings.Contains(err.Error(), HomeEnv) {
		t.Errorf("error %q does not name the variable to set", err)
	}
}

func TestDiscoverRequiresHome(t *testing.T) {
	if _, err := New().Discover(""); err == nil {
		t.Error("discovery without a home accepted")
	}
}

// TestDefaultHomeResolvesRelativeValue proves a relocated root keeps its meaning
// when it is expressed relative to the OS home.
func TestDefaultHomeResolvesRelativeValue(t *testing.T) {
	home := newHome(t)
	t.Setenv(HomeEnv, "relative-codex")
	got, err := DefaultHome(home)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := filepath.Join(home, "relative-codex"); got != want {
		t.Errorf("root = %q, want %q", got, want)
	}
}
