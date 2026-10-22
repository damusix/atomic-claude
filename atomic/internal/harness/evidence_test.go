package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssessEstablishesOwnershipOnlyFromVerifiableBytes(t *testing.T) {
	dir := t.TempDir()
	selected := []byte("embedded generation bytes\n")
	selectedDigest := managedfile.Digest(selected)

	selectedPath := filepath.Join(dir, "commands", "commit.md")
	writeFile(t, selectedPath, string(selected))
	driftedPath := filepath.Join(dir, "commands", "plan.md")
	writeFile(t, driftedPath, "hand-edited older generation\n")
	blockPath := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, blockPath, "user prose\n<atomic>\nold atomic body\n</atomic>\ntrailing\n")
	ambiguousPath := filepath.Join(dir, "ambiguous.md")
	writeFile(t, ambiguousPath, "<atomic>\none\n</atomic>\n<atomic>\ntwo\n</atomic>\n")
	missingPath := filepath.Join(dir, "missing.md")

	tests := []struct {
		name     string
		claim    Claim
		evidence Evidence
		owned    bool
	}{
		{
			name:     "selection bytes match the embedded generation",
			claim:    Claim{ID: "commands/commit.md", Kind: managedfile.KindFile, Path: selectedPath, SelectedDigest: selectedDigest},
			evidence: EvidenceSelection,
			owned:    true,
		},
		{
			name:     "different bytes stay unowned",
			claim:    Claim{ID: "commands/plan.md", Kind: managedfile.KindFile, Path: driftedPath, SelectedDigest: selectedDigest},
			evidence: EvidenceUnowned,
		},
		{
			name:     "a name alone never proves ownership",
			claim:    Claim{ID: "commands/commit.md", Kind: managedfile.KindFile, Path: selectedPath},
			evidence: EvidenceUnowned,
		},
		{
			name:     "a unique block is verifiable evidence",
			claim:    Claim{ID: "CLAUDE.md", Kind: managedfile.KindBlock, Path: blockPath},
			evidence: EvidenceBlock,
			owned:    true,
		},
		{
			name:     "a block matching the selected generation is selection evidence",
			claim:    Claim{ID: "CLAUDE.md", Kind: managedfile.KindBlock, Path: blockPath, SelectedDigest: managedfile.Digest([]byte("<atomic>\nold atomic body\n</atomic>\n"))},
			evidence: EvidenceSelection,
			owned:    true,
		},
		{
			name:     "two blocks are ambiguous and preserved",
			claim:    Claim{ID: "ambiguous.md", Kind: managedfile.KindBlock, Path: ambiguousPath},
			evidence: EvidenceConflict,
		},
		{
			name:     "a listed but absent resource is drift",
			claim:    Claim{ID: "missing.md", Kind: managedfile.KindFile, Path: missingPath, SelectedDigest: selectedDigest},
			evidence: EvidenceMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Assess(tt.claim)
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if got.Evidence != tt.evidence {
				t.Errorf("evidence = %s, want %s", got.Evidence, tt.evidence)
			}
			if got.Owned != tt.owned {
				t.Errorf("owned = %v, want %v", got.Owned, tt.owned)
			}
			if got.Observed.Path != tt.claim.Path {
				t.Errorf("observed path = %q, want %q", got.Observed.Path, tt.claim.Path)
			}
		})
	}
}

func TestAssessAllKeepsClaimOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.md")
	second := filepath.Join(dir, "b.md")
	writeFile(t, first, "a\n")
	writeFile(t, second, "b\n")

	got, err := AssessAll([]Claim{
		{ID: "a", Kind: managedfile.KindFile, Path: first, SelectedDigest: managedfile.Digest([]byte("a\n"))},
		{ID: "b", Kind: managedfile.KindFile, Path: second},
	})
	if err != nil {
		t.Fatalf("AssessAll: %v", err)
	}
	if len(got) != 2 || got[0].Claim.ID != "a" || got[1].Claim.ID != "b" {
		t.Fatalf("assessments = %+v", got)
	}
	if !got[0].Owned || got[1].Owned {
		t.Errorf("ownership = %v/%v, want true/false", got[0].Owned, got[1].Owned)
	}
}
