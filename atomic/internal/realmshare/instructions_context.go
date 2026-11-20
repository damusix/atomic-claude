package realmshare

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/pelletier/go-toml/v2"
)

const claudeRealmContextLimit = 10_000

// SessionInstructions reads the approved guidance for an enrolled realm startup.
// Claude's local hook returns team text only; its loader imports private text.
func SessionInstructions(realmRoot string, kind harness.Kind, claudeLocal bool) (string, error) {
	if realmRoot == "" || !kind.Valid() || claudeLocal != (kind == harness.KindClaude) {
		return "", nil
	}
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return "", err
	}
	bound, err := loc.hasBinding()
	if err != nil || !bound {
		return "", err
	}
	var b Binding
	if err := readBinding(loc.physicalState, &b); err != nil {
		return "", err
	}
	active := b.Instructions
	if active == nil || claudeLocal && active.ClaudeMember == "" {
		return "", nil
	}
	eligible, err := realmSessionEligible(loc.home, kind)
	if err != nil || !eligible {
		return "", err
	}
	if b.PendingShare != nil || b.PendingLocal != nil || active.Pending != nil {
		return "", fmt.Errorf("realm instructions have a pending local operation; finish it before starting a session")
	}
	if active.Source != sharedInstructionsPath {
		return "", fmt.Errorf("invalid realm instruction receipt; review %s with realm link", sharedInstructionsPath)
	}
	if err := loc.checkReceipts(b, true); err != nil {
		return "", err
	}
	linkIndex := slices.IndexFunc(b.Links, func(link LinkReceipt) bool {
		return link.Path == sharedInstructionsPath && link.Source == active.Source
	})
	if linkIndex < 0 {
		return "", fmt.Errorf("realm instructions have no matching local link receipt; review %s with realm link", sharedInstructionsPath)
	}
	realm, err := os.OpenRoot(loc.physicalRealm)
	if err != nil {
		return "", err
	}
	defer realm.Close()
	state, err := os.OpenRoot(loc.physicalState)
	if err != nil {
		return "", err
	}
	defer state.Close()
	if _, err := localLinkWitness(realm, state, b, b.Links[linkIndex]); err != nil {
		return "", fmt.Errorf("verify realm instruction link: %w; review it with realm link", err)
	}

	clone, err := openInstructionClone(loc, b)
	if err != nil {
		return "", err
	}
	defer clone.Close()
	if err := checkTrustedInstructionDefinition(clone, b); err != nil {
		return "", err
	}
	team, err := realmTeamText(clone, active.Source)
	if err != nil {
		return "", err
	}
	if claudeLocal {
		if err := checkClaudeRealmContext(realm, active); err != nil {
			return "", err
		}
		if _, err := claudeRealmContext(team); err != nil {
			return "", err
		}
		return team, nil
	}
	private, err := readLocalRegular(realm, privateInstructionsPath)
	if err != nil {
		return "", fmt.Errorf("read private realm instructions: %w", err)
	}
	if !validRealmInstructionText(private) {
		return "", fmt.Errorf("%s must be UTF-8 text without control characters other than tabs and line breaks", privateInstructionsPath)
	}
	if team == "" {
		return string(private), nil
	}
	if len(private) == 0 {
		return team, nil
	}
	return team + "\n\n" + string(private), nil
}

func realmSessionEligible(home string, kind harness.Kind) (bool, error) {
	var root string
	switch kind {
	case harness.KindClaude:
		if _, err := exec.LookPath("claude"); err != nil {
			return false, nil
		}
		root = claude.DefaultConfigDir(home)
	case harness.KindCodex:
		activation, err := codex.InspectSessionHookActivation(home)
		return activation == codex.SessionHookActive, err
	case harness.KindOMP:
		var err error
		root, err = effectiveOMPRoot(home)
		if err != nil {
			return false, nil
		}
	default:
		return false, nil
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return false, err
	}
	target, ok := ledger.FindTarget(string(kind), root)
	return ok && (target.NativeRoot == "" || target.NativeRoot == root), nil
}

func openInstructionClone(loc bindingLocation, b Binding) (*os.Root, error) {
	name, err := managedCloneName(loc.home, loc.physicalHome, b.ClonePath)
	if err != nil {
		return nil, err
	}
	clone, id, err := openWitnessedClone(loc.physicalHome, name, b)
	if err != nil {
		return nil, err
	}
	registry, err := loc.readOwners()
	if err != nil {
		clone.Close()
		return nil, err
	}
	clonePath := filepath.Join(config.Dir(loc.physicalHome), "realms", name)
	claim := ownership{Realm: loc.physicalRealm, Clone: clonePath, CloneID: id, RealmID: b.RealmID, Remote: b.Remote, Branch: b.Branch}
	owned, err := registry.lookup(claim)
	if !owned || err != nil {
		clone.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("realm %q has a binding without a matching clone owner", loc.realm)
	}
	return clone, nil
}

