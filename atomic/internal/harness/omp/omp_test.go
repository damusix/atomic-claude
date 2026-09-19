package omp

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
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

// newAdapter returns an adapter over the fixture corpus and a directory mapper,
// so no test depends on an installed OMP binary.
func newAdapter(t *testing.T, roots map[string]string) *Adapter {
	t.Helper()
	cat := fixtureCorpus(t)
	return &Adapter{
		ConfigPath: func(home, profile string) (string, error) {
			root, ok := roots[profile]
			if !ok {
				return "", fmt.Errorf("stub: no root for profile %q", profile)
			}
			return root, nil
		},
		Corpus: func() (*artifacts.Catalog, error) { return cat, nil },
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
		t.Fatalf("read golden %s: %v (run go test ./internal/harness/omp -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestAdapterReportsOMPKindAndCP0Capabilities(t *testing.T) {
	a := New()
	if a.Kind() != harness.KindOMP {
		t.Errorf("kind = %s", a.Kind())
	}
	caps := a.Capabilities()
	if caps.Root.DefaultDir != DefaultAgentDir {
		t.Errorf("adapter default root %q disagrees with the CP0 row %q", DefaultAgentDir, caps.Root.DefaultDir)
	}
	// The lifecycle cutover binds every hook: a profile target must project the
	// shared package and steering claims rather than report an unwired surface.
	home := newHome(t)
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := newAdapter(t, map[string]string{"": root})
	target := harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}
	plan, err := adapter.Lifecycle(home).Project(target, harness.PlanRequest{})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(plan.Claims) != 2 || plan.Generation == "" {
		t.Errorf("project plan = %+v, want the package and steering claims", plan)
	}
}

func TestDiscoverResolvesDefaultAndNamedProfiles(t *testing.T) {
	home := newHome(t)
	named := filepath.Join(home, ".omp", "profiles", "work", "agent")
	if err := os.MkdirAll(named, 0o755); err != nil {
		t.Fatal(err)
	}
	roots := map[string]string{
		"":     filepath.Join(home, ".omp", "agent"),
		"work": named,
	}
	a := newAdapter(t, roots)
	a.ProfileNames = func(string) []string { return []string{"", "work"} }

	instances, err := a.Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %+v, want the default and named profile roots", instances)
	}
	if instances[0].NativeRoot != roots[""] || instances[0].Exists {
		t.Errorf("default instance = %+v", instances[0])
	}
	if instances[1].NativeRoot != named || !instances[1].Exists {
		t.Errorf("named instance = %+v", instances[1])
	}
	if _, err := a.Discover(""); err == nil {
		t.Error("discovery without a home accepted")
	}
	// Discovery is read-only: it must not create enrollment, package, or journal
	// state.
	for _, path := range []string{
		filepath.Join(home, ".atomic"),
		config.LedgerPath(home),
		config.PackageRoot(home, "omp"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("discovery created %s", path)
		}
	}
}

