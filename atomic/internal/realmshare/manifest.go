package realmshare

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

const manifestName = "realm.toml"
const manifestVersion = 1

// Manifest defines the portable content and layout of a shared realm.
type Manifest struct {
	Version  int       `toml:"version"`
	ID       string    `toml:"id"`
	Members  []Member  `toml:"members,omitempty"`
	Buckets  []Bucket  `toml:"buckets,omitempty"`
	Mappings []Mapping `toml:"mappings,omitempty"`
}

// Member is a realm-relative repository definition, not a shared content root.
type Member struct {
	Key     string `toml:"key"`
	Path    string `toml:"path"`
	Exclude bool   `toml:"exclude,omitempty"`
}

// Bucket registers a capture folder relative to the consuming realm.
type Bucket struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// Mapping declares one clone source and one local realm destination.
type Mapping struct {
	Source      string      `toml:"source"`
	Destination string      `toml:"destination"`
	Kind        MappingKind `toml:"kind"`
}

// MappingKind identifies a content or authored artifact unit.
type MappingKind string

const (
	KindFile         MappingKind = "file"
	KindDirectory    MappingKind = "directory"
	KindInstructions MappingKind = "instructions"
	KindSkill        MappingKind = "skill"
	KindCommand      MappingKind = "command"
	KindAgent        MappingKind = "agent"
)

// LoadManifest reads a clone's definition and validates it against a local realm.
// A matching existing destination symlink is not proof of ownership.
func LoadManifest(cloneRoot, realmRoot string) (Manifest, error) {
	clone, err := os.OpenRoot(cloneRoot)
	if err != nil {
		return Manifest{}, err
	}
	defer clone.Close()
	manifest, err := readManifest(clone)
	if err != nil {
		return Manifest{}, err
	}

	realmRoot, err = filepath.Abs(realmRoot)
	if err != nil {
		return Manifest{}, err
	}
	realm, err := os.OpenRoot(realmRoot)
	if err != nil {
		return Manifest{}, err
	}
	defer realm.Close()
	loc, err := config.ResolveStateLocation(realmRoot)
	if err != nil {
		return Manifest{}, err
	}
	state, _ := relativeTo(realmRoot, loc.Root)
	if err := manifest.validate(clone, realm, state); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", filepath.Join(cloneRoot, manifestName), err)
	}
	return manifest, nil
}

