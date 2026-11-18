package harness

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

func TestSelectAgentsHarness(t *testing.T) {
	strongOMP := Detection{Kind: KindOMP, Confidence: ConfidenceStrong}
	weakOMP := Detection{Kind: KindOMP, Confidence: ConfidenceWeak}
	ambiguous := Detection{Confidence: ConfidenceAmbiguous, Candidates: []Kind{KindClaude, KindCodex}}
	none := Detection{Confidence: ConfidenceNone}

	tests := []struct {
		name     string
		flag     string
		detected Detection
		enrolled []Kind
		want     Kind
		wantErr  bool
	}{
		{"flag wins over strong detection", "codex", strongOMP, []Kind{KindClaude}, KindCodex, false},
		{"strong detection acts", "", strongOMP, []Kind{KindClaude, KindCodex}, KindOMP, false},
		{"weak falls back to the single enrolled", "", weakOMP, []Kind{KindClaude}, KindClaude, false},
		{"ambiguous falls back to the single enrolled", "", ambiguous, []Kind{KindOMP}, KindOMP, false},
		{"none falls back to the single enrolled", "", none, []Kind{KindCodex}, KindCodex, false},
		{"weak never acts on its own", "", weakOMP, nil, "", true},
		{"two enrolled refuse", "", none, []Kind{KindClaude, KindOMP}, "", true},
		{"invalid flag refuses", "bogus", strongOMP, []Kind{KindClaude}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectAgentsHarness(tt.flag, func() Detection { return tt.detected }, tt.enrolled)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("SelectAgentsHarness = %q, %v; want %q, err %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSelectAgentsHarnessRefusalListsEnrolledKinds(t *testing.T) {
	enrolled := []Kind{KindClaude, KindOMP}
	_, err := SelectAgentsHarness("", func() Detection { return Detection{Confidence: ConfidenceNone} }, enrolled)
	var unresolved *UnresolvedHarnessError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v, want *UnresolvedHarnessError", err)
	}
	if !reflect.DeepEqual(unresolved.Enrolled, enrolled) {
		t.Errorf("Enrolled = %v, want %v", unresolved.Enrolled, enrolled)
	}
}

func TestEnrolledKindsReadsTheLedgerOncePerKind(t *testing.T) {
	home := t.TempDir()
	ledger := &installstate.Ledger{}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: "omp", Instance: filepath.Join(home, "a"), NativeRoot: filepath.Join(home, "a")})
	ledger.UpsertTarget(installstate.TargetRecord{Harness: "claude", Instance: "c", NativeRoot: filepath.Join(home, ".claude")})
	ledger.UpsertTarget(installstate.TargetRecord{Harness: "omp", Instance: filepath.Join(home, "b"), NativeRoot: filepath.Join(home, "b")})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	got, err := EnrolledKinds(home)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Kind{KindClaude, KindOMP}; !reflect.DeepEqual(got, want) {
		t.Errorf("EnrolledKinds = %v, want %v", got, want)
	}
}

func TestEnrolledKindsOnAnEmptyHome(t *testing.T) {
	got, err := EnrolledKinds(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("EnrolledKinds = %v, %v; want none", got, err)
	}
}