// TestDefaultConfigPathEndToEnd drives root resolution through a real HOME and
// a real process: the adapter runs the CP0-proven `omp config path` command, so
// the named-profile selector travels as the environment OMP documents.
func TestDefaultConfigPathEndToEnd(t *testing.T) {
	home := newHome(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ -n \"$OMP_PROFILE\" ]; then\n" +
		"  printf '%s\\n' \"$HOME/.omp/profiles/$OMP_PROFILE/agent\"\n" +
		"else\n" +
		"  printf '%s\\n' \"$HOME/.omp/agent\"\n" +
		"fi\n"
	if err := os.WriteFile(filepath.Join(bin, "omp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OMP_PROFILE", "stale-ambient-value")

	wantDefault := filepath.Join(home, ".omp", "agent")
	got, err := DefaultConfigPath(home, "")
	if err != nil {
		t.Fatalf("default profile: %v", err)
	}
	if got != wantDefault {
		t.Errorf("default root = %q, want %q (an ambient OMP_PROFILE must not leak in)", got, wantDefault)
	}

	wantNamed := filepath.Join(home, ".omp", "profiles", "work", "agent")
	got, err = DefaultConfigPath(home, "work")
	if err != nil {
		t.Fatalf("named profile: %v", err)
	}
	if got != wantNamed {
		t.Errorf("named root = %q, want %q", got, wantNamed)
	}

	// A command that runs and answers ambiguously is refused, never guessed at.
	bad := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "omp"), []byte("#!/bin/sh\nprintf 'one\\ntwo\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bad)
	if _, err := DefaultConfigPath(home, ""); err == nil {
		t.Error("an ambiguous config-path answer was accepted")
	}
}

func TestBuildPackageIsDeterministicAndExcludesSteering(t *testing.T) {
	cat := fixtureCorpus(t)
	first, err := BuildPackage(cat, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := BuildPackage(cat, harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if first.Generation != second.Generation {
		t.Errorf("generation %s != %s", first.Generation, second.Generation)
	}
	if strings.Join(first.Paths(), ",") != strings.Join(second.Paths(), ",") {
		t.Errorf("package paths differ between builds")
	}
	for i := range first.Files {
		if string(first.Files[i].Bytes) != string(second.Files[i].Bytes) || first.Files[i].Digest != second.Files[i].Digest {
			t.Errorf("file %s is not deterministic", first.Files[i].Path)
		}
	}

	for _, f := range first.Files {
		switch {
		case strings.HasPrefix(f.Path, "output-styles/"):
			t.Errorf("package carries the output style at %s; global steering and style are never package artifacts", f.Path)
		case strings.Contains(f.Path, "AGENTS.md"):
			t.Errorf("package carries steering at %s", f.Path)
		case !strings.HasPrefix(f.Path, "commands/") && !strings.HasPrefix(f.Path, "agents/") &&
			!strings.HasPrefix(f.Path, "skills/") && !strings.HasPrefix(f.Path, "rules/") && f.Path != SkeletonPath:
			t.Errorf("package carries an unexpected path %s", f.Path)
		}
	}
	if _, ok := findFile(first, "rules/typescript/style.md"); !ok {
		t.Error("package is missing an actual path-scoped rule")
	}
	if _, ok := findFile(first, SkeletonPath); !ok {
		t.Error("package is missing the extension entry point")
	}
	for _, f := range first.Files {
		if f.Tier != Tier {
			t.Errorf("file %s claims tier %q; the CP0 record proves instruction-only delivery", f.Path, f.Tier)
		}
	}
	if len(first.Gaps) == 0 || len(first.Unproven) == 0 {
		t.Error("package reports no capability gap; OMP registered no package in CP0")
	}
}

func findFile(p Package, path string) (PackageFile, bool) {
	for _, f := range p.Files {
		if f.Path == path {
			return f, true
		}
	}
	return PackageFile{}, false
}

// TestPackageTreeGolden pins the generated tree's identity: every file's path,
// digest, and tier in order.
func TestPackageTreeGolden(t *testing.T) {
	pkg, err := BuildPackage(fixtureCorpus(t), harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "generation %s\n", pkg.Generation)
	for _, f := range pkg.Files {
		tier := f.Tier
		if tier == "" {
			tier = "unsupported"
		}
		fmt.Fprintf(&b, "%s %s %s\n", f.Path, f.Digest, tier)
	}
	for _, gap := range pkg.Gaps {
		fmt.Fprintf(&b, "gap %s %s\n", gap.Role, gap.Status)
	}
	for _, gap := range pkg.Unproven {
		fmt.Fprintf(&b, "unproven %s\n", gap.Surface)
	}
	checkGolden(t, "package.txt", []byte(b.String()))
}

// TestUnprovenSurfacesHaveSingleOwner pins every unproven surface to one
// reporting surface: the package gaps and the runtime delivery gaps never name
// the same surface, so extension disablement and the matched-body absence are
// reported once, by the runtime delivery that owns them.
func TestUnprovenSurfacesHaveSingleOwner(t *testing.T) {
	pkg, err := BuildPackage(fixtureCorpus(t), harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	owner := map[string]string{}
	for _, gap := range pkg.Unproven {
		owner[gap.Surface] = "package"
	}
	for _, gap := range pkg.Runtime.Unproven {
		if first, ok := owner[gap.Surface]; ok {
			t.Errorf("unproven surface %q is reported by both the %s gaps and the runtime delivery", gap.Surface, first)
			continue
		}
		owner[gap.Surface] = "runtime"
	}
	for _, surface := range []string{"extension disablement", "pre-operation matched-body delivery", "repository wiki card runtime index"} {
		if got := owner[surface]; got != "runtime" {
			t.Errorf("runtime surface %q is owned by %q, want the runtime delivery", surface, got)
		}
	}
}

func TestPackageCarriesNoProviderIDs(t *testing.T) {
	pkg, err := BuildPackage(fixtureCorpus(t), harness.OMPCapabilities())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, f := range pkg.Files {
		if ProviderID(string(f.Bytes)) {
			t.Errorf("package file %s carries a concrete provider or model identifier", f.Path)
		}
		for _, dropped := range f.Unsupported {
			if ProviderID(dropped) {
				t.Errorf("package file %s reports provider identifier %q", f.Path, dropped)
			}
		}
	}

	defaults, gaps := RoleDefaults(harness.OMPCapabilities(), []RolePreference{{Role: ModelPlan, Preference: "reasoning"}, {Role: ModelSmol, Preference: "fast"}})
	if len(defaults) != 0 {
		t.Errorf("role defaults = %+v, want none while the surface is unproven", defaults)
	}
	if len(gaps) != 1 || gaps[0].Role != harness.RoleModelDefaults {
		t.Errorf("role-default gaps = %+v, want the unproven model-defaults row", gaps)
	}
}

// providerCorpus builds a one-artifact corpus whose body names a concrete
// provider or model, so package generation must refuse it.
func providerCorpus(t *testing.T, kind artifacts.Kind, source string, body string) *artifacts.Catalog {
	t.Helper()
	cat, err := artifacts.NewCatalog([]artifacts.Artifact{{
		ID:     string(kind) + ":" + source,
		Kind:   kind,
		Source: source,
		Body:   []byte(body),
	}})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return cat
}

// TestBuildPackageRejectsProviderIDCommand proves the enforced sweep closes the
// hole the unit test alone left open: a provider or model identifier in a
// shipped command body fails generation, naming the offending artifact, instead
// of entering the package silently.
func TestBuildPackageRejectsProviderIDCommand(t *testing.T) {
	cat := providerCorpus(t, artifacts.KindCommand, "commands/provider.md",
		"---\ndescription: provider fixture\n---\n\nCall `anthropic/opus` for this step.\n")
	_, err := BuildPackage(cat, harness.OMPCapabilities())
	if err == nil {
		t.Fatal("package generated with a concrete provider or model identifier in a command")
	}
	if !strings.Contains(err.Error(), "commands/provider.md") {
		t.Errorf("error %q does not name the offending artifact", err)
	}
}

// TestBuildPackageRejectsProviderIDRule covers the other verbatim ship path: a
// path-scoped rule body is copied byte-for-byte, so a provider identifier there
// must fail generation too.
func TestBuildPackageRejectsProviderIDRule(t *testing.T) {
	cat := providerCorpus(t, artifacts.KindRule, "rules/provider.md",
		"---\npaths:\n  - \"**/*.ts\"\n---\n\nPrefer the anthropic/opus model for reviews.\n")
	_, err := BuildPackage(cat, harness.OMPCapabilities())
	if err == nil {
		t.Fatal("package generated with a concrete provider or model identifier in a rule")
	}
	if !strings.Contains(err.Error(), "rules/provider.md") {
		t.Errorf("error %q does not name the offending artifact", err)
	}
}

// TestEnrollWritesPackageAndSteeringThroughTransaction drives one enrollment
// against a real HOME: the journal precedes the native writes, the package tree
// matches its plan-time digest, the profile's AGENTS.md carries the Atomic
// block, and the ledger rows record the generation and tier.
func TestEnrollWritesPackageAndSteeringThroughTransaction(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newAdapter(t, map[string]string{"": root})

	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, OperationID: "enroll-test"})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if result.JournalPath == "" || len(result.Applied) != 2 {
		t.Fatalf("enroll applied = %+v, want the package tree and the steering block", result)
	}
	if !exists(result.JournalPath) {
		t.Errorf("journal %s was not written", result.JournalPath)
	}
	journal, err := installstate.LoadJournal(result.JournalPath)
	if err != nil {
		t.Fatalf("load journal: %v", err)
	}
	if !journal.Completed {
		t.Error("enrollment journal is not completed")
	}
	for _, unit := range []string{packageUnit, steeringUnit} {
		if state := journal.State(unit); state != installstate.StateCommitted {
			t.Errorf("journal unit %s state = %s, want committed", unit, state)
		}
	}

	// The published package matches the plan-time tree identity, and its files
	// are the generated ones.
	digest, _, err := managedfile.TreeDigest(config.PackageRoot(home, "omp"))
	if err != nil {
		t.Fatalf("tree digest: %v", err)
	}
	pkg, err := BuildPackage(fixtureCorpus(t), harness.OMPCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := pkg.TreeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if digest != wantDigest {
		t.Errorf("published tree digest %s != planned %s", digest, wantDigest)
	}
	for _, path := range pkg.Paths() {
		if !exists(filepath.Join(config.PackageRoot(home, "omp"), filepath.FromSlash(path))) {
			t.Errorf("published package is missing %s", path)
		}
	}

	// Steering: the profile AGENTS.md is exactly the CP2A composition wrapped in
	// the owned block — import-free steering, one blank line, the
	// frontmatter-free output-style body — not a second implementation of it.
	composition, err := artifacts.NewRenderer(fixtureCorpus(t)).OMPSteering()
	if err != nil {
		t.Fatalf("compose OMP steering: %v", err)
	}
	if got, want := readFile(t, SteeringPath(root)), string(managedfile.BlockDocument(composition.Bytes)); got != want {
		t.Errorf("live AGENTS.md does not match the CP2A composition:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	checkGolden(t, "profile-AGENTS.md", []byte(readFile(t, SteeringPath(root))))

	// Ledger rows carry the generation and tier for both resources.
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{PackageResource(home), SteeringResource(root)} {
		row, ok := ledger.Find(result.Target.Key(), resource)
		if !ok {
			t.Fatalf("ledger has no row for %s", resource)
		}
		if row.Generation != pkg.Generation {
			t.Errorf("row %s generation = %q, want %q", resource, row.Generation, pkg.Generation)
		}
		if row.Tier != string(Tier) {
			t.Errorf("row %s tier = %q, want %q", resource, row.Tier, Tier)
		}
		if row.Consumer != result.Target.Key() {
			t.Errorf("row %s consumer = %q", resource, row.Consumer)
		}
	}
	if rec, ok := ledger.FindTarget("omp", root); !ok || rec.Status != string(harness.StatusConverged) {
		t.Errorf("target record = %+v, want a converged enrollment", rec)
	}

	// The claims the adapter reports assess every resource as owned.
	claims, err := a.Claims(home, root)
	if err != nil {
		t.Fatal(err)
	}
	assessments, err := harness.AssessAll(claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, assessment := range assessments {
		if !assessment.Owned {
			t.Errorf("claim %s assessed %s, want owned", assessment.Claim.ID, assessment.Evidence)
		}
	}
}

// TestEnrollRerunIsNoOp proves repeated same-generation enrollment writes
// nothing: no journal, no native bytes, and no ledger change.
func TestEnrollRerunIsNoOp(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newAdapter(t, map[string]string{"": root})

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, OperationID: "first"}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	ledgerBefore := readFile(t, config.LedgerPath(home))
	agentsBefore := readFile(t, SteeringPath(root))
	treeBefore, _, err := managedfile.TreeDigest(config.PackageRoot(home, "omp"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, OperationID: "second"})
	if err != nil {
		t.Fatalf("second enroll: %v", err)
	}
	if result.JournalPath != "" || len(result.Applied) != 0 {
		t.Errorf("rerun applied = %+v, want a no-op", result)
	}
	if got := readFile(t, config.LedgerPath(home)); got != ledgerBefore {
		t.Error("rerun rewrote the ledger")
	}
	if got := readFile(t, SteeringPath(root)); got != agentsBefore {
		t.Error("rerun rewrote the profile steering")
	}
	treeAfter, _, err := managedfile.TreeDigest(config.PackageRoot(home, "omp"))
	if err != nil {
		t.Fatal(err)
	}
	if treeBefore != treeAfter {
		t.Error("rerun rewrote the package tree")
	}
	if _, err := os.Stat(config.TransactionDir(home, "second")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no-op rerun created a transaction directory: %v", err)
	}
}

// TestProfileLifecyclePreservesUserSettings runs the default and named profile
// lifecycle: each profile keeps its own guidance and settings, and the shared
// package carries both consumers.
func TestProfileLifecyclePreservesUserSettings(t *testing.T) {
	home := newHome(t)
	defaultRoot := filepath.Join(home, ".omp", "agent")
	namedRoot := filepath.Join(home, ".omp", "profiles", "work", "agent")
	for _, root := range []string{defaultRoot, namedRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const (
		defaultProse = "# My own guidance\n\nKeep this paragraph.\n"
		namedProse   = "# Work profile\n\nWork-only notes.\n"
		settings     = "defaultModel: user-selected\nskills:\n  enabled: false\n"
	)
	writeFile(t, SteeringPath(defaultRoot), defaultProse)
	writeFile(t, SteeringPath(namedRoot), namedProse)
	writeFile(t, SettingsPath(defaultRoot), settings)
	writeFile(t, SettingsPath(namedRoot), settings)

	a := newAdapter(t, map[string]string{"": defaultRoot, "work": namedRoot})
	a.ProfileNames = func(string) []string { return []string{"work"} }

	defaultResult, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: defaultRoot}, OperationID: "default"})
	if err != nil {
		t.Fatalf("enroll default: %v", err)
	}
	namedResult, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Name: "work", Root: namedRoot}, OperationID: "named"})
	if err != nil {
		t.Fatalf("enroll named: %v", err)
	}

	// The user's prose survives byte for byte outside the Atomic block, and the
	// profile's own settings file is never touched.
	for _, tc := range []struct{ root, prose string }{{defaultRoot, defaultProse}, {namedRoot, namedProse}} {
		got := readFile(t, SteeringPath(tc.root))
		if !strings.HasPrefix(got, tc.prose) {
			t.Errorf("profile %s lost its own guidance:\n%s", tc.root, got)
		}
		if !managedfile.HasBlock([]byte(got)) {
			t.Errorf("profile %s carries no Atomic block", tc.root)
		}
		if got := readFile(t, SettingsPath(tc.root)); got != settings {
			t.Errorf("profile %s settings changed:\n%s", tc.root, got)
		}
	}
	if _, err := managedfile.ManagedBlock([]byte(readFile(t, SteeringPath(defaultRoot)))); err != nil {
		t.Errorf("default steering block: %v", err)
	}

	// Both profiles consume the one shared package at the same generation.
	if defaultResult.Generation != namedResult.Generation {
		t.Errorf("profiles report generations %s and %s", defaultResult.Generation, namedResult.Generation)
	}
	resources, err := a.Resources(home)
	if err != nil {
		t.Fatal(err)
	}
	shared := resourceByID(t, resources, PackageResource(home))
	wantConsumers := []string{defaultResult.Target.Key(), namedResult.Target.Key()}
	sort.Strings(wantConsumers)
	if strings.Join(shared.Consumers, ",") != strings.Join(wantConsumers, ",") {
		t.Errorf("shared package consumers = %v, want %v", shared.Consumers, wantConsumers)
	}

	// Re-enrolling a profile stays a no-op, so the lifecycle is idempotent.
	rerun, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: defaultRoot}, OperationID: "default-again"})
	if err != nil {
		t.Fatalf("re-enroll default: %v", err)
	}
	if len(rerun.Applied) != 0 || rerun.JournalPath != "" {
		t.Errorf("re-enroll applied = %+v, want a no-op", rerun)
	}

	// Removing a profile root is a discovery fact, not an unenrollment:
	// enrollment is explicit, so the ledger keeps both consumers, the removed
	// root is reported as an absent candidate, and the surviving profile's
	// steering is untouched.
	if err := os.RemoveAll(namedRoot); err != nil {
		t.Fatal(err)
	}
	instances, err := a.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, inst := range instances {
		if inst.NativeRoot == namedRoot {
			removed = true
			if inst.Exists {
				t.Error("a removed profile root is still reported present")
			}
		}
	}
	if !removed {
		t.Error("the configured named profile root is no longer reported at all")
	}
	if got := readFile(t, SteeringPath(defaultRoot)); !strings.HasPrefix(got, defaultProse) {
		t.Error("removing another profile's root changed the surviving profile")
	}
	resources, err = a.Resources(home)
	if err != nil {
		t.Fatal(err)
	}
	shared = resourceByID(t, resources, PackageResource(home))
	want := []string{defaultResult.Target.Key(), namedResult.Target.Key()}
	sort.Strings(want)
	if strings.Join(shared.Consumers, ",") != strings.Join(want, ",") {
		t.Errorf("consumers after profile-root removal = %v, want both enrolled profiles", shared.Consumers)
	}
	if len(shared.VisibleTo) != 0 {
		t.Errorf("a removed profile root is still reported visible: %v", shared.VisibleTo)
	}
}

