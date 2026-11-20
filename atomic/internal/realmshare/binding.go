package realmshare

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/pelletier/go-toml/v2"
)

const bindingName = "realm-share.toml"
const ownersName = "realm-share-owners.toml"
const cloneProofDir = "realm-share-clones"
const cloneStagingPrefix = ".prepare-"
const cloneProof = ".git/atomic-realm-share-owner"
const shareParkingPrefix = ".rollback-"

// Binding records the local realm's relationship to its managed clone.
type Binding struct {
	RealmID        string                 `toml:"realm_id"`
	ClonePath      string                 `toml:"clone_path"`
	Remote         string                 `toml:"remote"`
	Branch         string                 `toml:"branch"`
	TrustedDigest  string                 `toml:"trusted_digest,omitempty"`
	PrivateBuckets []Bucket               `toml:"private_buckets,omitempty"`
	PendingShare   *PendingShare          `toml:"pending_share,omitempty"`
	PendingLocal   *pendingLocal          `toml:"pending_local,omitempty"`
	Instructions   *InstructionActivation `toml:"instructions,omitempty"`
	Loaders        []FileReceipt          `toml:"loaders,omitempty"`
	Artifacts      []artifactReceipt      `toml:"artifacts,omitempty"`
	NativeVersions []artifactAttestation  `toml:"artifact_versions,omitempty"`
	Links          []LinkReceipt          `toml:"links,omitempty"`
	Restorations   []RestorationReceipt   `toml:"restorations,omitempty"`
	Sync           *SyncState             `toml:"sync,omitempty"`
}

// PendingShare records a selected publication not yet completed locally.
type PendingShare struct {
	Paths      []string      `toml:"paths"`
	Base       string        `toml:"base,omitempty"`
	Commit     string        `toml:"commit,omitempty"`
	Published  bool          `toml:"published,omitempty"`
	Selections []pendingPath `toml:"selections,omitempty"`
	Buckets    []Bucket      `toml:"buckets,omitempty"`
}

type pendingPath struct {
	ShareSelection `toml:"review"`
	Digest         string `toml:"digest"`
	Backup         string `toml:"backup"`
	Parking        string `toml:"parking,omitempty"`
}

// FileReceipt identifies an owned loader by content digest.
type FileReceipt struct {
	Path   string `toml:"path"`
	Digest string `toml:"digest"`
}

// LinkReceipt records a realm-local path and its clone-relative source.
type LinkReceipt struct {
	Path    string `toml:"path"`
	Source  string `toml:"source"`
	Witness string `toml:"witness,omitempty"`
}

// RestorationReceipt identifies a restored original and its recoverable backup.
type RestorationReceipt struct {
	Path     string `toml:"path"`
	Digest   string `toml:"digest"`
	Backup   string `toml:"backup"`
	Restored bool   `toml:"restored,omitempty"`
	Identity string `toml:"identity,omitempty"`
}

// SyncState caches the last successful synchronization and known divergence.
type SyncState struct {
	LastSuccess time.Time `toml:"last_success,omitzero"`
	Ahead       int       `toml:"ahead"`
	Behind      int       `toml:"behind"`
	RemoteHead  string    `toml:"remote_head,omitempty"`
}

type bindingLocation struct {
	realm, physicalRealm string
	home, physicalHome   string
	state, physicalState string
}

type ownership struct {
	Realm   string `toml:"realm"`
	Clone   string `toml:"clone"`
	CloneID string `toml:"clone_id"`
	RealmID string `toml:"realm_id"`
	Remote  string `toml:"remote"`
	Branch  string `toml:"branch"`
}

type ownerRegistry struct {
	Owners []ownership `toml:"owners"`
}

// PrepareManagedClone populates a new checkout and records its local ownership.
// A checkout already at the requested path is never adopted.
func PrepareManagedClone(clonePath string, populate func(string) error) error {
	return prepareManagedClone(clonePath, populate, nil)
}

