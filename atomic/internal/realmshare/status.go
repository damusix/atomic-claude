package realmshare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// StatusReport describes the local binding and the state of its managed checkout.
type StatusReport struct {
	RealmRoot    string           `json:"realm_root"`
	StateRoot    string           `json:"state_root"`
	ClonePath    string           `json:"clone_path"`
	RealmID      string           `json:"realm_id"`
	Remote       string           `json:"remote"`
	Branch       string           `json:"branch"`
	Head         string           `json:"head"`
	Paths        []StatusPath     `json:"paths"`
	Changes      []StatusChange   `json:"changes"`
	Conflicts    []StatusConflict `json:"conflicts"`
	CachedRemote *StatusRemote    `json:"cached_remote,omitempty"`
}

// StatusPath describes a declared destination or a retained local receipt.
type StatusPath struct {
	Path               string               `json:"path"`
	Source             string               `json:"source"`
	Kind               MappingKind          `json:"kind"`
	State              string               `json:"state"`
	Drift              bool                 `json:"drift"`
	Detail             string               `json:"detail,omitempty"`
	SourceAvailability string               `json:"source_availability,omitempty"`
	SourceDetail       string               `json:"source_detail,omitempty"`
	Activations        []ArtifactActivation `json:"activations,omitempty"`
}

