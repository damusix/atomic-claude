package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestBareInstallDetectsInstalledHarnesses: a bare install names what it found
// before the plan and selects exactly the harnesses whose native root exists.
func TestBareInstallDetectsInstalledHarnesses(t *testing.T) {
	cases := []struct {
		name string
		dirs []string // relative to home
		env  []string // %H expands to the home
		want []string // harness:relative root
		not  []string
	}{
		{"none installs Claude", nil, nil, []string{"claude:.claude"}, []string{"omp ", "codex "}},
		{"only Claude", []string{".claude"}, nil, []string{"claude:.claude"}, []string{"omp ", "codex "}},
		{"only OMP", []string{".omp/agent"}, nil, []string{"omp:.omp/agent"}, []string{"claude ", "codex "}},
		{"only Codex", []string{"cx"}, []string{"CODEX_HOME=%H/cx"}, []string{"codex:cx"}, []string{"claude ", "omp "}},
		{"unset CODEX_HOME ignores ~/.codex", []string{".codex"}, nil, []string{"claude:.claude"}, []string{"codex "}},
		{"missing CODEX_HOME is not found", nil, []string{"CODEX_HOME=%H/cx"}, []string{"claude:.claude"}, []string{"codex "}},
		{"CLAUDE_CONFIG_DIR existing directory", []string{"cc"}, []string{"CLAUDE_CONFIG_DIR=%H/cc"}, []string{"claude:cc"}, []string{"omp ", "codex "}},
		{"all three", []string{".claude", ".omp/agent", "cx"}, []string{"CODEX_HOME=%H/cx"},
			[]string{"claude:.claude", "omp:.omp/agent", "codex:cx"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			for _, d := range tc.dirs {
				mkdir(t, filepath.Join(home, filepath.FromSlash(d)))
			}
			var env []string
			for _, e := range tc.env {
				env = append(env, strings.ReplaceAll(e, "%H", home))
			}
			out, code := runAtomicCLIEnv(t, home, env, "install", "--dry-run")
			if code != 0 {
				t.Fatalf("exit = %d:\n%s", code, out)
			}
			var parts []string
			for _, w := range tc.want {
				kind, rel, _ := strings.Cut(w, ":")
				parts = append(parts, kind+" ("+filepath.Join(home, filepath.FromSlash(rel))+")")
			}
			detected := "detected: " + strings.Join(parts, ", ") + "\n"
			if !strings.HasPrefix(out, detected) {
				t.Errorf("output = %q, want it to start with %q", out, detected)
			}
			plan := strings.TrimPrefix(out, detected)
			for _, n := range tc.not {
				if strings.Contains(plan, strings.TrimSpace(n)+":") {
					t.Errorf("plan selected %q:\n%s", n, plan)
				}
			}
		})
	}
}

// TestInstallHarnessNarrowsTheDetectedSet: --harness omp on a home holding all
// three harnesses installs only OMP.
func TestInstallHarnessNarrowsTheDetectedSet(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".claude", ".omp/agent", "cx"} {
		mkdir(t, filepath.Join(home, filepath.FromSlash(d)))
	}
	out, code := runAtomicCLIEnv(t, home, []string{"CODEX_HOME=" + filepath.Join(home, "cx")}, "install", "--harness", "omp", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if strings.Contains(out, "detected:") || strings.Contains(out, "claude:") || strings.Contains(out, "codex:") {
		t.Errorf("--harness omp touched other harnesses:\n%s", out)
	}
	if !strings.Contains(out, "omp:") {
		t.Errorf("--harness omp selected no OMP target:\n%s", out)
	}
}

func TestInstallUsageShowsHarnessOptional(t *testing.T) {
	if !strings.Contains(installUsage, "[--harness <claude|omp|codex>]") {
		t.Errorf("usage = %q, want --harness shown as optional", installUsage)
	}
}

// TestBareInstallDryRunOnFoundSetWritesNothing: even a legacy root, which a
// real install adopts, is only planned under --dry-run.
func TestBareInstallDryRunOnFoundSetWritesNothing(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)
	before := treeDigestCLI(t, home)

	out, code := runAtomicCLI(t, home, "install", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "detected: claude (") || !strings.Contains(out, "dry-run") {
		t.Errorf("output = %q, want the detected line and the dry-run notice", out)
	}
	if treeDigestCLI(t, home) != before {
		t.Error("--dry-run changed the home")
	}
}

// TestInstallAdoptsLegacyClaudeRoot: a legacy Claude root reached by discovery
// or by --harness claude adopts with the automatic-adoption defaults.
func TestInstallAdoptsLegacyClaudeRoot(t *testing.T) {
	for _, args := range [][]string{{"install", "--yes"}, {"install", "--harness", "claude", "--yes"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home := t.TempDir()
			root, older := agedLegacyClaude(t, home)

			out, code := runAtomicCLI(t, home, args...)
			if code != 0 {
				t.Fatalf("exit = %d:\n%s", code, out)
			}
			ledger, err := installstate.LoadLedger(config.LedgerPath(home))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := ledger.FindTarget("claude", root); !ok {
				t.Errorf("install did not enroll the Claude root: %+v", ledger.Targets)
			}
			for path, was := range older {
				now, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(now) == was {
					t.Errorf("%s still holds the older-version bytes", path)
				}
			}
			assertRecoveryCopies(t, home, root, older)
		})
	}
}