func prepareManagedClone(clonePath string, populate, acceptStaged func(string) error) error {
	home := config.ProjectHome()
	if home == "" {
		return fmt.Errorf("cannot resolve the user state root: home directory is unset")
	}
	physicalHome, err := physicalDirectory(home)
	if err != nil {
		return err
	}
	name, err := managedCloneName(home, physicalHome, clonePath)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(physicalHome)
	if err != nil {
		return err
	}
	defer root.Close()

	stateDir, _ := relativeTo(physicalHome, config.Dir(physicalHome))
	realms := stateDir + "/realms"
	proofs := stateDir + "/" + cloneProofDir
	for _, dir := range []string{realms, proofs} {
		if err := checkDirectory(root, dir); err != nil {
			return err
		}
	}
	witness := proofs + "/" + name
	rel := realms + "/" + name
	if _, err := lstatBelow(root, rel); !os.IsNotExist(err) {
		return fmt.Errorf("cannot prepare managed clone at %q: path already exists or is inaccessible (%v)", clonePath, err)
	}
	for _, dir := range []string{realms, proofs} {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := syncBindingAncestors(config.Dir(physicalHome), physicalHome); err != nil {
		return err
	}
	if proof, err := lstatBelow(root, witness); err == nil {
		staged, err := findStagedClone(root, proofs, proof)
		if err != nil {
			return fmt.Errorf("cannot recover managed clone at %q: %w", clonePath, err)
		}
		if acceptStaged != nil {
			if err := acceptStaged(filepath.Join(physicalHome, filepath.FromSlash(staged))); err != nil {
				return err
			}
		}
		return installStagedClone(root, staged, rel)
	} else if !os.IsNotExist(err) {
		return err
	}
	// A population failure must not strand the requested clone name or erase
	// the callback's work. Only a fully prepared checkout takes that name.
	staging, err := os.MkdirTemp(filepath.Join(physicalHome, filepath.FromSlash(proofs)), cloneStagingPrefix)
	if err != nil {
		return err
	}
	staged, _ := relativeTo(physicalHome, staging)
	if err := populate(staging); err != nil {
		return fmt.Errorf("cannot populate new checkout at %q (incomplete work at %q): %w", clonePath, staging, err)
	}
	if err := writeCloneProof(root, staged); err != nil {
		return err
	}
	proofParent, err := openShareDirectory(root, proofs)
	if err != nil {
		return err
	}
	if err := errors.Join(proofParent.Sync(), proofParent.Close()); err != nil {
		return err
	}
	if err := root.Link(staged+"/"+cloneProof, witness); err != nil {
		return err
	}
	return installStagedClone(root, staged, rel)
}

func writeCloneProof(root *os.Root, staged string) error {
	clone, err := root.OpenRoot(staged)
	if err != nil {
		return err
	}
	defer clone.Close()
	if err := checkGitDirectory(clone); err != nil {
		return err
	}
	marker, err := clone.OpenFile(cloneProof, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := marker.WriteString(rand.Text())
	syncErr := marker.Sync()
	if err := errors.Join(writeErr, syncErr, marker.Close()); err != nil {
		return err
	}
	git, err := openShareDirectory(clone, ".git")
	if err != nil {
		return err
	}
	defer git.Close()
	dir, err := openShareDirectory(clone, ".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return syncShareDirectories(git, dir)
}

func findStagedClone(root *os.Root, proofs string, proof os.FileInfo) (string, error) {
	if !proof.Mode().IsRegular() {
		return "", fmt.Errorf("invalid clone witness in %q", proofs)
	}
	entries, err := fs.ReadDir(root.FS(), proofs)
	if err != nil {
		return "", err
	}
	var found string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), cloneStagingPrefix) {
			continue
		}
		staged := proofs + "/" + entry.Name()
		git, err := lstatBelow(root, staged+"/.git")
		if os.IsNotExist(err) || err == nil && !git.IsDir() {
			continue
		}
		if err != nil {
			return "", err
		}
		marker, err := lstatBelow(root, staged+"/"+cloneProof)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !marker.Mode().IsRegular() || !os.SameFile(marker, proof) {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("multiple prepared checkouts match the same clone witness")
		}
		found = staged
	}
	if found == "" {
		return "", fmt.Errorf("no prepared checkout matches the clone witness in %q", proofs)
	}
	clone, err := root.OpenRoot(found)
	if err != nil {
		return "", err
	}
	defer clone.Close()
	if err := checkGitDirectory(clone); err != nil {
		return "", err
	}
	value, err := clone.ReadFile(cloneProof)
	if err != nil {
		return "", err
	}
	return found, config.ValidateSegment("managed clone identity", string(value))
}

