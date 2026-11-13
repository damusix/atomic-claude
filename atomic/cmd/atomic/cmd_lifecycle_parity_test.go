package main

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// assertBundleBytes requires the selected generation's bytes in every artifact
// under root except CLAUDE.md and agents, whose bytes carry overlays.
func assertBundleBytes(t *testing.T, root string) {
	t.Helper()
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" || a.Kind == "agent" {
			continue
		}
		want, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.Target)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s does not hold the selected generation's bytes", a.Target)
		}
	}
}

func assertUnchanged(t *testing.T, want map[string]string) {
	t.Helper()
	for path, was := range want {
		if got, err := os.ReadFile(path); err != nil || string(got) != was {
			t.Errorf("%s changed: %q (%v)", path, got, err)
		}
	}
}

// Users on an already-current binary still get migrations and the doctor when
// update adopts their legacy install.
func TestUpdateAlreadyCurrentAdoptsLegacyThenMigratesAndRunsDoctor(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)
	t.Setenv("HOME", home)

	deps := stubUpdateDeps(t)
	deps.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
		return false, nil
	}
	deps.converge = convergeEnrolledTargets
	deps.repoRoot = func() (string, bool) { return "", false }
	migrated, doctored := false, false
	deps.migrate = func(string) error { migrated = true; return nil }
	deps.doctor = func(io.Writer) { doctored = true }
	var errOut strings.Builder
	deps.errOut = &errOut

	if code := runUpdateWith(nil, deps); code != 0 {
		t.Fatalf("update exited %d:\n%s", code, errOut.String())
	}
	if !migrated || !doctored {
		t.Errorf("migrated = %v, doctor ran = %v; want both", migrated, doctored)
	}
}

// The documented Claude-only path: `claude install`, then `update`, then
// `update` again, all succeed with no manual adoption step.
func TestClaudeInstallThenUpdateNeedsNoManualAdoption(t *testing.T) {
	home := t.TempDir()
	if out, code := runAtomicCLI(t, home, "claude", "install"); code != 0 {
		t.Fatalf("claude install exited %d:\n%s", code, out)
	}
	for i := 0; i < 2; i++ {
		out, errOut, code := runUpdateConverge(t, home)
		if code != 0 || strings.Contains(errOut, "harness adopt") {
			t.Fatalf("update %d exited %d:\n%s\n%s", i+1, code, out, errOut)
		}
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", filepath.Join(home, ".claude")); !ok {
		t.Error("update did not take ownership of the Claude root")
	}
}

// The explicit adopt verb still asks for a decision, so it must name the flags
// that give one.
func TestAdoptRefusalNamesTheDecisionFlags(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)
	out, code := runAtomicCLI(t, home, "harness", "adopt", "claude", "--acknowledge-snapshot", "--yes")
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	for _, flag := range []string{"--replace", "--leave-unowned"} {
		if !strings.Contains(out, flag) {
			t.Errorf("refusal does not name %s:\n%s", flag, out)
		}
	}
}

func adoptLeavingUnowned(t *testing.T, home string) (string, map[string]string) {
	t.Helper()
	root, older := agedLegacyClaude(t, home)
	if out, code := runAtomicCLI(t, home, "harness", "adopt", "claude", "--leave-unowned", "--acknowledge-snapshot", "--yes"); code != 0 {
		t.Fatalf("adopt --leave-unowned exited %d:\n%s", code, out)
	}
	return root, older
}

