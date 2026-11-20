package realmshare

import (
	"context"
	"os"
	"path/filepath"
	"slices"
)

// LinkedRoots returns the realm-local discovery paths of declared, witnessed links.
func LinkedRoots(realmRoot string) ([]string, error) {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return nil, err
	}
	l, err := openLocalLinkView(loc, true)
	if err != nil {
		return nil, err
	}
	defer l.close()
	if len(l.binding.Links) == 0 {
		return nil, nil
	}
	clone, err := os.OpenRoot(l.binding.ClonePath)
	if err != nil {
		return nil, err
	}
	defer clone.Close()
	var m Manifest
	manifestErr := readTOML(clone, manifestName, &m)
	if manifestErr == nil && m.ID != l.binding.RealmID {
		return nil, l.checkMappingDeclarations(m)
	}
	ctx := context.Background()
	git := statusGit(l.binding.ClonePath)
	head, err := statusHead(ctx, clone, git, l.binding)
	if err != nil {
		return nil, err
	}
	fallback := os.IsNotExist(manifestErr)
	if !fallback {
		fallback, err = statusManifestUnmerged(ctx, git, head, loc)
		if err != nil {
			return nil, err
		}
	}
	if fallback {
		if head == "" {
			return nil, nil
		}
		m, err = l.committedManifest(ctx, git, head)
		if err != nil {
			return nil, err
		}
	} else if manifestErr != nil {
		return nil, manifestErr
	} else if err := l.checkMappingDeclarations(m); err != nil {
		return nil, err
	}
	var roots []string
	for _, link := range l.binding.Links {
		i := slices.IndexFunc(m.Mappings, func(mapping Mapping) bool {
			return mapping.Destination == link.Path && mapping.Source == link.Source
		})
		if i < 0 || checkMappingSource(clone, m.Mappings[i]) != nil {
			continue
		}
		if _, err := localLinkWitness(l.realm, l.state, l.binding, link); err != nil {
			continue
		}
		roots = append(roots, filepath.Join(loc.realm, filepath.FromSlash(link.Path)))
	}
	slices.Sort(roots)
	return roots, nil
}