func installStagedClone(root *os.Root, staged, rel string) error {
	from, err := openShareDirectory(root, path.Dir(staged))
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := openShareDirectory(root, path.Dir(rel))
	if err != nil {
		return err
	}
	defer to.Close()
	witness, err := lstatBelow(root, path.Join(path.Dir(staged), path.Base(rel)))
	if err != nil {
		return err
	}
	if err := checkCheckoutWitness(root, staged, witness); err != nil {
		return err
	}
	if err := syncShareDirectories(from); err != nil {
		return err
	}
	if err := renameShareNoReplace(from, path.Base(staged), to, path.Base(rel)); err != nil {
		return fmt.Errorf("cannot install managed clone at %q (prepared work at %q): %w", filepath.Join(root.Name(), rel), filepath.Join(root.Name(), staged), err)
	}
	return errors.Join(syncShareDirectories(to, from), checkCheckoutWitness(root, rel, witness))
}

func checkCheckoutWitness(root *os.Root, name string, witness os.FileInfo) error {
	marker, err := lstatBelow(root, name+"/"+cloneProof)
	if err != nil {
		return err
	}
	if !marker.Mode().IsRegular() || !os.SameFile(marker, witness) {
		return fmt.Errorf("checkout at %q does not match the prepared clone witness", name)
	}
	return nil
}

// LoadBinding reads a local binding and verifies its reciprocal ownership.
// A missing binding is reported as an error matching os.ErrNotExist.
func LoadBinding(realmRoot string) (Binding, error) {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return Binding{}, err
	}
	return loc.loadBinding()
}

func (loc bindingLocation) loadBinding() (Binding, error) {
	binding, _, err := loc.loadBindingClaim(false)
	return binding, err
}

func (loc bindingLocation) loadBindingClaim(observe bool) (Binding, ownership, error) {
	var binding Binding
	if err := readBinding(loc.physicalState, &binding); err != nil {
		return Binding{}, ownership{}, err
	}
	claim, err := loc.validateBinding(binding, observe)
	if err != nil {
		return Binding{}, ownership{}, err
	}
	registry, err := loc.readOwners()
	if err != nil {
		return Binding{}, ownership{}, err
	}
	owned, err := registry.lookup(claim)
	if err != nil {
		return Binding{}, ownership{}, err
	}
	if !owned {
		return Binding{}, ownership{}, fmt.Errorf("realm %q has a binding without a matching clone owner", loc.realm)
	}
	return binding, claim, nil
}

// SaveBinding records a binding without reassigning an already owned realm or clone.
func SaveBinding(realmRoot string, binding Binding) error {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return err
	}
	lock, err := loc.lockShare()
	if err != nil {
		return err
	}
	defer lock.Release()
	return loc.saveBinding(binding)
}

func (loc bindingLocation) lockShare() (*installstate.Lock, error) {
	home, err := os.OpenRoot(loc.physicalHome)
	if err != nil {
		return nil, err
	}
	defer home.Close()
	lockName, _ := relativeTo(loc.physicalHome, config.OperationLockPath(loc.physicalHome))
	if info, err := lstatBelow(home, lockName); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("operation lock %q must be a regular file", lockName)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return installstate.AcquireLock(loc.physicalHome, installstate.WriterIdentity{OperationID: "realm-share"})
}