func resourceByID(t *testing.T, resources []harness.Resource, id string) harness.Resource {
	t.Helper()
	for _, r := range resources {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no resource %s in %+v", id, resources)
	return harness.Resource{}
}

// TestResourcesSeparateConsumersFromVisibility proves an unenrolled profile
// that can read the shared package is reported as visible, never as a consumer.
func TestResourcesSeparateConsumersFromVisibility(t *testing.T) {
	home := newHome(t)
	enrolled := filepath.Join(home, ".omp", "agent")
	unenrolled := filepath.Join(home, ".omp", "profiles", "other", "agent")
	for _, root := range []string{enrolled, unenrolled} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a := newAdapter(t, map[string]string{"": enrolled, "other": unenrolled})
	a.ProfileNames = func(string) []string { return []string{"other"} }

	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: enrolled}, OperationID: "one"})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	resources, err := a.Resources(home)
	if err != nil {
		t.Fatal(err)
	}

	shared := resourceByID(t, resources, PackageResource(home))
	if len(shared.Consumers) != 1 || shared.Consumers[0] != result.Target.Key() {
		t.Errorf("consumers = %v, want only the enrolled profile", shared.Consumers)
	}
	other := harness.Target{Kind: harness.KindOMP, Instance: unenrolled}.Key()
	if len(shared.VisibleTo) != 1 || shared.VisibleTo[0] != other {
		t.Errorf("visible_to = %v, want the unenrolled profile %s", shared.VisibleTo, other)
	}
	// An unenrolled profile cannot see another profile's steering.
	steering := resourceByID(t, resources, SteeringResource(enrolled))
	if len(steering.Consumers) != 1 || len(steering.VisibleTo) != 0 {
		t.Errorf("steering consumers = %v visible = %v, want a profile-owned resource", steering.Consumers, steering.VisibleTo)
	}
	// Visibility is not a dependency: with one consumer the package is removable.
	if err := shared.Removable(result.Target.Key()); err != nil {
		t.Errorf("shared package removable = %v, want nil while one profile consumes it", err)
	}

	// Enrolling the second profile makes the package shared infrastructure, so
	// the CP1B removal plan retains it and still removes the profile-owned
	// steering of the target leaving.
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: unenrolled}, OperationID: "two"}); err != nil {
		t.Fatalf("enroll second profile: %v", err)
	}
	resources, err = a.Resources(home)
	if err != nil {
		t.Fatal(err)
	}
	shared = resourceByID(t, resources, PackageResource(home))
	if err := shared.Removable(result.Target.Key()); !errors.Is(err, harness.ErrSharedResource) {
		t.Errorf("shared package removable = %v, want ErrSharedResource once two profiles consume it", err)
	}
	// RemoveTarget plans over the resources the leaving target consumes; another
	// profile's own steering is not part of its plan.
	var owned []harness.Resource
	for _, r := range resources {
		if contains(r.Consumers, result.Target.Key()) {
			owned = append(owned, r)
		}
	}
	plan, err := harness.RemoveTarget(result.Target, owned)
	if err != nil {
		t.Fatalf("removal plan: %v", err)
	}
	if len(plan.Retained) != 1 || plan.Retained[0] != PackageResource(home) {
		t.Errorf("retained = %v, want the shared package", plan.Retained)
	}
	if len(plan.Removed) != 1 || plan.Removed[0] != SteeringResource(enrolled) {
		t.Errorf("removed = %v, want the leaving profile's steering", plan.Removed)
	}

	// Removing an unenrolled profile's root drops its visibility.
	if err := os.RemoveAll(unenrolled); err != nil {
		t.Fatal(err)
	}
	resources, err = a.Resources(home)
	if err != nil {
		t.Fatal(err)
	}
	if visible := resourceByID(t, resources, PackageResource(home)).VisibleTo; len(visible) != 0 {
		t.Errorf("visible_to = %v after the unenrolled root was removed, want none", visible)
	}
}

