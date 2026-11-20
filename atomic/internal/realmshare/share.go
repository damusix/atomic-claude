package realmshare

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/pelletier/go-toml/v2"
)

// ShareOptions supplies explicit selections and optional initial Git destination.
type ShareOptions struct {
	Paths          []string
	Remote, Branch string
}

// ShareSelection describes a mapping and the content included in its review.
type ShareSelection struct {
	Mapping    Mapping         `toml:"mapping"`
	Files      int             `toml:"files"`
	Bytes      int64           `toml:"bytes"`
	Exclusions []string        `toml:"exclusions,omitempty"`
	Buckets    []Bucket        `toml:"buckets,omitempty"`
	Artifact   *ArtifactReview `toml:"-"`
}

// SharePlan is a read-only review of the destination and selected content.
type SharePlan struct {
	Remote, Branch, ClonePath string
	Selections                []ShareSelection
	Members                   []Member
	Published                 bool
	state                     *sharePlanState
}

// ReviewDigest returns the sealed approval token, or an empty string for a zero-value plan.
func (plan SharePlan) ReviewDigest() string {
	if plan.state == nil {
		return ""
	}
	return plan.state.reviewDigest
}

type sharePlanState struct {
	loc               bindingLocation
	opts              ShareOptions
	binding           Binding
	initial, prepared bool
	indexReady        bool
	base, head        string
	atRemote          string
	published         bool
	manifest          Manifest
	paths             []pendingPath
	entries           [][]snapshotEntry
	artifacts         map[string]*ArtifactReview
	reviewStages      []shareLinkState
	reviewDigest      string
}

type snapshotEntry struct {
	name, target, digest string
	mode                 fs.FileMode
	size                 int64
	content              string
	projected            bool
}

const initialShareName = ".git/atomic-realm-share-initial.toml"

type initialShare struct {
	Realm    string   `toml:"realm"`
	Binding  Binding  `toml:"binding"`
	Manifest Manifest `toml:"manifest"`
}

// PreviewShare inspects explicit paths and remote refs without changing the realm or clone.
func PreviewShare(ctx context.Context, realmRoot string, opts ShareOptions) (SharePlan, error) {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return SharePlan{}, err
	}
	return previewShare(ctx, loc, opts)
}