func (loc bindingLocation) saveBinding(binding Binding) error {
	claim, err := loc.validate(binding)
	if err != nil {
		return err
	}

	registry, err := loc.readOwners()
	if err != nil {
		return err
	}
	owned, err := registry.lookup(claim)
	if err != nil {
		return err
	}
	var previous Binding
	bindingErr := readBinding(loc.physicalState, &previous)
	switch {
	case bindingErr == nil:
		if !owned || previous.RealmID != binding.RealmID || previous.ClonePath != binding.ClonePath || previous.Remote != binding.Remote || previous.Branch != binding.Branch {
			return fmt.Errorf("state at %q already records a different or unowned binding", loc.state)
		}
	case !os.IsNotExist(bindingErr):
		return bindingErr
	}
	if err := os.MkdirAll(loc.physicalState, 0o700); err != nil {
		return err
	}
	if os.IsNotExist(bindingErr) {
		state := loc.physicalState
		if state != loc.physicalRealm && state != loc.physicalHome {
			stop := ""
			if _, ok := relativeTo(loc.physicalRealm, state); ok {
				stop = loc.physicalRealm
			} else if _, ok := relativeTo(loc.physicalHome, state); ok {
				stop = loc.physicalHome
			}
			if err := syncBindingAncestors(filepath.Dir(state), stop); err != nil {
				return err
			}
		}
	}
	if !owned {
		if err := syncBindingAncestors(loc.physicalHome, loc.physicalHome); err != nil {
			return err
		}
		// A resumed checkout can be visible before its promotion rename is durable.
		root, err := os.OpenRoot(loc.physicalHome)
		if err != nil {
			return err
		}
		defer root.Close()
		stateDir, _ := relativeTo(loc.physicalHome, config.Dir(loc.physicalHome))
		for _, name := range []string{"realms", cloneProofDir} {
			dir, err := openShareDirectory(root, path.Join(stateDir, name))
			if err != nil {
				return err
			}
			if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
				return fmt.Errorf("cannot persist share directory %q: %w", dir.Name(), err)
			}
		}
		// Preserve ownership even if the local write fails; only the same pair may retry.
		registry.Owners = append(registry.Owners, claim)
		if err := writeBindingTOML(config.Dir(loc.physicalHome), ownersName, registry); err != nil {
			return err
		}
	}
	return writeBindingTOML(loc.physicalState, bindingName, binding)
}

func locateBinding(realmRoot string) (bindingLocation, error) {
	loc, err := locateBindingState(realmRoot)
	if err != nil {
		return loc, err
	}
	return loc, loc.checkShareLocation()
}

func locateBindingState(realmRoot string) (bindingLocation, error) {
	var loc bindingLocation
	var err error
	if loc.realm, err = filepath.Abs(realmRoot); err != nil {
		return loc, err
	}
	if loc.physicalRealm, err = physicalDirectory(loc.realm); err != nil {
		return loc, err
	}
	loc.home = config.ProjectHome()
	if loc.home == "" {
		return loc, fmt.Errorf("cannot resolve the user state root: home directory is unset")
	}
	if loc.physicalHome, err = physicalDirectory(loc.home); err != nil {
		return loc, err
	}
	selection, err := config.ResolveStateLocation(loc.realm)
	if err != nil {
		return loc, err
	}
	loc.state = selection.Root
	rel, ok := relativeTo(loc.realm, loc.state)
	if !ok {
		rel, ok = relativeTo(loc.physicalRealm, loc.state)
	}
	if ok {
		realm, err := os.OpenRoot(loc.physicalRealm)
		if err != nil {
			return loc, err
		}
		defer realm.Close()
		if err := checkDirectory(realm, rel); err != nil {
			return loc, err
		}
		loc.physicalState = filepath.Join(loc.physicalRealm, filepath.FromSlash(rel))
	} else {
		loc.physicalState, err = physicalStateDirectory(loc.state)
		if err != nil {
			return loc, err
		}
	}
	return loc, nil
}

