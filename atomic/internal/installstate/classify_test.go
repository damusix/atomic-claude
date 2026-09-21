package installstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func classifyReq(home, root string, t *testing.T) ClassifyRequest {
	t.Helper()
	return ClassifyRequest{Home: home, NativeRoot: root, Target: "claude:default", Claims: claimsFor(selectedArtifacts(t, root))}
}

func TestClassifyAbsent(t *testing.T) {
	home := newHome(t)
	c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if c.State != StateAbsent {
		t.Fatalf("state = %s, want %s", c.State, StateAbsent)
	}
}

func TestClassifyLegacyComplete(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	c, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if c.State != StateLegacyComplete {
		t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, StateLegacyComplete)
	}
	if !c.Legacy.SteeringBlock {
		t.Error("steering block evidence not observed")
	}
	if c.Legacy.Snapshot.State != "valid" {
		t.Errorf("snapshot state = %s, want valid", c.Legacy.Snapshot.State)
	}
	if len(c.Legacy.ListedMissing) != 0 {
		t.Errorf("listed missing = %v, want none", c.Legacy.ListedMissing)
	}
	stamp := config.BackupStampDir(home, "2026-09-01T10-00-00Z")
	mkfile(t, filepath.Join(stamp, "CLAUDE.md"), "backup of a prior generation\n")
	withBackup, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatal(err)
	}
	if len(withBackup.Legacy.Backups) != 1 {
		t.Errorf("timestamped backups = %v, want one", withBackup.Legacy.Backups)
	}
	// Structure is complete; ownership is not proven for the aged file artifacts.
	if len(c.Resources) != len(embedded.Manifest()) {
		t.Fatalf("resources = %d, want %d", len(c.Resources), len(embedded.Manifest()))
	}
	var unowned int
	for _, r := range c.Resources {
		if r.Assessment.Evidence == managedfile.EvidenceUnowned {
			unowned++
		}
	}
	if unowned != len(embedded.Manifest())-1 {
		t.Errorf("unowned resources = %d, want %d (all but the block)", unowned, len(embedded.Manifest())-1)
	}
}

func TestClassifyLegacyPartialDrift(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)
	deleteListed(t, root, "commands/commit.md")

	c, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if c.State != StateLegacyPartial {
		t.Fatalf("state = %s, want %s", c.State, StateLegacyPartial)
	}
	if !contains(c.Legacy.ListedMissing, "commands/commit.md") {
		t.Errorf("listed missing = %v, want commands/commit.md", c.Legacy.ListedMissing)
	}
	if !contains(c.Drift, "commands/commit.md") {
		t.Errorf("drift = %v, want commands/commit.md", c.Drift)
	}
}

func TestClassifyLegacyPartialPendingProposal(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	writeProposal(t, home)

	c, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if c.State != StateLegacyPartial || !c.Legacy.Proposed {
		t.Fatalf("state = %s proposed = %v, want partial with proposal", c.State, c.Legacy.Proposed)
	}
}

func TestClassifyLegacyPartialCorruptSnapshot(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	corruptSnapshot(t, home)

	c, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if c.State != StateLegacyPartial {
		t.Fatalf("state = %s, want %s", c.State, StateLegacyPartial)
	}
	if c.Legacy.Snapshot.State != "corrupt" {
		t.Errorf("snapshot state = %s, want corrupt", c.Legacy.Snapshot.State)
	}
	if len(c.Legacy.Snapshot.MissingCopies) == 0 {
		t.Error("corrupt snapshot recorded no missing copies")
	}
}

func TestClassifyV2States(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		home := newHome(t)
		writeLedgerFixture(t, home,
			[]TargetRecord{{Harness: "claude", Instance: "default", NativeRoot: filepath.Join(home, ".claude"), Status: "enrolled"}},
			[]Row{{Target: "claude:default", Resource: "commands/commit.md"}})

		c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
		if err != nil {
			t.Fatal(err)
		}
		if c.State != StateV2Clean {
			t.Fatalf("state = %s, want %s", c.State, StateV2Clean)
		}
	})

	t.Run("in-flight", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "commit.md")
		mkfile(t, path, "applied\n")
		m := fileMutationAt(t, "commands.commit.md", path, []byte("applied\n"))
		writeJournalFixture(t, home, journalWith("op-inflight", []Mutation{m}, nil))

		c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
		if err != nil {
			t.Fatal(err)
		}
		if c.State != StateV2InFlight {
			t.Fatalf("state = %s, want %s", c.State, StateV2InFlight)
		}
		if len(c.V2.Journals) != 1 {
			t.Errorf("unresolved journals = %v, want 1", c.V2.Journals)
		}
	})

	t.Run("orphaned", func(t *testing.T) {
		home := newHome(t)
		if err := os.MkdirAll(config.TransactionStageDir(home, "op-orphan"), 0o755); err != nil {
			t.Fatal(err)
		}
		c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
		if err != nil {
			t.Fatal(err)
		}
		if c.State != StateV2Orphaned {
			t.Fatalf("state = %s, want %s", c.State, StateV2Orphaned)
		}
		if len(c.V2.Orphans) == 0 {
			t.Error("orphan remnants not reported")
		}
	})
}