func previewShare(ctx context.Context, loc bindingLocation, opts ShareOptions) (SharePlan, error) {
	realm, err := os.OpenRoot(loc.physicalRealm)
	if err != nil {
		return SharePlan{}, err
	}
	defer realm.Close()
	statePath, _ := relativeTo(loc.physicalRealm, loc.physicalState)
	paths, err := sharePaths(opts.Paths, statePath)
	if err != nil {
		return SharePlan{}, err
	}
	opts.Paths = paths
	st := &sharePlanState{loc: loc, opts: opts}
	st.binding, err = LoadBinding(loc.realm)
	switch {
	case os.IsNotExist(err):
		st.initial, err = true, nil
		if err = st.initialDestination(realm); err != nil {
			return SharePlan{}, err
		}
	case err != nil:
		return SharePlan{}, err
	}
	b := st.binding
	if pending := b.PendingLocal; pending != nil {
		if pending.Unlink || pending.Cancel {
			return SharePlan{}, fmt.Errorf("a realm unlink is pending; run atomic realm unlink before sharing")
		}
		return SharePlan{}, fmt.Errorf("a realm link is pending; review and repeat exactly these paths: %q, or run atomic realm unlink to roll it back before sharing", pending.Paths)
	}
	if opts.Remote != "" && opts.Remote != b.Remote || opts.Branch != "" && opts.Branch != b.Branch {
		return SharePlan{}, fmt.Errorf("share uses the recorded remote and branch for this realm")
	}
	if err := validateShareRemote(b.Remote); err != nil {
		return SharePlan{}, err
	}
	if b.Branch == "" || strings.HasPrefix(b.Branch, "-") || strings.TrimSpace(b.Branch) != b.Branch {
		return SharePlan{}, fmt.Errorf("invalid share branch %q", b.Branch)
	}
	if _, err := (shareGit{dir: loc.physicalHome, outside: true}).run(ctx, nil, "check-ref-format", shareBranchRef(b.Branch)); err != nil {
		return SharePlan{}, fmt.Errorf("invalid share branch %q: %w", b.Branch, err)
	}
	if pending := b.PendingShare; pending != nil {
		if !slices.Equal(paths, pending.Paths) {
			return SharePlan{}, fmt.Errorf("another share is pending; repeat exactly these paths: %q", pending.Paths)
		}
		if err := st.previewPending(ctx, realm); err != nil {
			return SharePlan{}, err
		}
	} else {
		if !st.initial {
			st.manifest, err = LoadManifest(b.ClonePath, loc.realm)
			if err != nil {
				return SharePlan{}, err
			}
		}
		if err := st.selectPaths(ctx, realm, statePath); err != nil {
			return SharePlan{}, err
		}
		if st.initial || len(st.paths) != 0 {
			if err := st.checkDestination(ctx); err != nil {
				return SharePlan{}, err
			}
		}
	}
	if st.base != "" && !st.published && len(st.paths) != 0 {
		if err := checkShareBaseTree(ctx, localGit(b.ClonePath), st.base, st.paths); err != nil {
			return SharePlan{}, err
		}
	}
	if err := st.previewShareArtifacts(realm); err != nil {
		return SharePlan{}, err
	}
	plan := SharePlan{Remote: b.Remote, Branch: b.Branch, ClonePath: b.ClonePath, Published: st.published, state: st}
	if !st.published && (st.initial || b.PendingShare != nil && b.PendingShare.Base == "") {
		if pending := b.PendingShare; pending != nil && pending.Commit != "" {
			out, err := localGit(b.ClonePath).run(ctx, nil, "cat-file", "blob", pending.Commit+":"+manifestName)
			if err != nil {
				return SharePlan{}, err
			}
			var recorded Manifest
			if err := toml.Unmarshal(out, &recorded); err != nil {
				return SharePlan{}, fmt.Errorf("read recorded share members: %w", err)
			}
			st.manifest.Members = recorded.Members
		}
		plan.Members = slices.Clone(st.manifest.Members)
	}
	for _, selected := range st.paths {
		review := selected.ShareSelection
		review.Buckets = slices.Clone(review.Buckets)
		review.Exclusions = slices.Clone(review.Exclusions)
		if artifact := st.artifacts[review.Mapping.Source]; artifact != nil {
			public := *artifact
			public.Notes = slices.Clone(artifact.Notes)
			public.Native = slices.Clone(artifact.Native)
			review.Artifact = &public
		}
		plan.Selections = append(plan.Selections, review)
	}
	st.reviewDigest, err = st.sealReview()
	if err != nil {
		return SharePlan{}, err
	}
	return plan, nil
}

func (st *sharePlanState) sealReview() (string, error) {
	// A recorded commit identifies the publication tree on retries.
	publication := make([]string, len(st.entries))
	for i, entries := range st.entries {
		tree := sha256.New()
		for _, entry := range entries {
			digest := entry.digest
			if entry.projected {
				digest = fmt.Sprintf("%x", sha256.Sum256([]byte(entry.content)))
			}
			fmt.Fprintf(tree, "%q %o %q %s\n", entry.name, entry.mode, entry.target, digest)
		}
		publication[i] = fmt.Sprintf("%x", tree.Sum(nil))
	}
	b := st.binding
	return approvalDigest("share", struct {
		Realm, State, Clone, Remote, Branch string
		RealmID                             string
		Draft, Initial, Prepared            bool
		IndexReady, Published               bool
		Base, Head, RemoteHead              string
		Paths                               []string
		Manifest                            Manifest
		PrivateBuckets                      []Bucket
		Selections                          []pendingPath
		Publication                         []string
		Pending                             *PendingShare
		Stages                              []shareLinkState
		Artifacts                           map[string]*ArtifactReview
	}{
		Realm: st.loc.physicalRealm, State: st.loc.physicalState,
		Clone: b.ClonePath, Remote: b.Remote, Branch: b.Branch,
		RealmID: b.RealmID, Draft: st.initial && !st.prepared,
		Initial: st.initial, Prepared: st.prepared,
		IndexReady: st.indexReady, Published: st.published,
		Base: st.base, Head: st.head, RemoteHead: st.atRemote,
		Paths: st.opts.Paths, Manifest: st.manifest, PrivateBuckets: b.PrivateBuckets,
		Selections: st.paths, Publication: publication, Pending: b.PendingShare, Stages: st.reviewStages,
		Artifacts: st.artifacts,
	})
}