func (loc bindingLocation) checkShareLocation() error {
	realms := filepath.Join(config.Dir(loc.physicalHome), "realms")
	if overlapsAbsolute(realms, loc.physicalState) || overlapsAbsolute(realms, loc.physicalRealm) {
		return fmt.Errorf("local realm and state must be separate from managed clones at %q", realms)
	}
	return nil
}

func (loc bindingLocation) hasBinding() (bool, error) {
	if _, err := os.Lstat(filepath.Join(loc.physicalState, bindingName)); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	registry, err := loc.readOwners()
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, owner := range registry.Owners {
		if strings.EqualFold(owner.Realm, loc.physicalRealm) {
			return false, fmt.Errorf("realm %q has a clone owner but no local binding", loc.realm)
		}
	}
	return false, nil
}

func physicalStateDirectory(name string) (string, error) {
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		parent, err := physicalStateDirectory(filepath.Dir(name))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(name)), nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("state path %q must be a real directory", name)
	}
	return physicalDirectory(name)
}

func overlapsAbsolute(a, b string) bool {
	_, aContainsB := relativeTo(strings.ToLower(a), strings.ToLower(b))
	_, bContainsA := relativeTo(strings.ToLower(b), strings.ToLower(a))
	return aContainsB || bContainsA
}

func (loc bindingLocation) validate(binding Binding) (ownership, error) {
	return loc.validateBinding(binding, false)
}

func (loc bindingLocation) validateBinding(binding Binding, observe bool) (ownership, error) {
	if err := config.ValidateSegment("shared realm identity", binding.RealmID); err != nil {
		return ownership{}, err
	}
	for _, value := range []string{binding.Remote, binding.Branch} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return ownership{}, fmt.Errorf("binding requires a non-empty remote and branch without control characters")
		}
	}
	name, err := managedCloneName(loc.home, loc.physicalHome, binding.ClonePath)
	if err != nil {
		return ownership{}, err
	}
	if err := validateArtifactCloneScope(loc, binding.ClonePath); err != nil {
		return ownership{}, err
	}
	clonePath := filepath.Join(config.Dir(loc.physicalHome), "realms", name)
	cloneID, err := verifyClone(loc.physicalHome, name, binding, observe)
	if err != nil {
		return ownership{}, err
	}
	if err := loc.checkReceipts(binding, observe); err != nil {
		return ownership{}, err
	}
	return ownership{Realm: loc.physicalRealm, Clone: clonePath, CloneID: cloneID, RealmID: binding.RealmID, Remote: binding.Remote, Branch: binding.Branch}, nil
}

func managedCloneName(home, physicalHome, clonePath string) (string, error) {
	if !filepath.IsAbs(clonePath) || filepath.Clean(clonePath) != clonePath {
		return "", fmt.Errorf("clone path %q must be absolute and clean", clonePath)
	}
	name, ok := relativeTo(filepath.Join(config.Dir(home), "realms"), clonePath)
	if !ok {
		name, ok = relativeTo(filepath.Join(config.Dir(physicalHome), "realms"), clonePath)
	}
	if !ok || !portablePath(name) || strings.ContainsRune(name, '/') {
		return "", fmt.Errorf("clone %q must be directly under the user state root's realms directory", clonePath)
	}
	return name, nil
}