// TestIncompatibleGenerationRefusesBeforeMutation seeds a second consumer at a
// different generation and proves the enrollment refuses without touching the
// package, the steering file, or the ledger.
func TestIncompatibleGenerationRefusesBeforeMutation(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newAdapter(t, map[string]string{"": root})

	other := harness.Target{Kind: harness.KindOMP, Instance: filepath.Join(home, ".omp", "profiles", "other", "agent")}.Key()
	ledger := &installstate.Ledger{}
	ledger.Upsert(installstate.Row{
		Target:     other,
		Resource:   PackageResource(home),
		Consumer:   other,
		Generation: "generation-from-a-different-binary",
		Tier:       string(Tier),
		Applied:    installstate.AppliedValue{Path: config.PackageRoot(home, "omp"), Kind: managedfile.KindTree, Digest: "deadbeef"},
	})
	path := config.LedgerPath(home)
	if err := ledger.Save(path); err != nil {
		t.Fatal(err)
	}
	ledgerBefore := readFile(t, path)

	_, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, OperationID: "conflict"})
	if err == nil {
		t.Fatal("incompatible shared generation was accepted")
	}
	for _, want := range []string{PackageResource(home), root, other, "generation-from-a-different-binary"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	if exists(config.PackageRoot(home, "omp")) {
		t.Error("refusal mutated the shared package")
	}
	if exists(SteeringPath(root)) {
		t.Error("refusal wrote the profile steering")
	}
	if _, err := os.Stat(config.TransactionDir(home, "conflict")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refusal created a transaction directory: %v", err)
	}
	if got := readFile(t, path); got != ledgerBefore {
		t.Error("refusal rewrote the ledger")
	}
}