// StatusChange contains Git's index and worktree status codes for a clone path.
// An empty code means unchanged; untracked paths have a worktree code of "?".
type StatusChange struct {
	Path     string `json:"path"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
}

// StatusConflict identifies an unmerged clone path and its realm path, if mapped.
type StatusConflict struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

// StatusRemote is a local observation, not a live remote query.
type StatusRemote struct {
	Head            string    `json:"head"`
	Ahead           int       `json:"ahead"`
	Behind          int       `json:"behind"`
	DivergenceKnown bool      `json:"divergence_known"`
	LastSuccess     time.Time `json:"last_success,omitzero"`
}

// InspectStatus reads the bound realm without updating its checkout or contacting its remote.
func InspectStatus(ctx context.Context, realmRoot string) (StatusReport, error) {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return StatusReport{}, err
	}
	links, err := openLocalLinkView(loc, true)
	if err != nil {
		if _, statErr := os.Lstat(filepath.Join(loc.physicalState, bindingName)); os.IsNotExist(statErr) {
			return StatusReport{}, fmt.Errorf("realm %q is not bound; use `atomic realm share` or `atomic realm join`", loc.realm)
		}
		return StatusReport{}, err
	}
	defer links.close()
	b := links.binding
	clone, err := os.OpenRoot(b.ClonePath)
	if err != nil {
		return StatusReport{}, err
	}
	defer clone.Close()

	git := statusGit(b.ClonePath)
	head, err := statusHead(ctx, clone, git, b)
	if err != nil {
		return StatusReport{}, err
	}
	var manifest Manifest
	manifestErr := readTOML(clone, manifestName, &manifest)
	if manifestErr == nil {
		if manifest.ID != b.RealmID {
			return StatusReport{}, fmt.Errorf("realm identity %q does not match the bound identity %q", manifest.ID, b.RealmID)
		}
		manifestErr = links.checkMappingDeclarations(manifest)
	}
	if manifestErr != nil {
		manifest = Manifest{}
	}
	changes, conflicts, err := statusChanges(ctx, git, head, manifest, b.Links, loc)
	if err != nil {
		return StatusReport{}, err
	}
	if os.IsNotExist(manifestErr) || slices.ContainsFunc(conflicts, func(c StatusConflict) bool { return c.Source == manifestName }) {
		manifest, manifestErr = links.fallbackStatusManifest(ctx, clone, git, head)
		if manifestErr == nil {
			for i := range conflicts {
				conflicts[i].Path = statusConflictPath(manifest, b.Links, conflicts[i].Source)
			}
		}
	}
	if manifestErr != nil {
		return StatusReport{}, manifestErr
	}
	report := StatusReport{
		RealmRoot: loc.realm, StateRoot: loc.state, ClonePath: b.ClonePath,
		RealmID: b.RealmID, Remote: b.Remote, Branch: b.Branch, Head: head,
		Paths: links.statusPaths(ctx, clone, manifest, head, changes), Changes: changes, Conflicts: conflicts,
		CachedRemote: statusRemote(ctx, git, head, b.Sync),
	}
	if err := ctx.Err(); err != nil {
		return StatusReport{}, err
	}
	return report, nil
}

func statusGit(clonePath string) shareGit {
	git := localGit(clonePath)
	git.configEnv = []string{"GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL="}
	return git
}

func (l *localLinks) fallbackStatusManifest(ctx context.Context, clone *os.Root, git shareGit, head string) (Manifest, error) {
	if head != "" {
		return l.committedManifest(ctx, git, head)
	}
	var initial initialShare
	if err := readTOML(clone, initialShareName, &initial); err != nil {
		return Manifest{}, err
	}
	if initial.Realm != l.loc.physicalRealm || initial.Binding.RealmID != l.binding.RealmID {
		return Manifest{}, fmt.Errorf("prepared shared realm definition does not match the bound realm")
	}
	return initial.Manifest, l.checkMappingDeclarations(initial.Manifest)
}

func (l *localLinks) committedManifest(ctx context.Context, git shareGit, head string) (Manifest, error) {
	out, err := git.run(ctx, nil, "ls-tree", "-z", "--full-tree", head, "--", manifestName)
	if err != nil {
		return Manifest{}, err
	}
	entry, found := strings.CutSuffix(string(out), "\t"+manifestName+"\x00")
	mode, oid, blob := strings.Cut(entry, " blob ")
	if !found || !blob || mode != "100644" && mode != "100755" || !gitObjectID(oid) {
		return Manifest{}, fmt.Errorf("commit %s must contain a regular %s", head, manifestName)
	}
	data, err := git.run(ctx, nil, "cat-file", "blob", oid)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%s at %s: %w", manifestName, head, err)
	}
	return manifest, l.checkMappingDeclarations(manifest)
}

func (l *localLinks) checkMappingDeclarations(m Manifest) error {
	if m.Version != manifestVersion || m.ID != l.binding.RealmID {
		return fmt.Errorf("shared realm definition does not match the bound identity and supported version")
	}
	state, _ := relativeTo(l.loc.physicalRealm, l.loc.physicalState)
	// Source drift is checked per mapping so it cannot hide unrelated links.
	sourcePrefixes := make(map[string]string)
	destinationPrefixes := make(map[string]string)
	for i, mapping := range m.Mappings {
		if err := mapping.validate(state); err != nil {
			return err
		}
		if err := recordPathPrefixes(sourcePrefixes, mapping.Source); err != nil {
			return err
		}
		if err := recordPathPrefixes(destinationPrefixes, mapping.Destination); err != nil {
			return err
		}
		for _, previous := range m.Mappings[:i] {
			if pathsOverlap(previous.Source, mapping.Source) || pathsOverlap(previous.Destination, mapping.Destination) {
				return fmt.Errorf("overlapping shared mapping for %q", mapping.Destination)
			}
		}
	}
	return nil
}

func (l *localLinks) statusPaths(ctx context.Context, clone *os.Root, manifest Manifest, head string, changes []StatusChange) []StatusPath {
	b := l.binding
	pending := b.PendingLocal
	// A validated pending unlink has a change for every owned link.
	pendingUnlink := pending != nil && pending.Unlink
	paths := make(map[string]StatusPath, len(manifest.Mappings))
	for _, mapping := range manifest.Mappings {
		item := StatusPath{Path: mapping.Destination, Source: mapping.Source, Kind: mapping.Kind, State: "available"}
		if !pendingUnlink || !slices.ContainsFunc(b.Links, func(link LinkReceipt) bool { return link.Path == item.Path }) {
			selection, err := l.selection(ctx, mapping)
			if err != nil {
				item.State, item.Detail = "conflicting", err.Error()
				item.Drift = selection.Owned.Path != "" || selection.Pending.Link.Path != "" && !pending.Cancel
			} else if selection.Owned.Path != "" {
				item.State = "linked"
			}
		}
		paths[item.Path] = item
	}
	for _, link := range b.Links {
		item := paths[link.Path]
		if item.Path != "" && item.Source == link.Source {
			continue
		}
		switch {
		case item.Path == "":
			item.Path, item.Source = link.Path, link.Source
			item.State, item.Detail = "conflicting", "owned destination is no longer declared"
		default:
			item.State, item.Detail = "conflicting", fmt.Sprintf("owned symlink records source %q, not the declared source", link.Source)
		}
		item.Drift = true
		if !pendingUnlink {
			if _, err := localLinkWitness(l.realm, l.state, b, link); err != nil {
				item.Detail = err.Error()
			}
		}
		paths[link.Path] = item
	}
	if pending := b.PendingShare; pending != nil {
		for _, selected := range pending.Selections {
			m := selected.Mapping
			item := paths[m.Destination]
			declared := item.Source == m.Source && item.Kind != ""
			item.Path, item.Source, item.Kind = m.Destination, m.Source, m.Kind
			detail := "share awaits local cutover"
			if !pending.Published {
				detail = "share awaits publication or local cutover"
			}
			stage, err := inspectShareOriginal(ctx, l.realm, l.state, b, selected, true)
			item.markPending(detail, err)
			if pending.Published || stage == shareLinked || head != "" && declared {
				item.addDrift(checkMappingSource(clone, m))
			}
			paths[item.Path] = item
		}
	}
	if pending != nil {
		for _, change := range pending.Changes {
			item := paths[change.Link.Path]
			if item.Path != "" && item.Source != change.Link.Source {
				if !pendingUnlink {
					item.addDrift(fmt.Errorf("pending operation records source %q, not %q", change.Link.Source, item.Source))
				}
				item.Kind = ""
			}
			item.Path, item.Source = change.Link.Path, change.Link.Source
			var detail string
			var err error
			switch {
			case pendingUnlink:
				detail = "unlink awaits local completion"
				_, err = l.unlinkStage(ctx, change)
			case pending.Cancel:
				detail = "link cancellation awaits local completion"
				_, err = l.cancelStage(ctx, change)
			default:
				detail = "link awaits local completion"
				_, err = l.linkStage(ctx, change)
			}
			item.markPending(detail, err)
			item.addDrift(checkMappingSource(clone, Mapping{Source: item.Source, Kind: item.Kind}))
			paths[item.Path] = item
		}
	}
	out := make([]StatusPath, 0, len(paths))
	for _, item := range paths {
		if item.State != "pending" && item.Kind != "" {
			if err := checkMappingSource(clone, Mapping{Source: item.Source, Kind: item.Kind}); err != nil {
				if item.Drift || item.State == "linked" {
					item.addDrift(err)
				} else if item.Detail == "" {
					item.Detail = err.Error()
				}
				item.State = "conflicting"
			}
		}
		switch item.Kind {
		case KindSkill, KindCommand, KindAgent:
			mapping := Mapping{Source: item.Source, Destination: item.Path, Kind: item.Kind}
			item.SourceAvailability = "available"
			review, err := reviewArtifact(mapping.Source, mapping.Kind, false, liveArtifactReader(clone))
			item.Activations = l.artifactStatus(mapping, review.identityMissing)
			if err == nil {
				if i := slices.IndexFunc(changes, func(change StatusChange) bool { return containsPath(mapping.Source, change.Path) }); i >= 0 {
					err = fmt.Errorf("clone source %q has local changes not covered by shared artifact review", changes[i].Path)
				}
			}
			if err != nil {
				item.SourceDetail = err.Error()
				state := "drifted"
				switch {
				case errors.Is(err, errArtifactSourceUnavailable):
					item.SourceAvailability = "unavailable"
					state = "unavailable"
				case errors.Is(err, errUnverifiedArtifactOption):
					state = "unsupported"
				}
				for i := range item.Activations {
					activation := &item.Activations[i]
					switch activation.State {
					case "active", "available", "ready", "unavailable":
						activation.State, activation.Detail = state, item.SourceDetail
					}
				}
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (p *StatusPath) markPending(detail string, err error) {
	p.State = "pending"
	if !p.Drift {
		p.Detail = detail
	}
	p.addDrift(err)
}

func (p *StatusPath) addDrift(err error) {
	if err == nil {
		return
	}
	detail := err.Error()
	switch {
	case !p.Drift:
		p.Drift, p.Detail = true, detail
	case p.Detail != detail:
		p.Detail += "; " + detail
	}
}

func checkMappingSource(clone *os.Root, mapping Mapping) error {
	info, err := lstatBelow(clone, mapping.Source)
	if err != nil {
		return err
	}
	if mapping.Kind == "" {
		if info.IsDir() || info.Mode().IsRegular() {
			return nil
		}
		return fmt.Errorf("clone source %q must be a regular file or real directory", mapping.Source)
	}
	if mapping.Kind == KindDirectory || mapping.Kind == KindSkill {
		if info.IsDir() {
			return nil
		}
	} else if info.Mode().IsRegular() {
		return nil
	}
	return fmt.Errorf("clone source %q no longer matches mapping kind %q", mapping.Source, mapping.Kind)
}

func statusConflictPath(manifest Manifest, links []LinkReceipt, name string) string {
	if mapping, ok := syncMapping(manifest, name); ok {
		return mapping.Destination + strings.TrimPrefix(name, mapping.Source)
	}
	for _, link := range links {
		if containsPath(link.Source, name) {
			return link.Path + strings.TrimPrefix(name, link.Source)
		}
	}
	return ""
}