// A leave-unowned decision is the user's: every later verb that carries no
// decision keeps the files, converges, and the doctor stops warning.
func TestLeaveUnownedDecisionPersists(t *testing.T) {
	verbs := map[string]func(t *testing.T, home string) (string, int){
		"claude update": func(t *testing.T, home string) (string, int) { return runAtomicCLI(t, home, "claude", "update") },
		"install": func(t *testing.T, home string) (string, int) {
			return runAtomicCLI(t, home, "install", "--harness", "claude", "--yes")
		},
		"repair": func(t *testing.T, home string) (string, int) {
			return runAtomicCLI(t, home, "harness", "repair", "--yes")
		},
		"repair --all": func(t *testing.T, home string) (string, int) {
			return runAtomicCLI(t, home, "harness", "repair", "--all", "--yes")
		},
		"update": func(t *testing.T, home string) (string, int) {
			out, errOut, code := runUpdateConverge(t, home)
			if strings.Contains(errOut, "convergence failed") {
				code = 1
			}
			return out + errOut, code
		},
	}
	for name, run := range verbs {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			root, older := adoptLeavingUnowned(t, home)

			if out, code := run(t, home); code != 0 {
				t.Fatalf("exit = %d:\n%s", code, out)
			}
			assertUnchanged(t, older)
			out, _ := runAtomicCLI(t, home, "root", "doctor", "--only", "1")
			if !strings.Contains(out, "PASS  install") {
				t.Errorf("doctor flags kept files:\n%s", out)
			}
			status, code := runAtomicCLI(t, home, "harness", "status", "claude:"+root, "--json")
			if code != 0 || strings.Contains(status, `"blockers"`) || !strings.Contains(status, filepath.Join(root, "commands", "commit.md")) {
				t.Errorf("status (exit %d) does not report the kept files as left:\n%s", code, status)
			}
		})
	}
}

// `atomic install --leave-unowned` adopts a legacy root, and a later bare
// install must keep that decision rather than ask for it again.
func TestInstallLeaveUnownedDecisionPersists(t *testing.T) {
	home := t.TempDir()
	_, older := agedLegacyClaude(t, home)
	if out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--leave-unowned", "--yes"); code != 0 {
		t.Fatalf("install --leave-unowned exited %d:\n%s", code, out)
	}
	if out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("bare install after --leave-unowned exited %d:\n%s", code, out)
	}
	assertUnchanged(t, older)
}

// --replace overrides a recorded leave-unowned decision and forgets it.
func TestReplaceForgetsLeaveUnowned(t *testing.T) {
	home := t.TempDir()
	root, older := adoptLeavingUnowned(t, home)

	if out, code := runAtomicCLI(t, home, "harness", "repair", "--replace", "--yes"); code != 0 {
		t.Fatalf("repair --replace exited %d:\n%s", code, out)
	}
	assertBundleBytes(t, root)
	assertConvergeRecoveryCopies(t, home, older)
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Left) != 0 {
		t.Errorf("--replace kept left records: %v", ledger.Left)
	}
}

// A kept file the user deletes is recreated and stops being left.
func TestVanishedLeftFileIsRecreated(t *testing.T) {
	home := t.TempDir()
	root, _ := adoptLeavingUnowned(t, home)
	gone := filepath.Join(root, "commands", "commit.md")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	if out, code := runAtomicCLI(t, home, "claude", "update"); code != 0 {
		t.Fatalf("claude update exited %d:\n%s", code, out)
	}
	if !pathExists(gone) {
		t.Error("the vanished file was not recreated")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if ledger.IsLeft("claude:"+root, gone) {
		t.Error("the recreated file is still recorded as left")
	}
}

func assertConvergeRecoveryCopies(t *testing.T, home string, older map[string]string) {
	t.Helper()
	kept := map[string]bool{}
	_ = filepath.WalkDir(config.BackupDir(home), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil {
			kept[string(data)] = true
		}
		return nil
	})
	for path, was := range older {
		if !kept[was] {
			t.Errorf("no recovery copy of %s", path)
		}
	}
}

func enrolledClaude(t *testing.T, home string) string {
	t.Helper()
	if out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	return filepath.Join(home, ".claude")
}

// A user edit to a file Atomic owns blocks the refresh and survives it.
func TestClaudeUpdateBlocksOnOwnedFileEdit(t *testing.T) {
	home := t.TempDir()
	root := enrolledClaude(t, home)
	edited := filepath.Join(root, "commands", "commit.md")
	mustWrite(t, edited, "my edit\n")

	if out, code := runAtomicCLI(t, home, "claude", "update"); code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	assertUnchanged(t, map[string]string{edited: "my edit\n"})
}

