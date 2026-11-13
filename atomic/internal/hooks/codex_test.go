package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// userCodexHooks is a hooks.json the user wrote: a description, a user
// SessionStart group, and another event, in an order Atomic must not touch.
const userCodexHooks = `{
  "description": "my hooks",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [{ "type": "command", "command": "python3 ~/.codex/hooks/mine.py" }]
      }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "say done" }] }
    ]
  }
}
`

func writeCodexHooks(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if body != "" {
		if err := os.WriteFile(hooks.CodexHooksPath(root), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readHooksFile(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(hooks.CodexHooksPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstallCodexCreatesHooksFileWithOwnedMember(t *testing.T) {
	root := writeCodexHooks(t, "")
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	got := readHooksFile(t, root)
	for _, want := range []string{`"matcher": "startup|resume"`, `"command": "atomic hooks session-start --harness codex"`} {
		if !strings.Contains(got, want) {
			t.Errorf("hooks.json missing %s:\n%s", want, got)
		}
	}
}

// hooks.json stays the user's file: install then uninstall must give back the
// original bytes, and the user's group keeps its place ahead of Atomic's.
func TestCodexMemberRoundTripPreservesUserBytes(t *testing.T) {
	root := writeCodexHooks(t, userCodexHooks)
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	got := readHooksFile(t, root)
	if strings.Count(got, hooks.CodexSessionStartCommand) != 1 {
		t.Fatalf("want exactly one owned member:\n%s", got)
	}
	if strings.Index(got, "mine.py") > strings.Index(got, hooks.CodexSessionStartCommand) {
		t.Errorf("the user's SessionStart group lost its position:\n%s", got)
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if again := readHooksFile(t, root); again != got {
		t.Errorf("a second install rewrote the file:\n%s", again)
	}
	if _, err := hooks.UninstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if after := readHooksFile(t, root); after != userCodexHooks {
		t.Errorf("uninstall did not restore the user's bytes:\n%s", after)
	}
}

// Mirrors the Claude settings.json precedent: removing the last hook leaves
// the file in place rather than deleting a file the user may expect to find.
func TestUninstallCodexKeepsAnEmptiedFile(t *testing.T) {
	root := writeCodexHooks(t, "")
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.UninstallCodex(root); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readHooksFile(t, root)); got != "{}" {
		t.Errorf("emptied hooks.json = %q, want {}", got)
	}
}

func TestObserveCodexHooksOwnedDigestsOnlyTheOwnedMember(t *testing.T) {
	root := writeCodexHooks(t, userCodexHooks)
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	before, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Exists() || before.Digest == "" || before.Kind != managedfile.KindSettings {
		t.Fatalf("observation = %+v, want an owned settings observation", before)
	}

	edited := strings.Replace(readHooksFile(t, root), "say done", "say finished", 1)
	if err := os.WriteFile(hooks.CodexHooksPath(root), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	userEdit, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if userEdit.Digest != before.Digest {
		t.Error("a user edit outside the owned member changed the digest")
	}

	ownedEdit := strings.Replace(edited, `"startup|resume"`, `"startup"`, 1)
	if err := os.WriteFile(hooks.CodexHooksPath(root), []byte(ownedEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == before.Digest {
		t.Error("an edit to the owned member left the digest unchanged")
	}
}

func TestObserveOwnedMembersDispatchesOnTheFile(t *testing.T) {
	root := writeCodexHooks(t, "")
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	direct, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	viaPath, err := hooks.ObserveOwnedMembers(hooks.CodexHooksPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if viaPath.Digest != direct.Digest || viaPath.Path != filepath.Join(root, "hooks.json") {
		t.Errorf("path dispatch = %+v, want the Codex observation %+v", viaPath, direct)
	}
	if _, err := hooks.RemoveOwnedMembers(hooks.CodexHooksPath(root)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readHooksFile(t, root), hooks.CodexSessionStartCommand) {
		t.Error("path dispatch removal left the owned member")
	}
}

func TestCodexSessionStartPosition(t *testing.T) {
	root := writeCodexHooks(t, userCodexHooks)
	if _, _, found, err := hooks.CodexSessionStartPosition(root); err != nil || found {
		t.Fatalf("found = %v, err = %v before install", found, err)
	}
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	group, handler, found, err := hooks.CodexSessionStartPosition(root)
	if err != nil || !found || group != 1 || handler != 0 {
		t.Errorf("position = (%d, %d, %v, %v), want (1, 0, true, nil)", group, handler, found, err)
	}
}

func TestMalformedCodexShapesRefuseAndKeepTheirBytes(t *testing.T) {
	for _, body := range []string{
		`{"hooks":{"SessionStart":{}}}`,
		`{"hooks":{"SessionStart":"x"}}`,
		`{"hooks":[]}`,
	} {
		root := writeCodexHooks(t, body)
		obs, err := hooks.ObserveCodexHooksOwned(root)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Conflict != managedfile.ConflictMalformedSettings {
			t.Errorf("%s observed as %+v, want a malformed conflict", body, obs)
		}
		if _, err := hooks.InstallCodex(root); err == nil {
			t.Errorf("%s: install succeeded over a malformed shape", body)
		}
		if got := readHooksFile(t, root); got != body {
			t.Errorf("%s: bytes changed to %s", body, got)
		}
	}
}

func TestCanonicalDigestMatchesOnlyTheCanonicalMember(t *testing.T) {
	root := writeCodexHooks(t, "")
	if _, err := hooks.InstallCodex(root); err != nil {
		t.Fatal(err)
	}
	fresh, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("a fresh install digests %s, want the canonical %s", fresh.Digest, hooks.CodexCanonicalDigest())
	}

	wildcard := writeCodexHooks(t, `{"hooks":{"SessionStart":[{"matcher":".*","hooks":[{"type":"command","command":"`+hooks.CodexSessionStartCommand+`"}]}]}}`)
	if _, err := hooks.InstallCodex(wildcard); err != nil {
		t.Fatal(err)
	}
	obs, err := hooks.ObserveCodexHooksOwned(wildcard)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Digest == hooks.CodexCanonicalDigest() {
		t.Error("a .* member reads canonical")
	}
}

const coLocatedCodexHooks = `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo mine"},{"type":"command","command":"atomic hooks session-start --harness codex"}]}]}}`

func TestReplaceCodexKeepsCoLocatedUserHandlers(t *testing.T) {
	root := writeCodexHooks(t, coLocatedCodexHooks)
	if _, err := hooks.ReplaceCodex(root); err != nil {
		t.Fatal(err)
	}
	obs, err := hooks.ObserveCodexHooksOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Digest != hooks.CodexCanonicalDigest() {
		t.Errorf("replace left a non-canonical member:\n%s", readHooksFile(t, root))
	}
	if !strings.Contains(readHooksFile(t, root), "echo mine") {
		t.Fatalf("replace dropped the user's handler:\n%s", readHooksFile(t, root))
	}
	if _, err := hooks.UninstallCodex(root); err != nil {
		t.Fatal(err)
	}
	got := readHooksFile(t, root)
	if !strings.Contains(got, "echo mine") || strings.Contains(got, hooks.CodexSessionStartCommand) {
		t.Errorf("uninstall after replace = %s, want only the user's handler", got)
	}
}