// TestBareInstallLeaveUnownedKeepsOlderArtifacts: an explicit --leave-unowned
// outranks the replace default.
func TestBareInstallLeaveUnownedKeepsOlderArtifacts(t *testing.T) {
	home := t.TempDir()
	_, older := agedLegacyClaude(t, home)

	out, code := runAtomicCLI(t, home, "install", "--leave-unowned", "--yes")
	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for path, was := range older {
		now, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(now) != was {
			t.Errorf("%s was replaced despite --leave-unowned", path)
		}
	}
}

// TestInstallLegacyBlockerExitsOneWithoutWriting: a pending proposed CLAUDE.md
// is user content, so install names it and the resolution and changes nothing.
func TestInstallLegacyBlockerExitsOneWithoutWriting(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	proposed := claudeinstall.ProposedPath(home)
	mustWrite(t, proposed, "proposed\n")
	before := treeDigestCLI(t, root)

	out, code := runAtomicCLI(t, home, "install", "--yes")
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	for _, want := range []string{proposed, "re-run `atomic install`"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
	if treeDigestCLI(t, root) != before {
		t.Error("a blocked install changed the native tree")
	}
	if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
		t.Errorf("a blocked install wrote a ledger (stat err = %v)", err)
	}
}

// TestInstallAdoptionNeedsConsentBeforeWriting: a non-interactive run without
// --yes refuses before the legacy Claude root is adopted.
func TestInstallAdoptionNeedsConsentBeforeWriting(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	ompRoot := filepath.Join(home, ".omp", "agent")
	mkdir(t, ompRoot)
	atomicDir := filepath.Join(home, ".atomic")
	claudeBefore, ompBefore, atomicBefore := treeDigestCLI(t, root), treeDigestCLI(t, ompRoot), treeDigestCLI(t, atomicDir)

	_, diag, code := runAtomicCLIFull(t, home, nil, "install")
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, diag)
	}
	if !strings.Contains(diag, "explicit consent") {
		t.Errorf("diagnostic = %q, want the consent refusal", diag)
	}
	if treeDigestCLI(t, root) != claudeBefore || treeDigestCLI(t, ompRoot) != ompBefore {
		t.Error("a refused install changed a native tree")
	}
	if treeDigestCLI(t, atomicDir) != atomicBefore {
		t.Error("a refused install wrote backups or state")
	}
	if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
		t.Errorf("a refused install wrote a ledger (stat err = %v)", err)
	}
}

// TestInstallBlockerReportedBeforeConsent: a pending proposed CLAUDE.md is
// named even when no --yes can be given, not hidden behind the consent refusal.
func TestInstallBlockerReportedBeforeConsent(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	proposed := claudeinstall.ProposedPath(home)
	mustWrite(t, proposed, "proposed\n")
	before := treeDigestCLI(t, root)

	out, diag, code := runAtomicCLIFull(t, home, nil, "install")
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s\n%s", code, out, diag)
	}
	if !strings.Contains(out, proposed) || strings.Contains(diag, "explicit consent") {
		t.Errorf("stdout = %q, stderr = %q, want the blocker named, not the consent refusal", out, diag)
	}
	if treeDigestCLI(t, root) != before {
		t.Error("a blocked install changed the native tree")
	}
}

// TestInstallBlockedAdoptionStopsMixedHome: a blocked Claude adoption stops
// the whole install, so no other harness is written either.
func TestInstallBlockedAdoptionStopsMixedHome(t *testing.T) {
	for _, args := range [][]string{{"install"}, {"install", "--yes"}} {
		home := t.TempDir()
		root, _ := agedLegacyClaude(t, home)
		proposed := claudeinstall.ProposedPath(home)
		mustWrite(t, proposed, "proposed\n")
		ompRoot := filepath.Join(home, ".omp", "agent")
		mkdir(t, ompRoot)
		before := treeDigestCLI(t, root)

		out, diag, code := runAtomicCLIFull(t, home, nil, args...)
		if code != 1 || !strings.Contains(out, proposed) {
			t.Errorf("%v: exit = %d, want 1 naming %q:\n%s\n%s", args, code, proposed, out, diag)
		}
		if _, err := os.Stat(filepath.Join(ompRoot, "AGENTS.md")); !os.IsNotExist(err) {
			t.Errorf("%v: OMP was written despite the blocker (stat err = %v)", args, err)
		}
		if treeDigestCLI(t, root) != before {
			t.Errorf("%v: a blocked install changed the Claude tree", args)
		}
		if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
			t.Errorf("%v: a blocked install wrote a ledger (stat err = %v)", args, err)
		}
	}
}

