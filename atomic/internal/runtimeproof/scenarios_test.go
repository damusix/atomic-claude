// Each scenario drives a production engine against a home it created and asserts
// the outcome a consumer observes, never plumbing; the recorder retains one record each.
package runtimeproof

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ScenarioEvidence is one retained proof record. Digest is its own content
// identity, so a reviewer can tell whether it changed between runs.
type ScenarioEvidence struct {
	Scenario   string   `json:"scenario"`
	Group      string   `json:"group"`
	Engine     string   `json:"engine"`
	Criterion  string   `json:"criterion"`
	Command    string   `json:"command"`
	Paths      []string `json:"isolated_paths,omitempty"`
	Outcome    string   `json:"outcome"`
	Status     string   `json:"status"`
	Detail     string   `json:"detail,omitempty"`
	Digest     string   `json:"digest"`
	RecordedAt string   `json:"recorded_at"`
}

// scenarioRecorder collects every scenario record and writes the matrix index once
// the package's tests have all run.
var scenarioRecorder = struct {
	mu      sync.Mutex
	records []ScenarioEvidence
	root    string
}{}

// Keeps scenarios off the real home. The Go caches default to paths under HOME,
// so they are pinned first or the nested `go build` runs cold.
func TestMain(m *testing.M) {
	for _, name := range []string{"GOMODCACHE", "GOCACHE"} {
		out, err := exec.Command("go", "env", name).Output()
		if err != nil {
			panic(err)
		}
		if err := os.Setenv(name, strings.TrimSpace(string(out))); err != nil {
			panic(err)
		}
	}
	home, err := os.MkdirTemp("", "runtimeproof-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	if err := os.Unsetenv(config.StateDirEnvVar); err != nil {
		panic(err)
	}
	restore := config.SetHomeDirForTest(home)
	scenarioRecorder.root = resolveEvidenceRoot()
	code := m.Run()
	writeEvidenceIndex()
	restore()
	if err := os.RemoveAll(home); err != nil {
		panic(err)
	}
	os.Exit(code)
}

// resolveEvidenceRoot picks the override, else the scratchpad bundle, else a
// directory beside the test, so a fresh clone or CI needs no scratchpad.
func resolveEvidenceRoot() string {
	if override := strings.TrimSpace(os.Getenv("ATOMIC_RUNTIMEPROOF_EVIDENCE")); override != "" {
		return override
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for probe := dir; ; {
		bundle := filepath.Join(probe, ".atomic", ".scratchpad", "omp-plugin-compatibility")
		if st, err := os.Stat(bundle); err == nil && st.IsDir() {
			return filepath.Join(bundle, "runtime")
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return filepath.Join(dir, "testdata", "evidence")
}

// A repository migrated to .atomic keeps its scratchpad there, so evidence must
// land in that bundle rather than beside the test.
func TestEvidenceRootResolvesTheAtomicScratchpadBundle(t *testing.T) {
	repo := isolatedHome(t)
	bundle := filepath.Join(repo, ".atomic", ".scratchpad", "omp-plugin-compatibility")
	writeTree(t, bundle, map[string]string{"BRIEF.md": "# brief\n"})
	writeTree(t, repo, map[string]string{"atomic/internal/runtimeproof/x.go": "package runtimeproof\n"})
	t.Setenv("ATOMIC_RUNTIMEPROOF_EVIDENCE", "")
	t.Chdir(filepath.Join(repo, "atomic", "internal", "runtimeproof"))

	if got, want := resolveEvidenceRoot(), filepath.Join(bundle, "runtime"); got != want {
		t.Errorf("evidence root = %q, want %q", got, want)
	}
}

// recordScenario retains one scenario's evidence from a cleanup, so the record
// carries the test's real outcome and a failed scenario never records pass.
func recordScenario(t *testing.T, ev ScenarioEvidence) {
	t.Helper()
	if ev.Status == "" {
		ev.Status = "pass"
	}
	ev.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	t.Cleanup(func() {
		// A skip is not a failure, so a recorded skip keeps its status.
		if t.Failed() && ev.Status == "pass" {
			ev.Status = "failed"
			if ev.Detail == "" {
				ev.Detail = "the scenario's assertions failed; see the test output"
			}
		}
		ev.Digest = evidenceDigest(ev)
		persistScenario(t, ev)
	})
}

// persistScenario writes one record beside the matrix index and collects it for
// the index.
func persistScenario(t *testing.T, ev ScenarioEvidence) {
	t.Helper()
	root := scenarioRecorder.root
	if root == "" {
		t.Logf("scenario %s: no evidence root resolvable; record kept in-process only", ev.Scenario)
	} else if err := os.MkdirAll(root, 0o755); err != nil {
		t.Errorf("create evidence root: %v", err)
	} else if data, err := json.MarshalIndent(ev, "", "  "); err != nil {
		t.Errorf("marshal evidence: %v", err)
	} else {
		path := filepath.Join(root, safeEvidenceName(ev.Scenario)+".json")
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Errorf("write evidence %s: %v", path, err)
		}
	}

	scenarioRecorder.mu.Lock()
	scenarioRecorder.records = append(scenarioRecorder.records, ev)
	scenarioRecorder.mu.Unlock()
}

// recordSkip retains a skip with its reason and stops the test loudly, so an
// unexercisable environment is listed as uncovered.
func recordScenarioSkip(t *testing.T, ev ScenarioEvidence, reason string) {
	t.Helper()
	ev.Status = "skip"
	ev.Detail = reason
	recordScenario(t, ev)
	t.Skip(reason)
}

// evidenceDigest hashes the record's canonical content without its own digest
// field, so a re-run's record is comparable to the retained one.
func evidenceDigest(ev ScenarioEvidence) string {
	ev.Digest = ""
	data, err := json.Marshal(ev)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// safeEvidenceName maps a scenario name to one filesystem-safe file stem.
func safeEvidenceName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// writeEvidenceIndex rebuilds the matrix from the records on disk, so a filtered
// or repeated run cannot shrink or double-count it.
func writeEvidenceIndex() {
	root := scenarioRecorder.root
	if root == "" {
		return
	}
	var records []ScenarioEvidence
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "matrix.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		var ev ScenarioEvidence
		if json.Unmarshal(data, &ev) == nil && ev.Scenario != "" {
			records = append(records, ev)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Scenario < records[j].Scenario })

	index := struct {
		Schema    string             `json:"schema"`
		Count     int                `json:"count"`
		Passed    int                `json:"passed"`
		Failed    int                `json:"failed"`
		Skipped   int                `json:"skipped"`
		Scenarios []ScenarioEvidence `json:"scenarios"`
	}{Schema: "runtimeproof-scenario-matrix/1", Count: len(records), Scenarios: records}
	for _, r := range records {
		switch r.Status {
		case "pass":
			index.Passed++
		case "skip":
			index.Skipped++
		default:
			index.Failed++
		}
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(root, "matrix.json"), append(data, '\n'), 0o644)
}

// isolatedHome returns a symlink-resolved temporary home so every path a
// scenario compares is the canonical one the harnesses report.
func isolatedHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root
}

// legacyClaudeInstall runs the real Claude installer so a migration fixture
// carries the installer's own output shape.
func legacyClaudeInstall(t *testing.T, home string) string {
	t.Helper()
	restore := claudeinstall.ProfileRefresh
	claudeinstall.ProfileRefresh = func(string, string, int) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.ProfileRefresh = restore })

	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A pre-existing settings.json is what the write-once snapshot records a copy
	// of; on a fresh home every other listed file is absent.
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"model":"sonnet"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := claudeinstall.Install(root, home, false, claudeinstall.RealClock); err != nil {
		t.Fatalf("legacy install: %v", err)
	}
	return root
}

