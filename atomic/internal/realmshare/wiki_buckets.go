package realmshare

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// DocumentRoot is a declared local destination and its witnessed backing path.
type DocumentRoot struct {
	Local, Target, Clone           string
	Witness, SourceInfo, CloneInfo os.FileInfo
}

// DocumentView returns shared document roots and bucket registrations verified together.
// On error no shared roots are usable.
func DocumentView(realmRoot string) (wiki.BucketView, []DocumentRoot, error) {
	return verifiedWikiBuckets(realmRoot, true)
}

// WikiBuckets returns usable registrations rooted at the consuming realm.
func WikiBuckets(realmRoot string) (wiki.BucketView, error) {
	view, _, err := verifiedWikiBuckets(realmRoot, false)
	return view, err
}

func verifiedWikiBuckets(realmRoot string, documents bool) (wiki.BucketView, []DocumentRoot, error) {
	loc, err := locateBindingState(realmRoot)
	if err != nil {
		return wiki.BucketView{}, nil, err
	}
	bound, err := loc.hasBinding()
	if err != nil || !bound {
		return wiki.BucketView{}, nil, err
	}
	if err := loc.checkShareLocation(); err != nil {
		return wiki.BucketView{}, nil, err
	}
	l, err := openLocalLinkView(loc, true)
	if err != nil {
		return wiki.BucketView{}, nil, err
	}
	defer l.close()
	var roots []DocumentRoot
	clone, err := os.OpenRoot(l.binding.ClonePath)
	if err != nil {
		return wiki.BucketView{}, nil, err
	}
	defer clone.Close()
	var record func(LinkReceipt, os.FileInfo, os.FileInfo)
	if documents {
		cloneInfo, err := clone.Stat(".")
		if err != nil {
			return wiki.BucketView{}, nil, err
		}
		record = func(link LinkReceipt, witness, source os.FileInfo) {
			roots = append(roots, DocumentRoot{
				Local:      link.Path,
				Target:     filepath.Join(l.binding.ClonePath, filepath.FromSlash(link.Source)),
				Clone:      l.binding.ClonePath,
				Witness:    witness,
				SourceInfo: source,
				CloneInfo:  cloneInfo,
			})
		}
	}
	manifest, owned, err := l.wikiBucketOwnership(clone, record)
	if err != nil {
		return wiki.BucketView{}, nil, err
	}

	view := wiki.BucketView{Bound: true, SharedWiki: owned["wiki"].Kind == KindDirectory}
	privateNames := make(map[string]bool, len(l.binding.PrivateBuckets))
	for _, bucket := range l.binding.PrivateBuckets {
		privateNames[strings.ToLower(bucket.Name)] = true
		if dir := l.privateWikiBucketDir(bucket); dir != "" {
			view.Private = append(view.Private, wiki.BucketEntry{Name: bucket.Name, Path: dir})
		}
	}
	for _, bucket := range manifest.Buckets {
		if privateNames[strings.ToLower(bucket.Name)] {
			continue
		}
		for dest, mapping := range owned {
			if mapping.Kind != KindDirectory || !containsPath(dest, bucket.Path) {
				continue
			}
			source := mapping.Source + strings.TrimPrefix(bucket.Path, dest)
			info, err := lstatBelow(clone, source)
			if err != nil {
				if documents {
					continue
				}
				return wiki.BucketView{}, nil, err
			}
			if !info.IsDir() {
				if documents {
					continue
				}
				return wiki.BucketView{}, nil, fmt.Errorf("shared bucket %q must refer to a real directory at %q", bucket.Name, source)
			}
			view.Shared = append(view.Shared, wiki.BucketEntry{Name: bucket.Name, Path: filepath.Join(loc.realm, filepath.FromSlash(bucket.Path))})
			break
		}
	}
	return view, roots, nil
}

func (l *localLinks) wikiBucketDefinition() (*os.Root, Manifest, map[string]Mapping, error) {
	clone, err := os.OpenRoot(l.binding.ClonePath)
	if err != nil {
		return nil, Manifest{}, nil, err
	}
	manifest, owned, err := l.wikiBucketOwnership(clone, nil)
	if err != nil {
		clone.Close()
		return nil, Manifest{}, nil, err
	}
	return clone, manifest, owned, nil
}

func (l *localLinks) wikiBucketOwnership(clone *os.Root, record func(LinkReceipt, os.FileInfo, os.FileInfo)) (Manifest, map[string]Mapping, error) {
	manifest, err := readManifest(clone)
	if err != nil {
		return Manifest{}, nil, err
	}
	if err := l.checkMappingDeclarations(manifest); err != nil {
		return Manifest{}, nil, err
	}
	state, _ := relativeTo(l.loc.physicalRealm, l.loc.physicalState)
	names := make(map[string]bool, len(manifest.Buckets))
	for _, bucket := range manifest.Buckets {
		if err := config.ValidateSegment("bucket name", bucket.Name); err != nil {
			return Manifest{}, nil, err
		}
		if err := validateDestinationPath(bucket.Path, state); err != nil {
			return Manifest{}, nil, err
		}
		key := strings.ToLower(bucket.Name)
		if names[key] {
			return Manifest{}, nil, fmt.Errorf("duplicate shared bucket name %q", bucket.Name)
		}
		names[key] = true
		covered := false
		for _, mapping := range manifest.Mappings {
			if mapping.Kind == KindDirectory && containsPath(mapping.Destination, bucket.Path) {
				covered = true
				break
			}
		}
		if !covered {
			return Manifest{}, nil, fmt.Errorf("bucket %q at %q has no shared directory mapping", bucket.Name, bucket.Path)
		}
	}
	owned := make(map[string]Mapping, len(l.binding.Links))
	for _, link := range l.binding.Links {
		for _, mapping := range manifest.Mappings {
			if mapping.Destination == link.Path && mapping.Source == link.Source {
				owned[link.Path] = mapping
				break
			}
		}
		mapping, ok := owned[link.Path]
		if !ok {
			if record != nil {
				continue
			}
			return Manifest{}, nil, fmt.Errorf("local link %q has no matching shared mapping", link.Path)
		}
		witness, err := localLinkWitness(l.realm, l.state, l.binding, link)
		if err != nil {
			if record != nil {
				delete(owned, link.Path)
				continue
			}
			return Manifest{}, nil, err
		}
		if record != nil {
			source, err := lstatBelow(clone, mapping.Source)
			directory := mapping.Kind == KindDirectory || mapping.Kind == KindSkill
			if err != nil || source.IsDir() != directory || (!directory && !source.Mode().IsRegular()) {
				delete(owned, link.Path)
				continue
			}
			record(link, witness, source)
		} else if err := checkMappingSource(clone, mapping); err != nil {
			return Manifest{}, nil, err
		}
	}
	return manifest, owned, nil
}

