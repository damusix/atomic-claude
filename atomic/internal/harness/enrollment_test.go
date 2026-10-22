package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// newHome points HOME at a temp dir and returns the resolved home, so the test
// exercises the production path helpers rather than a hardcoded layout.
func newHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestEnrollRequiresApprovedPlan(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".claude")
	path := filepath.Join(root, "commands", "commit.md")
	writeFile(t, path, "selected bytes\n")

	req := EnrollmentRequest{
		Target: Target{Kind: KindClaude, Instance: root, NativeRoot: root},
		Claims: []Claim{{ID: "commands/commit.md", Kind: managedfile.KindFile, Path: path, SelectedDigest: managedfile.Digest([]byte("selected bytes\n"))}},
	}
	got, err := Enroll(home, req)
	if !errors.Is(err, ErrPlanNotApproved) {
		t.Fatalf("unapproved enroll error = %v, want ErrPlanNotApproved", err)
	}
	if len(got.Assessments) != 1 || !got.Assessments[0].Owned {
		t.Errorf("assessments = %+v, want one verified claim reported read-only", got.Assessments)
	}
	if len(got.Adopted) != 0 {
		t.Errorf("adopted = %v, want none outside an approved plan", got.Adopted)
	}
	if _, err := os.Stat(config.LedgerPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ledger written without approval: %v", err)
	}
}

func TestEnrollRecordsOnlyVerifiedResources(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".claude")
	verified := filepath.Join(root, "commands", "commit.md")
	drifted := filepath.Join(root, "agents", "atomic-implementer.md")
	steering := filepath.Join(root, "CLAUDE.md")
	writeFile(t, verified, "selected bytes\n")
	writeFile(t, drifted, "an older generation\n")
	writeFile(t, steering, "mine\n<atomic>\natomic body\n</atomic>\n")

	req := EnrollmentRequest{
		Target: Target{Kind: KindClaude, Instance: root, NativeRoot: root},
		Claims: []Claim{
			{ID: "commands/commit.md", Kind: managedfile.KindFile, Path: verified, SelectedDigest: managedfile.Digest([]byte("selected bytes\n"))},
			{ID: "agents/atomic-implementer.md", Kind: managedfile.KindFile, Path: drifted, SelectedDigest: managedfile.Digest([]byte("selected bytes\n"))},
			{ID: "CLAUDE.md", Kind: managedfile.KindBlock, Path: steering},
			{ID: "skills/atomic-verify/SKILL.md", Kind: managedfile.KindFile, Path: filepath.Join(root, "skills/atomic-verify/SKILL.md"), SelectedDigest: managedfile.Digest([]byte("selected bytes\n"))},
		},
		Approved: true,
	}
	got, err := Enroll(home, req)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if got.Target.Status != StatusEnrolled {
		t.Errorf("target status = %s, want %s", got.Target.Status, StatusEnrolled)
	}
	if len(got.Adopted) != 2 {
		t.Errorf("adopted = %v, want the verified file and the block", got.Adopted)
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	record, ok := ledger.FindTarget(string(KindClaude), root)
	if !ok {
		t.Fatalf("no target record in %+v", ledger.Targets)
	}
	if record.NativeRoot != root || record.Status != string(StatusEnrolled) {
		t.Errorf("target record = %+v", record)
	}

	wantRows := map[string]string{
		"commands/commit.md": managedfile.Digest([]byte("selected bytes\n")),
		"CLAUDE.md":          managedfile.Digest([]byte("<atomic>\natomic body\n</atomic>\n")),
	}
	if len(ledger.Rows) != len(wantRows) {
		t.Fatalf("rows = %+v, want only verified resources", ledger.Rows)
	}
	for _, row := range ledger.Rows {
		wantDigest, ok := wantRows[row.Resource]
		if !ok {
			t.Errorf("unverified resource recorded: %+v", row)
			continue
		}
		if row.Target != got.Target.Key() || row.Consumer != got.Target.Key() {
			t.Errorf("row identity = %+v", row)
		}
		if row.Applied.Digest != wantDigest {
			t.Errorf("%s applied digest = %q, want %q", row.Resource, row.Applied.Digest, wantDigest)
		}
	}
}

func TestEnrollRefusesConflictingEvidenceWithoutWriting(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".claude")
	steering := filepath.Join(root, "CLAUDE.md")
	writeFile(t, steering, "<atomic>\none\n</atomic>\n<atomic>\ntwo\n</atomic>\n")

	_, err := Enroll(home, EnrollmentRequest{
		Target:   Target{Kind: KindClaude, Instance: root, NativeRoot: root},
		Claims:   []Claim{{ID: "CLAUDE.md", Kind: managedfile.KindBlock, Path: steering}},
		Approved: true,
	})
	if !errors.Is(err, ErrEvidenceConflict) {
		t.Fatalf("conflicting evidence error = %v, want ErrEvidenceConflict", err)
	}
	if _, err := os.Stat(config.LedgerPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ledger written despite conflict: %v", err)
	}
}

func TestEnrollRefusesUnknownOrAnonymousTargets(t *testing.T) {
	home := newHome(t)
	if _, err := Enroll(home, EnrollmentRequest{Target: Target{Kind: "vim", Instance: "/x"}, Approved: true}); err == nil {
		t.Error("unknown harness accepted")
	}
	if _, err := Enroll(home, EnrollmentRequest{Target: Target{Kind: KindClaude}, Approved: true}); err == nil {
		t.Error("empty instance identity accepted")
	}
}

func TestEnrollIsIdempotent(t *testing.T) {
	home := newHome(t)
	root := filepath.Join(home, ".claude")
	path := filepath.Join(root, "commands", "commit.md")
	writeFile(t, path, "selected bytes\n")

	req := EnrollmentRequest{
		Target:   Target{Kind: KindClaude, Instance: root, NativeRoot: root},
		Claims:   []Claim{{ID: "commands/commit.md", Kind: managedfile.KindFile, Path: path, SelectedDigest: managedfile.Digest([]byte("selected bytes\n"))}},
		Approved: true,
	}
	first, err := Enroll(home, req)
	if err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	before, err := os.ReadFile(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Enroll(home, req)
	if err != nil {
		t.Fatalf("second enroll: %v", err)
	}
	after, err := os.ReadFile(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Adopted) != len(first.Adopted) {
		t.Errorf("second adopted = %v, want %v", second.Adopted, first.Adopted)
	}
	if string(before) != string(after) {
		t.Errorf("re-enrolling the same target rewrote the ledger:\n%s\n---\n%s", before, after)
	}
}