// claudeArtifacts builds the selected-generation adoption artifacts exactly as
// the Claude adapter hands them to the migration engine.
func claudeArtifacts(t *testing.T, nativeRoot string) []installstate.Artifact {
	t.Helper()
	var out []installstate.Artifact
	for _, a := range embedded.Manifest() {
		data, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			t.Fatalf("read embedded %s: %v", a.Source, err)
		}
		kind := managedfile.KindFile
		if a.Kind == "claude-md" {
			kind = managedfile.KindBlock
		}
		out = append(out, installstate.Artifact{
			ID:   a.Target,
			Kind: kind,
			Path: filepath.Join(nativeRoot, filepath.FromSlash(a.Target)),
			Data: data,
		})
	}
	return out
}

// ageClaudeInstall turns the install into a complete prior version whose bytes
// the selected generation cannot prove ownership of.
func ageClaudeInstall(t *testing.T, home, nativeRoot string) {
	t.Helper()
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" {
			continue
		}
		path := filepath.Join(nativeRoot, filepath.FromSlash(a.Target))
		if err := os.WriteFile(path, []byte("older "+a.Target+"\n"), 0o644); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}
	ageClaudeSteeringBlock(t, nativeRoot)

	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Install.Version = "0.1.0"
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("record older install version: %v", err)
	}
}

// ageClaudeSteeringBlock replaces only the managed block, so adoption must
// splice the block rather than clobber the file.
func ageClaudeSteeringBlock(t *testing.T, nativeRoot string) {
	t.Helper()
	path := filepath.Join(nativeRoot, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "user prose before the block\n") {
		data = append([]byte("user prose before the block\n"), data...)
	}
	if !strings.HasSuffix(string(data), "user prose after the block\n") {
		data = append(data, []byte("user prose after the block\n")...)
	}
	aged, err := managedfile.ReplaceBlock(data, []byte("<atomic>\nolder atomic body\n</atomic>\n"))
	if err != nil {
		t.Fatalf("age block: %v", err)
	}
	if err := os.WriteFile(path, aged, 0o644); err != nil {
		t.Fatal(err)
	}
}

// deleteClaudeListed removes one [install]-listed artifact, producing structural
// drift on an otherwise complete install.
func deleteClaudeListed(t *testing.T, nativeRoot, target string) {
	t.Helper()
	if err := os.Remove(filepath.Join(nativeRoot, filepath.FromSlash(target))); err != nil {
		t.Fatalf("delete %s: %v", target, err)
	}
}

// writeClaudeProposal creates the legacy diverged-CLAUDE.md proposal file.
func writeClaudeProposal(t *testing.T, home string) {
	t.Helper()
	path := config.ProposedCLAUDEMD(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proposed steering\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// corruptClaudeSnapshot removes one recorded pre-install copy so the manifest no
// longer matches its own copies.
func corruptClaudeSnapshot(t *testing.T, home string) {
	t.Helper()
	dir := config.PreInstallDir(home)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, e := range entries {
		if e.IsDir() || e.Name() == "manifest.json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
		removed = true
		break
	}
	if !removed {
		t.Fatalf("no pre-install snapshot copy to corrupt under %s", dir)
	}
}

// treeDigest hashes every file under root, path-qualified and sorted, so a
// byte-identical filesystem has an identical digest.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			lines = append(lines, "d "+path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, "f "+path+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// fileExists reports whether path is a regular file.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
