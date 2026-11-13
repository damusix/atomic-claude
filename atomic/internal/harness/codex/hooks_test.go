package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const userHooksJSON = `{
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "say done" }] }
    ]
  }
}
`

func sessionHookRow(t *testing.T, home, root string) (installstate.Row, bool) {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	return ledger.Find(codexTarget(root).Key(), hooks.CodexHooksPath(root))
}

// A crash between the hooks.json write and the transaction's ledger commit must
// leave a record of the claim, so the next verb can repair it.
func TestEnrollRecordsSessionHookRowBeforeWritingHooksJSON(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, hooks.CodexHooksPath(root), userHooksJSON)

	var rowAtWrite, targetAtWrite bool
	crash := fmt.Errorf("simulated crash mid-write")
	defer func(orig func(string, bool) (bool, error)) { writeSessionHook = orig }(writeSessionHook)
	writeSessionHook = func(string, bool) (bool, error) {
		_, rowAtWrite = sessionHookRow(t, home, root)
		ledger, err := installstate.LoadLedger(config.LedgerPath(home))
		if err != nil {
			t.Fatal(err)
		}
		_, targetAtWrite = ledger.FindTarget(string(harness.KindCodex), root)
		return false, crash
	}

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("enroll succeeded despite the failed write")
	}
	if !rowAtWrite {
		t.Error("the ledger held no hooks.json row when the write began")
	}
	if !targetAtWrite {
		t.Error("the row had no enrolled target for repair, status and uninstall to visit")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget(string(harness.KindCodex), root); ok {
		t.Error("a failed write left the target record it had added")
	}
}

// A write the read-only file skipped must not leave the pre-write claim on disk.
func TestEnrollSkippedHooksWriteLeavesNoPersistedRow(t *testing.T) {
	home, root, a := enrolledHome(t)
	path := hooks.CodexHooksPath(root)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	// Everything else is converged, so the second enroll commits no transaction
	// and nothing later overwrites the pre-write row.
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	dropRow(ledger, codexTarget(root).Key(), path)
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, userHooksJSON)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, ok := sessionHookRow(t, home, root); ok {
		t.Error("a skipped hooks.json write left its pre-write row in the persisted ledger")
	}
}

// A failed write must not leave the pre-write claim either.
func TestEnrollFailedHooksWriteLeavesNoPersistedRow(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, hooks.CodexHooksPath(root), userHooksJSON)
	defer func(orig func(string, bool) (bool, error)) { writeSessionHook = orig }(writeSessionHook)
	writeSessionHook = func(string, bool) (bool, error) { return false, fmt.Errorf("disk full") }

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("enroll succeeded despite the failed write")
	}
	if _, ok := sessionHookRow(t, home, root); ok {
		t.Error("a failed hooks.json write left its pre-write row in the persisted ledger")
	}
}

// A crash between the row and the write leaves a canonical row over a member
// in another form; --replace rewrites it instead of blocking on the row.
func TestEnrollReplaceClearsJournaledRowOverNonCanonicalMember(t *testing.T) {
	home, root, a := enrolledHome(t)
	path := hooks.CodexHooksPath(root)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	writeFile(t, path, `{"hooks":{"SessionStart":[{"matcher":"other","hooks":[{"type":"command","command":"`+hooks.CodexSessionStartCommand+`"}]}]}}`+"\n")

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("a changed member converged without a decision")
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	obs, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil || obs.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("member digest = %q (err %v), want the canonical form", obs.Digest, err)
	}
}

func TestEnrollWritesOwnedSessionStartMember(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, hooks.CodexHooksPath(root), userHooksJSON)

	result, err := a.Enroll(EnrollRequest{Home: home, Root: root})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, hooks.CodexHooksPath(root))
	if !strings.Contains(got, `"command": "atomic hooks session-start --harness codex"`) || !strings.Contains(got, `"matcher": "startup|resume"`) {
		t.Errorf("hooks.json lacks the owned member:\n%s", got)
	}
	if !strings.Contains(got, "say done") {
		t.Errorf("enroll dropped the user's hook:\n%s", got)
	}
	found := false
	for _, p := range result.Applied {
		found = found || p == hooks.CodexHooksPath(root)
	}
	if !found {
		t.Errorf("applied %v does not name hooks.json", result.Applied)
	}

	row, ok := sessionHookRow(t, home, root)
	if !ok {
		t.Fatal("ledger records no hooks.json resource")
	}
	owned, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if row.Applied.Kind != managedfile.KindSettings || row.Applied.Digest != owned.Digest {
		t.Errorf("row = %+v, want a settings row digesting only the owned member (%s)", row.Applied, owned.Digest)
	}
}