func readManifest(clone *os.Root) (Manifest, error) {
	var manifest Manifest
	if err := readTOML(clone, manifestName, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Version != manifestVersion {
		return Manifest{}, fmt.Errorf("%s: unsupported realm manifest version %d", clone.Name(), manifest.Version)
	}
	if err := config.ValidateSegment("shared realm identity", manifest.ID); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readTOML(root *os.Root, name string, dest any) error {
	file, err := openRegular(root, name)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := toml.NewDecoder(file).DisallowUnknownFields().Decode(dest); err != nil {
		return fmt.Errorf("%s: %w", filepath.Join(root.Name(), name), err)
	}
	return nil
}

func (m Manifest) validate(clone, realm *os.Root, state string) error {
	sourcePrefixes := make(map[string]string)
	destinationPrefixes := make(map[string]string)
	for i, mapping := range m.Mappings {
		if err := mapping.validate(state); err != nil {
			return err
		}
		for _, prev := range m.Mappings[:i] {
			if pathsOverlap(prev.Source, mapping.Source) {
				return fmt.Errorf("overlapping sources %q and %q", prev.Source, mapping.Source)
			}
			if pathsOverlap(prev.Destination, mapping.Destination) {
				return fmt.Errorf("overlapping destinations %q and %q", prev.Destination, mapping.Destination)
			}
		}
		if err := recordPathPrefixes(sourcePrefixes, mapping.Source); err != nil {
			return fmt.Errorf("shared sources: %w", err)
		}
		if err := recordPathPrefixes(destinationPrefixes, mapping.Destination); err != nil {
			return fmt.Errorf("shared destinations: %w", err)
		}
	}
	sources := make(map[string]sourceEntry, len(m.Mappings))
	var links []string
	for _, mapping := range m.Mappings {
		if err := validateSource(clone, mapping, sources, &links); err != nil {
			return err
		}
		if realm != nil {
			if err := validateDestination(clone, realm, mapping); err != nil {
				return err
			}
		}
	}
	directories := make(directoryLinks)
	for _, name := range links {
		target, err := validateSourceLink(clone, name, sources)
		if err != nil {
			return err
		}
		if target != "" {
			directories.add(path.Dir(name), target)
		}
	}
	if err := directories.checkCycles(); err != nil {
		return err
	}

	members := make(map[string]bool, len(m.Members))
	for _, member := range m.Members {
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
		for _, mapping := range m.Mappings {
			if pathsOverlap(mapping.Destination, member.Path) {
				return fmt.Errorf("member path %q overlaps shared destination %q", member.Path, mapping.Destination)
			}
		}
		if realm != nil {
			if err := checkDirectory(realm, member.Path); err != nil {
				return err
			}
		}
	}

	buckets := make(map[string]bool, len(m.Buckets))
	for _, bucket := range m.Buckets {
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
		for _, mapping := range m.Mappings {
			if mapping.Kind != KindDirectory || !containsPath(mapping.Destination, bucket.Path) {
				continue
			}
			source := mapping.Source + strings.TrimPrefix(bucket.Path, mapping.Destination)
			info, err := lstatBelow(clone, source)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("bucket %q must refer to a shared directory at %q", bucket.Name, source)
			}
			if realm != nil {
				local, err := realm.Lstat(mapping.Destination)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if err == nil && local.Mode()&fs.ModeSymlink == 0 {
					if err := checkDirectory(realm, bucket.Path); err != nil {
						return fmt.Errorf("bucket %q: %w", bucket.Name, err)
					}
				}
			}
			covered = true
			break
		}
		if !covered {
			return fmt.Errorf("bucket %q at %q has no shared directory mapping", bucket.Name, bucket.Path)
		}
	}
	return nil
}

func (mapping Mapping) validate(state string) error {
	if err := validateSharedPath(mapping.Source); err != nil {
		return err
	}
	if err := validateDestinationPath(mapping.Destination, state); err != nil {
		return err
	}
	for _, name := range []string{mapping.Source, mapping.Destination} {
		var valid bool
		switch mapping.Kind {
		case KindFile, KindDirectory:
			valid = !containsPath(".agents", strings.ToLower(name))
		case KindInstructions:
			valid = name == ".agents/realm.md"
		case KindSkill:
			valid = artifactUnit(name, ".agents/skills/", "")
		case KindCommand:
			valid = artifactUnit(name, ".agents/commands/", ".md")
		case KindAgent:
			valid = artifactUnit(name, ".agents/agents/", ".md")
		default:
			return fmt.Errorf("unsupported mapping kind %q", mapping.Kind)
		}
		if !valid {
			return fmt.Errorf("path %q is not a selectable %s unit", name, mapping.Kind)
		}
	}
	switch mapping.Kind {
	case KindSkill, KindCommand, KindAgent:
		if mapping.Source != mapping.Destination {
			return fmt.Errorf("%s mapping destination %q must match source %q to preserve native identity", mapping.Kind, mapping.Destination, mapping.Source)
		}
	}
	return nil
}

func artifactUnit(name, prefix, extension string) bool {
	unit, ok := strings.CutPrefix(name, prefix)
	return ok && !strings.ContainsRune(unit, '/') && strings.HasSuffix(unit, extension) && len(unit) > len(extension)
}

func validateDestinationPath(name, state string) error {
	if err := validateSharedPath(name); err != nil {
		return err
	}
	if state != "" && pathsOverlap(state, name) {
		return fmt.Errorf("path %q overlaps local state at %q", name, state)
	}
	return nil
}

func validateSharedPath(name string) error {
	if !portablePath(name) {
		return fmt.Errorf("%q must be a portable relative path", name)
	}
	for part := range strings.SplitSeq(name, "/") {
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, ".env.") || (lower == ".agents" && !containsPath(".agents", name)) {
			return fmt.Errorf("path %q contains local-only entry %q", name, part)
		}
		switch lower {
		case ".git", ".atomic", ".atomic-index", ".scratchpad", ".claude", ".codex", ".omp", ".pi", ".cursor", ".gemini", ".opencode", ".windsurf",
			"agents.md", "agents.local.md", "claude.md", "claude.local.md", "gemini.md", "gemini.local.md",
			"realm-share.toml", "realm-share-owners.toml", ".mcp.json", "mcp.json", "hooks.json", "opencode.json", "opencode.jsonc",
			".env", ".netrc", ".git-credentials", "credentials.json", "auth.json":
			return fmt.Errorf("path %q contains local-only entry %q", name, part)
		}
	}
	if containsPath(manifestName, strings.ToLower(name)) {
		return fmt.Errorf("path %q overlaps the shared manifest", name)
	}
	return nil
}

