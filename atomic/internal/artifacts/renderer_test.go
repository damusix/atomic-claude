package artifacts

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate golden testdata fixtures")

func renderer(t *testing.T) *Renderer {
	t.Helper()
	return NewRenderer(loadFixture(t))
}

// checkGolden compares bytes against a committed golden, or rewrites it under
// -update. Deterministic renders are the point of these fixtures, so a diff is
// the failure output.
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
		t.Fatalf("read golden %s: %v (run go test ./internal/artifacts -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "loader", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// Claude consumes the authored contract directly: same bytes, no importer file
// of its own, and no reference to an AGENTS.md path.
func TestClaudeGlobal_DirectRenderedContract(t *testing.T) {
	r := renderer(t)
	cat := r.catalog

	steering, err := r.Steering()
	if err != nil {
		t.Fatalf("Steering: %v", err)
	}
	got, err := r.ClaudeGlobal()
	if err != nil {
		t.Fatalf("ClaudeGlobal: %v", err)
	}

	if got.Path != "CLAUDE.md" {
		t.Errorf("Path = %q, want %q", got.Path, "CLAUDE.md")
	}
	if got.Delivery != DeliveryDirect {
		t.Errorf("Delivery = %q, want %q", got.Delivery, DeliveryDirect)
	}
	if string(got.Bytes) != string(steering.Body) {
		t.Error("Claude global bytes are not the authored contract bytes")
	}
	if strings.Contains(string(got.Bytes), "AGENTS.md") {
		t.Error("Claude global body references AGENTS.md")
	}
	if !strings.Contains(string(got.Bytes), "@~/.atomic/profile.md") {
		t.Error("Claude global body lost the profile import")
	}
	if got.Digest != ProjectionDigest(got.Bytes) {
		t.Errorf("Digest = %q, want the digest of Bytes", got.Digest)
	}
	if _, ok := cat.Get(got.Artifact); !ok {
		t.Errorf("projection names unknown artifact %q", got.Artifact)
	}

	checkGolden(t, "claude-global.md", got.Bytes)
}

func TestLoaderPair_RepoAndRealm(t *testing.T) {
	r := renderer(t)

	cases := []struct {
		name     string
		dir      string
		guidance string
		source   string
	}{
		{"repo", ".", "repo-guidance.md", "repo"},
		{"realm", "wiki", "wiki-guidance.md", "realm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guidance := readFixture(t, tc.guidance)
			pair, err := r.LoaderPair(tc.dir, guidance)
			if err != nil {
				t.Fatalf("LoaderPair: %v", err)
			}
			if len(pair) != 2 {
				t.Fatalf("pair len = %d, want 2", len(pair))
			}

			source, loader := pair[0], pair[1]
			if source.Delivery != DeliveryShared {
				t.Errorf("source Delivery = %q, want %q", source.Delivery, DeliveryShared)
			}
			if loader.Delivery != DeliveryLoader {
				t.Errorf("loader Delivery = %q, want %q", loader.Delivery, DeliveryLoader)
			}
			if filepath.Base(source.Path) != "AGENTS.md" {
				t.Errorf("source Path = %q, want an adjacent AGENTS.md", source.Path)
			}
			if filepath.Base(loader.Path) != "CLAUDE.md" {
				t.Errorf("loader Path = %q, want an adjacent CLAUDE.md", loader.Path)
			}
			if filepath.Dir(source.Path) != filepath.Dir(loader.Path) {
				t.Errorf("loader %q is not adjacent to source %q", loader.Path, source.Path)
			}
			if string(loader.Bytes) != "@AGENTS.md\n" {
				t.Errorf("loader body = %q, want %q", loader.Bytes, "@AGENTS.md\n")
			}
			if loader.Digest != ProjectionDigest(loader.Bytes) {
				t.Errorf("loader Digest = %q, want the digest of Bytes", loader.Digest)
			}
			if got := string(source.Bytes); strings.HasSuffix(got, "\n\n") {
				t.Errorf("source bytes carry more than one trailing newline: %q", got)
			}

			checkGolden(t, tc.source+"-AGENTS.md", source.Bytes)
			checkGolden(t, tc.source+"-CLAUDE.md", loader.Bytes)
		})
	}
}

