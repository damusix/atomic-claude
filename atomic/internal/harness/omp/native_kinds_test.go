package omp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func fixtureCommandPath(root string) string {
	return filepath.Join(root, "commands", "example.md")
}

func fixtureSkillPath(root string) string {
	return filepath.Join(root, "skills", "atomic-example", "SKILL.md")
}

func fixtureReferencePath(root string) string {
	return filepath.Join(root, "skills", "atomic-example", "references", "extra.md")
}

func packageBytes(t *testing.T, cat *artifacts.Catalog, path string) string {
	t.Helper()
	pkg, err := BuildPackage(cat, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range pkg.Files {
		if f.Path == path {
			return string(f.Bytes)
		}
	}
	t.Fatalf("package carries no %s", path)
	return ""
}

func ownedRow(t *testing.T, home, root, path string) (installstate.Row, bool) {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	return ledger.Find(harness.Target{Kind: harness.KindOMP, Instance: root}.Key(), path)
}

// referencedSkillCorpus is the fixture corpus with one referenced file shipped
// beside its skill manifest, so the skill tree spans a nested directory.
func referencedSkillCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	base := fixtureCorpus(t)
	source := "skills/atomic-example/references/extra.md"
	return &artifacts.Catalog{Artifacts: append(append([]artifacts.Artifact(nil), base.Artifacts...), artifacts.Artifact{
		ID:     "skill:" + source,
		Kind:   artifacts.KindSkill,
		Source: source,
		Body:   []byte("# Extra\n"),
	})}
}

// OMP reads commands and skills from the profile agent root
// (TestOMPNativeKindDelivery), so enrollment owns them there.
func TestEnrollWritesCommandsAndSkillsToProfileRoot(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	cat := fixtureCorpus(t)
	for path, source := range map[string]string{
		fixtureCommandPath(root): "commands/example.md",
		fixtureSkillPath(root):   "skills/atomic-example/SKILL.md",
	} {
		want := packageBytes(t, cat, source)
		if got := readFile(t, path); got != want {
			t.Errorf("%s is not the package projection:\n%s", path, got)
		}
		row, ok := ownedRow(t, home, root, path)
		if !ok {
			t.Errorf("ledger has no row for %s", path)
			continue
		}
		if row.Applied.Kind != managedfile.KindFile || row.Applied.Digest != managedfile.Digest([]byte(want)) {
			t.Errorf("row %+v does not claim the written bytes of %s", row.Applied, path)
		}
	}
	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Converged || len(plan.Blockers) > 0 {
		t.Errorf("plan after enroll = converged %v blockers %v, want converged", plan.Converged, plan.Blockers)
	}
}

const userCommand = "the user's own command\n"

func TestPreexistingUnownedCommandConflicts(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureCommandPath(root), userCommand)

	_, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}})
	if err == nil || !strings.Contains(err.Error(), "was not written by Atomic") || !strings.Contains(err.Error(), "--leave-unowned") {
		t.Fatalf("enroll err = %v, want a refusal naming --replace and --leave-unowned", err)
	}
	if got := readFile(t, fixtureCommandPath(root)); got != userCommand {
		t.Errorf("the user's command was overwritten:\n%s", got)
	}
}

func TestLeaveUnownedKeepsTheCommandAcrossLaterVerbs(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureCommandPath(root), userCommand)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if _, ok := ownedRow(t, home, root, fixtureCommandPath(root)); ok {
		t.Error("--leave-unowned recorded ownership of the user's command")
	}
	if !exists(fixtureSkillPath(root)) {
		t.Error("--leave-unowned skipped the rest of the delivery")
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("a later enroll with no decision refused the left command: %v", err)
	}
	if got := readFile(t, fixtureCommandPath(root)); got != userCommand {
		t.Errorf("a later enroll overwrote the left command:\n%s", got)
	}
}

func TestReplaceOwnsAnUnownedSkill(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureSkillPath(root), "the user's skill\n")
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	if got := readFile(t, fixtureSkillPath(root)); got != packageBytes(t, fixtureCorpus(t), "skills/atomic-example/SKILL.md") {
		t.Errorf("--replace kept the user's bytes:\n%s", got)
	}
}

func TestSkillReferenceIsDeliveredAndRetired(t *testing.T) {
	home, root, a := enrolledProfile(t)
	referenced := referencedSkillCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return referenced, nil }
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if got := readFile(t, fixtureReferencePath(root)); got != "# Extra\n" {
		t.Fatalf("the referenced skill file was not delivered beside its manifest: %q", got)
	}

	base := fixtureCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return base, nil }
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll without the reference: %v", err)
	}
	if exists(fixtureReferencePath(root)) {
		t.Error("the reference the corpus no longer ships is still on disk")
	}
	if exists(filepath.Dir(fixtureReferencePath(root))) {
		t.Error("retiring the only reference left its emptied directory behind")
	}
	if _, ok := ownedRow(t, home, root, fixtureReferencePath(root)); ok {
		t.Error("the retired reference's ledger row survived")
	}
	if !exists(fixtureSkillPath(root)) {
		t.Error("retiring the reference removed the manifest")
	}
}

func TestTargetUninstallRemovesCommandsAndSkills(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !exists(fixtureCommandPath(root)) || !exists(fixtureSkillPath(root)) {
		t.Fatal("enroll delivered no command or skill to remove")
	}
	if _, err := a.Lifecycle(home).Remove(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, path := range []string{fixtureCommandPath(root), fixtureSkillPath(root)} {
		if exists(path) {
			t.Errorf("target uninstall left %s behind", path)
		}
	}
	for _, dir := range []string{filepath.Join(root, "commands"), filepath.Join(root, "skills")} {
		if exists(dir) {
			t.Errorf("target uninstall left the emptied %s behind", dir)
		}
	}
	if !exists(root) {
		t.Error("pruning climbed past the profile root")
	}
}

// No OMP surface selects or revokes an output style, so the style that ships
// inside the steering block is reported rather than presented as selectable.
func TestOutputStyleSelectionReadsUnsupported(t *testing.T) {
	home, root, a := enrolledProfile(t)
	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	found := false
	for _, gap := range result.Unproven {
		found = found || gap.Surface == "output-style selection"
	}
	if !found {
		t.Errorf("enrollment unproven %+v does not report output-style selection", result.Unproven)
	}
	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Unproven, "output-style selection: ") {
		t.Errorf("plan unproven %v does not report output-style selection", plan.Unproven)
	}
}

// OMP expands a whole-line `@~/...` import in the profile AGENTS.md
// (TestOMPNativeKindDelivery), so the profile import ships.
func TestSteeringBlockKeepsTheProfileImport(t *testing.T) {
	block, err := SteeringBlock(fixtureCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(block), "\n@~/.atomic/profile.md\n") {
		t.Errorf("the profile steering block dropped the profile import:\n%s", block)
	}
}
