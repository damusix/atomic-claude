package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

func enrollLedgerKinds(t *testing.T, home string, kinds ...string) {
	t.Helper()
	ledger := &installstate.Ledger{}
	for _, kind := range kinds {
		root := filepath.Join(home, kind)
		ledger.UpsertTarget(installstate.TargetRecord{Harness: kind, Instance: root, NativeRoot: root})
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyInstallRecord(t *testing.T, home string) {
	t.Helper()
	cfg := config.Default()
	cfg.Install.Version = "6.0.0"
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
}

// stubAgentsChoice answers the interactive harness choice and records the
// options it was offered; a nil answer behaves as a non-interactive terminal.
func stubAgentsChoice(t *testing.T, answer harness.Kind) *[]harness.Kind {
	t.Helper()
	var offered []harness.Kind
	orig := chooseAgentsHarness
	chooseAgentsHarness = func(options []harness.Kind) (harness.Kind, error) {
		offered = options
		if answer == "" {
			return "", prompt.ErrNonInteractive
		}
		return answer, nil
	}
	t.Cleanup(func() { chooseAgentsHarness = orig })
	return &offered
}

func TestAgentsHarnesses(t *testing.T) {
	strongCodex := map[string]string{"CODEX_THREAD_ID": "t"}
	weakOMP := map[string]string{"AGENT": "1", "CI": "true"}
	ambiguous := map[string]string{"CLAUDECODE": "1", "CODEX_THREAD_ID": "t"}
	tests := []struct {
		name     string
		flag     string
		env      map[string]string
		enrolled []string
		legacy   bool
		choice   harness.Kind
		want     []harness.Kind
		offered  []harness.Kind
	}{
		{name: "flag wins over a strong detection", flag: "omp", env: strongCodex, enrolled: []string{"claude"}, want: []harness.Kind{harness.KindOMP}},
		{name: "strong detection", env: strongCodex, enrolled: []string{"claude", "omp"}, want: []harness.Kind{harness.KindCodex}},
		{name: "single enrolled on weak", env: weakOMP, enrolled: []string{"codex"}, want: []harness.Kind{harness.KindCodex}},
		{name: "single enrolled on ambiguous", env: ambiguous, enrolled: []string{"omp"}, want: []harness.Kind{harness.KindOMP}},
		{name: "single enrolled on none", enrolled: []string{"claude"}, want: []harness.Kind{harness.KindClaude}},
		{name: "unadopted legacy claude counts as installed", legacy: true, want: []harness.Kind{harness.KindClaude}},
		{name: "interactive choice among the enrolled", enrolled: []string{"claude", "omp"}, choice: harness.KindOMP,
			want: []harness.Kind{harness.KindOMP}, offered: []harness.Kind{harness.KindClaude, harness.KindOMP}},
		{name: "interactive choice on an empty home offers every harness", choice: harness.KindCodex,
			want: []harness.Kind{harness.KindCodex}, offered: []harness.Kind{harness.KindClaude, harness.KindOMP, harness.KindCodex}},
		{name: "all edits every enrolled harness", flag: "all", env: strongCodex, enrolled: []string{"claude", "omp"}, want: []harness.Kind{harness.KindClaude, harness.KindOMP}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			enrollLedgerKinds(t, home, tt.enrolled...)
			if tt.legacy {
				writeLegacyInstallRecord(t, home)
			}
			offered := stubAgentsChoice(t, tt.choice)

			got, _, err := agentsHarnesses(tt.flag, home, tt.env)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("agentsHarnesses = %v, %v; want %v", got, err, tt.want)
			}
			if !slices.Equal(*offered, tt.offered) {
				t.Errorf("choice offered %v, want %v", *offered, tt.offered)
			}
		})
	}
}

func TestAgentsHarnessesRefusesNonInteractivelyListingTheEnrolledKinds(t *testing.T) {
	home := t.TempDir()
	enrollLedgerKinds(t, home, "claude", "omp")
	stubAgentsChoice(t, "")

	_, _, err := agentsHarnesses("", home, nil)
	if err == nil || !strings.Contains(err.Error(), "enrolled: claude, omp") || !strings.Contains(err.Error(), "--harness") {
		t.Fatalf("err = %v, want a refusal listing the enrolled kinds and naming --harness", err)
	}
}

// A bare ~/.claude directory is not an Atomic install, so it does not make
// Claude the single harness.
func TestAgentsHarnessesIgnoresAClaudeRootWithoutAnInstallRecord(t *testing.T) {
	home := t.TempDir()
	enrollLedgerKinds(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubAgentsChoice(t, "")
	if got, _, err := agentsHarnesses("", home, nil); err == nil {
		t.Fatalf("agentsHarnesses = %v, want a refusal", got)
	}
}

func TestAgentsHarnessesAllRefusesWithNothingEnrolled(t *testing.T) {
	home := t.TempDir()
	enrollLedgerKinds(t, home)
	if got, _, err := agentsHarnesses("all", home, nil); err == nil {
		t.Fatalf("agentsHarnesses(all) = %v, want a refusal", got)
	}
}

func TestAgentsHarnessesRefusesAnUnknownFlag(t *testing.T) {
	if _, _, err := agentsHarnesses("pi", t.TempDir(), nil); err == nil {
		t.Fatal("agentsHarnesses accepted --harness pi")
	}
}

func TestAgentsHarnessesSurfacesALedgerReadError(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.LedgerPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LedgerPath(home), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	offered := stubAgentsChoice(t, harness.KindClaude)
	if _, _, err := agentsHarnesses("", home, nil); err == nil {
		t.Fatal("an unreadable ledger was read as nothing enrolled")
	}
	if *offered != nil {
		t.Error("an unreadable ledger opened the interactive choice")
	}
}
