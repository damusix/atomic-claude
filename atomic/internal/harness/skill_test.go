package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

const representativeSkills = 10

func loadSkillCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	cat, err := artifacts.Load(repoRoot)
	if err != nil {
		t.Fatalf("artifacts.Load: %v", err)
	}
	return cat
}

// The real corpus projects cleanly for both targets: every declared reference
// resolves inside its shipped skill tree and no two files share a native path.
func TestSkill_CanonicalCorpusProjectsCleanly(t *testing.T) {
	cat := loadSkillCorpus(t)

	for _, target := range []artifacts.Target{artifacts.TargetClaude, artifacts.TargetOMP, artifacts.TargetCodex} {
		report, err := ProjectSkills(cat, target, SkillPolicy{}, matrixFor(target))
		if err != nil {
			t.Fatalf("ProjectSkills(%s): %v", target, err)
		}
		if len(report.Reference) != 0 {
			t.Errorf("%s reference gaps = %v", target, report.Reference)
		}
		if len(report.Collisions) != 0 {
			t.Errorf("%s collisions = %v", target, report.Collisions)
		}
		if len(report.Disabled) != 0 {
			t.Errorf("%s disabled = %v, want none under an empty policy", target, report.Disabled)
		}
		if len(report.Files) != len(cat.OfKind(artifacts.KindSkill)) {
			t.Errorf("%s shipped %d files, want %d", target, len(report.Files), len(cat.OfKind(artifacts.KindSkill)))
		}
		if len(report.Gaps) != len(skillRoles) {
			t.Errorf("%s gaps = %d, want %d", target, len(report.Gaps), len(skillRoles))
		}
	}
}

// Every canonical skill file projects into both targets with the path, delivery
// class, and digest each projection owes its caller.
func TestSkill_AllCanonicalSkillsProject(t *testing.T) {
	cat := loadSkillCorpus(t)

	skills := cat.OfKind(artifacts.KindSkill)
	if len(skills) == 0 {
		t.Fatal("corpus carries no skills")
	}
	manifests := 0
	for _, a := range skills {
		if SkillManifest(a) {
			manifests++
		}
		t.Run(a.Source, func(t *testing.T) {
			claude, err := ClaudeSkill(a)
			if err != nil {
				t.Fatalf("ClaudeSkill: %v", err)
			}
			if claude.Target != artifacts.TargetClaude || claude.Path != a.Source {
				t.Errorf("Claude projection = %s %s, want claude %s", claude.Target, claude.Path, a.Source)
			}
			if string(claude.Bytes) != string(a.Body) {
				t.Error("Claude projection is not the canonical bytes")
			}
			if claude.Enforcement != artifacts.EnforcementUnsupported {
				t.Errorf("Claude enforcement = %q, want unsupported", claude.Enforcement)
			}

			omp, err := OMPSkill(a)
			if err != nil {
				t.Fatalf("OMPSkill: %v", err)
			}
			if omp.Target != artifacts.TargetOMP || omp.Path != a.Source {
				t.Errorf("OMP projection = %s %s, want omp %s", omp.Target, omp.Path, a.Source)
			}
			if omp.Enforcement != artifacts.EnforcementUnsupported {
				t.Errorf("OMP enforcement = %q, want unsupported", omp.Enforcement)
			}
			if len(omp.Unsupported) != 0 {
				t.Errorf("OMP projection reports unsupported metadata %v for a portable manifest", omp.Unsupported)
			}

			codex, err := CodexSkill(a)
			if err != nil {
				t.Fatalf("CodexSkill: %v", err)
			}
			if codex.Target != artifacts.TargetCodex || codex.Path != a.Source {
				t.Errorf("Codex projection = %s %s, want codex %s", codex.Target, codex.Path, a.Source)
			}
			if codex.Enforcement != artifacts.EnforcementUnsupported {
				t.Errorf("Codex enforcement = %q, want unsupported", codex.Enforcement)
			}
			if len(codex.Unsupported) != 0 {
				t.Errorf("Codex projection reports unsupported metadata %v for a portable manifest", codex.Unsupported)
			}

			for _, p := range []artifacts.Projection{claude, omp, codex} {
				if p.Digest != artifacts.ProjectionDigest(p.Bytes) {
					t.Errorf("%s %s digest is not the digest of its bytes", p.Target, p.Path)
				}
				if p.Artifact != a.ID {
					t.Errorf("%s projection names artifact %q, want %q", p.Target, p.Artifact, a.ID)
				}
			}
		})
	}
	if manifests != representativeSkills {
		t.Fatalf("canonical skill manifest count = %d, want %d", manifests, representativeSkills)
	}
}