func (st *sharePlanState) previewShareArtifacts(realm *os.Root) error {
	pending := st.binding.PendingShare
	snapshotInstalled := pending != nil && pending.Commit != "" && (st.head != pending.Base || st.indexReady)
	root := realm
	for i, selected := range st.paths {
		mapping := selected.Mapping
		switch mapping.Kind {
		case KindSkill, KindCommand, KindAgent:
		default:
			continue
		}
		if err := validateArtifactScope(st.loc, realm, mapping.Destination); err != nil {
			return err
		}
		if snapshotInstalled && root == realm {
			var err error
			root, err = os.OpenRoot(st.binding.ClonePath)
			if err != nil {
				return err
			}
			defer root.Close()
		}
		review, err := reviewArtifactSource(root, mapping.Source, mapping.Kind)
		if err != nil {
			return err
		}
		if i < len(st.entries) {
			for name, digest := range review.digests {
				index := slices.IndexFunc(st.entries[i], func(entry snapshotEntry) bool { return entry.name == name })
				if index < 0 {
					return fmt.Errorf("artifact source metadata %q is excluded from the publication; include it before sharing %q", name, mapping.Destination)
				}
				if entry := st.entries[i][index]; !entry.mode.IsRegular() || entry.digest != digest {
					return fmt.Errorf("artifact source %q changed during review; preview again before sharing", name)
				}
			}
		}
		review.Native = artifactShareDiscovery(mapping, review.identityMissing)
		if st.artifacts == nil {
			st.artifacts = make(map[string]*ArtifactReview)
		}
		st.artifacts[mapping.Source] = review
	}
	return nil
}

func (st *sharePlanState) verifyShareArtifactCutover() error {
	if err := validateArtifactCloneScope(st.loc, st.binding.ClonePath); err != nil {
		return err
	}
	if len(st.artifacts) == 0 {
		return nil
	}
	clone, err := os.OpenRoot(st.binding.ClonePath)
	if err != nil {
		return err
	}
	defer clone.Close()
	realm, err := os.OpenRoot(st.loc.physicalRealm)
	if err != nil {
		return err
	}
	defer realm.Close()
	for _, selected := range st.paths {
		mapping := selected.Mapping
		approved := st.artifacts[mapping.Source]
		if approved == nil {
			continue
		}
		if err := validateArtifactScope(st.loc, realm, mapping.Destination); err != nil {
			return err
		}
		current, err := reviewArtifactSource(clone, mapping.Source, mapping.Kind)
		if err != nil {
			return err
		}
		if !maps.Equal(approved.digests, current.digests) || !slices.Equal(approved.Native, artifactShareDiscovery(mapping, current.identityMissing)) {
			return fmt.Errorf("artifact source or native discovery for %q changed after review; preview again before sharing", mapping.Destination)
		}
	}
	return nil
}

func sharePaths(inputs []string, state string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("select at least one explicit realm-relative path to share")
	}
	paths := make([]string, 0, len(inputs))
	for _, input := range inputs {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(input), "./"), "/")
		if err := validateDestinationPath(name, state); err != nil {
			return nil, err
		}
		paths = append(paths, name)
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

func (st *sharePlanState) initialDestination(realm *os.Root) error {
	name := filepath.Base(st.loc.physicalRealm)
	if !portablePath(name) || strings.ContainsRune(name, '/') {
		return fmt.Errorf("realm directory name %q cannot name a managed clone", name)
	}
	clonePath := filepath.Join(config.Dir(st.loc.physicalHome), "realms", name)
	if err := validateArtifactCloneScope(st.loc, clonePath); err != nil {
		return err
	}
	home, err := os.OpenRoot(st.loc.physicalHome)
	if err != nil {
		return err
	}
	defer home.Close()
	rel, _ := relativeTo(st.loc.physicalHome, clonePath)
	if _, err := lstatBelow(home, rel); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		if err := checkDirectory(home, rel); err != nil {
			return err
		}
		clone, err := home.OpenRoot(rel)
		if err != nil {
			return err
		}
		defer clone.Close()
		var initial initialShare
		if err := readTOML(clone, initialShareName, &initial); err != nil || initial.Realm != st.loc.physicalRealm || initial.Binding.ClonePath != clonePath || initial.Binding.PendingShare == nil {
			return fmt.Errorf("managed clone collision at %q; existing checkout was not adopted", clonePath)
		}
		if _, err := st.loc.validate(initial.Binding); err != nil {
			return err
		}
		st.binding, st.prepared = initial.Binding, true
		return nil
	}
	if st.opts.Remote == "" {
		return fmt.Errorf("an unbound realm requires --remote pointing to an existing empty Git repository")
	}
	branch := st.opts.Branch
	if branch == "" {
		branch = "main"
	}
	st.binding = Binding{ClonePath: clonePath, Remote: st.opts.Remote, Branch: branch}
	return st.readInitialMetadata(realm)
}

