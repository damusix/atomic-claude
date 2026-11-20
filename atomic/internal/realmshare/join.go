package realmshare

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

const initialJoinName = ".git/atomic-realm-share-join.toml"

// JoinResult reports the local binding and its available mappings without granting trust.
type JoinResult struct {
	Binding  Binding
	Mappings []JoinMapping
}

// JoinMapping reports whether a shared mapping's local destination is occupied or blocked.
type JoinMapping struct {
	Mapping   Mapping
	Collision bool
}

// initialJoin ties a prepared download to the realm that requested it before binding is durable.
type initialJoin struct {
	Realm      string `toml:"realm"`
	Remote     string `toml:"remote"`
	Branch     string `toml:"branch"`
	RealmID    string `toml:"realm_id"`
	RemoteHead string `toml:"remote_head,omitempty"`
}

// Join downloads a shared realm, validates its definition and records an untrusted local binding.
// Repeating a join against the recorded remote only inspects the existing checkout.
func Join(ctx context.Context, realmRoot, remote string) (JoinResult, error) {
	if err := validateShareRemote(remote); err != nil {
		return JoinResult{}, err
	}
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return JoinResult{}, err
	}
	lock, err := loc.lockShare()
	if err != nil {
		return JoinResult{}, err
	}
	defer lock.Release()
	if err := ctx.Err(); err != nil {
		return JoinResult{}, err
	}
	realm, err := os.OpenRoot(loc.physicalRealm)
	if err != nil {
		return JoinResult{}, err
	}
	defer realm.Close()
	state, _ := relativeTo(loc.physicalRealm, loc.physicalState)

	binding, err := LoadBinding(loc.realm)
	switch {
	case err == nil:
		if binding.Remote != remote {
			return JoinResult{}, fmt.Errorf("realm %q is already bound to a different remote", loc.realm)
		}
		clone, err := os.OpenRoot(binding.ClonePath)
		if err != nil {
			return JoinResult{}, err
		}
		defer clone.Close()
		return previewJoinedCheckout(clone, realm, state, binding)
	case !os.IsNotExist(err):
		return JoinResult{}, err
	}

	name := filepath.Base(loc.physicalRealm)
	if !portablePath(name) || strings.ContainsRune(name, '/') {
		return JoinResult{}, fmt.Errorf("realm directory name %q cannot name a managed clone", name)
	}
	binding = Binding{ClonePath: filepath.Join(config.Dir(loc.physicalHome), "realms", name), Remote: remote}
	if err := validateArtifactCloneScope(loc, binding.ClonePath); err != nil {
		return JoinResult{}, err
	}
	var result JoinResult
	populate := func(dir string) error {
		if err := validateArtifactCloneStage(loc, binding.ClonePath, dir); err != nil {
			return err
		}
		branch, remoteHead, err := downloadJoin(ctx, loc.physicalHome, dir, remote)
		if err != nil {
			return err
		}
		git := localGit(dir)
		manifest, err := newSyncHistory(git, "", state).commitManifest(ctx, remoteHead)
		if err != nil {
			return fmt.Errorf("refusing invalid received snapshot %s: %w", remoteHead, err)
		}
		if err := probeShareSymlinks(dir); err != nil {
			return fmt.Errorf("cannot join: %w", err)
		}
		if err := checkoutJoin(ctx, git, branch, remoteHead); err != nil {
			return err
		}
		clone, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer clone.Close()
		binding.Branch, binding.RealmID = branch, manifest.ID
		tracking, err := readSyncCommitRef(ctx, git, joinRemoteHeads+branch)
		if err != nil {
			return err
		}
		head, err := readSyncHead(ctx, clone, git, shareBranchRef(branch))
		if err != nil {
			return err
		}
		if head != remoteHead || tracking != remoteHead {
			return errJoinSnapshotChanged
		}
		binding.Sync = &SyncState{RemoteHead: remoteHead}
		result, err = previewJoinedCheckout(clone, realm, state, binding)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateArtifactCloneStage(loc, binding.ClonePath, dir); err != nil {
			return err
		}
		if err := writeBindingTOML(filepath.Join(dir, ".git"), path.Base(initialJoinName), initialJoin{
			Realm: loc.physicalRealm, Remote: remote, Branch: branch, RealmID: result.Binding.RealmID,
			RemoteHead: remoteHead,
		}); err != nil {
			return err
		}
		return validateArtifactCloneStage(loc, binding.ClonePath, dir)
	}
	acceptPrepared := func(dir string) error {
		if err := validateArtifactCloneStage(loc, binding.ClonePath, dir); err != nil {
			return err
		}
		clone, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer clone.Close()
		var saved initialJoin
		if err := readTOML(clone, initialJoinName, &saved); err != nil {
			return err
		}
		if saved.Realm != loc.physicalRealm || saved.Remote != remote || saved.Branch == "" || saved.RealmID == "" {
			return fmt.Errorf("prepared checkout belongs to a different join")
		}
		binding.Branch, binding.RealmID = saved.Branch, saved.RealmID
		git := localGit(dir)
		head, err := readSyncHead(ctx, clone, git, shareBranchRef(binding.Branch))
		if err != nil {
			return err
		}
		history := newSyncHistory(git, binding.RealmID, state)
		if _, err := history.commitManifest(ctx, head); err != nil {
			return fmt.Errorf("refusing invalid prepared snapshot %s: %w", head, err)
		}
		if gitObjectID(saved.RemoteHead) {
			if saved.RemoteHead != head {
				if history.publishedCommit(ctx, head, saved.RemoteHead) == "" {
					return fmt.Errorf("prepared remote snapshot %s is not a commit", saved.RemoteHead)
				}
				if _, err := history.commitManifest(ctx, saved.RemoteHead); err != nil {
					return fmt.Errorf("refusing invalid received snapshot %s: %w", saved.RemoteHead, err)
				}
			}
			binding.Sync = &SyncState{RemoteHead: saved.RemoteHead}
		}
		result, err = previewJoinedCheckout(clone, realm, state, binding)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return validateArtifactCloneStage(loc, binding.ClonePath, dir)
	}

	home, err := os.OpenRoot(loc.physicalHome)
	if err != nil {
		return JoinResult{}, err
	}
	defer home.Close()
	cloneRel, _ := relativeTo(loc.physicalHome, binding.ClonePath)
	info, err := lstatBelow(home, cloneRel)
	switch {
	case os.IsNotExist(err):
		if err := prepareManagedClone(binding.ClonePath, populate, acceptPrepared); err != nil {
			return JoinResult{}, err
		}
	case err != nil:
		return JoinResult{}, err
	case !info.IsDir():
		return JoinResult{}, fmt.Errorf("managed clone collision at %q; existing path was not adopted", binding.ClonePath)
	default:
		proofRel, _ := relativeTo(loc.physicalHome, filepath.Join(config.Dir(loc.physicalHome), cloneProofDir, name))
		witness, err := lstatBelow(home, proofRel)
		if err != nil || !witness.Mode().IsRegular() || checkCheckoutWitness(home, cloneRel, witness) != nil {
			return JoinResult{}, fmt.Errorf("managed clone collision at %q; existing checkout does not match a local witness", binding.ClonePath)
		}
		if err := acceptPrepared(binding.ClonePath); err != nil {
			return JoinResult{}, fmt.Errorf("cannot resume join at %q: %w", binding.ClonePath, err)
		}
	}
	index, err := localWikiIndex(realm)
	if err != nil {
		return JoinResult{}, err
	}
	result.Binding.PrivateBuckets, err = readPrivateWikiBuckets(index)
	if err != nil {
		return JoinResult{}, err
	}
	if err := validateArtifactCloneScope(loc, binding.ClonePath); err != nil {
		return JoinResult{}, err
	}
	if err := loc.saveBinding(result.Binding); err != nil {
		return JoinResult{}, err
	}
	return result, nil
}