// OMP gets the Atomic-owned content inline: the steering body with imports
// dropped, one blank line, then the output style without its harness
// frontmatter.
func TestOMPSteering_ComposedGolden(t *testing.T) {
	r := renderer(t)

	got, err := r.OMPSteering()
	if err != nil {
		t.Fatalf("OMPSteering: %v", err)
	}

	if got.Path != "AGENTS.md" {
		t.Errorf("Path = %q, want %q", got.Path, "AGENTS.md")
	}
	if got.Delivery != DeliveryComposed {
		t.Errorf("Delivery = %q, want %q", got.Delivery, DeliveryComposed)
	}
	if strings.Contains(string(got.Bytes), "keep-coding-instructions") {
		t.Error("OMP steering kept the output style's parsed YAML frontmatter")
	}
	if !strings.Contains(string(got.Bytes), "You respond in atomic style.") {
		t.Error("OMP steering did not compose the output-style body")
	}
	if !strings.Contains(string(got.Bytes), "\n\n# Cut\n") {
		t.Error("OMP steering did not separate the composed bodies with one blank line")
	}
	for _, line := range strings.Split(string(got.Bytes), "\n") {
		if isImportDirective(line) {
			t.Errorf("OMP steering still carries an import directive: %q", line)
		}
	}
	if got.Digest != ProjectionDigest(got.Bytes) {
		t.Errorf("Digest = %q, want the digest of Bytes", got.Digest)
	}

	checkGolden(t, "omp-steering.md", got.Bytes)
}

// Same corpus, same generation, same bytes and digests — every render.
func TestRender_Deterministic(t *testing.T) {
	first := renderer(t)
	second := renderer(t)

	globalA, err := first.ClaudeGlobal()
	if err != nil {
		t.Fatalf("ClaudeGlobal: %v", err)
	}
	globalB, err := second.ClaudeGlobal()
	if err != nil {
		t.Fatalf("ClaudeGlobal: %v", err)
	}
	if globalA.Digest != globalB.Digest || string(globalA.Bytes) != string(globalB.Bytes) {
		t.Error("Claude global render is not stable across runs")
	}

	ompA, err := first.OMPSteering()
	if err != nil {
		t.Fatalf("OMPSteering: %v", err)
	}
	ompB, err := second.OMPSteering()
	if err != nil {
		t.Fatalf("OMPSteering: %v", err)
	}
	if ompA.Digest != ompB.Digest || string(ompA.Bytes) != string(ompB.Bytes) {
		t.Error("OMP steering render is not stable across runs")
	}

	pairA, err := first.LoaderPair("wiki", readFixture(t, "wiki-guidance.md"))
	if err != nil {
		t.Fatalf("LoaderPair: %v", err)
	}
	pairB, err := second.LoaderPair("wiki", readFixture(t, "wiki-guidance.md"))
	if err != nil {
		t.Fatalf("LoaderPair: %v", err)
	}
	for i := range pairA {
		if pairA[i].Digest != pairB[i].Digest || string(pairA[i].Bytes) != string(pairB[i].Bytes) {
			t.Errorf("loader pair projection %d is not stable across runs", i)
		}
	}
}

func TestImportFree(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"separated import", "## Profile\n\n@~/.atomic/profile.md\n\nFacts.\n", "## Profile\n\nFacts.\n"},
		{"trailing import", "## Profile\n\nFacts.\n\n@handles.md\n", "## Profile\n\nFacts.\n"},
		{"no import", "## Profile\n\nFacts.\n", "## Profile\n\nFacts.\n"},
		{"prose mention kept", "Mail bob@host.com about @handles today.\n", "Mail bob@host.com about @handles today.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(ImportFree([]byte(tc.in))); got != tc.want {
				t.Errorf("ImportFree(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