func (st *sharePlanState) readInitialMetadata(realm *os.Root) error {
	st.manifest.Version = manifestVersion
	index, err := localWikiIndex(realm)
	if err != nil || index == "" {
		return err
	}
	st.binding.PrivateBuckets, err = readPrivateWikiBuckets(index)
	if err != nil {
		return err
	}
	members, err := wiki.ReadScanMembers(index)
	if err != nil {
		return err
	}
	for _, member := range members {
		name := st.loc.bucketPath(Bucket{Path: member.Path})
		if portablePath(name) {
			st.manifest.Members = append(st.manifest.Members, Member{Key: path.Base(name), Path: name})
		}
	}
	return nil
}

func (loc bindingLocation) bucketPath(bucket Bucket) string {
	if filepath.IsAbs(bucket.Path) {
		if name, ok := relativeTo(loc.realm, bucket.Path); ok {
			return name
		}
		name, _ := relativeTo(loc.physicalRealm, bucket.Path)
		return name
	}
	return filepath.ToSlash(bucket.Path)
}

func (st *sharePlanState) selectPaths(ctx context.Context, realm *os.Root, state string) error {
	var probe []string
	destinations := make(map[string]string, len(st.manifest.Mappings)+len(st.opts.Paths))
	for _, existing := range st.manifest.Mappings {
		if err := recordPathPrefixes(destinations, existing.Destination); err != nil {
			return err
		}
	}
	for _, name := range st.opts.Paths {
		if i := slices.IndexFunc(st.manifest.Mappings, func(existing Mapping) bool { return mappingCovers(existing, name) }); i >= 0 {
			if err := verifyShareLink(realm, st.binding, st.manifest.Mappings[i]); err != nil {
				return err
			}
			continue
		}
		for _, existing := range st.manifest.Mappings {
			if pathsOverlap(existing.Source, name) {
				return fmt.Errorf("selection %q overlaps existing clone source %q", name, existing.Source)
			}
			if pathsOverlap(existing.Destination, name) {
				return fmt.Errorf("selection %q overlaps existing shared path %q", name, existing.Destination)
			}
		}
		covered := false
		for _, selected := range st.paths {
			if mappingCovers(selected.Mapping, name) {
				covered = true
				break
			}
			if pathsOverlap(selected.Mapping.Destination, name) {
				return fmt.Errorf("overlapping selected paths %q and %q", selected.Mapping.Destination, name)
			}
		}
		if covered {
			continue
		}
		mapping, err := CheckShareCandidate(realm, name, state)
		if err != nil {
			return err
		}
		if err := recordPathPrefixes(destinations, mapping.Destination); err != nil {
			return fmt.Errorf("shared destinations: %w", err)
		}
		snapshot, entries, err := scanSharePath(ctx, realm, mapping)
		if err != nil {
			return err
		}
		st.paths = append(st.paths, snapshot)
		st.entries = append(st.entries, entries)
		for _, entry := range entries {
			probe = append(probe, entry.name)
		}
	}
	ignored, err := shareIgnored(ctx, realm, probe)
	if err != nil {
		return err
	}
	for i := range st.paths {
		snapshot := &st.paths[i]
		kept := st.entries[i][:0]
		for _, entry := range st.entries[i] {
			if ignored[entry.name] {
				snapshot.Exclusions = append(snapshot.Exclusions, entry.name+" (Git ignored)")
				continue
			}
			kept = append(kept, entry)
			snapshot.Files++
			snapshot.Bytes += entry.size
		}
		if len(kept) == 0 {
			return fmt.Errorf("%q has no shareable files after exclusions and Git ignore rules", snapshot.Mapping.Destination)
		}
		if snapshot.Mapping.Kind == KindSkill && !slices.ContainsFunc(kept, func(entry snapshotEntry) bool {
			return entry.name == snapshot.Mapping.Destination+"/SKILL.md" && entry.mode.IsRegular()
		}) {
			return fmt.Errorf("skill %q requires a regular, included SKILL.md", snapshot.Mapping.Destination)
		}
		st.entries[i] = kept
		snapshot.Buckets = st.selectedBuckets(snapshot.Mapping)
		for _, bucket := range snapshot.Buckets {
			if !slices.ContainsFunc(kept, func(entry snapshotEntry) bool {
				return containsPath(bucket.Path, entry.name) && bucket.Path != entry.name
			}) {
				return fmt.Errorf("bucket %q has no shareable files", bucket.Name)
			}
		}
		slices.Sort(snapshot.Exclusions)
	}
	if err := st.validateShareMetadata(state); err != nil {
		return err
	}
	return st.validateSelectionLinks()
}