// A ledger row whose recorded bytes no longer match the artifact on disk is
// ordinary per-resource drift, not a mixed install: the owning resource's
// verdict carries it, so the state stays decidable and repair can replace it.
func TestClassifyDriftedRowIsResourceDrift(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)
	drifted := filepath.Join(root, "commands", "commit.md")
	writeLedgerFixture(t, home, nil, []Row{{
		Target:   "claude:default",
		Resource: "commands/commit.md",
		Applied:  AppliedValue{Path: drifted, Kind: managedfile.KindFile, Digest: "0000"},
	}})

	c, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if c.State == StateMixed {
		t.Fatalf("drifted row classified the install mixed (%v); a row/bytes disagreement is per-resource drift", c.Conflicts)
	}
	if !containsPrefix(c.Drift, drifted) {
		t.Errorf("drift = %v, want the changed resource reported", c.Drift)
	}
	var assessed bool
	for _, r := range c.Resources {
		if r.Assessment.Claim.Path != drifted {
			continue
		}
		assessed = true
		if r.Assessment.Evidence != managedfile.EvidenceUnowned {
			t.Errorf("drifted resource evidence = %s, want %s (a replace-or-leave-unowned decision)", r.Assessment.Evidence, managedfile.EvidenceUnowned)
		}
	}
	if !assessed {
		t.Errorf("the drifted resource was not assessed: %+v", c.Resources)
	}
}

func containsPrefix(values []string, prefix string) bool {
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

func TestClassifyRefusesNewerSchema(t *testing.T) {
	home := newHome(t)
	if err := os.MkdirAll(config.InstallDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := `{"schema_version":99,"writer_version":"v99","minimum_reader_version":1,"rows":[],"targets":[]}`
	if err := os.WriteFile(config.LedgerPath(home), []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Classify(ClassifyRequest{Home: home}); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Classify error = %v, want newer-schema refusal", err)
	}
}

// A journal the binary cannot parse is still evidence: it could own native
// bytes, so its install must never classify as absent or v2-clean, and its
// transaction directory must not be reported as an unjournaled remnant.
func TestClassifyUnreadableJournalIsEvidence(t *testing.T) {
	cases := map[string]string{
		"malformed":    "{ not json",
		"newer schema": `{"schema_version":99,"writer_version":"v99","minimum_reader_version":1,"operation_id":"op-unreadable","created":"2026-09-16T10:00:00Z"}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			for _, withTransaction := range []bool{false, true} {
				home := newHome(t)
				journalPath := config.JournalPath(home, "op-unreadable")
				mkfile(t, journalPath, content)
				tx := config.TransactionDir(home, "op-unreadable")
				if withTransaction {
					if err := os.MkdirAll(tx, 0o755); err != nil {
						t.Fatal(err)
					}
				}

				c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
				if err != nil {
					t.Fatalf("Classify: %v", err)
				}
				if c.State == StateAbsent || c.State == StateV2Clean {
					t.Fatalf("state = %s, want a refusal state for an unreadable journal", c.State)
				}
				if c.State != StateMixed {
					t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, StateMixed)
				}
				if !contains(c.V2.Unreadable, journalPath) {
					t.Errorf("unreadable journals = %v, want %s", c.V2.Unreadable, journalPath)
				}
				if len(c.Conflicts) == 0 {
					t.Error("unreadable journal reported no conflict detail")
				}
				if withTransaction && contains(c.V2.Orphans, tx) {
					t.Errorf("orphans = %v, want the unreadable journal's transaction dir excluded", c.V2.Orphans)
				}
			}
		})
	}
}

// A v2-only install has no legacy evidence to hide behind, so the ledger is the
// only ownership proof the classifier can judge: a row whose bytes no longer
// match is surface drift, never a mixed install, and a matching row is clean.
func TestClassifyV2OnlyLedgerDisagreement(t *testing.T) {
	const native = "native bytes\n"

	t.Run("drifted row is drift", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "commit.md")
		mkfile(t, path, native)
		writeLedgerFixture(t, home, nil, []Row{{
			Target: "claude:default", Resource: "commands/commit.md",
			Applied: AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"},
		}})

		c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
		if err != nil {
			t.Fatal(err)
		}
		if c.State != StateV2Clean {
			t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, StateV2Clean)
		}
		if !containsPrefix(c.Drift, path) {
			t.Errorf("drift = %v, want the changed resource reported", c.Drift)
		}
	})

	t.Run("matching row is clean", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "commit.md")
		mkfile(t, path, native)
		digest, err := managedfile.DigestResourceBytes([]byte(native), managedfile.KindFile)
		if err != nil {
			t.Fatal(err)
		}
		writeLedgerFixture(t, home, nil, []Row{{
			Target: "claude:default", Resource: "commands/commit.md",
			Applied: AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: digest},
		}})

		c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
		if err != nil {
			t.Fatal(err)
		}
		if c.State != StateV2Clean {
			t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, StateV2Clean)
		}
	})

	t.Run("journal-owned row stays in flight", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "commit.md")
		mkfile(t, path, "applied\n")
		writeLedgerFixture(t, home, nil, []Row{{
			Target: "claude:default", Resource: "commands/commit.md",
			Applied: AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"},
		}})
		m := fileMutationAt(t, "commands.commit.md", path, []byte("applied\n"))
		writeJournalFixture(t, home, journalWith("op-inflight", []Mutation{m}, nil))

		c, err := Classify(ClassifyRequest{Home: home, NativeRoot: filepath.Join(home, ".claude")})
		if err != nil {
			t.Fatal(err)
		}
		if c.State != StateV2InFlight {
			t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, StateV2InFlight)
		}
	})
}

// A ledger the binary cannot parse must be refused outright, never read as an
// empty enrollment that claims nothing.
func TestClassifyRefusesMalformedLedger(t *testing.T) {
	home := newHome(t)
	mkfile(t, config.LedgerPath(home), "{ not json")
	if _, err := Classify(ClassifyRequest{Home: home}); err == nil || !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("Classify error = %v, want ledger refusal", err)
	}
}

func mkfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