func openWitnessedClone(home, name string, binding Binding) (clone *os.Root, cloneID string, err error) {
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	stateDir, _ := relativeTo(home, config.Dir(home))
	rel := stateDir + "/realms/" + name
	if err := checkDirectory(root, rel); err != nil {
		return nil, "", err
	}
	clone, err = root.OpenRoot(rel)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open managed clone at %q: %v", binding.ClonePath, err)
	}
	opened := clone
	defer func() {
		if err != nil {
			opened.Close()
		}
	}()
	witness, err := lstatBelow(root, stateDir+"/"+cloneProofDir+"/"+name)
	if err != nil || !witness.Mode().IsRegular() {
		return nil, "", fmt.Errorf("clone collision at %q: no valid local ownership record", binding.ClonePath)
	}
	marker, err := lstatBelow(clone, cloneProof)
	if err != nil || !marker.Mode().IsRegular() || !os.SameFile(marker, witness) {
		return nil, "", fmt.Errorf("clone collision at %q: checkout does not match its local ownership record", binding.ClonePath)
	}
	proof, err := readLocalRegular(clone, cloneProof)
	if err != nil {
		return nil, "", err
	}
	if current, err := lstatBelow(clone, cloneProof); err != nil || !os.SameFile(current, witness) {
		return nil, "", fmt.Errorf("clone collision at %q: ownership record changed while reading it", binding.ClonePath)
	}
	cloneID = string(proof)
	if err := config.ValidateSegment("managed clone identity", cloneID); err != nil {
		return nil, "", err
	}
	return clone, cloneID, nil
}

func verifyClone(home, name string, binding Binding, observe bool) (string, error) {
	clone, cloneID, err := openWitnessedClone(home, name, binding)
	if err != nil {
		return "", err
	}
	defer clone.Close()
	if err := checkGitDirectory(clone); err != nil {
		return "", err
	}
	clonePath := filepath.Join(config.Dir(home), "realms", name)
	out, err := localGit(clonePath).run(context.Background(), nil, "config", "--file", ".git/config", "--no-includes", "--null", "--get-all", "remote.origin.url")
	if err != nil || string(out) != binding.Remote+"\x00" {
		return "", fmt.Errorf("clone collision at %q: Git origin does not match the binding", binding.ClonePath)
	}
	if observe {
		return cloneID, nil
	}
	manifest, err := readManifest(clone)
	if os.IsNotExist(err) && binding.PendingShare != nil && binding.PendingShare.Base == "" {
		var initial initialShare
		err = readTOML(clone, initialShareName, &initial)
		manifest.ID = initial.Binding.RealmID
	}
	if err != nil {
		return "", fmt.Errorf("clone collision at %q: %w", binding.ClonePath, err)
	}
	if manifest.ID != binding.RealmID {
		return "", fmt.Errorf("clone collision at %q: realm identity %q does not match %q", binding.ClonePath, manifest.ID, binding.RealmID)
	}
	return cloneID, nil
}

func checkGitDirectory(clone *os.Root) error {
	for _, name := range []string{".git/objects", ".git/refs"} {
		if info, err := lstatBelow(clone, name); err != nil || !info.IsDir() {
			return fmt.Errorf("clone collision at %q: missing Git directory at %q", clone.Name(), name)
		}
	}
	for _, name := range []string{".git/HEAD", ".git/config"} {
		if info, err := lstatBelow(clone, name); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("clone collision at %q: missing regular Git metadata at %q", clone.Name(), name)
		}
	}
	return nil
}

func (loc bindingLocation) validateReceipts(binding Binding) error {
	return loc.checkReceipts(binding, false)
}