func (st *sharePlanState) selectedBuckets(mapping Mapping) []Bucket {
	if mapping.Kind != KindDirectory {
		return nil
	}
	var buckets []Bucket
	for _, bucket := range st.binding.PrivateBuckets {
		name := st.loc.bucketPath(bucket)
		if name != "" && containsPath(mapping.Destination, name) {
			buckets = append(buckets, Bucket{Name: bucket.Name, Path: name})
		}
	}
	return buckets
}

func (st *sharePlanState) validateShareMetadata(state string) error {
	buckets := make(map[string]bool)
	for _, bucket := range st.manifest.Buckets {
		buckets[strings.ToLower(bucket.Name)] = true
	}
	for _, selected := range st.paths {
		for _, bucket := range selected.Buckets {
			if err := config.ValidateSegment("bucket name", bucket.Name); err != nil {
				return err
			}
			if key := strings.ToLower(bucket.Name); buckets[key] {
				return fmt.Errorf("bucket name %q is already shared or selected", bucket.Name)
			} else {
				buckets[key] = true
			}
		}
	}
	members := make(map[string]bool)
	for _, member := range st.manifest.Members {
		if err := config.ValidateSegment("member key", member.Key); err != nil {
			return err
		}
		if err := validateDestinationPath(member.Path, state); err != nil {
			return err
		}
		if key := strings.ToLower(member.Key); members[key] {
			return fmt.Errorf("duplicate member key %q", member.Key)
		} else {
			members[key] = true
		}
		for _, selected := range st.paths {
			if pathsOverlap(selected.Mapping.Destination, member.Path) {
				return fmt.Errorf("selected path %q overlaps member repository %q", selected.Mapping.Destination, member.Path)
			}
		}
	}
	return nil
}

func mappingCovers(mapping Mapping, name string) bool {
	return mapping.Destination == name || (mapping.Kind == KindDirectory || mapping.Kind == KindSkill) && containsPath(mapping.Destination, name)
}

func checkShareWikiCase(realm *os.Root, name string) error {
	top, _, _ := strings.Cut(name, "/")
	if !strings.EqualFold(top, "wiki") {
		return nil
	}
	if top != "wiki" || name != "wiki/index.md" && strings.EqualFold(name, "wiki/index.md") {
		return fmt.Errorf("case-aliased wiki selection %q; use wiki and wiki/index.md", name)
	}
	if err := requireWikiSpelling(realm, "wiki"); err != nil {
		return err
	}
	if name == "wiki/index.md" {
		return requireWikiSpelling(realm, name)
	}
	return nil
}

func requireWikiSpelling(realm *os.Root, name string) error {
	dir, err := realm.Open(path.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}
	if !slices.Contains(names, path.Base(name)) {
		return fmt.Errorf("wiki path %q must exist with that exact spelling", name)
	}
	return nil
}

// CheckShareCandidate classifies one realm-relative path for the selection UI.
func CheckShareCandidate(realm *os.Root, name, state string) (Mapping, error) {
	if err := validateDestinationPath(name, state); err != nil {
		return Mapping{}, err
	}
	info, err := lstatBelow(realm, name)
	if err != nil {
		return Mapping{}, err
	}
	if err := checkShareWikiCase(realm, name); err != nil {
		return Mapping{}, err
	}
	var kind MappingKind
	switch {
	case name == ".agents/realm.md" && info.Mode().IsRegular():
		kind = KindInstructions
	case artifactUnit(name, ".agents/skills/", "") && info.IsDir():
		kind = KindSkill
	case artifactUnit(name, ".agents/commands/", ".md") && info.Mode().IsRegular():
		kind = KindCommand
	case artifactUnit(name, ".agents/agents/", ".md") && info.Mode().IsRegular():
		kind = KindAgent
	case containsPath(".agents", strings.ToLower(name)):
		return Mapping{}, fmt.Errorf("select an individual authored instruction, skill, command, or agent, not %q", name)
	case info.IsDir():
		kind = KindDirectory
	case info.Mode().IsRegular():
		kind = KindFile
	default:
		return Mapping{}, fmt.Errorf("unsupported selected file %q; select a regular file or real directory", name)
	}
	if info.IsDir() {
		if bare, err := shareHasBareGit(realm, name); err != nil {
			return Mapping{}, err
		} else if bare {
			return Mapping{}, fmt.Errorf("selected path %q is a bare Git repository", name)
		}
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if _, err := lstatBelow(realm, parent+"/.git"); !os.IsNotExist(err) {
			return Mapping{}, fmt.Errorf("%q is inside Git repository %q; only an entire wiki may be snapshotted", name, parent)
		}
		if bare, err := shareHasBareGit(realm, parent); err != nil {
			return Mapping{}, err
		} else if bare {
			return Mapping{}, fmt.Errorf("%q is inside bare Git repository %q", name, parent)
		}
	}
	mapping := Mapping{Source: name, Destination: name, Kind: kind}
	return mapping, mapping.validate(state)
}