// The OMP manifest carries the portable name/description pair as its own
// frontmatter and preserves the canonical instruction body byte-for-byte.
func TestSkill_OMPManifestRoundTrip(t *testing.T) {
	cat := loadSkillCorpus(t)

	for _, a := range cat.OfKind(artifacts.KindSkill) {
		if !SkillManifest(a) {
			continue
		}
		t.Run(a.Semantics.Name, func(t *testing.T) {
			proj, err := OMPSkill(a)
			if err != nil {
				t.Fatalf("OMPSkill: %v", err)
			}
			meta, body, err := frontmatter.Parse(string(proj.Bytes))
			if err != nil {
				t.Fatalf("parse OMP projection: %v", err)
			}
			if meta["name"] != a.Semantics.Name {
				t.Errorf("OMP name = %v, want %q", meta["name"], a.Semantics.Name)
			}
			if meta["description"] != a.Semantics.Description {
				t.Errorf("OMP description = %v, want %q", meta["description"], a.Semantics.Description)
			}
			want, err := artifacts.SkillBody(a)
			if err != nil {
				t.Fatalf("SkillBody: %v", err)
			}
			if body != string(want) {
				t.Errorf("OMP projection lost the canonical body:\n--- got ---\n%s\n--- want ---\n%s", body, want)
			}

			again, err := OMPSkill(a)
			if err != nil {
				t.Fatalf("OMPSkill (second): %v", err)
			}
			if string(again.Bytes) != string(proj.Bytes) {
				t.Error("OMP skill projection is not stable across runs")
			}
		})
	}
}

// OMP has no skill-metadata row, so a canonical manifest key outside the
// portable pair is reported unsupported instead of copied with an implied
// native claim.
func TestSkill_OMPReportsUnsupportedMetadata(t *testing.T) {
	manifest := skillArtifact("atomic-extra", "SKILL.md",
		"---\nname: atomic-extra\ndescription: Fixture skill.\nallowed-tools: Read\n---\nBody.\n")

	proj, err := OMPSkill(manifest)
	if err != nil {
		t.Fatalf("OMPSkill: %v", err)
	}
	if !slices.Contains(proj.Unsupported, "allowed-tools") {
		t.Errorf("OMP unsupported metadata = %v, want it to include allowed-tools", proj.Unsupported)
	}
	meta, _, err := frontmatter.Parse(string(proj.Bytes))
	if err != nil {
		t.Fatalf("parse OMP projection: %v", err)
	}
	if _, ok := meta["allowed-tools"]; ok {
		t.Error("OMP projection carries an unproven metadata key")
	}
	if meta["name"] != "atomic-extra" {
		t.Errorf("OMP name = %v, want atomic-extra", meta["name"])
	}
}

// A manifest's referenced files ship beside it under the same relative path, so
// a relative reference resolves under both target layouts.
func TestSkill_ReferencesShipBesideManifest(t *testing.T) {
	manifest := skillArtifact("atomic-example", "SKILL.md",
		"---\nname: atomic-example\ndescription: Fixture.\n---\nRead `references/guide.md`.\n")
	reference := skillArtifact("atomic-example", "references/guide.md", "# Guide\n")
	cat := &artifacts.Catalog{Artifacts: []artifacts.Artifact{manifest, reference}}

	for _, target := range []artifacts.Target{artifacts.TargetClaude, artifacts.TargetOMP} {
		report, err := ProjectSkills(cat, target, SkillPolicy{}, matrixFor(target))
		if err != nil {
			t.Fatalf("ProjectSkills(%s): %v", target, err)
		}
		if len(report.Files) != 2 {
			t.Fatalf("%s shipped %d files, want 2", target, len(report.Files))
		}
		if report.Files[0].Projection.Path != "skills/atomic-example/SKILL.md" ||
			report.Files[1].Projection.Path != "skills/atomic-example/references/guide.md" {
			t.Errorf("%s paths = %q, %q", target, report.Files[0].Projection.Path, report.Files[1].Projection.Path)
		}
		if len(report.Reference) != 0 {
			t.Errorf("%s reported an unresolved reference: %v", target, report.Reference)
		}
	}
}

