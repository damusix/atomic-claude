package realmshare

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func syncPathspec(manifest Manifest) string {
	var paths strings.Builder
	paths.WriteString(manifestName)
	paths.WriteByte('\x00')
	for _, mapping := range manifest.Mappings {
		paths.WriteString(mapping.Source)
		paths.WriteByte('\x00')
	}
	return paths.String()
}

func syncMapping(manifest Manifest, name string) (Mapping, bool) {
	for _, mapping := range manifest.Mappings {
		if containsPath(mapping.Source, name) {
			return mapping, true
		}
	}
	return Mapping{}, false
}

func syncLiveManifest(ctx context.Context, clone *os.Root, git shareGit, realmID, state string, artifacts *syncArtifactGuard) (Manifest, error) {
	manifest, err := readManifest(clone)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.ID != realmID {
		return Manifest{}, fmt.Errorf("realm identity %q does not match %q", manifest.ID, realmID)
	}
	if err := manifest.validate(clone, nil, state); err != nil {
		return Manifest{}, err
	}
	seen := make(map[string]bool)
	checkNested := func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true
		if _, err := clone.Lstat(name + "/.git"); err == nil {
			return fmt.Errorf("nested Git repository at %q", name)
		} else if !os.IsNotExist(err) {
			return err
		}
		if bare, err := shareHasBareGit(clone, name); err != nil {
			return err
		} else if bare {
			return fmt.Errorf("nested bare Git repository at %q", name)
		}
		return nil
	}
	for _, mapping := range manifest.Mappings {
		for parent := path.Dir(mapping.Source); parent != "."; parent = path.Dir(parent) {
			if err := checkNested(parent); err != nil {
				return Manifest{}, err
			}
		}
		if mapping.Kind == KindDirectory || mapping.Kind == KindSkill {
			if err := fs.WalkDir(clone.FS(), mapping.Source, func(name string, entry fs.DirEntry, err error) error {
				if err == nil && entry.IsDir() {
					return checkNested(name)
				}
				return err
			}); err != nil {
				return Manifest{}, err
			}
		}
	}
	if artifacts != nil {
		if err := artifacts.review(liveArtifactReader(clone)); err != nil {
			return Manifest{}, err
		}
		err := artifacts.inspect(manifest, func(source string, visit func(name, target string) error) error {
			if _, err := lstatBelow(clone, source); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			return fs.WalkDir(clone.FS(), source, func(name string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				if entry.Type()&fs.ModeSymlink == 0 {
					return visit(name, "")
				}
				target, err := clone.Readlink(name)
				if err != nil {
					return err
				}
				return visit(name, target)
			})
		}, func(files, attributes map[string]struct{}) error {
			for name := range attributes {
				if err := checkSyncAttributeFile(clone, name); err != nil {
					return err
				}
			}
			return checkSyncAttributes(ctx, git, "", files)
		})
		if err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

type syncTreeEntry struct {
	mode         string
	oid          string
	materialized bool
}

func (entry syncTreeEntry) regular() bool {
	return entry.mode == "100644" || entry.mode == "100755"
}

func syncTreeManifest(ctx context.Context, git shareGit, tree, realmID, state string, guards ...*syncArtifactGuard) (Manifest, error) {
	out, err := git.run(ctx, nil, "ls-tree", "-r", "-t", "-z", "--full-tree", tree)
	if err != nil {
		return Manifest{}, err
	}
	entries := make(map[string]syncTreeEntry)
	spellings := make(map[string]string)
	var links []string
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		mode, object, spaced := strings.Cut(meta, " ")
		kind, oid, typed := strings.Cut(object, " ")
		if !ok || !spaced || !typed || !gitObjectID(oid) {
			return Manifest{}, fmt.Errorf("invalid entry %q in Git tree %s", name, tree)
		}
		if _, exists := entries[name]; exists {
			return Manifest{}, fmt.Errorf("duplicate Git tree entry %q", name)
		}
		switch {
		case mode == "160000":
			return Manifest{}, fmt.Errorf("nested Git repository at %q", name)
		case mode == "040000" && kind == "tree":
		case (mode == "100644" || mode == "100755") && kind == "blob":
		case mode == "120000" && kind == "blob":
			links = append(links, name)
		default:
			return Manifest{}, fmt.Errorf("unsupported Git tree entry %q (%s %s)", name, mode, kind)
		}
		if name != manifestName {
			if err := validateSharedPath(name); err != nil {
				return Manifest{}, err
			}
		}
		if err := recordPathPrefixes(spellings, name); err != nil {
			return Manifest{}, err
		}
		entries[name] = syncTreeEntry{mode: mode, oid: oid}
		if kind == "blob" {
			// Recursive ls-tree lists each directory before its descendants.
			for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
				entry := entries[parent]
				if entry.materialized {
					break
				}
				entry.materialized = true
				entries[parent] = entry
			}
		}
	}
	if !entries[manifestName].regular() {
		return Manifest{}, fmt.Errorf("Git tree %s must contain a regular %s", tree, manifestName)
	}
	data, err := git.run(ctx, nil, "cat-file", "blob", entries[manifestName].oid)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%s at %s: %w", manifestName, tree, err)
	}
	if manifest.Version != manifestVersion {
		return Manifest{}, fmt.Errorf("unsupported realm manifest version %d at %s", manifest.Version, tree)
	}
	if err := config.ValidateSegment("shared realm identity", manifest.ID); err != nil {
		return Manifest{}, err
	}
	if realmID != "" && manifest.ID != realmID {
		return Manifest{}, fmt.Errorf("realm identity %q at %s does not match %q", manifest.ID, tree, realmID)
	}
	if err := validateSyncTreeDefinition(manifest, entries, state); err != nil {
		return Manifest{}, err
	}
	var artifacts *syncArtifactGuard
	var targets map[string]string
	if len(guards) != 0 && guards[0] != nil {
		artifacts = guards[0]
		targets = make(map[string]string, len(links))
	}
	if err := validateSyncTreeLinks(ctx, git, manifest, entries, links, targets); err != nil {
		return Manifest{}, err
	}
	if artifacts != nil {
		if err := artifacts.review(treeArtifactReader(ctx, git, entries, spellings)); err != nil {
			return Manifest{}, err
		}
		names := slices.Sorted(maps.Keys(entries))
		err := artifacts.inspect(manifest, func(source string, visit func(name, target string) error) error {
			source = spellings[strings.ToLower(source)]
			if source == "" {
				return nil
			}
			if entries[source].mode != "040000" {
				return visit(source, targets[source])
			}
			prefix := source + "/"
			i, _ := slices.BinarySearch(names, prefix)
			for ; i < len(names) && strings.HasPrefix(names[i], prefix); i++ {
				name := names[i]
				if entries[name].mode != "040000" {
					if err := visit(name, targets[name]); err != nil {
						return err
					}
				}
			}
			return nil
		}, func(files, attributes map[string]struct{}) error {
			for name := range attributes {
				if entries[name].regular() {
					continue
				}
				if actual := spellings[strings.ToLower(name)]; actual != "" {
					return fmt.Errorf("Git attributes path %q must be a regular file with exact spelling %q", actual, name)
				}
				// Checkout falls back to the live file where the target index has no attributes file.
				if _, err := lstatBelow(artifacts.clone, name); err == nil {
					return fmt.Errorf("cannot review checkout conversion at %q: the working-tree Git attributes file is absent from the target; remove it or unlink the affected artifact before syncing and reviewing again", name)
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return checkSyncAttributes(ctx, git, tree, files)
		})
		if err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

type syncArtifactGuard struct {
	clone         *os.Root
	sources       map[string]bool
	approved      map[string]string
	conflictPaths map[string]struct{}
}

func newSyncArtifactGuard(loc bindingLocation, clone *os.Root, binding Binding) (*syncArtifactGuard, error) {
	sources := make(map[string]bool)
	hasLinkedSkill := false
	for _, link := range binding.Links {
		if kind := artifactKind(link.Source); kind != "" {
			sources[link.Source] = kind == KindSkill
			hasLinkedSkill = hasLinkedSkill || kind == KindSkill
		}
	}
	if pending := binding.PendingShare; pending != nil {
		for _, selected := range pending.Selections {
			if source := selected.Mapping.Source; artifactKind(source) != "" {
				if _, linked := sources[source]; !linked {
					sources[source] = false
				}
			}
		}
	}
	if len(sources) == 0 {
		return nil, nil
	}
	if err := checkSyncAttributeFiles(clone); err != nil {
		return nil, err
	}
	guard := &syncArtifactGuard{clone: clone, sources: sources, conflictPaths: make(map[string]struct{})}
	if !hasLinkedSkill {
		return guard, nil
	}
	realm, err := os.OpenRoot(loc.physicalRealm)
	if err != nil {
		return nil, err
	}
	defer realm.Close()
	state, err := os.OpenRoot(loc.physicalState)
	if err != nil {
		return nil, err
	}
	defer state.Close()
	guard.approved = make(map[string]string)
	for _, link := range binding.Links {
		if _, err := localLinkWitness(realm, state, binding, link); err == nil {
			guard.approved[link.Path] = link.Source
		}
	}
	return guard, nil
}

func (g *syncArtifactGuard) review(read func(string, int64) ([]byte, error)) error {
	for source := range g.sources {
		if _, err := read(source, 0); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if _, err := reviewArtifact(source, artifactKind(source), false, read); err != nil {
			return err
		}
	}
	return nil
}

func (g *syncArtifactGuard) inspect(manifest Manifest, walk func(string, func(string, string) error) error, check func(files, attributes map[string]struct{}) error) error {
	type scope struct {
		source            string
		checkDependencies bool
	}
	queue := make([]scope, 0, len(g.sources))
	seen := make(map[scope]struct{})
	for source, checkDependencies := range g.sources {
		unit := scope{source, checkDependencies}
		queue = append(queue, unit)
		seen[unit] = struct{}{}
	}
	files := make(map[string]struct{})
	attributes := make(map[string]struct{})
	for i := 0; i < len(queue); i++ {
		unit := queue[i]
		g.conflictPaths[unit.source] = struct{}{}
		err := walk(unit.source, func(name, target string) error {
			if target == "" {
				files[name] = struct{}{}
			}
			for dir := path.Dir(name); ; dir = path.Dir(dir) {
				attr := path.Join(dir, ".gitattributes")
				attributes[attr] = struct{}{}
				if dir == "." {
					break
				}
			}
			if target == "" || !unit.checkDependencies {
				return nil
			}
			resolved := path.Join(path.Dir(name), target)
			if containsPath(unit.source, resolved) {
				return nil
			}
			mapping, ok := syncMapping(manifest, resolved)
			if !ok || g.approved[mapping.Destination] != mapping.Source {
				return fmt.Errorf("linked skill dependency %q at %q has no matching approved local link; unlink the skill, sync, then review and link both the skill and the dependency", resolved, name)
			}
			dependency := scope{resolved, true}
			if _, ok := seen[dependency]; !ok {
				queue = append(queue, dependency)
				seen[dependency] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for name := range attributes {
		files[name] = struct{}{}
		g.conflictPaths[name] = struct{}{}
	}
	return check(files, attributes)
}

func treeArtifactReader(ctx context.Context, git shareGit, entries map[string]syncTreeEntry, spellings map[string]string) func(string, int64) ([]byte, error) {
	return func(name string, limit int64) ([]byte, error) {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if actual := spellings[strings.ToLower(parent)]; actual != "" && entries[actual].mode != "040000" {
				return nil, fmt.Errorf("parent %q must be a real directory", actual)
			}
		}
		actual, ok := spellings[strings.ToLower(name)]
		if !ok {
			return nil, fs.ErrNotExist
		}
		if limit == 0 {
			return nil, nil
		}
		entry := entries[actual]
		if !entry.regular() {
			return nil, fmt.Errorf("%q must be a regular file in the Git tree", actual)
		}
		out, err := git.run(ctx, nil, "cat-file", "-s", entry.oid)
		if err != nil {
			return nil, err
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid Git blob size for %q", actual)
		}
		if size > limit {
			return nil, fmt.Errorf("%q exceeds the %d-byte review limit", actual, limit)
		}
		return git.run(ctx, nil, "cat-file", "blob", entry.oid)
	}
}

func syncArtifactMergeTree(ctx context.Context, git shareGit, head, remote string, artifacts *syncArtifactGuard) (string, error) {
	git.userConfig = true
	if _, err := git.run(ctx, nil, "config", "--includes", "--get", "merge.default"); err == nil {
		return "", fmt.Errorf("Git merge.default can select a custom merge driver for protected artifacts; remove it before syncing divergent history")
	} else if !gitExit(err, 1) {
		return "", err
	}
	out, err := git.run(ctx, nil, "merge-tree", "--write-tree", "--no-messages", "--name-only", "-z", head, remote)
	if err != nil && !gitExit(err, 1) {
		return "", fmt.Errorf("cannot review received merge before updating linked artifacts: %w", err)
	}
	tree, conflicts, ok := strings.Cut(string(out), "\x00")
	if !ok || len(tree) != len(head) || !gitObjectID(tree) {
		return "", fmt.Errorf("Git did not return a valid proposed merge tree")
	}
	if err == nil {
		return tree, nil
	}
	checkPaths := func(paths string) error {
		for name := range strings.SplitSeq(paths, "\x00") {
			if name == "" {
				continue
			}
			for source := range artifacts.conflictPaths {
				if pathsOverlap(source, name) {
					return fmt.Errorf("merge conflict with changes at linked artifact %q; resolve in a separate checkout or unlink the affected artifact before syncing", name)
				}
			}
		}
		return nil
	}
	if err := checkPaths(conflicts); err != nil {
		return "", err
	}
	// Keep Git's normal conflict workflow only when neither side can change exposed sources.
	for _, candidate := range []string{remote, tree} {
		changed, err := git.run(ctx, nil, "diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-z", head, candidate, "--")
		if err != nil {
			return "", err
		}
		if err := checkPaths(string(changed)); err != nil {
			return "", err
		}
	}
	return "", nil
}

func validateSyncTreeDefinition(manifest Manifest, entries map[string]syncTreeEntry, state string) error {
	destinationPrefixes := make(map[string]string)
	parents := make(map[string]bool)
	for i, mapping := range manifest.Mappings {
		if err := mapping.validate(state); err != nil {
			return err
		}
		for _, previous := range manifest.Mappings[:i] {
			if pathsOverlap(previous.Source, mapping.Source) {
				return fmt.Errorf("overlapping sources %q and %q", previous.Source, mapping.Source)
			}
			if pathsOverlap(previous.Destination, mapping.Destination) {
				return fmt.Errorf("overlapping destinations %q and %q", previous.Destination, mapping.Destination)
			}
		}
		if err := recordPathPrefixes(destinationPrefixes, mapping.Destination); err != nil {
			return err
		}
		entry := entries[mapping.Source]
		switch mapping.Kind {
		case KindDirectory, KindSkill:
			if entry.mode != "040000" {
				return fmt.Errorf("source %q must be a real directory in the Git tree", mapping.Source)
			}
			if mapping.Kind == KindSkill && !entries[mapping.Source+"/SKILL.md"].regular() {
				return fmt.Errorf("skill %q must contain a regular SKILL.md", mapping.Source)
			}
			if !entry.materialized {
				return fmt.Errorf("source %q has no file or symlink descendants in the Git tree", mapping.Source)
			}
		default:
			if !entry.regular() {
				return fmt.Errorf("source %q must be a regular file in the Git tree", mapping.Source)
			}
		}
		for parent := path.Dir(mapping.Source); parent != "."; parent = path.Dir(parent) {
			parents[parent] = true
		}
	}
	for name, entry := range entries {
		if name != manifestName && !(entry.mode == "040000" && parents[name]) {
			if _, ok := syncMapping(manifest, name); !ok {
				return fmt.Errorf("undeclared Git tree path %q", name)
			}
		}
		if entry.mode == "040000" {
			head, objects, refs := entries[name+"/HEAD"], entries[name+"/objects"], entries[name+"/refs"]
			if (head.regular() || head.mode == "120000") && (objects.mode == "040000" || objects.mode == "120000") && (refs.mode == "040000" || refs.mode == "120000") {
				return fmt.Errorf("nested bare Git repository at %q", name)
			}
		}
	}
	members := make(map[string]bool, len(manifest.Members))
	for _, member := range manifest.Members {
		if err := config.ValidateSegment("member key", member.Key); err != nil {
			return err
		}
		key := strings.ToLower(member.Key)
		if members[key] {
			return fmt.Errorf("duplicate member key %q", member.Key)
		}
		members[key] = true
		if err := validateDestinationPath(member.Path, state); err != nil {
			return err
		}
		for _, mapping := range manifest.Mappings {
			if pathsOverlap(mapping.Destination, member.Path) {
				return fmt.Errorf("member path %q overlaps shared destination %q", member.Path, mapping.Destination)
			}
		}
	}
	buckets := make(map[string]bool, len(manifest.Buckets))
	for _, bucket := range manifest.Buckets {
		if err := config.ValidateSegment("bucket name", bucket.Name); err != nil {
			return err
		}
		key := strings.ToLower(bucket.Name)
		if buckets[key] {
			return fmt.Errorf("duplicate bucket name %q", bucket.Name)
		}
		buckets[key] = true
		if err := validateDestinationPath(bucket.Path, state); err != nil {
			return err
		}
		covered := false
		for _, mapping := range manifest.Mappings {
			if mapping.Kind == KindDirectory && containsPath(mapping.Destination, bucket.Path) {
				source := mapping.Source + strings.TrimPrefix(bucket.Path, mapping.Destination)
				entry := entries[source]
				covered = entry.mode == "040000"
				if covered && !entry.materialized {
					return fmt.Errorf("bucket %q at %q has no file or symlink descendants in Git tree directory %q", bucket.Name, bucket.Path, source)
				}
				break
			}
		}
		if !covered {
			return fmt.Errorf("bucket %q at %q must refer to a shared directory", bucket.Name, bucket.Path)
		}
	}
	return nil
}

func validateSyncTreeLinks(ctx context.Context, git shareGit, manifest Manifest, entries map[string]syncTreeEntry, links []string, targets map[string]string) error {
	if len(links) == 0 {
		return nil
	}
	var requests strings.Builder
	for _, name := range links {
		requests.WriteString(entries[name].oid)
		requests.WriteByte('\n')
	}
	out, err := git.run(ctx, strings.NewReader(requests.String()), "cat-file", "--batch")
	if err != nil {
		return err
	}
	directories := make(directoryLinks)
	for _, name := range links {
		header, data, ok := bytes.Cut(out, []byte("\n"))
		size, blob := strings.CutPrefix(string(header), entries[name].oid+" blob ")
		n, err := strconv.Atoi(size)
		if !ok || !blob || err != nil || n < 0 || n >= len(data) || data[n] != '\n' {
			return fmt.Errorf("invalid Git symlink blob for %q", name)
		}
		target := string(data[:n])
		out = data[n+1:]
		suffix := target
		for strings.HasPrefix(suffix, "../") {
			suffix = suffix[3:]
		}
		resolved := path.Join(path.Dir(name), target)
		if !portablePath(suffix) || !portablePath(resolved) {
			return fmt.Errorf("unsafe symlink %q -> %q", name, target)
		}
		entry := entries[resolved]
		if _, ok := syncMapping(manifest, resolved); !ok || !(entry.regular() || entry.mode == "040000") {
			return fmt.Errorf("symlink %q -> %q must resolve directly to declared content", name, target)
		}
		if targets != nil {
			targets[name] = target
		}
		if entry.mode == "040000" {
			if !entry.materialized {
				return fmt.Errorf("symlink %q -> %q resolves to Git tree directory %q with no file or symlink descendants", name, target, resolved)
			}
			directories.add(path.Dir(name), resolved)
		}
	}
	return directories.checkCycles()
}
