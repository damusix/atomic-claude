package claude

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func newHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
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

// copyTree copies a fixture tree into dest, preserving the relative layout.
func copyTree(t *testing.T, src, dest string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", src, err)
	}
}

// treeDigests maps every file under root to its bytes, so a read-only test can
// prove discovery changed nothing.
func treeDigests(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func TestAdapterReportsClaudeKindAndCP0Capabilities(t *testing.T) {
	a := New()
	if a.Kind() != harness.KindClaude {
		t.Errorf("kind = %s", a.Kind())
	}
	caps := a.Capabilities()
	if caps.Root.Env != ConfigDirEnv {
		t.Errorf("root env = %q, adapter constant = %q", caps.Root.Env, ConfigDirEnv)
	}
	if caps.Supports(harness.RoleStaticScope) {
		t.Error("adapter claims a static-scope surface CP0 never proved")
	}
	// The lifecycle cutover binds every hook: a target must project the selected
	// generation's claims rather than report an unwired surface.
	home := newHome(t)
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := harness.Target{Kind: harness.KindClaude, Instance: root, NativeRoot: root}
	plan, err := a.Lifecycle(home).Project(target, harness.PlanRequest{})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(plan.Claims) == 0 || plan.Generation == "" {
		t.Errorf("project plan = %+v, want claims and a generation", plan)
	}
}

func TestDiscoverReportsConfiguredAndDefaultRoots(t *testing.T) {
	home := newHome(t)
	defaultRoot := filepath.Join(home, ".claude")
	if err := os.MkdirAll(defaultRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(t.TempDir(), "isolated")
	if err := os.MkdirAll(configured, 0o755); err != nil {
		t.Fatal(err)
	}

	a := &Adapter{ConfigDir: func(string) string { return configured }}
	instances, err := a.Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %+v, want the configured and default roots", instances)
	}
	if instances[0].NativeRoot != configured || !instances[0].Exists {
		t.Errorf("configured instance = %+v", instances[0])
	}
	if instances[1].NativeRoot != defaultRoot || !instances[1].Exists {
		t.Errorf("default instance = %+v", instances[1])
	}

	// A configured root that does not exist yet is still reported: the user
	// asked for it.
	absent := &Adapter{ConfigDir: func(string) string { return filepath.Join(home, "nowhere") }}
	instances, err = absent.Discover(home)
	if err != nil {
		t.Fatalf("discover absent root: %v", err)
	}
	if len(instances) != 2 || instances[0].Exists {
		t.Errorf("instances = %+v, want an absent configured root plus the default", instances)
	}

	// With default resolution the configured root is the default one, so it
	// appears once.
	implicit := &Adapter{ConfigDir: DefaultConfigDir}
	t.Setenv(ConfigDirEnv, "")
	instances, err = implicit.Discover(home)
	if err != nil {
		t.Fatalf("discover default: %v", err)
	}
	if len(instances) != 1 || instances[0].NativeRoot != defaultRoot {
		t.Errorf("instances = %+v, want only %s", instances, defaultRoot)
	}

	if _, err := a.Discover(""); err == nil {
		t.Error("discovery without a home accepted")
	}
}

func TestDefaultConfigDirHonorsEnvOverride(t *testing.T) {
	home := t.TempDir()
	// The adapter's root resolution and its CP0 capability row must agree, so a
	// changed native root cannot drift from the evidence that justifies it.
	if caps := harness.ClaudeCapabilities(); caps.Root.Env != ConfigDirEnv || caps.Root.DefaultDir != defaultRootDir {
		t.Fatalf("adapter root (%s, %s) disagrees with CP0 row (%s, %s)", ConfigDirEnv, defaultRootDir, caps.Root.Env, caps.Root.DefaultDir)
	}
	t.Setenv(ConfigDirEnv, "/tmp/isolated-claude")
	if got := DefaultConfigDir(home); got != "/tmp/isolated-claude" {
		t.Errorf("DefaultConfigDir with override = %q", got)
	}
	t.Setenv(ConfigDirEnv, "  ")
	if got := DefaultConfigDir(home); got != filepath.Join(home, ".claude") {
		t.Errorf("DefaultConfigDir with blank override = %q", got)
	}
}

func TestSelectionClaimsMatchTheEmbeddedGeneration(t *testing.T) {
	root := "/home/u/.claude"
	claims := SelectionClaims(root)
	manifest := embedded.Manifest()

	if len(claims) != len(manifest)-1 {
		t.Fatalf("claims = %d, want one per artifact minus global steering (%d)", len(claims), len(manifest)-1)
	}
	for _, c := range claims {
		if c.ID == steeringTarget {
			t.Errorf("global steering duplicated as a file claim: %+v", c)
		}
		if c.Kind != "file" || c.SelectedDigest == "" {
			t.Errorf("claim %+v carries no selected-generation digest", c)
		}
		if !strings.HasPrefix(c.Path, root+string(filepath.Separator)) {
			t.Errorf("claim path %q escapes %s", c.Path, root)
		}
	}

	steering := SteeringClaim(root)
	if steering.Kind != "block" || steering.ID != steeringTarget {
		t.Errorf("steering claim = %+v", steering)
	}
	if steering.Path != filepath.Join(root, "CLAUDE.md") {
		t.Errorf("steering path = %q", steering.Path)
	}
}

func TestDiscoveryDoesNotEnroll(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	copyTree(t, filepath.Join("..", "testdata", "claude", "native"), root)
	before := treeDigests(t, root)

	adapter := New()
	instances, err := adapter.Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	selected, err := harness.Select(instances, harness.Selector{Kind: harness.KindClaude})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if selected.NativeRoot != root {
		t.Fatalf("selected %s, want %s", selected.NativeRoot, root)
	}

	claims := append([]harness.Claim{SteeringClaim(root)}, SelectionClaims(root)...)
	assessments, err := harness.AssessAll(claims)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	owned := 0
	for _, a := range assessments {
		if a.Owned {
			owned++
		}
	}
	if owned != 1 {
		t.Errorf("owned = %d, want only the global steering block", owned)
	}

	after := treeDigests(t, root)
	if len(before) != len(after) {
		t.Errorf("native tree changed: %d files before, %d after", len(before), len(after))
	}
	for path, data := range before {
		if after[path] != data {
			t.Errorf("discovery rewrote %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("discovery created %s/.atomic: %v", home, err)
	}
}

// TestEnrollVerifiedClaudeTargetEndToEnd drives the production entry points
// against a real HOME: discovery finds the native root, explicit selection
// picks it, and an approved plan records exactly the resources whose bytes the
// selected generation verifies.
func TestEnrollVerifiedClaudeTargetEndToEnd(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	copyTree(t, filepath.Join("..", "testdata", "claude", "native"), root)

	// One artifact installed with the selected binary's exact bytes, one drifted
	// legacy file, and the global steering block from the fixture.
	selectedPath := filepath.Join(root, "commands", "commit.md")
	selected, err := embedded.FS.ReadFile("bundle/commands/commit.md")
	if err != nil {
		t.Fatalf("read embedded artifact: %v", err)
	}
	writeFile(t, selectedPath, string(selected))

	adapter := New()
	instances, err := adapter.Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	selectedInstance, err := harness.Select(instances, harness.Selector{Kind: harness.KindClaude})
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	claims := append([]harness.Claim{SteeringClaim(root)}, SelectionClaims(root)...)
	enrollment, err := harness.Enroll(home, harness.EnrollmentRequest{
		Target:   selectedInstance.Target(),
		Claims:   claims,
		Approved: true,
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	sort.Strings(enrollment.Adopted)
	want := []string{"CLAUDE.md", "commands/commit.md"}
	if len(enrollment.Adopted) != len(want) {
		t.Fatalf("adopted = %v, want %v (the drifted legacy agent must stay unowned)", enrollment.Adopted, want)
	}
	for i, id := range want {
		if enrollment.Adopted[i] != id {
			t.Errorf("adopted = %v, want %v", enrollment.Adopted, want)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	record, ok := ledger.FindTarget(string(harness.KindClaude), root)
	if !ok {
		t.Fatalf("target not enrolled: %+v", ledger.Targets)
	}
	if record.NativeRoot != root || record.Status != string(harness.StatusEnrolled) {
		t.Errorf("target record = %+v", record)
	}
	if len(ledger.Rows) != len(want) {
		t.Fatalf("rows = %+v, want %v", ledger.Rows, want)
	}
	for _, row := range ledger.Rows {
		if row.Target != selectedInstance.Target().Key() {
			t.Errorf("row target = %q", row.Target)
		}
	}
}

// TestConvergeWritesSettingsUnderCustomNamedRoot proves a Claude target whose
// root is not literally named `.claude` still owns its settings mutations.
// Resolving settings from the root's parent writes `<parent>/.claude/
// settings.json` — never read by Claude, and for a root like ~/.claude-work a
// mutation of the user's default target. Each root below is the shape the
// reported defect took: a config dir nested under `.config`, and an instance
// directory sitting beside the default target.
func TestConvergeWritesSettingsUnderCustomNamedRoot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		root    func(home string) string
		sibling string // parent-relative path a pre-fix write would create
	}{
		{"nested config dir", func(home string) string { return filepath.Join(home, ".config", "claude") }, ".config/.claude"},
		{"instance beside default", func(home string) string { return filepath.Join(home, ".claude-work") }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := newHome(t)
			customRoot := tc.root(home)
			t.Setenv(ConfigDirEnv, customRoot)

			// A default target discovery also reports, and that converging the
			// custom root must not touch.
			defaultSettingsPath := filepath.Join(home, ".claude", "settings.json")
			writeFile(t, defaultSettingsPath, "{\"model\":\"sonnet\"}\n")
			before, err := os.ReadFile(defaultSettingsPath)
			if err != nil {
				t.Fatal(err)
			}

			adapter := New()
			instances, err := adapter.Discover(home)
			if err != nil {
				t.Fatalf("discover: %v", err)
			}
			var selected harness.Instance
			for _, inst := range instances {
				if inst.NativeRoot == customRoot {
					selected = inst
				}
			}
			if selected.NativeRoot != customRoot {
				t.Fatalf("discovery did not report the configured root: %+v", instances)
			}

			root := selected.NativeRoot
			claims := append([]harness.Claim{SteeringClaim(root)}, SelectionClaims(root)...)
			if _, err := harness.Enroll(home, harness.EnrollmentRequest{
				Target:   selected.Target(),
				Claims:   claims,
				Approved: true,
			}); err != nil {
				t.Fatalf("enroll: %v", err)
			}

			life := adapter.Lifecycle(home)
			target := selected.Target()
			plan, err := life.Project(target, harness.PlanRequest{BatchDecision: installstate.DecisionReplace})
			if err != nil {
				t.Fatalf("project: %v", err)
			}
			if len(plan.Blockers) > 0 {
				t.Fatalf("unexpected blockers: %v", plan.Blockers)
			}
			if _, err := life.Converge(target, plan); err != nil {
				t.Fatalf("converge: %v", err)
			}

			// Both settings mutations landed in the custom root's own
			// settings.json.
			installed, drifted, err := hooks.IsInstalledInDir(root)
			if err != nil {
				t.Fatalf("IsInstalled: %v", err)
			}
			if !installed || drifted {
				t.Errorf("hook state = installed:%v drifted:%v, want a clean registration", installed, drifted)
			}
			style, present, err := hooks.ReadOutputStyle(filepath.Join(root, "settings.json"))
			if err != nil {
				t.Fatalf("read outputStyle: %v", err)
			}
			if !present || style != hooks.OutputStyleName {
				t.Errorf("outputStyle = %q present=%v, want %q", style, present, hooks.OutputStyleName)
			}

			if tc.sibling != "" {
				if _, err := os.Stat(filepath.Join(home, tc.sibling)); !os.IsNotExist(err) {
					t.Errorf("stray %s written beside the custom root: %v", tc.sibling, err)
				}
			}
			after, err := os.ReadFile(defaultSettingsPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("default target settings mutated: %q", after)
			}
		})
	}
}

// setSetting rewrites one top-level member of a JSON settings file, preserving
// every other member, so a test can model a user edit without rebuilding the
// file's exact bytes.
func setSetting(t *testing.T, path, key string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	settings[key] = value
	out, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// convergeDefaultClaudeTarget enrolls and converges the default Claude root so
// its settings mutations and their ledger ownership are in place.
func convergeDefaultClaudeTarget(t *testing.T, home, root string) harness.Target {
	t.Helper()
	adapter := New()
	instances, err := adapter.Discover(home)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	selected, err := harness.Select(instances, harness.Selector{Kind: harness.KindClaude})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	target := selected.Target()
	claims := append([]harness.Claim{SteeringClaim(root)}, SelectionClaims(root)...)
	if _, err := harness.Enroll(home, harness.EnrollmentRequest{Target: target, Claims: claims, Approved: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	life := adapter.Lifecycle(home)
	plan, err := life.Project(target, harness.PlanRequest{BatchDecision: installstate.DecisionReplace})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(plan.Blockers) > 0 {
		t.Fatalf("unexpected blockers: %v", plan.Blockers)
	}
	if _, err := life.Converge(target, plan); err != nil {
		t.Fatalf("converge: %v", err)
	}
	return target
}

// TestConvergeRecordsSettingsOwnership proves the settings mutations converge
// owns are ledger resources: after converge the ledger carries a settings row,
// a target uninstall removes exactly the Atomic members, and the user's
// unrelated keys survive.
func TestConvergeRecordsSettingsOwnership(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(root, "settings.json")
	writeFile(t, settingsPath, "{\"model\":\"opus\"}\n")

	target := convergeDefaultClaudeTarget(t, home, root)

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	row, ok := ledger.Find(target.Key(), "settings.json")
	if !ok {
		t.Fatalf("no settings row: %+v", ledger.Rows)
	}
	if row.Applied.Kind != managedfile.KindSettings || row.Applied.Digest == "" {
		t.Fatalf("settings row applied = %+v, want a settings kind with a digest", row.Applied)
	}

	// The ledger must agree with the bytes on disk: the member-wise settings
	// observation is what classification reads, so a mismatch here would report
	// a healthy install as mixed.
	classification, err := installstate.Classify(installstate.ClassifyRequest{
		Home:       home,
		NativeRoot: root,
		Claims:     GlobalClaims(root),
	})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if classification.State != installstate.StateV2Clean {
		t.Errorf("post-converge state = %s (conflicts %v), want v2-clean", classification.State, classification.Conflicts)
	}

	// A user edit to an unrelated key is not drift on the owned members.
	setSetting(t, settingsPath, "model", "sonnet")

	if _, err := harness.RemoveTargetResources(home, target); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	installed, drifted, err := hooks.IsInstalledInDir(root)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed || drifted {
		t.Errorf("hook registration survived uninstall: installed=%v drifted=%v", installed, drifted)
	}
	if _, present, err := hooks.ReadOutputStyle(settingsPath); err != nil || present {
		t.Errorf("outputStyle survived uninstall: present=%v err=%v", present, err)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json removed by uninstall: %v", err)
	}
	var remaining map[string]any
	if err := json.Unmarshal(after, &remaining); err != nil {
		t.Fatalf("settings.json unparseable after uninstall: %v", err)
	}
	if remaining["model"] != "sonnet" {
		t.Errorf("unrelated user key lost: %s", after)
	}
}

// TestSettingsDriftIsRepairableNotMixed proves a cleared settings.json does not
// classify the install mixed: the settings row is drift convergence repairs, so
// a repair converges and re-registers the hook, and a later uninstall still
// tolerates the absent members.
func TestSettingsDriftIsRepairableNotMixed(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(root, "settings.json")
	writeFile(t, settingsPath, "{\"model\":\"opus\"}\n")

	target := convergeDefaultClaudeTarget(t, home, root)

	// Clear the members Atomic owns; the ledger row still records what it wrote.
	writeFile(t, settingsPath, "{}\n")

	classification, err := installstate.Classify(installstate.ClassifyRequest{
		Home:       home,
		NativeRoot: root,
		Claims:     GlobalClaims(root),
	})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if classification.State != installstate.StateV2Clean {
		t.Fatalf("cleared settings state = %s (conflicts %v), want v2-clean", classification.State, classification.Conflicts)
	}
	if len(classification.Drift) == 0 {
		t.Error("cleared settings reported no drift")
	}

	life := New().Lifecycle(home)
	plan, err := life.Project(target, harness.PlanRequest{BatchDecision: installstate.DecisionReplace})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(plan.Blockers) > 0 {
		t.Fatalf("cleared settings blocked repair: %v", plan.Blockers)
	}
	if _, err := life.Converge(target, plan); err != nil {
		t.Fatalf("converge: %v", err)
	}

	installed, drifted, err := hooks.IsInstalledInDir(root)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed || drifted {
		t.Errorf("repair did not restore the registration: installed=%v drifted=%v", installed, drifted)
	}

	// Absence is not a conflict: uninstall clears the claim's row.
	if _, err := harness.RemoveTargetResources(home, target); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	if _, ok := ledger.Find(target.Key(), "settings.json"); ok {
		t.Error("uninstall left the settings row behind")
	}
}

// TestUninstallKeepsClaimWhenSettingsAreReadOnly proves a removal that cannot
// write a read-only settings.json reports the resource skipped and keeps its
// ledger row and enrollment, so a later uninstall still clears the claim.
func TestUninstallKeepsClaimWhenSettingsAreReadOnly(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(root, "settings.json")
	writeFile(t, settingsPath, "{\"model\":\"opus\"}\n")

	target := convergeDefaultClaudeTarget(t, home, root)

	if err := os.Chmod(settingsPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(settingsPath, 0o644) })

	removal, err := harness.RemoveTargetResources(home, target)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(removal.Skipped) != 1 || removal.Skipped[0] != "settings.json" {
		t.Fatalf("skipped = %v, want [settings.json]", removal.Skipped)
	}
	for _, id := range removal.Removed {
		if id == "settings.json" {
			t.Errorf("a skipped resource is reported removed: %+v", removal)
		}
	}
	installed, _, err := hooks.IsInstalledInDir(root)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("the read-only registration was removed")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	if _, ok := ledger.Find(target.Key(), "settings.json"); !ok {
		t.Error("the skipped removal dropped the ownership claim")
	}
	if _, ok := ledger.FindTarget(string(target.Kind), target.Instance); !ok {
		t.Error("the skipped removal dropped the enrollment")
	}

	// Once the file is writable, a later uninstall clears the claim.
	if err := os.Chmod(settingsPath, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := harness.RemoveTargetResources(home, target)
	if err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
	if len(second.Skipped) != 0 {
		t.Errorf("second uninstall skipped = %v, want none", second.Skipped)
	}
	ledger, err = installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if _, ok := ledger.Find(target.Key(), "settings.json"); ok {
		t.Error("the settings claim survived a writable uninstall")
	}
	if _, present, err := hooks.ReadOutputStyle(settingsPath); err != nil || present {
		t.Errorf("outputStyle survived: present=%v err=%v", present, err)
	}
}

// TestUninstallSkipsUserEditedOwnedSetting proves a later edit to an owned
// settings member is skipped rather than aborting the whole removal: the
// resource keeps its claim and the user's bytes are never overwritten, while the
// rest of the target still uninstalls.
func TestUninstallSkipsUserEditedOwnedSetting(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(root, "settings.json")
	writeFile(t, settingsPath, "{\"model\":\"opus\"}\n")

	target := convergeDefaultClaudeTarget(t, home, root)
	setSetting(t, settingsPath, "outputStyle", "Default")
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	removal, err := harness.RemoveTargetResources(home, target)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(removal.Skipped) != 1 || removal.Skipped[0] != "settings.json" {
		t.Fatalf("skipped = %v, want [settings.json]", removal.Skipped)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("uninstall rewrote the user's settings: %s", after)
	}
	if _, err := os.Stat(filepath.Join(root, "commands", "commit.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("uninstall left an otherwise-removable resource in place: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	if _, ok := ledger.Find(target.Key(), "settings.json"); !ok {
		t.Error("the skipped removal dropped the ownership claim")
	}
}