// A relative reference the shipped tree cannot resolve is an explicit gap, not
// a silent drop.
func TestSkill_UnresolvedReferenceIsGap(t *testing.T) {
	manifest := skillArtifact("atomic-example", "SKILL.md",
		"---\nname: atomic-example\ndescription: Fixture.\n---\nRead `references/missing.md` and [the spec](docs/spec/x.md).\n")
	cat := &artifacts.Catalog{Artifacts: []artifacts.Artifact{manifest}}

	report, err := ProjectSkills(cat, artifacts.TargetClaude, SkillPolicy{}, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills: %v", err)
	}
	if len(report.Reference) != 1 {
		t.Fatalf("reference gaps = %v, want exactly the unresolvable reference", report.Reference)
	}
	if gap := report.Reference[0]; gap.Skill != "atomic-example" || gap.Ref != "references/missing.md" {
		t.Errorf("reference gap = %+v", gap)
	}
}

// Two canonical skills that project to one native path are reported, never
// silently resolved.
func TestSkill_CollisionsReported(t *testing.T) {
	projections := []artifacts.Projection{
		{Artifact: "skill:skills/a/SKILL.md", Target: artifacts.TargetClaude, Path: "skills/shared/SKILL.md"},
		{Artifact: "skill:skills/b/SKILL.md", Target: artifacts.TargetClaude, Path: "skills/shared/SKILL.md"},
		{Artifact: "skill:skills/c/SKILL.md", Target: artifacts.TargetClaude, Path: "skills/unique/SKILL.md"},
	}

	got := SkillCollisions(projections)
	if len(got) != 1 {
		t.Fatalf("collisions = %v, want exactly one", got)
	}
	if got[0].Path != "skills/shared/SKILL.md" || got[0].Target != artifacts.TargetClaude {
		t.Errorf("collision = %+v", got[0])
	}
	if !slices.Equal(got[0].Sources, []string{"skill:skills/a/SKILL.md", "skill:skills/b/SKILL.md"}) {
		t.Errorf("collision sources = %v", got[0].Sources)
	}
}

// A user-disabled skill is honored: it ships nothing and is reported. A
// disabled referenced file is dropped and the manifest that requires it records
// the gap.
func TestSkill_DisabledHonored(t *testing.T) {
	manifest := skillArtifact("atomic-example", "SKILL.md",
		"---\nname: atomic-example\ndescription: Fixture.\n---\nRead `references/guide.md`.\n")
	reference := skillArtifact("atomic-example", "references/guide.md", "# Guide\n")
	cat := &artifacts.Catalog{Artifacts: []artifacts.Artifact{manifest, reference}}

	report, err := ProjectSkills(cat, artifacts.TargetClaude, SkillPolicy{Disabled: []string{manifest.ID}}, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills: %v", err)
	}
	if len(report.Files) != 0 {
		t.Errorf("disabled skill shipped %d files", len(report.Files))
	}
	if !slices.Equal(report.Disabled, []string{manifest.ID}) {
		t.Errorf("disabled = %v, want [%s]", report.Disabled, manifest.ID)
	}

	report, err = ProjectSkills(cat, artifacts.TargetClaude, SkillPolicy{Disabled: []string{reference.ID}}, ClaudeCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills (reference): %v", err)
	}
	if len(report.Files) != 1 || report.Files[0].Artifact != manifest.ID {
		t.Errorf("files = %v, want only the manifest", report.Files)
	}
	if !slices.Equal(report.Disabled, []string{reference.ID}) {
		t.Errorf("disabled = %v, want [%s]", report.Disabled, reference.ID)
	}
	if len(report.Reference) != 1 || report.Reference[0].Ref != "references/guide.md" {
		t.Errorf("reference gaps = %v, want the disabled reference", report.Reference)
	}
}

