package realmshare

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// ArtifactActivation reports a realm-root native effect separately from the shared source.
type ArtifactActivation struct {
	Harness harness.Kind `json:"harness"`
	Path    string       `json:"path,omitempty"`
	State   string       `json:"state"`
	Version string       `json:"version,omitempty"`
	Detail  string       `json:"detail,omitempty"`
}

type artifactReceipt struct {
	Logical string      `toml:"logical"`
	Link    LinkReceipt `toml:"link"`
}

type artifactEffect struct {
	ArtifactActivation
	VersionEvidence artifactVersion
	Owned           LinkReceipt
	Pending         localChange
	Phase           localLinkStage
	Staged          bool
	Witness         string
}

var realmArtifactMatrices = [...]harness.CapabilityMatrix{
	harness.ClaudeCapabilities(), harness.CodexCapabilities(), harness.OMPCapabilities(),
}

type artifactInstance struct {
	root, unavailable string
	checked, probed   bool
	version           artifactVersion
	probeErr          error
}

type artifactInstances = [len(realmArtifactMatrices)]artifactInstance

func realmArtifactRole(kind MappingKind) harness.Role {
	switch kind {
	case KindSkill:
		return harness.RoleRealmSkill
	case KindCommand:
		return harness.RoleRealmCommand
	case KindAgent:
		return harness.RoleRealmAgent
	default:
		return ""
	}
}

func artifactNativePath(row harness.Capability, mapping Mapping) string {
	base := path.Base(mapping.Destination)
	if mapping.Kind != KindSkill {
		base = strings.TrimSuffix(base, ".md")
	}
	return strings.Replace(row.Native, "<name>", base, 1)
}

func enrolledArtifactInstances(home string) artifactInstances {
	var instances artifactInstances
	ledger, ledgerErr := installstate.LoadLedger(config.LedgerPath(home))
	for i, matrix := range realmArtifactMatrices {
		instance := &instances[i]
		if ledgerErr != nil {
			instance.unavailable = "Cannot read the enrollment ledger: " + ledgerErr.Error()
			continue
		}
		root, err := effectiveArtifactRoot(home, matrix.Harness)
		if err != nil {
			instance.unavailable = err.Error()
			continue
		}
		instance.root = root
		record, ok := ledger.FindTarget(string(matrix.Harness), root)
		if !ok || record.NativeRoot != "" && record.NativeRoot != root {
			instance.unavailable = "The effective harness instance is not enrolled."
			continue
		}
	}
	return instances
}

func effectiveArtifactRoot(home string, kind harness.Kind) (string, error) {
	switch kind {
	case harness.KindClaude:
		return filepath.Clean(claude.DefaultConfigDir(home)), nil
	case harness.KindCodex:
		return codex.DefaultHome(home)
	case harness.KindOMP:
		return effectiveOMPRoot(home)
	default:
		return "", harness.ErrUnsupported
	}
}

func effectiveOMPRoot(home string) (string, error) {
	for _, name := range []string{omp.ProfileEnv, "PI_PROFILE"} {
		if profile := os.Getenv(name); profile != "" && profile != "default" {
			return "", fmt.Errorf("named OMP profiles are outside the verified realm artifact scope")
		}
	}
	// OMP joins PI_CONFIG_DIR to HOME before adding the agent subdirectory.
	defaultDir := filepath.FromSlash(omp.DefaultAgentDir)
	if dir := os.Getenv("PI_CONFIG_DIR"); dir != "" && dir != filepath.Dir(defaultDir) {
		return "", fmt.Errorf("the selected OMP config directory is outside the verified realm artifact scope")
	}
	root := filepath.Join(home, defaultDir)
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" && (!filepath.IsAbs(dir) || dir != root) {
		return "", fmt.Errorf("the selected OMP agent directory is outside the verified realm artifact scope")
	}
	return root, nil
}