// Leaving an edited owned file unowned releases Atomic's claim on it, so
// status and doctor agree it is the user's.
func TestLeavingAnEditedOwnedFileReleasesItsRow(t *testing.T) {
	home := t.TempDir()
	root := enrolledClaude(t, home)
	edited := filepath.Join(root, "commands", "commit.md")
	mustWrite(t, edited, "my edit\n")

	if out, code := runAtomicCLI(t, home, "harness", "repair", "--leave-unowned", "--yes"); code != 0 {
		t.Fatalf("repair --leave-unowned exited %d:\n%s", code, out)
	}
	status, _ := runAtomicCLI(t, home, "harness", "status", "claude:"+root, "--json")
	if strings.Contains(status, `"path": "`+edited+`"`) {
		t.Errorf("status still reports a row for the released file:\n%s", status)
	}
	if out, _ := runAtomicCLI(t, home, "root", "doctor", "--only", "1"); !strings.Contains(out, "PASS  install") {
		t.Errorf("doctor flags the released file:\n%s", out)
	}
	assertUnchanged(t, map[string]string{edited: "my edit\n"})
}

// A user's file at a path the selected generation newly claims is not
// Atomic's to replace: it waits for the batched decision.
func TestNewlyClaimedPathWithUserFileWaitsForDecision(t *testing.T) {
	home := t.TempDir()
	root := enrolledClaude(t, home)
	path := filepath.Join(root, "commands", "commit.md")
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	kept := ledger.Rows[:0:0]
	for _, row := range ledger.Rows {
		if row.Applied.Path != path {
			kept = append(kept, row)
		}
	}
	ledger.Rows = kept
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, "user file\n")

	out, code := runAtomicCLI(t, home, "harness", "repair", "--yes")
	if code != 1 || !strings.Contains(out, "--leave-unowned") {
		t.Fatalf("exit = %d, want 1 naming the decision:\n%s", code, out)
	}
	assertUnchanged(t, map[string]string{path: "user file\n"})
}

// olderBinaryRewrite is an older binary's `claude update` over an owned root:
// an [install] record without the adopted marker, and older artifact bytes.
func olderBinaryRewrite(t *testing.T, home, root string) map[string]string {
	t.Helper()
	if _, err := claudeinstall.Update(root, home, false, claudeinstall.RealClock); err != nil {
		t.Fatal(err)
	}
	older := map[string]string{}
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(a.Target))
		older[path] = "older " + a.Target + "\n"
		mustWrite(t, path, older[path])
	}
	return older
}

// An older binary's bytes match neither the selected generation nor the
// ledger, so the refresh waits for the decision it names.
func TestOlderBinaryRewriteWaitsForDecision(t *testing.T) {
	home := t.TempDir()
	root := enrolledClaude(t, home)
	older := olderBinaryRewrite(t, home, root)

	_, diag, code := runAtomicCLIFull(t, home, nil, "claude", "update")
	if code != 1 || !strings.Contains(diag, "--replace") {
		t.Fatalf("exit = %d, want 1 naming --replace:\n%s", code, diag)
	}
	assertUnchanged(t, older)

	out, _, _ := runUpdateConverge(t, home)
	if strings.Contains(out, "adopting existing Claude install") {
		t.Errorf("update re-adopted the owned root:\n%s", out)
	}
	assertUnchanged(t, older)

	if out, code := runAtomicCLI(t, home, "harness", "repair", "--replace", "--yes"); code != 0 {
		t.Fatalf("repair --replace exited %d:\n%s", code, out)
	}
	assertBundleBytes(t, root)
	assertConvergeRecoveryCopies(t, home, older)
}

