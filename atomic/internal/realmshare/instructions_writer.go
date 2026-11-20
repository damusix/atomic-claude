package realmshare

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/pelletier/go-toml/v2"
)

const (
	sharedInstructionsDir  = ".agents"
	sharedInstructionsFile = "realm.md"
	sharedInstructionsPath = sharedInstructionsDir + "/" + sharedInstructionsFile
)

// UpdateSharedInstructions transforms the owned team source without rewriting its local link.
func UpdateSharedInstructions(realmRoot string, edit func([]byte) ([]byte, error)) error {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return err
	}
	bound, err := loc.hasBinding()
	if err != nil {
		return err
	}
	if !bound {
		return fmt.Errorf("realm %q has no shared binding", loc.realm)
	}
	lock, err := loc.lockShare()
	if err != nil {
		return err
	}
	defer lock.Release()

	dir, err := openSharedInstructionsDir(loc)
	if err != nil {
		return err
	}
	defer dir.Close()
	file, err := openRegular(dir, sharedInstructionsFile)
	if err != nil {
		return err
	}
	before, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	original, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	updated, err := edit(slices.Clone(original))
	if err != nil || bytes.Equal(original, updated) {
		return err
	}

	currentDir, err := openSharedInstructionsDir(loc)
	if err != nil {
		return err
	}
	defer currentDir.Close()
	current, err := currentDir.Lstat(sharedInstructionsFile)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(before, current) ||
		current.Mode() != before.Mode() || current.Size() != before.Size() || !current.ModTime().Equal(before.ModTime()) {
		return fmt.Errorf("shared instruction source %q changed during the edit", sharedInstructionsPath)
	}
	return publishSharedInstructions(currentDir, updated, before.Mode())
}

func openSharedInstructionsDir(loc bindingLocation) (*os.Root, error) {
	current, err := locateBinding(loc.realm)
	if err != nil {
		return nil, err
	}
	if current != loc {
		return nil, fmt.Errorf("realm state location changed during the instruction update")
	}
	l, err := openLocalLinkView(loc, true)
	if err != nil {
		return nil, err
	}
	defer l.close()
	if l.binding.PendingShare != nil || l.binding.PendingLocal != nil {
		return nil, fmt.Errorf("finish the pending realm operation before updating shared instructions")
	}
	for _, link := range l.binding.Links {
		if link.Path == sharedInstructionsPath && link.Source == sharedInstructionsPath {
			if _, err := localLinkWitness(l.realm, l.state, l.binding, link); err != nil {
				return nil, err
			}
			return l.openTrustedInstructionsDir()
		}
	}
	return nil, fmt.Errorf("realm %q has no owned link for %q", loc.realm, sharedInstructionsPath)
}

func (l *localLinks) openTrustedInstructionsDir() (*os.Root, error) {
	clone, err := os.OpenRoot(l.binding.ClonePath)
	if err != nil {
		return nil, err
	}
	defer clone.Close()
	data, err := readLocalRegular(clone, manifestName)
	if err != nil {
		return nil, err
	}
	if localDigest(data) != l.binding.TrustedDigest {
		return nil, fmt.Errorf("shared realm definition has not been trusted; review it with realm link")
	}
	var manifest Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&manifest); err != nil {
		return nil, fmt.Errorf("read shared realm definition: %w", err)
	}
	want := Mapping{Source: sharedInstructionsPath, Destination: sharedInstructionsPath, Kind: KindInstructions}
	if !slices.Contains(manifest.Mappings, want) || manifest.Version != manifestVersion || manifest.ID != l.binding.RealmID {
		return nil, fmt.Errorf("trusted realm definition has no exact instruction mapping for %q", sharedInstructionsPath)
	}
	if err := checkMappingSource(clone, want); err != nil {
		return nil, err
	}
	return clone.OpenRoot(sharedInstructionsDir)
}

func publishSharedInstructions(dir *os.Root, data []byte, mode os.FileMode) error {
	temp := "." + sharedInstructionsFile + "." + rand.Text()
	file, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return dir.Rename(temp, sharedInstructionsFile)
}