// A settings.json whose hooks a writer would coerce is an adoption blocker, so
// the whole install stops before consent and no harness converges.
func TestInstallSettingsShapeBlocksWholeInstall(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	settings := filepath.Join(root, "settings.json")
	mustWrite(t, settings, `{"model":"sonnet","hooks":[]}`+"\n")
	ompRoot := filepath.Join(home, ".omp", "agent")
	mkdir(t, ompRoot)
	before := treeDigestCLI(t, root)

	out, diag, code := runAtomicCLIFull(t, home, nil, "install", "--yes")
	if code != 1 || !strings.Contains(out+diag, settings+`: "hooks" must be an object; found array`) {
		t.Fatalf("exit = %d, want 1 naming the file and shape:\n%s\n%s", code, out, diag)
	}
	if treeDigestCLI(t, root) != before {
		t.Error("a shape-blocked adoption changed the Claude tree")
	}
	if _, err := os.Stat(filepath.Join(ompRoot, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("OMP was written beside a blocked adoption (stat err = %v)", err)
	}
	if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
		t.Errorf("a blocked install wrote a ledger (stat err = %v)", err)
	}

	mustWrite(t, settings, `{"model":"sonnet"}`+"\n")
	if out, diag, code := runAtomicCLIFull(t, home, nil, "install", "--yes"); code != 0 {
		t.Fatalf("rerun after the fix: exit = %d:\n%s\n%s", code, out, diag)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", root); !ok {
		t.Error("adoption did not proceed once the shape was fixed")
	}
	if _, ok := ledger.FindTarget("omp", ompRoot); !ok {
		t.Error("OMP did not converge once the shape was fixed")
	}
}

// TestInstallAllLeavesLegacyClaudeUnadopted: --all behaves as before, which
// never adopted.
func TestInstallAllLeavesLegacyClaudeUnadopted(t *testing.T) {
	home := t.TempDir()
	root, older := agedLegacyClaude(t, home)

	out, code := runAtomicCLI(t, home, "install", "--all", "--yes")
	if code != 1 || !strings.Contains(out, "conflicted") {
		t.Fatalf("exit = %d, want 1 with the legacy root conflicted:\n%s", code, out)
	}
	for path, was := range older {
		if now, _ := os.ReadFile(path); string(now) != was {
			t.Errorf("--all replaced %s", path)
		}
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", root); ok {
		t.Error("--all enrolled the legacy Claude root")
	}
	for _, row := range ledger.Rows {
		if strings.HasPrefix(row.Target, "claude:") {
			t.Errorf("--all recorded a Claude row: %+v", row)
		}
	}
}

// TestInstallJSONAdoptionBlockerExitsOne: the blocker exit holds under --json,
// and stdout stays one JSON document naming the blocker.
func TestInstallJSONAdoptionBlockerExitsOne(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)
	proposed := claudeinstall.ProposedPath(home)
	mustWrite(t, proposed, "proposed\n")

	out, code := runAtomicCLI(t, home, "install", "--json", "--yes")
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if !strings.Contains(out, proposed) {
		t.Errorf("stdout = %q, want the blocker naming %s", out, proposed)
	}
}

// A scripted caller reads only the exit code, so --json must not turn a blocked
// or conflicted target into success.
func TestPrintConvergeReportsJSONReportsBlocked(t *testing.T) {
	cases := map[string]install.ConvergeReport{
		"blocked":    {Target: harness.Target{Kind: harness.KindClaude, Instance: "/c"}, Status: harness.StatusStale, Blockers: []string{"declined"}},
		"conflicted": {Target: harness.Target{Kind: harness.KindClaude, Instance: "/c"}, Status: harness.StatusConflicted},
	}
	for name, report := range cases {
		for _, jsonOut := range []bool{false, true} {
			var blocked bool
			captureOutput(t, func() {
				blocked = printConvergeReports("install", []install.ConvergeReport{report}, false, jsonOut)
			})
			if !blocked {
				t.Errorf("%s, json=%v: printConvergeReports reported not blocked", name, jsonOut)
			}
		}
	}
	var blocked bool
	captureOutput(t, func() {
		blocked = printConvergeReports("install", []install.ConvergeReport{{Status: harness.StatusConverged}}, false, true)
	})
	if blocked {
		t.Error("a converged target reported blocked")
	}
}

// TestInstallReportsAdoptionAsApplied: an adoption replaced files, so the
// output says so.
func TestInstallReportsAdoptionAsApplied(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)

	out, code := runAtomicCLI(t, home, "install", "--yes")
	if code != 0 || !strings.Contains(out, "applied") {
		t.Errorf("exit = %d, output = %q, want an applied target", code, out)
	}
}

// TestBareInstallSurvivesOMPProbeFailure: a failing `omp` makes OMP not found
// with one warning; a Claude-only install still succeeds.
func TestBareInstallSurvivesOMPProbeFailure(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".claude"))
	bin := t.TempDir()
	mustWrite(t, filepath.Join(bin, "omp"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "omp"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + bin}, "install", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d:\n%s\n%s", code, out, diag)
	}
	if !strings.Contains(out, "detected: claude (") || strings.Contains(out, "omp (") {
		t.Errorf("output = %q, want Claude only", out)
	}
	if n := strings.Count(diag, "not installed"); n != 1 {
		t.Errorf("diagnostic holds %d omp warnings, want 1:\n%s", n, diag)
	}
}