func (l *localLinks) privateWikiBucketDir(bucket Bucket) string {
	if name := l.loc.bucketPath(bucket); name != "" {
		if info, err := lstatBelow(l.realm, name); err == nil && info.IsDir() {
			return filepath.Join(l.loc.realm, filepath.FromSlash(name))
		}
		return ""
	}
	if !filepath.IsAbs(bucket.Path) {
		return ""
	}
	dir := filepath.Clean(bucket.Path)
	if overlapsAbsolute(dir, l.loc.physicalState) || overlapsAbsolute(dir, filepath.Join(config.Dir(l.loc.physicalHome), "realms")) {
		return ""
	}
	rootPath := filepath.VolumeName(dir) + string(filepath.Separator)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return ""
	}
	defer root.Close()
	name, _ := relativeTo(rootPath, dir)
	if info, err := lstatBelow(root, name); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// AddWikiBucket runs the legacy add for an unbound realm or prepares and registers a private bucket.
func AddWikiBucket(realmRoot, name string, prepare func(string) error, legacy func() error) error {
	loc, err := locateBindingState(realmRoot)
	if err != nil {
		return err
	}
	locationErr := loc.checkShareLocation()
	if locationErr == nil {
		lock, err := loc.lockShare()
		if err != nil {
			return err
		}
		defer lock.Release()
		if current, err := locateBindingState(loc.realm); err != nil {
			return err
		} else if current != loc {
			return fmt.Errorf("realm state location changed while acquiring the writer lock")
		}
	}
	bound, err := loc.hasBinding()
	if err != nil {
		return err
	}
	if !bound {
		return legacy()
	}
	if locationErr != nil {
		return locationErr
	}
	if err := config.ValidateSegment("bucket name", name); err != nil {
		return err
	}
	if strings.HasPrefix(name, "-") || strings.EqualFold(name, "wiki") {
		return fmt.Errorf("bucket: name %q is reserved", name)
	}

	state, _ := relativeTo(loc.physicalRealm, loc.physicalState)
	if err := validateDestinationPath(name, state); err != nil {
		return err
	}
	l, err := openLocalLinks(loc)
	if err != nil {
		return err
	}
	defer l.close()
	if l.binding.PendingLocal != nil {
		return fmt.Errorf("finish the pending realm link or unlink before adding a bucket")
	}
	clone, manifest, _, err := l.wikiBucketDefinition()
	if err != nil {
		return err
	}
	defer clone.Close()
	for _, bucket := range l.binding.PrivateBuckets {
		if strings.EqualFold(bucket.Name, name) || strings.EqualFold(loc.bucketPath(bucket), name) {
			return fmt.Errorf("bucket: %q is already registered", name)
		}
	}
	for _, bucket := range manifest.Buckets {
		if strings.EqualFold(bucket.Name, name) {
			return fmt.Errorf("bucket: %q is already declared in the shared realm", name)
		}
	}
	for _, mapping := range manifest.Mappings {
		if pathsOverlap(mapping.Destination, name) {
			return fmt.Errorf("bucket %q overlaps shared path %q", name, mapping.Destination)
		}
	}
	indexInfo, err := lstatBelow(l.realm, name+"/index.md")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && !indexInfo.Mode().IsRegular() {
		return fmt.Errorf("bucket %q index must be a regular file", name)
	}
	if err := prepare(filepath.Join(loc.realm, name)); err != nil {
		return err
	}
	info, err := lstatBelow(l.realm, name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("bucket %q must be a real local directory", name)
	}
	l.binding.PrivateBuckets = append(l.binding.PrivateBuckets, Bucket{Name: name, Path: name})
	return loc.saveBinding(l.binding)
}

func localWikiIndex(realm *os.Root) (string, error) {
	const index = "wiki/index.md"
	info, err := lstatBelow(realm, "wiki")
	if os.IsNotExist(err) || err == nil && !info.IsDir() {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	info, err = lstatBelow(realm, index)
	if os.IsNotExist(err) || err == nil && !info.Mode().IsRegular() {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := checkShareWikiCase(realm, index); err != nil {
		return "", err
	}
	return filepath.Join(realm.Name(), index), nil
}

func readPrivateWikiBuckets(index string) ([]Bucket, error) {
	if index == "" {
		return nil, nil
	}
	entries, err := wiki.ReadBucketEntries(index)
	if err != nil {
		return nil, err
	}
	var buckets []Bucket
	for _, entry := range entries {
		buckets = append(buckets, Bucket{Name: entry.Name, Path: entry.Path})
	}
	return buckets, nil
}