// Neither tested harness has a skill row, so every skill surface is an explicit
// gap; a surface the record proves drops out of the list.
func TestSkill_GapsReportUnprovenSurfaces(t *testing.T) {
	for _, m := range []CapabilityMatrix{ClaudeCapabilities(), OMPCapabilities()} {
		gaps := SkillGaps(m)
		if len(gaps) != len(skillRoles) {
			t.Fatalf("%s gaps = %d, want %d", m.Harness, len(gaps), len(skillRoles))
		}
		for _, gap := range gaps {
			if gap.Status != StatusUnsupported {
				t.Errorf("%s %s status = %q, want unsupported", m.Harness, gap.Role, gap.Status)
			}
			if gap.Limitation == "" {
				t.Errorf("%s %s gap carries no limitation", m.Harness, gap.Role)
			}
		}
	}

	proven := CapabilityMatrix{
		Harness: KindClaude,
		Version: "test",
		rows:    map[Role]Capability{RoleSkillDiscovery: {Role: RoleSkillDiscovery, Status: StatusSupported, Native: "skills/"}},
	}
	gaps := SkillGaps(proven)
	if len(gaps) != len(skillRoles)-1 {
		t.Errorf("gaps = %d, want %d after one surface is proven", len(gaps), len(skillRoles)-1)
	}
	for _, gap := range gaps {
		if gap.Role == RoleSkillDiscovery {
			t.Error("a proven surface stayed in the gap list")
		}
	}
}

// Codex shares OMP's portable manifest pair, so a Codex manifest carries the
// name and description as its own frontmatter, preserves the canonical
// instruction body byte-for-byte, and reports every other canonical metadata key
// unsupported. Codex proved no skill surface, so the bytes ship against explicit
// gaps rather than an implied native claim.
func TestSkill_CodexSharesPortableProjection(t *testing.T) {
	manifest := skillArtifact("atomic-extra", "SKILL.md",
		"---\nname: atomic-extra\ndescription: Fixture skill.\nallowed-tools: Read\n---\nBody.\n")

	proj, err := CodexSkill(manifest)
	if err != nil {
		t.Fatalf("CodexSkill: %v", err)
	}
	if proj.Target != artifacts.TargetCodex {
		t.Errorf("target = %s, want codex", proj.Target)
	}
	if !slices.Contains(proj.Unsupported, "allowed-tools") {
		t.Errorf("Codex unsupported metadata = %v, want it to include allowed-tools", proj.Unsupported)
	}
	meta, body, err := frontmatter.Parse(string(proj.Bytes))
	if err != nil {
		t.Fatalf("parse Codex projection: %v", err)
	}
	if meta["name"] != "atomic-extra" || meta["description"] != "Fixture skill." {
		t.Errorf("Codex metadata = %v, want the portable pair", meta)
	}
	if _, ok := meta["allowed-tools"]; ok {
		t.Error("Codex projection carries an unproven metadata key")
	}
	want, err := artifacts.SkillBody(manifest)
	if err != nil {
		t.Fatalf("SkillBody: %v", err)
	}
	if body != string(want) {
		t.Errorf("Codex projection lost the canonical body:\n--- got ---\n%s\n--- want ---\n%s", body, want)
	}

	gaps := SkillGaps(CodexCapabilities())
	if len(gaps) != len(skillRoles) {
		t.Errorf("Codex skill gaps = %d, want %d", len(gaps), len(skillRoles))
	}
}

// The same corpus renders the same bytes, digests, reference gaps, and runtime
// classification every run.
func TestSkill_Deterministic(t *testing.T) {
	first := loadSkillCorpus(t)
	second := loadSkillCorpus(t)

	for _, target := range []artifacts.Target{artifacts.TargetClaude, artifacts.TargetOMP, artifacts.TargetCodex} {
		one, err := ProjectSkills(first, target, SkillPolicy{}, matrixFor(target))
		if err != nil {
			t.Fatalf("ProjectSkills(%s): %v", target, err)
		}
		two, err := ProjectSkills(second, target, SkillPolicy{}, matrixFor(target))
		if err != nil {
			t.Fatalf("ProjectSkills(%s, second): %v", target, err)
		}
		if !slices.Equal(one.Reference, two.Reference) || !slices.Equal(one.Runtime, two.Runtime) {
			t.Errorf("%s report is not stable across runs:\n%+v\n%+v", target, one, two)
		}
	}

	for _, a := range first.OfKind(artifacts.KindSkill) {
		other, ok := second.Get(a.ID)
		if !ok {
			t.Fatalf("second corpus is missing %s", a.ID)
		}
		for name, project := range map[string]func(artifacts.Artifact) (artifacts.Projection, error){
			"claude": ClaudeSkill,
			"omp":    OMPSkill,
		} {
			one, err := project(a)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			two, err := project(other)
			if err != nil {
				t.Fatalf("%s (second): %v", name, err)
			}
			if one.Digest != two.Digest || string(one.Bytes) != string(two.Bytes) {
				t.Errorf("%s projection of %s is not stable across runs", name, a.ID)
			}
		}
	}
}