// A legacy install into an unrelated project clears the home-wide adopted
// mark; that is no evidence about the owned default root.
func TestUnrelatedLegacyInstallDoesNotReadoptOwnedRoot(t *testing.T) {
	home := t.TempDir()
	root := enrolledClaude(t, home)
	edited := filepath.Join(root, "commands", "commit.md")
	mustWrite(t, edited, "my edit\n")
	project := filepath.Join(t.TempDir(), ".claude")
	if out, code := runAtomicCLI(t, home, "claude", "install", "--target", project); code != 0 {
		t.Fatalf("claude install --target exited %d:\n%s", code, out)
	}

	out, _, _ := runUpdateConverge(t, home)
	if strings.Contains(out, "adopting existing Claude install at "+root) {
		t.Errorf("update re-adopted the owned root:\n%s", out)
	}
	assertUnchanged(t, map[string]string{edited: "my edit\n"})
}

// --no-hooks on an owned root never registers the session-start hook, and an
// already-registered one stays: removing it is `atomic hooks uninstall`'s job.
func TestClaudeNoHooksOnEnrolledRoot(t *testing.T) {
	home := t.TempDir()
	root := enrolledClaude(t, home)

	if out, code := runAtomicCLI(t, home, "claude", "update", "--no-hooks"); code != 0 {
		t.Fatalf("claude update --no-hooks exited %d:\n%s", code, out)
	}
	if installed, _, _ := hooks.IsInstalledInDir(root); !installed {
		t.Error("--no-hooks removed an already-registered hook")
	}

	if _, err := hooks.UninstallInDir(root); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"install", "update"} {
		if out, code := runAtomicCLI(t, home, "claude", verb, "--no-hooks"); code != 0 {
			t.Fatalf("claude %s --no-hooks exited %d:\n%s", verb, code, out)
		}
		if installed, _, _ := hooks.IsInstalledInDir(root); installed {
			t.Errorf("claude %s --no-hooks registered the hook", verb)
		}
	}
}

// An install on any harness leaves the per-user state a first install left:
// the profile stub and config.toml, plus the pre-install snapshot for Claude.
func TestInstallSeedsUserState(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		snapshot bool
	}{
		{"install claude", []string{"install", "--harness", "claude", "--yes"}, true},
		{"install omp", []string{"install", "--harness", "omp", "--yes"}, false},
		{"enroll claude", []string{"harness", "enroll", "claude", "--yes"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			mkdir(t, filepath.Join(home, ".omp", "agent"))
			if out, code := runAtomicCLI(t, home, c.args...); code != 0 {
				t.Fatalf("exit = %d:\n%s", code, out)
			}
			for _, path := range []string{config.ProfilePath(home), config.TOMLPath(home)} {
				if !pathExists(path) {
					t.Errorf("did not create %s", path)
				}
			}
			manifest := filepath.Join(config.PreInstallDir(home), "manifest.json")
			if pathExists(manifest) != c.snapshot {
				t.Errorf("pre-install manifest present = %v, want %v", pathExists(manifest), c.snapshot)
			}
		})
	}
}

// The pre-install snapshot is write-once, so an install that writes nothing
// must not take it: the next install would find it frozen.
func TestRefusedInstallTakesNoSnapshot(t *testing.T) {
	cases := map[string]struct {
		seed func(t *testing.T, home string)
		args []string
	}{
		"blocked": {
			seed: func(t *testing.T, home string) {
				mustWrite(t, filepath.Join(home, ".claude", "commands", "commit.md"), "user file\n")
			},
			args: []string{"install", "--harness", "claude", "--yes"},
		},
		"declined": {
			seed: func(t *testing.T, home string) { mkdir(t, filepath.Join(home, ".claude")) },
			args: []string{"install", "--harness", "claude"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			c.seed(t, home)
			if out, code := runAtomicCLI(t, home, c.args...); code == 0 {
				t.Fatalf("install exited 0, want a refusal:\n%s", out)
			}
			if pathExists(config.PreInstallDir(home)) {
				t.Error("a refused install took the write-once snapshot")
			}
		})
	}
}

func TestInstallKeepsExistingProfile(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, config.ProfilePath(home), "mine\n")
	enrolledClaude(t, home)
	assertUnchanged(t, map[string]string{config.ProfilePath(home): "mine\n"})
}