func (loc bindingLocation) checkReceipts(binding Binding, observe bool) error {
	if cached := binding.Sync; cached != nil {
		if cached.RemoteHead != "" && !gitObjectID(cached.RemoteHead) || cached.Ahead < 0 || cached.Behind < 0 {
			return fmt.Errorf("invalid cached Git synchronization state")
		}
	}
	var realm *os.Root
	if !observe {
		var err error
		realm, err = os.OpenRoot(loc.physicalRealm)
		if err != nil {
			return err
		}
		defer realm.Close()
	}
	state, _ := relativeTo(loc.physicalRealm, loc.physicalState)
	localPath := func(name string, shared bool) error {
		if !portablePath(name) {
			return fmt.Errorf("receipt path %q must be relative to the realm", name)
		}
		if shared {
			if err := validateDestinationPath(name, state); err != nil {
				return err
			}
		} else {
			for part := range strings.SplitSeq(name, "/") {
				if strings.EqualFold(part, ".git") || strings.EqualFold(part, bindingName) || strings.EqualFold(part, ownersName) {
					return fmt.Errorf("receipt path %q refers to protected metadata", name)
				}
			}
		}
		if realm == nil {
			return nil
		}
		_, err := lstatBelow(realm, name)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, bucket := range binding.PrivateBuckets {
		name := bucket.Path
		if filepath.IsAbs(name) {
			name = loc.bucketPath(bucket)
			if name == "" {
				continue
			}
		}
		if err := validateDestinationPath(name, state); err != nil {
			return err
		}
		if realm == nil {
			continue
		}
		cuttingOver := false
		if pending := binding.PendingShare; pending != nil && pending.Published {
			for _, selected := range pending.Selections {
				if selected.Mapping.Kind == KindDirectory && containsPath(selected.Mapping.Destination, name) {
					cuttingOver = true
					break
				}
			}
		}
		if !cuttingOver {
			if err := checkDirectory(realm, name); err != nil {
				return err
			}
		}
	}
	if pending := binding.PendingShare; pending != nil {
		for _, name := range pending.Paths {
			if err := validateDestinationPath(name, state); err != nil {
				return err
			}
		}
		parkingNames := make(map[string]struct{})
		for _, selected := range pending.Selections {
			if err := selected.Mapping.validate(state); err != nil {
				return err
			}
			if err := localPath(selected.Mapping.Destination, true); err != nil {
				return err
			}
			if path.Dir(selected.Backup) != shareBackupDir || selected.Digest == "" {
				return fmt.Errorf("invalid pending backup receipt for %q", selected.Mapping.Destination)
			}
			if err := loc.validateBackup(selected.Backup, observe); err != nil {
				return err
			}
			if name := selected.Parking; name != "" {
				if !pending.Published {
					return fmt.Errorf("unpublished share has a pending rollback parking receipt for %q", selected.Mapping.Destination)
				}
				if name == shareParkingPrefix || !strings.HasPrefix(name, shareParkingPrefix) || path.Base(name) != name || !portablePath(name) {
					return fmt.Errorf("invalid pending rollback parking basename %q for %q", name, selected.Mapping.Destination)
				}
				key := strings.ToLower(name)
				if _, exists := parkingNames[key]; exists {
					return fmt.Errorf("duplicate pending rollback parking basename %q", name)
				}
				parkingNames[key] = struct{}{}
			}
		}
	}
	for _, receipt := range binding.Loaders {
		if err := localPath(receipt.Path, false); err != nil {
			return err
		}
	}
	for i, link := range binding.Links {
		for _, previous := range binding.Links[:i] {
			if pathsOverlap(previous.Path, link.Path) || link.Witness != "" && previous.Witness == link.Witness {
				return fmt.Errorf("ambiguous symlink receipts for %q", link.Path)
			}
		}
		if err := localPath(link.Path, true); err != nil {
			return err
		}
		if err := validateSharedPath(link.Source); err != nil {
			return err
		}
		if link.Witness != "" && !validLocalWitness(link.Witness) {
			return fmt.Errorf("invalid symlink witness receipt for %q", link.Path)
		}
	}
	for i, artifact := range binding.Artifacts {
		if err := validateArtifactReceipt(artifact, state); err != nil {
			return err
		}
		if backing := findArtifactBacking(binding, nil, artifact.Logical, artifact.Link.Source); backing.Path == "" {
			return fmt.Errorf("native artifact receipt for %q has no matching logical link", artifact.Link.Path)
		}
		for _, link := range binding.Links {
			if pathsOverlap(link.Path, artifact.Link.Path) || strings.EqualFold(localWitnessPath(binding, link), artifact.Link.Witness) {
				return fmt.Errorf("ambiguous symlink receipts for %q", artifact.Link.Path)
			}
		}
		for _, previous := range binding.Artifacts[:i] {
			if pathsOverlap(previous.Link.Path, artifact.Link.Path) || strings.EqualFold(previous.Link.Witness, artifact.Link.Witness) {
				return fmt.Errorf("ambiguous native artifact receipts for %q", artifact.Link.Path)
			}
		}
	}
	for i, receipt := range binding.NativeVersions {
		if err := validateArtifactAttestation(binding, receipt); err != nil {
			return err
		}
		if slices.ContainsFunc(binding.NativeVersions[:i], func(previous artifactAttestation) bool {
			return previous.Logical == receipt.Logical && previous.Harness == receipt.Harness
		}) {
			return fmt.Errorf("ambiguous native artifact version evidence for %q", receipt.Logical)
		}
	}
	if err := validateInstructionActivation(binding); err != nil {
		return err
	}
	for i, receipt := range binding.Restorations {
		for _, previous := range binding.Restorations[:i] {
			if strings.EqualFold(previous.Path, receipt.Path) || previous.Backup == receipt.Backup {
				return fmt.Errorf("ambiguous restoration receipts for %q", receipt.Path)
			}
		}
		if err := localPath(receipt.Path, true); err != nil {
			return err
		}
		if err := loc.validateBackup(receipt.Backup, observe); err != nil {
			return err
		}
		if receipt.Restored != (receipt.Identity != "") {
			return fmt.Errorf("invalid restored original identity for %q", receipt.Path)
		}
	}
	return validatePendingLocal(binding, state)
}

func (loc bindingLocation) validateBackup(original string, observe bool) error {
	name := original
	if filepath.IsAbs(name) {
		var ok bool
		if name, ok = relativeTo(loc.state, original); !ok {
			name, _ = relativeTo(loc.physicalState, original)
		}
	}
	if !portablePath(name) || pathsOverlap(bindingName, name) {
		return fmt.Errorf("backup %q must be below the local state root", original)
	}
	if observe {
		return nil
	}
	root, err := os.OpenRoot(loc.physicalState)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := lstatBelow(root, name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("backup %q must be a regular file or real directory", original)
	}
	return nil
}

func readBinding(dir string, binding *Binding) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return readTOML(root, bindingName, binding)
}