// A nested reference — one a referenced file declares deeper in the tree —
// resolves against its own directory, and an unresolved one gaps at its source.
func TestSkill_NestedReferencesResolve(t *testing.T) {
	manifest := skillArtifact("atomic-example", "SKILL.md",
		"---\nname: atomic-example\ndescription: Fixture.\n---\nRead `references/guide.md`.\n")
	guide := skillArtifact("atomic-example", "references/guide.md",
		"# Guide\n\nRead `./deep.md`.\n")
	deep := skillArtifact("atomic-example", "references/deep.md", "# Deep\n")

	cat := &artifacts.Catalog{Artifacts: []artifacts.Artifact{manifest, guide, deep}}
	report, err := ProjectSkills(cat, artifacts.TargetOMP, SkillPolicy{}, OMPCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills: %v", err)
	}
	if len(report.Reference) != 0 {
		t.Fatalf("nested reference gaps = %v, want none", report.Reference)
	}

	cat = &artifacts.Catalog{Artifacts: []artifacts.Artifact{manifest, guide}}
	report, err = ProjectSkills(cat, artifacts.TargetOMP, SkillPolicy{}, OMPCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills (missing nested): %v", err)
	}
	want := SkillReferenceGap{
		Skill: "atomic-example",
		File:  "skills/atomic-example/references/guide.md",
		Ref:   "references/deep.md",
	}
	if len(report.Reference) != 1 || report.Reference[0] != want {
		t.Errorf("nested reference gaps = %+v, want [%+v]", report.Reference, want)
	}
}

// The runtime-oriented instructions in the ported skills are classified, not
// assumed: the portable skills name only the atomic binary or a canonical
// agent, and atomic-bus's background-delivery instruction — whose delivery
// depends on harness output injection no CP0 row proves — is reported as a gap
// rather than shipped as if it resolved.
func TestSkill_RuntimeInstructionsClassified(t *testing.T) {
	cat := loadSkillCorpus(t)
	report, err := ProjectSkills(cat, artifacts.TargetOMP, SkillPolicy{}, OMPCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills: %v", err)
	}

	bySkill := map[string][]SkillRuntimeUse{}
	for _, use := range report.Runtime {
		bySkill[use.Skill] = append(bySkill[use.Skill], use)
	}

	for _, name := range []string{"atomic-writing", "atomic-debug", "atomic-tdd"} {
		for _, use := range bySkill[name] {
			if use.Surface == SkillRuntimeHarness {
				t.Errorf("%s carries a harness runtime token: %+v", name, use)
			}
		}
	}
	busHarness := 0
	for _, use := range bySkill["atomic-bus"] {
		if use.Surface != SkillRuntimeHarness {
			continue
		}
		busHarness++
		want := SkillRuntimeUse{
			Skill:   "atomic-bus",
			File:    "skills/atomic-bus/SKILL.md",
			Surface: SkillRuntimeHarness,
			Name:    "harness background delivery",
		}
		if use != want {
			t.Errorf("atomic-bus harness gap = %+v, want %+v", use, want)
		}
	}
	if busHarness != 1 {
		t.Errorf("atomic-bus harness gaps = %d, want exactly 1", busHarness)
	}
	for _, name := range []string{"atomic-bus", "atomic-debug"} {
		if !usesCarry(bySkill[name], SkillRuntimeBinary, "") {
			t.Errorf("%s classifies no atomic-binary runtime instruction: %v", name, bySkill[name])
		}
	}
	if !usesCarry(bySkill["atomic-debug"], SkillRuntimeAgent, "atomic-investigator") {
		t.Errorf("atomic-debug does not classify its investigator dispatch: %v", bySkill["atomic-debug"])
	}
}

// A runtime instruction that names a harness tool is reported as a gap, never
// shipped as if it resolved.
func TestSkill_HarnessRuntimeTokenIsGap(t *testing.T) {
	manifest := skillArtifact("atomic-example", "SKILL.md",
		"---\nname: atomic-example\ndescription: Fixture.\n---\nThen start the listener:\n\n```\nMonitor(command=\"atomic bus recv r\")\n```\n")
	cat := &artifacts.Catalog{Artifacts: []artifacts.Artifact{manifest}}

	report, err := ProjectSkills(cat, artifacts.TargetOMP, SkillPolicy{}, OMPCapabilities())
	if err != nil {
		t.Fatalf("ProjectSkills: %v", err)
	}
	if !usesCarry(report.Runtime, SkillRuntimeHarness, "harness tool call") {
		t.Errorf("harness tool call not classified as a gap: %v", report.Runtime)
	}
	if !usesCarry(report.Runtime, SkillRuntimeBinary, "atomic bus") {
		t.Errorf("portable atomic invocation not classified: %v", report.Runtime)
	}
}