// TestEnrollRefusesAmbiguousManagedBlock proves a malformed Atomic block is
// refused rather than clobbered or guessed at.
func TestEnrollRefusesAmbiguousManagedBlock(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, SteeringPath(root), "# Guidance\n\n"+managedfile.BlockOpen+"\nunclosed\n")

	a := newAdapter(t, map[string]string{"": root})
	_, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, OperationID: "ambiguous"})
	if err == nil || !strings.Contains(err.Error(), managedfile.BlockOpen) {
		t.Fatalf("ambiguous block error = %v, want a refusal naming the block tag", err)
	}
	if got := readFile(t, SteeringPath(root)); got != "# Guidance\n\n"+managedfile.BlockOpen+"\nunclosed\n" {
		t.Error("refusal rewrote the ambiguous file")
	}
	if exists(config.PackageRoot(home, "omp")) {
		t.Error("refusal published the package")
	}
}

// TestEmbeddedCorpusLoadsSelectedGeneration proves the shipped binary's own
// corpus is a usable package source, so a shipped OMP install has no checkout
// dependency.
func TestEmbeddedCorpusLoadsSelectedGeneration(t *testing.T) {
	cat, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load embedded: %v", err)
	}
	// The shipped corpus deliberately tolerates host frontmatter that is not
	// valid YAML, so validation is not the gate here; the package build is.
	steering, err := artifacts.NewRenderer(cat).Steering()
	if err != nil {
		t.Fatalf("embedded steering: %v", err)
	}
	if steering.Source != "AGENTS.md" {
		t.Errorf("steering source = %q, want the canonical AGENTS.md", steering.Source)
	}
	if _, err := BuildPackage(cat, harness.OMPCapabilities()); err != nil {
		t.Fatalf("build package from embedded corpus: %v", err)
	}
}

// contains reports whether list holds value. It lives here because the
// production helper it replaced moved to the harness package with the resource
// merge.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
