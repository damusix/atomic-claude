package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// The adoption prompt is the only consent for a replacement, so it names the
// replacement and recovery copies; leave-unowned replaces nothing.
func TestAdoptApprovalDescNamesReplacementAndRecovery(t *testing.T) {
	plan := installstate.AdoptionPlan{}
	got := adoptApprovalDesc(plan, installstate.DecisionReplace)
	for _, want := range []string{"older-version", "recovery copies"} {
		if !strings.Contains(got, want) {
			t.Errorf("replace prompt = %q, want %q", got, want)
		}
	}
	if left := adoptApprovalDesc(plan, installstate.DecisionLeaveUnowned); strings.Contains(left, "replaced") {
		t.Errorf("leave-unowned prompt = %q, must not claim a replacement", left)
	}
}

// detectSteps wires the real Claude, OMP, and Codex adapters to injected roots,
// so installed-harness discovery never depends on the process environment.
func detectSteps(t *testing.T, home, ompProfile, codexRoot string) Steps {
	t.Helper()
	c := claude.New()
	c.ConfigDir = func(h string) string { return filepath.Join(h, ".claude") }
	o := omp.New()
	o.AmbientProfile = func() string { return ompProfile }
	o.ConfigPath = func(h, profile string) (string, error) {
		if profile == "" {
			return filepath.Join(h, ".omp", "agent"), nil
		}
		return filepath.Join(h, ".omp", "profiles", profile), nil
	}
	x := codex.New()
	x.ResolveHome = func(string) (string, error) {
		if codexRoot == "" {
			return "", harness.ErrUnsupported
		}
		return codexRoot, nil
	}
	return testSteps(t, home, c, o, x)
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func detectedKeys(t *testing.T, steps Steps, sel Selection) string {
	t.Helper()
	sel.Detect = true
	instances, err := steps.Resolve(sel)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var keys []string
	for _, inst := range instances {
		keys = append(keys, string(inst.Kind)+":"+inst.NativeRoot)
	}
	return strings.Join(keys, "|")
}

// TestDetectSelectsEveryInstalledNativeRoot: a bare install reaches exactly the
// harnesses whose native root exists, and falls back to Claude when none does.
func TestDetectSelectsEveryInstalledNativeRoot(t *testing.T) {
	cases := []struct {
		name     string
		dirs     []string // relative to home
		codexRel string   // CODEX_HOME relative to home; "" is unset
		want     []string
	}{
		{"none installs Claude", nil, "", []string{"claude:.claude"}},
		{"only Claude", []string{".claude"}, "", []string{"claude:.claude"}},
		{"only OMP", []string{".omp/agent"}, "", []string{"omp:.omp/agent"}},
		{"only Codex", []string{"cx"}, "cx", []string{"codex:cx"}},
		{"unset CODEX_HOME never finds Codex", []string{".codex"}, "", []string{"claude:.claude"}},
		{"set CODEX_HOME naming a missing directory is not found", nil, "cx", []string{"claude:.claude"}},
		{"all three", []string{".claude", ".omp/agent", "cx"}, "cx",
			[]string{"claude:.claude", "omp:.omp/agent", "codex:cx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			for _, d := range tc.dirs {
				mkdirs(t, filepath.Join(home, filepath.FromSlash(d)))
			}
			codexRoot := ""
			if tc.codexRel != "" {
				codexRoot = filepath.Join(home, tc.codexRel)
			}
			var want []string
			for _, w := range tc.want {
				kind, rel, _ := strings.Cut(w, ":")
				want = append(want, kind+":"+filepath.Join(home, filepath.FromSlash(rel)))
			}
			got := detectedKeys(t, detectSteps(t, home, "", codexRoot), Selection{})
			if got != strings.Join(want, "|") {
				t.Errorf("detected = %s, want %v", got, want)
			}
		})
	}
}

// TestDetectChecksTheNamedOMPProfileOnly: OMP_PROFILE makes the default
// profile's root irrelevant.
func TestDetectChecksTheNamedOMPProfileOnly(t *testing.T) {
	home := t.TempDir()
	claudeRoot := filepath.Join(home, ".claude")
	workRoot := filepath.Join(home, ".omp", "profiles", "work")

	mkdirs(t, filepath.Join(home, ".omp", "agent"))
	got := detectedKeys(t, detectSteps(t, home, "work", ""), Selection{})
	if want := "claude:" + claudeRoot; got != want {
		t.Errorf("named profile absent: detected = %s, want %s", got, want)
	}

	mkdirs(t, workRoot)
	got = detectedKeys(t, detectSteps(t, home, "work", ""), Selection{})
	if want := "omp:" + workRoot; got != want {
		t.Errorf("named profile present: detected = %s, want %s", got, want)
	}
}

// TestDetectInstanceNarrowsTheFoundSet: --instance applies to the found set as
// it does to an explicit harness.
func TestDetectInstanceNarrowsTheFoundSet(t *testing.T) {
	home := t.TempDir()
	ompRoot := filepath.Join(home, ".omp", "agent")
	mkdirs(t, filepath.Join(home, ".claude"), ompRoot)
	steps := detectSteps(t, home, "", "")

	if got, want := detectedKeys(t, steps, Selection{Instance: ompRoot}), "omp:"+ompRoot; got != want {
		t.Errorf("detected = %s, want %s", got, want)
	}
	if _, err := steps.Resolve(Selection{Detect: true, Instance: filepath.Join(home, "elsewhere")}); err == nil {
		t.Error("an --instance matching nothing found resolved without error")
	}
}

// TestDetectNeverEnrolls: discovery selects targets; only convergence enrolls.
func TestDetectNeverEnrolls(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, filepath.Join(home, ".claude"))
	detectedKeys(t, detectSteps(t, home, "", ""), Selection{})
	if _, err := os.Stat(ledgerPath(home)); !os.IsNotExist(err) {
		t.Errorf("detection wrote a ledger (stat err = %v)", err)
	}
}