// usesCarry reports whether uses holds a use of the given surface and, when
// name is non-empty, the given name.
func usesCarry(uses []SkillRuntimeUse, surface SkillRuntimeSurface, name string) bool {
	for _, use := range uses {
		if use.Surface == surface && (name == "" || use.Name == name) {
			return true
		}
	}
	return false
}

func TestSkill_GoldenProjections(t *testing.T) {
	cat := loadSkillCorpus(t)

	for _, name := range []string{"atomic-verify", "atomic-documentation", "atomic-writing", "atomic-debug"} {
		a, ok := cat.Get(artifacts.SkillID(name))
		if !ok {
			t.Fatalf("skill %s missing from the corpus", name)
		}

		claude, err := ClaudeSkill(a)
		if err != nil {
			t.Fatalf("ClaudeSkill(%s): %v", name, err)
		}
		checkSkillGolden(t, "claude/skills/"+name+"/SKILL.md", claude.Bytes)

		omp, err := OMPSkill(a)
		if err != nil {
			t.Fatalf("OMPSkill(%s): %v", name, err)
		}
		checkSkillGolden(t, "omp/skills/"+name+"/SKILL.md", omp.Bytes)

		for golden, projection := range map[string]artifacts.Projection{
			"claude/skills/" + name + "/SKILL.md": claude,
			"omp/skills/" + name + "/SKILL.md":    omp,
		} {
			raw, err := os.ReadFile(filepath.Join("testdata", "skill", golden))
			if err != nil {
				t.Fatalf("read golden %s: %v", golden, err)
			}
			if got := artifacts.ProjectionDigest(raw); got != projection.Digest {
				t.Errorf("golden %s digest = %q, projection digest = %q", golden, got, projection.Digest)
			}
		}
	}

	// atomic-writing's largest reference ships byte-identical beside its
	// manifest under both target layouts, so a nested reference resolves to the
	// same bytes it does in the canonical tree.
	ref, ok := cat.Get("skill:skills/atomic-writing/references/mermaid.md")
	if !ok {
		t.Fatal("atomic-writing reference missing from the corpus")
	}
	for _, target := range []struct {
		name   string
		render func(artifacts.Artifact) (artifacts.Projection, error)
	}{
		{"claude", ClaudeSkill},
		{"omp", OMPSkill},
	} {
		projection, err := target.render(ref)
		if err != nil {
			t.Fatalf("%s reference projection: %v", target.name, err)
		}
		if string(projection.Bytes) != string(ref.Body) {
			t.Errorf("%s reference projection is not the canonical bytes", target.name)
		}
		checkSkillGolden(t, target.name+"/skills/atomic-writing/references/mermaid.md", projection.Bytes)
	}
}

func checkSkillGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	dst := filepath.Join("testdata", "skill", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(dst, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", dst, err)
		}
		return
	}
	want, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read golden %s: %v (run go test ./internal/harness -update)", dst, err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// skillArtifact builds a canonical skill artifact with the frontmatter derived
// from its body, mirroring what artifacts.Load produces.
func skillArtifact(dir, rel, body string) artifacts.Artifact {
	source := "skills/" + dir + "/" + rel
	sem := artifacts.Semantics{}
	if meta, _, err := frontmatter.Parse(body); err == nil {
		if v, ok := meta["name"].(string); ok {
			sem.Name = v
		}
		if v, ok := meta["description"].(string); ok {
			sem.Description = strings.TrimSpace(v)
		}
	}
	return artifacts.Artifact{
		ID:        "skill:" + source,
		Kind:      artifacts.KindSkill,
		Source:    source,
		Body:      []byte(body),
		Semantics: sem,
	}
}

// matrixFor resolves the capability record a test target projects against.
func matrixFor(target artifacts.Target) CapabilityMatrix {
	switch target {
	case artifacts.TargetOMP:
		return OMPCapabilities()
	case artifacts.TargetCodex:
		return CodexCapabilities()
	default:
		return ClaudeCapabilities()
	}
}