type sourceEntry struct {
	name string
	mode fs.FileMode
}

func rememberSource(sources map[string]sourceEntry, name string, mode fs.FileMode) error {
	key := strings.ToLower(name)
	if prev, ok := sources[key]; ok && prev.name != name {
		return fmt.Errorf("case aliases %q and %q in shared sources", prev.name, name)
	}
	sources[key] = sourceEntry{name, mode}
	return nil
}

func validateSource(clone *os.Root, mapping Mapping, sources map[string]sourceEntry, links *[]string) error {
	info, err := lstatBelow(clone, mapping.Source)
	if err != nil {
		return err
	}
	if mapping.Kind != KindDirectory && mapping.Kind != KindSkill {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source %q must be a regular file", mapping.Source)
		}
		return rememberSource(sources, mapping.Source, 0)
	}
	if !info.IsDir() {
		return fmt.Errorf("source %q must be a real directory", mapping.Source)
	}
	if mapping.Kind == KindSkill {
		entry, err := lstatBelow(clone, mapping.Source+"/SKILL.md")
		if err != nil || !entry.Mode().IsRegular() {
			return fmt.Errorf("skill %q must contain a regular SKILL.md", mapping.Source)
		}
	}
	return fs.WalkDir(clone.FS(), mapping.Source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := validateSharedPath(name); err != nil {
			return err
		}
		if err := rememberSource(sources, name, entry.Type()); err != nil {
			return err
		}
		switch entry.Type() {
		case 0, fs.ModeDir:
			return nil
		case fs.ModeSymlink:
			*links = append(*links, name)
			return nil
		default:
			return fmt.Errorf("source %q is neither a regular file, directory, nor symlink", name)
		}
	})
}

func validateSourceLink(clone *os.Root, name string, sources map[string]sourceEntry) (string, error) {
	target, err := clone.Readlink(name)
	if err != nil {
		return "", err
	}
	local := filepath.ToSlash(target)
	suffix := local
	for strings.HasPrefix(suffix, "../") {
		suffix = suffix[3:]
	}
	resolved := path.Join(path.Dir(name), local)
	if local != target || !portablePath(suffix) || !portablePath(resolved) {
		return "", fmt.Errorf("unsafe symlink %q -> %q", name, target)
	}
	entry := sources[strings.ToLower(resolved)]
	if entry.name == resolved {
		if entry.mode == fs.ModeDir {
			return resolved, nil
		}
		if entry.mode == 0 {
			return "", nil
		}
	}
	return "", fmt.Errorf("symlink %q -> %q must resolve directly to declared content", name, target)
}

func validateDestination(clone, realm *os.Root, mapping Mapping) error {
	info, err := lstatBelow(realm, mapping.Destination)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	destination := filepath.Join(realm.Name(), filepath.FromSlash(mapping.Destination))
	source := filepath.Join(clone.Name(), filepath.FromSlash(mapping.Source))
	dst, err := os.Stat(destination)
	if err != nil {
		return fmt.Errorf("destination symlink %q: %w", mapping.Destination, err)
	}
	src, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !os.SameFile(dst, src) {
		return fmt.Errorf("destination symlink %q does not point to declared source %q", mapping.Destination, mapping.Source)
	}
	return nil
}