func previewJoinedCheckout(clone, realm *os.Root, state string, binding Binding) (JoinResult, error) {
	file, err := openRegular(clone, ".git/HEAD")
	if err != nil {
		return JoinResult{}, err
	}
	defer file.Close()
	head, err := io.ReadAll(file)
	if err != nil {
		return JoinResult{}, err
	}
	if string(head) != "ref: "+shareBranchRef(binding.Branch)+"\n" {
		return JoinResult{}, fmt.Errorf("managed checkout at %q must remain on branch %q to join", clone.Name(), binding.Branch)
	}
	manifest, err := readManifest(clone)
	if err != nil {
		return JoinResult{}, err
	}
	if binding.RealmID != "" && manifest.ID != binding.RealmID {
		return JoinResult{}, fmt.Errorf("shared definition at %q has realm identity %q, expected %q", clone.Name(), manifest.ID, binding.RealmID)
	}
	if err := manifest.validate(clone, nil, state); err != nil {
		return JoinResult{}, fmt.Errorf("%s: %w", filepath.Join(clone.Name(), manifestName), err)
	}
	binding.RealmID = manifest.ID
	result := JoinResult{Binding: binding, Mappings: make([]JoinMapping, 0, len(manifest.Mappings))}
	for _, mapping := range manifest.Mappings {
		collision, err := joinCollision(realm, mapping.Destination)
		if err != nil {
			return JoinResult{}, err
		}
		result.Mappings = append(result.Mappings, JoinMapping{Mapping: mapping, Collision: collision})
	}
	return result, nil
}

func joinCollision(realm *os.Root, name string) (bool, error) {
	for offset := 0; ; {
		end := len(name)
		if slash := strings.IndexByte(name[offset:], '/'); slash >= 0 {
			end = offset + slash
		}
		info, err := realm.Lstat(name[:end])
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if end == len(name) || !info.IsDir() {
			return true, nil
		}
		offset = end + 1
	}
}