func (loc bindingLocation) readOwners() (ownerRegistry, error) {
	var registry ownerRegistry
	root, err := os.OpenRoot(config.Dir(loc.physicalHome))
	if err != nil {
		return registry, err
	}
	defer root.Close()
	if err := readTOML(root, ownersName, &registry); err != nil && !os.IsNotExist(err) {
		return registry, err
	}
	return registry, nil
}

func (registry ownerRegistry) lookup(claim ownership) (bool, error) {
	matched := false
	for _, owner := range registry.Owners {
		sameRealm := strings.EqualFold(claim.Realm, owner.Realm)
		sameClone := claim.CloneID == owner.CloneID || strings.EqualFold(claim.Clone, owner.Clone)
		if !sameRealm && !sameClone {
			continue
		}
		if claim != owner || matched {
			return false, fmt.Errorf("realm %q or clone %q already has a different owner", claim.Realm, claim.Clone)
		}
		matched = true
	}
	return matched, nil
}

func syncBindingAncestors(dir, stop string) error {
	for {
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		if err := errors.Join(f.Sync(), f.Close()); err != nil {
			return fmt.Errorf("cannot persist share directory %q: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if dir == stop || parent == dir {
			return nil
		}
		dir = parent
	}
}

func writeBindingTOML(dir, name string, value any) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	temp := "." + name + "." + rand.Text()
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if err := toml.NewEncoder(f).Encode(value); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := root.Rename(temp, name); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