func validateArtifactScope(loc bindingLocation, root *os.Root, name string) error {
	candidate := filepath.Join(loc.physicalRealm, filepath.FromSlash(name))
	profiles := []string{
		claude.DefaultConfigDir(loc.home),
		filepath.Join(loc.physicalHome, ".claude"),
		filepath.Join(loc.physicalHome, codex.DefaultHomeDir),
		filepath.Join(loc.physicalHome, filepath.Dir(filepath.FromSlash(omp.DefaultAgentDir))),
	}
	if profile, err := codex.DefaultHome(loc.home); err == nil {
		profiles = append(profiles, profile)
	}
	if dir := os.Getenv("PI_CONFIG_DIR"); dir != "" {
		profiles = append(profiles, filepath.Join(loc.home, dir))
	}
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		if profile, err := filepath.Abs(dir); err == nil {
			profiles = append(profiles, profile)
		}
	}
	namespace, err := readArtifactNamespace(candidate)
	if err != nil {
		return fmt.Errorf("cannot resolve artifact path %q: %w", name, err)
	}
	for _, profile := range profiles {
		global, err := readArtifactNamespace(profile)
		if err != nil {
			return fmt.Errorf("cannot resolve global harness profile %q: %w", profile, err)
		}
		if namespace.contains(global, "") || global.contains(namespace, "") {
			return fmt.Errorf("artifact path %q overlaps the global harness profile %q", name, profile)
		}
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if _, err := lstatBelow(root, path.Join(parent, ".git")); err == nil {
			return fmt.Errorf("artifact path %q is inside the separate Git repository at %q", name, parent)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func describeArtifactEffect(matrix harness.CapabilityMatrix, mapping Mapping, identityMissing bool) artifactEffect {
	row := matrix.Capability(realmArtifactRole(mapping.Kind))
	a := ArtifactActivation{Harness: matrix.Harness, Path: artifactNativePath(row, mapping), Version: row.Version, State: "unsupported"}
	if row.Status != harness.StatusSupported || row.Version == "" {
		a.Detail = "No verified native invocation for this kind at the realm root."
		if row.Limitation != "" {
			a.Detail += " " + row.Limitation + "."
		}
		if a.Path == mapping.Destination {
			a.Detail += " The canonical path can be discovered without installing a separate projection."
		}
		return artifactEffect{ArtifactActivation: a}
	}
	if identityMissing {
		a.State = "unavailable"
		a.Detail = "Native identity is unavailable: the source does not declare a name."
		if a.Path == mapping.Destination {
			a.Detail += " The canonical path is exposed by the logical link, but native invocation is not verified."
		}
		return artifactEffect{ArtifactActivation: a}
	}
	a.State = "ready"
	a.Detail = "Supported in the tested version only; start a new session from the realm root. " + row.Limitation + ". Native trust and permissions are unchanged."
	if a.Path == mapping.Destination {
		a.Detail += " The canonical path is exposed by the logical link; no separate projection or import is installed."
	}
	return artifactEffect{ArtifactActivation: a}
}

func artifactShareDiscovery(mapping Mapping, identityMissing bool) []ArtifactActivation {
	role := realmArtifactRole(mapping.Kind)
	if role == "" {
		return nil
	}
	var out []ArtifactActivation
	for _, matrix := range realmArtifactMatrices {
		if artifactNativePath(matrix.Capability(role), mapping) != mapping.Destination {
			continue
		}
		effect := describeArtifactEffect(matrix, mapping, identityMissing)
		if effect.State == "ready" {
			effect.State = "unverified"
			effect.Detail = "Publishing exposes the canonical path but does not attest the local executable. Review and link to verify native delivery. " + effect.Detail
		}
		out = append(out, effect.ArtifactActivation)
	}
	return out
}

func (l *localLinks) artifactStatus(mapping Mapping, identityMissing bool) []ArtifactActivation {
	effects, _ := l.artifactEffects(context.Background(), mapping, identityMissing, false)
	return artifactDisplays(effects)
}

func artifactDisplays(effects []artifactEffect) []ArtifactActivation {
	if len(effects) == 0 {
		return nil
	}
	out := make([]ArtifactActivation, 0, len(effects))
	for _, effect := range effects {
		out = append(out, effect.ArtifactActivation)
	}
	return out
}

func (l *localLinks) artifactEffects(ctx context.Context, mapping Mapping, identityMissing, preview bool) ([]artifactEffect, error) {
	if realmArtifactRole(mapping.Kind) == "" {
		return nil, nil
	}
	if l.artifacts == nil {
		instances := enrolledArtifactInstances(l.loc.home)
		l.artifacts = &instances
	}
	logical := findArtifactBacking(l.binding, nil, mapping.Destination, mapping.Source)
	var logicalErr error
	if !preview {
		logicalErr = validateArtifactScope(l.loc, l.realm, mapping.Destination)
	}
	if logicalErr == nil && logical.Path != "" {
		_, logicalErr = localLinkWitness(l.realm, l.state, l.binding, logical)
	}
	out := make([]artifactEffect, 0, len(realmArtifactMatrices))
	for i, matrix := range realmArtifactMatrices {
		effect := describeArtifactEffect(matrix, mapping, identityMissing)
		if effect.State == "ready" {
			if preview {
				l.reviewArtifactVersion(ctx, &l.artifacts[i], &effect)
			} else if l.artifacts[i].unavailable != "" {
				effect.State = "unavailable"
				effect.Detail = l.artifacts[i].unavailable + " " + effect.Detail
			}
		}
		switch {
		case effect.State == "unsupported":
		case effect.Path == mapping.Destination:
			pending := l.binding.PendingLocal
			switch {
			case pending != nil && (pending.Unlink && logical.Path != "" || logical.Path == "" && slices.Contains(pending.Paths, mapping.Destination)):
				effect.State = "pending"
			case logicalErr != nil:
				effect.State, effect.Detail = "drifted", logicalErr.Error()
			case effect.State != "ready":
			case logical.Path != "":
				l.reportOwnedArtifact(mapping, logical, preview, &l.artifacts[i], &effect)
			case !preview:
				effect.State = "available"
				if _, err := lstatBelow(l.realm, effect.Path); err == nil {
					effect.State, effect.Detail = "collision", "The canonical destination is occupied; it is not an Atomic-owned link."
				}
			}
		default:
			if err := l.inspectArtifactProjection(ctx, mapping, preview, logicalErr, &l.artifacts[i], &effect); err != nil {
				return nil, err
			}
		}
		out = append(out, effect)
	}
	return out, nil
}

func (l *localLinks) inspectArtifactProjection(ctx context.Context, mapping Mapping, preview bool, logicalErr error, instance *artifactInstance, effect *artifactEffect) error {
	name := effect.Path
	if err := validateArtifactScope(l.loc, l.realm, name); err != nil {
		effect.State, effect.Detail = "collision", err.Error()
		if pending := l.binding.PendingLocal; preview && pending != nil && slices.ContainsFunc(pending.Changes, func(change localChange) bool {
			return change.Artifact == mapping.Destination && change.Link.Path == name
		}) {
			return err
		}
		return nil
	}
	collision := ""
	if effect.Harness == harness.KindClaude && mapping.Kind == KindAgent {
		collision = l.claudeArtifactCollision(name)
	}
	for _, receipt := range l.binding.Artifacts {
		if !pathsOverlap(receipt.Link.Path, name) {
			continue
		}
		if receipt.Logical != mapping.Destination || receipt.Link.Path != name || receipt.Link.Source != mapping.Source {
			effect.State, effect.Detail = "collision", "A different owned projection occupies this path; it will be preserved."
			return nil
		}
		effect.Owned = receipt.Link
		if pending := l.binding.PendingLocal; pending != nil && pending.Unlink {
			effect.State = "pending"
			return nil
		}
		_, err := localLinkWitness(l.realm, l.state, l.binding, receipt.Link)
		switch {
		case err != nil:
			effect.State, effect.Detail = "drifted", err.Error()
		case logicalErr != nil:
			effect.State, effect.Detail = "drifted", logicalErr.Error()
		case collision != "":
			effect.State, effect.Detail = "collision", collision
		case effect.State == "ready":
			l.reportOwnedArtifact(mapping, receipt.Link, preview, instance, effect)
		}
		return nil
	}
	if pending := l.binding.PendingLocal; pending != nil {
		for _, change := range pending.Changes {
			if change.Artifact != mapping.Destination || change.Link.Path != name {
				continue
			}
			effect.Pending = change
			if !preview {
				effect.State = "pending"
				return nil
			}
			if collision != "" {
				return fmt.Errorf("native activation at %q changed during a pending link; use realm unlink to recover it: %s", name, collision)
			}
			if effect.State != "ready" || change.Link.Source != mapping.Source {
				return fmt.Errorf("native activation at %q changed during a pending link; use realm unlink to recover it", name)
			}
			var progress linkSelectionReview
			if err := l.pendingLinkReview(ctx, change, &progress); err != nil {
				return err
			}
			effect.Phase, effect.Staged, effect.Witness = progress.Phase, progress.Staged, progress.Witness
			effect.State = "pending"
			return nil
		}
	}
	state, _ := relativeTo(l.loc.physicalRealm, l.loc.physicalState)
	if state != "" && pathsOverlap(name, state) {
		effect.State, effect.Detail = "collision", "The native path overlaps local Atomic state; it will be preserved."
		return nil
	}
	if _, err := lstatBelow(l.realm, name); !os.IsNotExist(err) {
		effect.State, effect.Detail = "collision", "The native destination is occupied or inaccessible; it will be preserved."
		if err != nil {
			effect.Detail += " " + err.Error()
		}
		return nil
	}
	if collision != "" {
		effect.State, effect.Detail = "collision", collision
		return nil
	}
	if effect.State == "ready" && !preview {
		if findArtifactBacking(l.binding, nil, mapping.Destination, mapping.Source).Path != "" {
			effect.State, effect.Detail = "unverified", "No native version evidence was recorded for this link. Review and link to verify native delivery."
		} else {
			effect.State = "available"
		}
	} else if effect.State == "ready" && l.binding.PendingLocal != nil {
		effect.State = "available"
	}
	return nil
}

type artifactAgentInventory struct {
	names map[string][]string
	err   error
}

func (l *localLinks) claudeArtifactCollision(native string) string {
	if l.claudeAgents == nil {
		inventory := artifactAgentInventory{names: make(map[string][]string)}
		inventory.err = l.scanClaudeArtifactAgents(&inventory)
		l.claudeAgents = &inventory
	}
	name := strings.TrimSuffix(path.Base(native), ".md")
	for _, personal := range l.claudeAgents.names[name] {
		if personal != native {
			return fmt.Sprintf("Claude agent name %q is already claimed by %q; the original is preserved.", name, personal)
		}
	}
	if l.claudeAgents.err != nil {
		return fmt.Sprintf("Cannot rule out a Claude agent name collision for %q: %v", name, l.claudeAgents.err)
	}
	return ""
}

func (l *localLinks) scanClaudeArtifactAgents(inventory *artifactAgentInventory) error {
	const rootName = ".claude/agents"
	if _, err := lstatBelow(l.realm, rootName); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	remaining := 1024
	byteBudget := 4 << 20
	var clone *os.Root
	defer func() {
		if clone != nil {
			clone.Close()
		}
	}()
	var scan func(string, int) error
	scan = func(dir string, depth int) error {
		if depth > 12 {
			return fmt.Errorf("native agent directory depth exceeds 12")
		}
		f, err := openShareDirectory(l.realm, dir)
		if err != nil {
			return err
		}
		entries, err := f.ReadDir(remaining + 1)
		f.Close()
		if err != nil && err != io.EOF {
			return err
		}
		remaining -= len(entries)
		if remaining < 0 {
			return fmt.Errorf("native agent scan exceeds 1024 entries")
		}
		slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			native := path.Join(dir, entry.Name())
			if entry.IsDir() {
				if err := scan(native, depth+1); err != nil {
					return err
				}
				continue
			}
			if entry.Type()&fs.ModeSymlink == 0 && !strings.EqualFold(path.Ext(native), ".md") {
				continue
			}
			readRoot, readPath := l.realm, native
			if entry.Type()&fs.ModeSymlink != 0 {
				link := l.ownedClaudeAgent(native)
				if _, err := localLinkWitness(l.realm, l.state, l.binding, link); err != nil {
					return fmt.Errorf("native agent path %q is an unverified symlink", native)
				}
				if clone == nil {
					clone, err = os.OpenRoot(l.binding.ClonePath)
					if err != nil {
						return err
					}
				}
				readRoot, readPath = clone, link.Source
			}
			data, err := readArtifactFile(readPath, false, liveArtifactReader(readRoot))
			if err != nil {
				return err
			}
			byteBudget -= len(data)
			if byteBudget < 0 {
				return fmt.Errorf("native agent scan exceeds its 4 MiB review limit")
			}
			name, err := declaredArtifactName(data)
			if err != nil {
				return fmt.Errorf("read Claude agent name in %q: %w", native, err)
			}
			if name != "" {
				inventory.names[name] = append(inventory.names[name], native)
			}
		}
		return nil
	}
	return scan(rootName, 0)
}

func (l *localLinks) ownedClaudeAgent(native string) LinkReceipt {
	for _, receipt := range l.binding.Artifacts {
		if receipt.Link.Path == native {
			return receipt.Link
		}
	}
	if p := l.binding.PendingLocal; p != nil {
		for _, change := range p.Changes {
			if change.Artifact != "" && change.Link.Path == native {
				return change.Link
			}
		}
	}
	return LinkReceipt{}
}

func validateArtifactReceipt(receipt artifactReceipt, state string) error {
	link := receipt.Link
	m := Mapping{Kind: artifactKind(receipt.Logical), Source: link.Source, Destination: receipt.Logical}
	if validLocalWitness(link.Witness) && link.Path != receipt.Logical && (state == "" || !pathsOverlap(link.Path, state)) && m.validate(state) == nil {
		for _, matrix := range realmArtifactMatrices {
			row := matrix.Capability(realmArtifactRole(m.Kind))
			if row.Status == harness.StatusSupported && link.Path == artifactNativePath(row, m) {
				return nil
			}
		}
	}
	return fmt.Errorf("invalid native artifact receipt for %q", link.Path)
}

func findArtifactBacking(b Binding, changes []localChange, logical, source string) LinkReceipt {
	for _, link := range b.Links {
		if link.Path == logical && link.Source == source {
			return link
		}
	}
	for _, c := range changes {
		if c.Artifact == "" && c.Link.Path == logical && c.Link.Source == source {
			return c.Link
		}
	}
	return LinkReceipt{}
}