func TestUserEditOutsideTheOwnedMemberIsNotDrift(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	before, _ := sessionHookRow(t, home, root)
	edited := strings.Replace(readFile(t, hooks.CodexHooksPath(root)), `"hooks": {`, `"description": "mine", "hooks": {`, 1)
	writeFile(t, hooks.CodexHooksPath(root), edited)

	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) > 0 || !plan.Converged {
		t.Errorf("plan = %+v, want converged with no blocker", plan)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("re-enroll: %v", err)
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != edited {
		t.Errorf("re-enroll rewrote the user's edit:\n%s", got)
	}
	if after, _ := sessionHookRow(t, home, root); after.Applied.Digest != before.Applied.Digest {
		t.Error("a user edit outside the owned member moved the recorded digest")
	}
}

func TestEditedOwnedMemberConflictsAndKeepsItsBytes(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	edited := strings.Replace(readFile(t, hooks.CodexHooksPath(root)), `"startup|resume"`, `"startup"`, 1)
	writeFile(t, hooks.CodexHooksPath(root), edited)

	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Blockers, hooks.CodexHooksPath(root)) {
		t.Errorf("plan blockers %v do not name hooks.json", plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("enroll converged over an edited owned member")
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != edited {
		t.Errorf("the edited member was rewritten:\n%s", got)
	}
}

func TestTargetUninstallStripsOnlyTheOwnedMember(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, hooks.CodexHooksPath(root), userHooksJSON)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, err := a.Lifecycle(home).Remove(codexTarget(root)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != userHooksJSON {
		t.Errorf("uninstall changed the user's bytes:\n%s", got)
	}
	if _, ok := sessionHookRow(t, home, root); ok {
		t.Error("uninstall kept the hooks.json claim")
	}
}

func TestCodexSessionContextDeliveryReadsUnsupported(t *testing.T) {
	home, root, a := enrolledHome(t)
	result, err := a.Enroll(EnrollRequest{Home: home, Root: root})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	found := false
	for _, gap := range result.Unproven {
		found = found || gap.Surface == "session-context delivery"
	}
	if !found {
		t.Errorf("enrollment unproven %+v does not report session-context delivery", result.Unproven)
	}
	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Unproven, "session-context delivery: ") {
		t.Errorf("plan unproven %v does not report session-context delivery", plan.Unproven)
	}
	if _, open := harness.CodexCapabilities().Gap(harness.RoleSessionContext); !open {
		t.Error("the Codex session-context row reads proven before a runtime probe")
	}
}