func (st *sharePlanState) checkDestination(ctx context.Context) error {
	b := st.binding
	st.indexReady, st.published = false, false
	if !st.initial || st.prepared {
		git := localGit(b.ClonePath)
		head, err := shareHead(ctx, git, b.Branch)
		if err != nil {
			return err
		}
		st.head, st.base = head, head
		if p := b.PendingShare; p != nil {
			st.base = p.Base
			if head == p.Base && p.Commit != "" {
				if err := shareClean(ctx, git, head); err != nil {
					if err := shareClean(ctx, git, p.Commit); err != nil {
						return err
					}
					st.indexReady = true
				}
			} else if err := shareClean(ctx, git, head); err != nil {
				return err
			}
		} else if err := shareClean(ctx, git, head); err != nil {
			return err
		}
	}
	refs, err := shareRemoteRefs(ctx, st.loc.physicalHome, b.Remote)
	if err != nil {
		return err
	}
	st.atRemote = refs[shareBranchRef(b.Branch)]
	if p := b.PendingShare; p != nil && p.Commit != "" {
		switch {
		case st.head == p.Commit || st.head == p.Base:
			if st.atRemote == p.Commit {
				st.published = true
				return nil
			}
		case st.head != "" && st.head == st.atRemote:
			if _, err := localGit(b.ClonePath).run(ctx, nil, "merge-base", "--is-ancestor", p.Commit, st.head); err == nil {
				st.published = true
				return nil
			}
			fallthrough
		default:
			return fmt.Errorf("clone HEAD differs from the recorded share; inspect %q before retrying", b.ClonePath)
		}
	} else if b.PendingShare != nil && st.head != st.base {
		return fmt.Errorf("clone HEAD differs from the recorded share; inspect %q before retrying", b.ClonePath)
	}
	if st.base == "" && len(refs) != 0 {
		return fmt.Errorf("initial share requires an empty remote; use realm join for an existing shared repository")
	}
	if st.atRemote != st.base {
		return fmt.Errorf("managed clone is not synchronized with %q; handle existing Git work before sharing", b.Branch)
	}
	if b.PendingShare != nil && b.PendingShare.Published {
		return fmt.Errorf("the recorded publication is no longer at the expected remote revision; inspect %q before completing local links", b.ClonePath)
	}
	return nil
}

// ApplyShare revalidates an approved plan, publishes it, then installs owned links.
func ApplyShare(ctx context.Context, plan SharePlan) error {
	digest := plan.ReviewDigest()
	if digest == "" {
		return fmt.Errorf("share requires a reviewed plan from PreviewShare")
	}
	approved := plan.state
	lock, err := approved.loc.lockShare()
	if err != nil {
		return err
	}
	defer lock.Release()
	loc, err := locateBinding(approved.loc.realm)
	if err != nil {
		return err
	}
	current, err := previewShare(ctx, loc, approved.opts)
	if err != nil {
		return err
	}
	st := current.state
	if current.ReviewDigest() != digest {
		return fmt.Errorf("share selection, binding, or remote changed after review; preview again before publishing")
	}
	if len(st.paths) == 0 {
		return nil
	}
	if err := validateArtifactCloneScope(st.loc, st.binding.ClonePath); err != nil {
		return err
	}
	if err := probeShareSymlinks(st.loc.physicalRealm); err != nil {
		return err
	}
	return st.apply(ctx)
}