func checkTrustedInstructionDefinition(clone *os.Root, b Binding) error {
	data, err := readLocalRegular(clone, manifestName)
	if err != nil {
		return fmt.Errorf("read shared realm definition: %w", err)
	}
	if b.TrustedDigest != localDigest(data) {
		return fmt.Errorf("shared realm definition has not been trusted; review it with realm link")
	}
	var manifest Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&manifest); err != nil {
		return fmt.Errorf("read shared realm definition: %w", err)
	}
	want := Mapping{Source: sharedInstructionsPath, Destination: sharedInstructionsPath, Kind: KindInstructions}
	if manifest.Version != manifestVersion || manifest.ID != b.RealmID || !slices.Contains(manifest.Mappings, want) {
		return fmt.Errorf("trusted realm definition has no exact instruction mapping for %q", sharedInstructionsPath)
	}
	return nil
}

func checkClaudeRealmContext(realm *os.Root, active *InstructionActivation) error {
	settings, err := readLocalRegular(realm, claude.RealmSettingsPath)
	if err != nil {
		return fmt.Errorf("read Claude local settings: %w; review the realm instruction link", err)
	}
	if !claude.RealmSessionStartOwned(settings, active.ClaudeMember) {
		return fmt.Errorf("Claude local SessionStart hook does not match its receipt; resolve the conflict and review the realm instruction link")
	}
	loader, err := readLocalRegular(realm, claudeRealmLoaderPath)
	if err != nil {
		return fmt.Errorf("read Claude private loader: %w; review the realm instruction link", err)
	}
	if active.ClaudeLoader == "" && !claudeRealmLoaderImportsPrivate(loader) ||
		active.ClaudeLoader != "" && !claudeRealmLoaderOwned(loader, active.ClaudeLoader) {
		return fmt.Errorf("Claude private loader does not match its receipt; resolve the conflict and review the realm instruction link")
	}
	info, err := lstatBelow(realm, privateInstructionsPath)
	if err != nil {
		return fmt.Errorf("check Claude private instructions: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular local file", privateInstructionsPath)
	}
	return nil
}

func realmTeamText(clone *os.Root, source string) (string, error) {
	if source != sharedInstructionsPath {
		return "", fmt.Errorf("invalid team instruction source %q; expected %q", source, sharedInstructionsPath)
	}
	data, err := readBoundedTeamText(clone, source)
	if err != nil {
		return "", fmt.Errorf("read team instructions: %w", err)
	}
	if !validRealmInstructionText(data) {
		return "", fmt.Errorf("%s must be UTF-8 text without control characters other than tabs and line breaks", source)
	}
	return string(data), nil
}

func readBoundedTeamText(clone *os.Root, source string) ([]byte, error) {
	before, err := lstatBelow(clone, source)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%q must be a regular file", source)
	}
	if before.Size() > claudeRealmContextLimit {
		return nil, fmt.Errorf("%q exceeds the %d-byte realm instruction source limit; shorten it", source, claudeRealmContextLimit)
	}
	file, err := clone.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) {
		return nil, fmt.Errorf("%q changed while opening it", source)
	}
	data := make([]byte, int(before.Size())+1)
	n, err := io.ReadFull(file, data)
	if err != io.EOF && err != io.ErrUnexpectedEOF && err != nil {
		return nil, err
	}
	if int64(n) != before.Size() {
		return nil, fmt.Errorf("%q changed while reading it", source)
	}
	after, err := lstatBelow(clone, source)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) ||
		before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("%q changed while reading it", source)
	}
	return data[:n], nil
}

func validRealmInstructionText(data []byte) bool {
	for len(data) > 0 {
		r, n := utf8.DecodeRune(data)
		if r == utf8.RuneError && n == 1 {
			return false
		}
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
		data = data[n:]
	}
	return true
}

func claudeRealmContext(body string) (string, error) {
	out, err := hooks.FormatSessionStartContext(body)
	if err != nil {
		return "", err
	}
	if len(out)+1 > claudeRealmContextLimit {
		return "", fmt.Errorf("Claude SessionStart realm instructions exceed the %d-byte output limit; shorten %s", claudeRealmContextLimit, sharedInstructionsPath)
	}
	return out, nil
}