// codexIdentityHash is the trust hash Codex derives for the owned member: the
// sha256 of its normalized identity serialized as key-sorted compact JSON.
func codexIdentityHash() string {
	identity := `{"event_name":"session_start","hooks":[{"async":false,"command":"atomic hooks session-start --harness codex","timeout":600,"type":"command"}],"matcher":"startup|resume"}`
	sum := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeHookState(t *testing.T, root, trustedHash string) {
	t.Helper()
	key := hooks.CodexHooksPath(root) + ":session_start:0:0"
	writeFile(t, ConfigPath(root), fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\n", key, trustedHash))
}

func TestSessionHookTrustReadsWhatCodexRecorded(t *testing.T) {
	home, root, a := enrolledHome(t)
	if trust, _ := ObserveSessionHookTrust(home, root); trust != HookTrustUnknown {
		t.Errorf("trust with no owned member = %s, want unknown", trust)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, err := os.Stat(ConfigPath(root)); !os.IsNotExist(err) {
		t.Fatalf("enroll wrote Codex's config.toml (stat err = %v); trust is Codex's to record", err)
	}
	if trust, _ := ObserveSessionHookTrust(home, root); trust != HookUntrusted {
		t.Errorf("trust with no Codex record = %s, want untrusted", trust)
	}

	writeHookState(t, root, codexIdentityHash())
	if trust, ev := ObserveSessionHookTrust(home, root); trust != HookTrusted {
		t.Errorf("trust with the matching hash = %s (%s), want trusted", trust, ev)
	}

	// Codex reads a stale hash on the member's key as modified and runs nothing
	// until the user trusts the hook again.
	writeHookState(t, root, "sha256:0000")
	if trust, ev := ObserveSessionHookTrust(home, root); trust != HookTrustModified || !strings.Contains(ev, "trust it again") {
		t.Errorf("trust with a different hash = %s (%s), want modified telling the user to trust it again", trust, ev)
	}

	key := hooks.CodexHooksPath(root) + ":session_start:0:0"
	writeFile(t, ConfigPath(root), fmt.Sprintf("[hooks.state.%q]\nenabled = false\n", key))
	if trust, ev := ObserveSessionHookTrust(home, root); trust != HookUntrusted || !strings.Contains(ev, "disabled") {
		t.Errorf("trust for a disabled, never-trusted member = %s (%s), want untrusted naming the disablement", trust, ev)
	}
}

func TestSessionHookTrustOfALeftMemberIsUnknown(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, hooks.CodexHooksPath(root), `{"hooks":{"SessionStart":[{"matcher":".*","hooks":[{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if trust, ev := ObserveSessionHookTrust(home, root); trust != HookTrustUnknown || !strings.Contains(ev, "member left unowned") {
		t.Errorf("trust = %s (%s), want unknown for a left member", trust, ev)
	}
}

// Codex keys trust by the member's own hooks.json path and position, so a
// record under any other key leaves the member untrusted.
func TestSessionHookTrustUnderAnotherKeyIsUntrusted(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	writeFile(t, ConfigPath(root), "[hooks.state.\"/elsewhere/hooks.json:session_start:0:0\"]\ntrusted_hash = \"sha256:abc\"\n")
	trust, evidence := ObserveSessionHookTrust(home, root)
	if trust != HookUntrusted || strings.Contains(evidence, "unverified") {
		t.Errorf("trust = %s (%s), want untrusted", trust, evidence)
	}
}

func TestCoLocatedUnownedMemberBlocksUntilDecided(t *testing.T) {
	home, root, a := enrolledHome(t)
	body := `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo mine"},{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`
	writeFile(t, hooks.CodexHooksPath(root), body)

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("enroll err = %v, want a blocker offering --replace or --leave-unowned", err)
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != body {
		t.Fatalf("a blocked enroll changed hooks.json:\n%s", got)
	}

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	if _, ok := sessionHookRow(t, home, root); !ok {
		t.Error("--replace recorded no hooks.json claim")
	}
	if _, err := a.Lifecycle(home).Remove(codexTarget(root)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got := readFile(t, hooks.CodexHooksPath(root))
	if !strings.Contains(got, "echo mine") || strings.Contains(got, hooks.CodexSessionStartCommand) {
		t.Errorf("uninstall after --replace = %s, want only the user's handler", got)
	}
}

func TestWildcardUnownedMemberBlocks(t *testing.T) {
	home, root, a := enrolledHome(t)
	body := `{"hooks":{"SessionStart":[{"matcher":".*","hooks":[{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`
	writeFile(t, hooks.CodexHooksPath(root), body)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("enroll adopted a .* member Atomic never wrote")
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != body {
		t.Errorf("a blocked enroll changed hooks.json:\n%s", got)
	}
}

func TestLeaveUnownedKeepsTheMemberUnclaimed(t *testing.T) {
	home, root, a := enrolledHome(t)
	body := `{"hooks":{"SessionStart":[{"matcher":".*","hooks":[{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`
	writeFile(t, hooks.CodexHooksPath(root), body)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !ledger.IsLeft(codexTarget(root).Key(), hooks.CodexHooksPath(root)) {
		t.Error("the left member is not recorded")
	}
	if _, ok := sessionHookRow(t, home, root); ok {
		t.Error("a left member was claimed")
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("a later enroll refused the recorded decision: %v", err)
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != body {
		t.Errorf("the left member changed:\n%s", got)
	}
}

func TestCanonicalUnownedMemberIsAdopted(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, ok := sessionHookRow(t, home, root); !ok {
		t.Error("the canonical member was not adopted")
	}
}

func TestMalformedHooksJSONBlocksEnroll(t *testing.T) {
	home, root, a := enrolledHome(t)
	body := `{"hooks":[]}`
	writeFile(t, hooks.CodexHooksPath(root), body)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("enroll wrote into a malformed hooks.json")
	}
	if got := readFile(t, hooks.CodexHooksPath(root)); got != body {
		t.Errorf("hooks.json changed:\n%s", got)
	}
}

func TestMemberWithoutARowIsNotConverged(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	dropRow(ledger, codexTarget(root).Key(), hooks.CodexHooksPath(root))
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Converged {
		t.Error("a member with no ledger row reads converged")
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if _, ok := sessionHookRow(t, home, root); !ok {
		t.Error("repair recorded no row")
	}
}
