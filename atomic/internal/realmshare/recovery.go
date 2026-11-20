package realmshare

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

func (st *sharePlanState) previewPending(ctx context.Context, realm *os.Root) error {
	pending := st.binding.PendingShare
	if len(pending.Selections) == 0 || pending.Commit != "" && !gitObjectID(pending.Commit) || pending.Base != "" && !gitObjectID(pending.Base) {
		return fmt.Errorf("pending publication receipt is incomplete; inspect the retained checkout at %q", st.binding.ClonePath)
	}
	for _, selected := range pending.Selections {
		if !slices.Equal(selected.Buckets, st.selectedBuckets(selected.Mapping)) {
			return fmt.Errorf("bucket registration for %q changed; restore the reviewed registration before retrying", selected.Mapping.Destination)
		}
	}
	if err := st.checkDestination(ctx); err != nil {
		return err
	}
	if err := st.verifyOriginals(ctx, realm); err != nil {
		if st.published || pending.Published {
			return publishedShareError(st.binding, err)
		}
		return err
	}
	if pending.Commit == "" {
		if pending.Base != "" {
			manifest, err := LoadManifest(st.binding.ClonePath, st.loc.realm)
			if err != nil {
				return err
			}
			st.manifest = manifest
		} else {
			clone, err := os.OpenRoot(st.binding.ClonePath)
			if err != nil {
				return err
			}
			var initial initialShare
			err = readTOML(clone, initialShareName, &initial)
			clone.Close()
			if err != nil {
				return err
			}
			st.manifest = initial.Manifest
		}
		state, _ := relativeTo(st.loc.physicalRealm, st.loc.physicalState)
		if err := st.selectPaths(ctx, realm, state); err != nil {
			return err
		}
		if len(st.paths) != len(pending.Selections) {
			return fmt.Errorf("pending share selection changed; restore the reviewed originals to resume")
		}
		for i := range st.paths {
			st.paths[i].Backup = pending.Selections[i].Backup
		}
		if !reflect.DeepEqual(st.paths, pending.Selections) {
			return fmt.Errorf("pending share selection changed; restore the reviewed originals and registrations to resume")
		}
	} else {
		st.paths = pending.Selections
	}
	return nil
}

func (st *sharePlanState) verifyOriginals(ctx context.Context, realm *os.Root) error {
	state, err := os.OpenRoot(st.loc.physicalState)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if state != nil {
		defer state.Close()
	}
	b := st.binding
	st.reviewStages = slices.Grow(st.reviewStages[:0], len(b.PendingShare.Selections))
	for _, selected := range b.PendingShare.Selections {
		stage, err := shareOriginalState(ctx, realm, state, b, selected)
		if err != nil {
			return err
		}
		st.reviewStages = append(st.reviewStages, stage)
	}
	return nil
}

func (st *sharePlanState) apply(ctx context.Context) error {
	b := st.binding
	if err := validateArtifactCloneScope(st.loc, b.ClonePath); err != nil {
		return err
	}
	if !st.published && (b.PendingShare == nil || !b.PendingShare.Published) {
		if err := st.checkBackupFilesystem(); err != nil {
			return fmt.Errorf("backup filesystem at %q cannot safely retain and restore the selected paths before publication: %w", st.loc.physicalState, err)
		}
	}
	if b.PendingShare == nil {
		pending := &PendingShare{Paths: st.opts.Paths, Base: st.base, Selections: st.paths}
		for i := range pending.Selections {
			selected := &pending.Selections[i]
			selected.Backup = shareBackupDir + "/" + rand.Text()
			pending.Buckets = append(pending.Buckets, selected.Buckets...)
		}
		b.PendingShare = pending
		if st.initial {
			var err error
			b, err = st.prepareInitialShare(ctx, b)
			if err != nil {
				return err
			}
			st.prepared = true
		}
	}
	st.binding = b
	if err := st.loc.saveBinding(b); err != nil {
		return err
	}
	pending := b.PendingShare
	git := localGit(b.ClonePath)
	if pending.Commit == "" {
		commit, err := st.prepareCommit(ctx, git)
		if err != nil {
			return fmt.Errorf("cannot prepare the recorded share in %q; originals retained: %w", b.ClonePath, err)
		}
		pending.Commit = commit
		if err := st.loc.saveBinding(b); err != nil {
			return err
		}
	}
	if st.head == pending.Base {
		if !st.indexReady {
			base := pending.Base
			if base == "" {
				out, err := git.run(ctx, nil, "mktree")
				if err != nil {
					return err
				}
				base = strings.TrimSpace(string(out))
			}
			if err := validateArtifactCloneScope(st.loc, b.ClonePath); err != nil {
				return err
			}
			if _, err := git.run(ctx, nil, "read-tree", "-m", "-u", base, pending.Commit); err != nil {
				return fmt.Errorf("cannot install the prepared snapshot in %q; originals retained: %w", b.ClonePath, err)
			}
		}
		if _, err := git.run(ctx, nil, "update-ref", shareBranchRef(b.Branch), pending.Commit, pending.Base); err != nil {
			return err
		}
	}
	manifest, err := LoadManifest(b.ClonePath, st.loc.realm)
	if err != nil {
		return err
	}
	if err := verifyPendingMappings(manifest, pending); err != nil {
		return err
	}
	var publishedHead string
	if st.published {
		publishedHead = st.atRemote
	}
	if !st.published && !pending.Published {
		if err := shareClean(ctx, git, pending.Commit); err != nil {
			return err
		}
		if err := st.verifyShareArtifactCutover(); err != nil {
			return err
		}
		if err := st.checkDestination(ctx); err != nil {
			return err
		}
		realm, err := os.OpenRoot(st.loc.physicalRealm)
		if err != nil {
			return err
		}
		err = st.verifyOriginals(ctx, realm)
		realm.Close()
		if err != nil {
			return err
		}
		if !st.published {
			if err := validateArtifactCloneScope(st.loc, b.ClonePath); err != nil {
				return err
			}
			if err := sharePush(ctx, b); err != nil {
				return fmt.Errorf("share push did not finish; commit %s retained in %q, originals unchanged; repeat the same share to resume: %w", pending.Commit, b.ClonePath, err)
			}
			publishedHead = pending.Commit
		}
	}
	if publishedHead != "" {
		if b.Sync == nil {
			b.Sync = &SyncState{}
		}
		b.Sync.Ahead, b.Sync.Behind, b.Sync.RemoteHead = 0, 0, publishedHead
	}
	pending.Published = true
	if err := st.loc.saveBinding(b); err != nil {
		return publishedShareError(b, err)
	}
	head, err := shareHead(ctx, git, b.Branch)
	if err != nil {
		return publishedShareError(b, err)
	}
	if head != pending.Commit && !(st.published && head == st.head) {
		return publishedShareError(b, fmt.Errorf("managed checkout changed after publication"))
	}
	if err := shareClean(ctx, git, head); err != nil {
		return publishedShareError(b, err)
	}
	if err := st.verifyShareArtifactCutover(); err != nil {
		return publishedShareError(b, err)
	}
	if err := st.cutoverShare(ctx, b); err != nil {
		return publishedShareError(b, err)
	}
	return nil
}

func (st *sharePlanState) prepareInitialShare(ctx context.Context, b Binding) (Binding, error) {
	b.RealmID = rand.Text()
	st.manifest.ID = b.RealmID
	approved := initialShare{Realm: st.loc.physicalRealm, Binding: b, Manifest: st.manifest}
	populate := func(dir string) error {
		if err := validateArtifactCloneStage(st.loc, b.ClonePath, dir); err != nil {
			return err
		}
		git := localGit(dir)
		if _, err := git.run(ctx, nil, "init", "--quiet", "--template=", "--initial-branch="+b.Branch); err != nil {
			return err
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer root.Close()
		if err := checkGitDirectory(root); err != nil {
			return err
		}
		if _, err := git.run(ctx, nil, "config", "--file", ".git/config", "--no-includes", "remote.origin.url", b.Remote); err != nil {
			return err
		}
		if err := writeBindingTOML(filepath.Join(dir, ".git"), path.Base(initialShareName), approved); err != nil {
			return err
		}
		return validateArtifactCloneStage(st.loc, b.ClonePath, dir)
	}
	acceptStaged := func(dir string) error {
		if err := validateArtifactCloneStage(st.loc, b.ClonePath, dir); err != nil {
			return err
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer root.Close()
		var saved initialShare
		if err := readTOML(root, initialShareName, &saved); err != nil {
			return err
		}
		expect := approved
		expect.Binding.RealmID = saved.Binding.RealmID
		expect.Manifest.ID = saved.Binding.RealmID
		pending := saved.Binding.PendingShare
		wanted := *expect.Binding.PendingShare
		if pending == nil || len(pending.Selections) != len(wanted.Selections) {
			return fmt.Errorf("prepared checkout at %q does not match the reviewed share", dir)
		}
		wanted.Selections = slices.Clone(wanted.Selections)
		for i := range wanted.Selections {
			wanted.Selections[i].Backup = pending.Selections[i].Backup
		}
		expect.Binding.PendingShare = &wanted
		if !reflect.DeepEqual(expect, saved) {
			return fmt.Errorf("prepared checkout at %q does not match the reviewed share", dir)
		}
		if err := st.loc.validateReceipts(saved.Binding); err != nil {
			return err
		}
		b, st.manifest = saved.Binding, saved.Manifest
		return validateArtifactCloneStage(st.loc, b.ClonePath, dir)
	}
	if err := prepareManagedClone(b.ClonePath, populate, acceptStaged); err != nil {
		return Binding{}, err
	}
	return b, nil
}

func (st *sharePlanState) checkBackupFilesystem() error {
	if err := ensureShareState(st.loc.physicalState); err != nil {
		return err
	}
	state, err := os.OpenRoot(st.loc.physicalState)
	if err != nil {
		return err
	}
	defer state.Close()
	backups, err := openShareBackups(state)
	if err != nil {
		return err
	}
	defer backups.Close()
	backupDir, err := openShareDirectory(backups, ".")
	if err != nil {
		return err
	}
	defer backupDir.Close()
	realm, err := os.OpenRoot(st.loc.physicalRealm)
	if err != nil {
		return err
	}
	defer realm.Close()
	checked := make(map[string]bool)
	for _, selected := range st.paths {
		parent := path.Dir(selected.Mapping.Destination)
		if checked[parent] {
			continue
		}
		if err := probeShareBackupMove(realm, parent, backupDir); err != nil {
			return fmt.Errorf("%s: %w", selected.Mapping.Destination, err)
		}
		checked[parent] = true
	}
	probe := ".atomic-share-probe-" + rand.Text()
	defer backups.Remove(probe)
	defer backups.Remove(probe + ".stage")
	_, err = stageShareLink(backups, backupDir, probe, probe+".stage", backups.Name())
	return err
}

func ensureShareState(name string) error {
	info, err := os.Lstat(name)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("state path %q must be a real directory", name)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(name)
	if err := ensureShareState(parent); err != nil {
		return err
	}
	if err := os.Mkdir(name, 0o700); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return syncShareDirectories(dir)
}

func probeShareBackupMove(realm *os.Root, parent string, backupDir *os.File) error {
	originalDir, err := openShareDirectory(realm, parent)
	if err != nil {
		return err
	}
	defer originalDir.Close()
	original, err := os.MkdirTemp(originalDir.Name(), ".atomic-share-probe-")
	if err != nil {
		return err
	}
	base := filepath.Base(original)
	cleanup := original
	defer func() { os.Remove(cleanup) }()
	if err := renameShareNoReplace(originalDir, base, backupDir, base); err != nil {
		return err
	}
	cleanup = filepath.Join(backupDir.Name(), base)
	if err := renameShareNoReplace(backupDir, base, originalDir, base); err != nil {
		return err
	}
	cleanup = original
	return syncShareDirectories(backupDir, originalDir)
}

func publishedShareError(b Binding, cause error) error {
	return fmt.Errorf("snapshot %s is published, but local link cutover did not finish; originals or backups are retained; repeat the same share after resolving the issue: %w", b.PendingShare.Commit, cause)
}

func verifyPendingMappings(manifest Manifest, pending *PendingShare) error {
	for _, selected := range pending.Selections {
		if !slices.Contains(manifest.Mappings, selected.Mapping) {
			return fmt.Errorf("clone definition no longer contains the approved mapping for %q", selected.Mapping.Destination)
		}
	}
	for _, bucket := range pending.Buckets {
		if !slices.Contains(manifest.Buckets, bucket) {
			return fmt.Errorf("clone definition no longer contains approved bucket %q", bucket.Name)
		}
	}
	return nil
}

func (st *sharePlanState) prepareCommit(ctx context.Context, git shareGit) (string, error) {
	stage, err := os.MkdirTemp(filepath.Join(git.dir, ".git"), "atomic-realm-share-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	work := filepath.Join(stage, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		return "", err
	}
	out, err := os.OpenRoot(work)
	if err != nil {
		return "", err
	}
	defer out.Close()
	realm, err := os.OpenRoot(st.loc.physicalRealm)
	if err != nil {
		return "", err
	}
	defer realm.Close()
	manifest := st.manifest
	files := []snapshotEntry{{name: manifestName, mode: 0o644}}
	for i, selected := range st.paths {
		for _, file := range st.entries[i] {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if err := copyShareFile(realm, out, file); err != nil {
				return "", fmt.Errorf("could not stage %q; check available space and access: %w", file.name, err)
			}
			files = append(files, file)
		}
		manifest.Mappings = append(manifest.Mappings, selected.Mapping)
		manifest.Buckets = append(manifest.Buckets, selected.Buckets...)
	}
	if err := writeBindingTOML(work, manifestName, manifest); err != nil {
		return "", err
	}
	git.workTree, git.index = work, filepath.Join(stage, "index")
	if st.base != "" {
		if _, err := git.run(ctx, nil, "read-tree", st.base); err != nil {
			return "", err
		}
	}
	if err := stageShareBlobs(ctx, git, files); err != nil {
		return "", err
	}
	tree, err := git.run(ctx, nil, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(string(tree)), "--no-gpg-sign", "-m", "Publish realm selections"}
	if st.base != "" {
		args = append(args, "-p", st.base)
	}
	git.userConfig = true
	commit, err := git.run(ctx, nil, args...)
	return strings.TrimSpace(string(commit)), err
}

func stageShareBlobs(ctx context.Context, git shareGit, files []snapshotEntry) error {
	var names strings.Builder
	for _, file := range files {
		if file.mode.IsRegular() {
			fmt.Fprintf(&names, "./%s\n", file.name)
		}
	}
	out, err := git.run(ctx, strings.NewReader(names.String()), "hash-object", "-w", "--no-filters", "--stdin-paths")
	if err != nil {
		return err
	}
	oids := strings.TrimSpace(string(out))
	var index strings.Builder
	for _, file := range files {
		mode := "100644"
		var oid string
		if file.mode&fs.ModeSymlink != 0 {
			mode = "120000"
			result, err := git.run(ctx, strings.NewReader(file.target), "hash-object", "-w", "--stdin")
			if err != nil {
				return err
			}
			oid = strings.TrimSpace(string(result))
		} else {
			oid, oids, _ = strings.Cut(oids, "\n")
			if file.mode&0o100 != 0 {
				mode = "100755"
			}
		}
		if !gitObjectID(oid) {
			return fmt.Errorf("Git did not return a blob identity for %q", file.name)
		}
		fmt.Fprintf(&index, "%s %s\t%s\x00", mode, oid, file.name)
	}
	_, err = git.run(ctx, strings.NewReader(index.String()), "update-index", "-z", "--index-info")
	return err
}

func copyShareFile(realm, out *os.Root, file snapshotEntry) error {
	if file.mode&fs.ModeSymlink != 0 {
		return nil
	}
	if err := out.MkdirAll(path.Dir(file.name), 0o755); err != nil {
		return err
	}
	f, err := out.OpenFile(file.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.mode.Perm())
	if err != nil {
		return err
	}
	var writtenErr error
	if file.projected {
		_, writtenErr = f.WriteString(file.content)
	} else {
		source, err := openRegular(realm, file.name)
		if err != nil {
			return errors.Join(err, f.Close())
		}
		h := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(f, h), source)
		writtenErr = errors.Join(copyErr, source.Close())
		if writtenErr == nil && fmt.Sprintf("%x", h.Sum(nil)) != file.digest {
			writtenErr = fmt.Errorf("selected file changed while preparing its snapshot")
		}
	}
	return errors.Join(writtenErr, f.Close())
}

type pendingLocal struct {
	Unlink     bool          `toml:"unlink,omitempty"`
	Cancel     bool          `toml:"cancel,omitempty"`
	Definition string        `toml:"definition,omitempty"`
	Paths      []string      `toml:"paths,omitempty"`
	Behavior   []string      `toml:"behavior,omitempty"`
	Changes    []localChange `toml:"changes"`
}

type localChange struct {
	Token        string      `toml:"token"`
	Link         LinkReceipt `toml:"link"`
	Artifact     string      `toml:"artifact,omitempty"`
	RestoreID    string      `toml:"restore_id,omitempty"`
	RestoreStage string      `toml:"restore_stage,omitempty"`
}

func validLocalBackup(name string) bool {
	return portablePath(name) && path.Dir(name) == shareBackupDir && path.Ext(name) == ""
}

func validLocalWitness(name string) bool {
	base, ok := strings.CutSuffix(name, ".link")
	return ok && validLocalBackup(base)
}

func validatePendingLocal(b Binding, state string) error {
	p := b.PendingLocal
	if p == nil {
		return nil
	}
	if b.PendingShare != nil {
		return fmt.Errorf("finish the pending local realm link or unlink before sharing")
	}
	if len(p.Changes) == 0 ||
		p.Unlink && (p.Cancel || len(p.Paths) != 0 || len(p.Behavior) != 0 || p.Definition != "" || len(p.Changes) != len(b.Links)+len(b.Artifacts)) ||
		!p.Unlink && (len(p.Paths) == 0 || len(p.Paths) != len(p.Behavior) || p.Definition == "") {
		return fmt.Errorf("invalid local realm operation receipt")
	}
	for _, name := range p.Paths {
		if err := validateDestinationPath(name, state); err != nil {
			return err
		}
	}
	for i, change := range p.Changes {
		link := change.Link
		if !validLocalBackup(path.Join(shareBackupDir, change.Token)) || path.Base(change.Token) != change.Token {
			return fmt.Errorf("invalid local realm operation token for %q", link.Path)
		}
		if change.Artifact == "" {
			if err := validateDestinationPath(link.Path, state); err != nil {
				return err
			}
			if err := validateSharedPath(link.Source); err != nil {
				return err
			}
		} else {
			if err := validateArtifactReceipt(artifactReceipt{Logical: change.Artifact, Link: link}, state); err != nil {
				return err
			}
			if change.RestoreID != "" || change.RestoreStage != "" {
				return fmt.Errorf("native artifact %q cannot retain an original", link.Path)
			}
		}
		for _, prev := range p.Changes[:i] {
			if pathsOverlap(prev.Link.Path, link.Path) || strings.EqualFold(prev.Token, change.Token) {
				return fmt.Errorf("ambiguous local realm operation receipt for %q", link.Path)
			}
		}
		if p.Unlink && ((i < len(b.Artifacts)) != (change.Artifact != "")) {
			return fmt.Errorf("native artifact unlink receipts must precede logical unlink receipts")
		}
		if p.Unlink {
			if change.Artifact != "" {
				if !slices.Contains(b.Artifacts, artifactReceipt{Logical: change.Artifact, Link: link}) {
					return fmt.Errorf("unlink receipt for %q has no matching owned native artifact", link.Path)
				}
			} else {
				if !slices.Contains(b.Links, link) {
					return fmt.Errorf("unlink receipt for %q has no matching owned link", link.Path)
				}
				r, retained := restorationFor(b, link.Path)
				if change.RestoreID == "" && change.RestoreStage != "" ||
					change.RestoreID != "" && (!retained || r.Restored || !portablePath(change.RestoreStage) ||
						path.Base(change.RestoreStage) != change.RestoreStage || !strings.HasPrefix(change.RestoreStage, change.Token+".restore-")) {
					return fmt.Errorf("invalid staged restoration receipt for %q", link.Path)
				}
			}
			continue
		}
		selected := link.Path
		if change.Artifact != "" {
			selected = change.Artifact
			if findArtifactBacking(b, p.Changes[:i], selected, link.Source).Path == "" {
				return fmt.Errorf("native artifact receipt for %q has no matching approved logical link", link.Path)
			}
		}
		if !slices.Contains(p.Paths, selected) || link.Witness != path.Join(shareBackupDir, change.Token+".link") {
			return fmt.Errorf("link receipt for %q is not part of the approved selection", link.Path)
		} else if r, retained := restorationFor(b, link.Path); change.RestoreStage != "" || change.RestoreID != r.Identity || retained && !r.Restored {
			return fmt.Errorf("invalid retained original identity for %q", link.Path)
		}
		for _, active := range b.Links {
			if pathsOverlap(active.Path, link.Path) || change.Artifact != "" && strings.EqualFold(localWitnessPath(b, active), link.Witness) {
				return fmt.Errorf("pending link receipt overlaps an installed link for %q", link.Path)
			}
		}
		for _, active := range b.Artifacts {
			if pathsOverlap(active.Link.Path, link.Path) || strings.EqualFold(active.Link.Witness, link.Witness) {
				return fmt.Errorf("pending link receipt overlaps an installed native artifact for %q", link.Path)
			}
		}
	}
	return nil
}

func checkLocalOriginal(ctx context.Context, root *os.Root, name string, r RestorationReceipt) error {
	info, err := lstatBelow(root, name)
	if err != nil {
		return fmt.Errorf("cannot read retained or restored original %q: %w", r.Path, err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("retained or restored original %q is not a regular file or directory", r.Path)
	}
	digest, err := digestSharePath(ctx, root, name, nil)
	if err != nil {
		return err
	}
	if digest != r.Digest {
		return fmt.Errorf("retained or restored original %q changed; preserve it and recover the reviewed original before retrying", r.Path)
	}
	return nil
}

func localRestorationIdentity(root *os.Root, name string) (string, error) {
	info, err := lstatBelow(root, name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", fmt.Errorf("%q must be a regular file or directory", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("%q changed while checking its restoration identity", name)
	}
	return restorationIdentity(file)
}

func checkLocalRestoration(ctx context.Context, root *os.Root, name string, r RestorationReceipt, identity string) error {
	got, err := localRestorationIdentity(root, name)
	if err != nil {
		return err
	}
	if identity == "" || got != identity {
		return fmt.Errorf("destination or retained original for %q is not the recorded entry; preserve the replacement before retrying", r.Path)
	}
	return checkLocalOriginal(ctx, root, name, r)
}

type localLinkStage uint8

const (
	localDestinationReady localLinkStage = iota
	localOriginalRetained
	localLinkInstalled
	localCanceledParked
	localCanceledRetained
	localCanceledRestored
)

func (l *localLinks) linkStage(ctx context.Context, c localChange) (localLinkStage, error) {
	name := c.Link.Path
	local, err := lstatBelow(l.realm, name)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	r, retained := restorationFor(l.binding, name)
	stage := localDestinationReady
	if retained {
		if !r.Restored || !validLocalBackup(r.Backup) {
			return 0, fmt.Errorf("original %q has no completed restoration receipt", name)
		}
		if err := checkLocalOriginal(ctx, l.state, r.Backup, r); err != nil {
			return 0, err
		}
		backup := path.Join(shareBackupDir, c.Token)
		if _, err := lstatBelow(l.state, backup); err == nil {
			if err := checkLocalRestoration(ctx, l.state, backup, r, c.RestoreID); err != nil {
				return 0, err
			}
			stage = localOriginalRetained
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		if stage == localDestinationReady {
			if local == nil {
				return 0, fmt.Errorf("restored original %q is missing", name)
			}
			if err := checkShareMount(l.realm, name); err != nil {
				return 0, err
			}
			return stage, checkLocalRestoration(ctx, l.realm, name, r, c.RestoreID)
		}
	}
	if local == nil {
		return stage, nil
	}
	if _, err := localLinkWitness(l.realm, l.state, l.binding, c.Link); err != nil {
		return 0, fmt.Errorf("cannot install link at occupied destination %q: %w", name, err)
	}
	if _, err := lstatBelow(l.state, path.Join(shareBackupDir, c.Token+".stage")); err == nil {
		return 0, fmt.Errorf("installed link %q still has a staging entry; preserve it before retrying", name)
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	return localLinkInstalled, nil
}

func (l *localLinks) pendingLinkReview(ctx context.Context, c localChange, review *linkSelectionReview) error {
	phase, err := l.linkStage(ctx, c)
	if err != nil {
		return err
	}
	review.Phase = phase
	stage := path.Join(shareBackupDir, c.Token+".stage")
	present, owned, err := inspectShareParking(l.state, stage, c.Link.Witness)
	if err != nil {
		return err
	}
	if present && !owned {
		return fmt.Errorf("staged link for %q has no matching ownership witness; preserve it before retrying", c.Link.Path)
	}
	review.Staged = present
	witness, err := lstatBelow(l.state, c.Link.Witness)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target := filepath.Join(l.binding.ClonePath, filepath.FromSlash(c.Link.Source))
	if err := checkWitnessedLink(l.state, c.Link.Witness, target, witness); err != nil {
		return err
	}
	review.Witness = c.Link.Witness
	return nil
}

type localUnlinkStage uint8

const (
	localLinkLive localUnlinkStage = iota
	localLinkParked
	localOriginalRestored
)

func (l *localLinks) localParking(c localChange) (bool, error) {
	park := path.Join(shareBackupDir, c.Token+".park")
	if _, err := lstatBelow(l.state, park); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	proof, err := lstatBelow(l.state, localWitnessPath(l.binding, c.Link))
	if err == nil && proof.Mode()&fs.ModeSymlink != 0 {
		target := filepath.Join(l.binding.ClonePath, filepath.FromSlash(c.Link.Source))
		if err := checkWitnessedLink(l.state, park, target, proof); err == nil {
			return true, nil
		}
	}
	return false, fmt.Errorf("unowned entry for %q is parked at %q; recover it before retrying unlink", c.Link.Path, filepath.Join(l.state.Name(), filepath.FromSlash(park)))
}

func (l *localLinks) unlinkStage(ctx context.Context, c localChange) (localUnlinkStage, error) {
	name := c.Link.Path
	r, retained := restorationFor(l.binding, name)
	if retained {
		if !validLocalBackup(r.Backup) || r.Restored {
			return 0, fmt.Errorf("invalid retained original receipt for %q", name)
		}
		if err := checkLocalOriginal(ctx, l.state, r.Backup, r); err != nil {
			return 0, err
		}
	}
	present, err := l.localParking(c)
	if err != nil {
		return 0, err
	}
	staged := false
	if c.RestoreStage != "" {
		restore := path.Join(shareBackupDir, c.RestoreStage)
		_, err := lstatBelow(l.state, restore)
		switch {
		case err == nil:
			staged = true
			if err := checkLocalRestoration(ctx, l.state, restore, r, c.RestoreID); err != nil {
				return 0, err
			}
		case !os.IsNotExist(err):
			return 0, err
		}
	}
	if !present {
		if c.RestoreStage != "" && !staged {
			return 0, fmt.Errorf("recorded restoration stage for %q is missing; recover it before retrying unlink", name)
		}
		_, err := localLinkWitness(l.realm, l.state, l.binding, c.Link)
		return localLinkLive, err
	}
	_, err = lstatBelow(l.realm, name)
	if os.IsNotExist(err) {
		if c.RestoreStage != "" && !staged {
			return 0, fmt.Errorf("recorded restored original for %q is missing; recover it before retrying unlink", name)
		}
		return localLinkParked, nil
	}
	if err != nil {
		return 0, err
	}
	if c.RestoreStage != "" && !staged {
		return localOriginalRestored, checkLocalRestoration(ctx, l.realm, name, r, c.RestoreID)
	}
	return 0, fmt.Errorf("destination %q is occupied after the owned link was parked; preserve the replacement before retrying unlink", name)
}

func (l *localLinks) preflightLocalMoves(ctx context.Context, p *pendingLocal, backups *os.File) error {
	checked := make(map[string]bool)
	for _, c := range p.Changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		parent := path.Dir(c.Link.Path)
		for {
			_, err := lstatBelow(l.realm, parent)
			if err == nil {
				break
			}
			if !os.IsNotExist(err) {
				return err
			}
			parent = path.Dir(parent)
		}
		if !checked[parent] {
			if err := probeShareBackupMove(l.realm, parent, backups); err != nil {
				return fmt.Errorf("backup filesystem cannot safely move %q: %w", c.Link.Path, err)
			}
			checked[parent] = true
		}
	}
	return nil
}

func checkLocalDirectory(root *os.Root, name string, dir *os.File) error {
	opened, err := dir.Stat()
	if err != nil {
		return err
	}
	current, err := lstatBelow(root, name)
	if err != nil {
		return err
	}
	if !current.IsDir() || !os.SameFile(opened, current) {
		return fmt.Errorf("directory %q changed during local realm recovery", name)
	}
	return nil
}

// A visible binding rename does not prove its directory entry is durable.
func (l *localLinks) syncBindingDirectory() (err error) {
	dir, err := openShareDirectory(l.state, ".")
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := dir.Sync(); err != nil {
		return err
	}
	opened, err := dir.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(l.loc.physicalState)
	if err != nil {
		return err
	}
	if !current.IsDir() || !os.SameFile(opened, current) {
		return fmt.Errorf("binding directory %q changed during local realm recovery", l.loc.physicalState)
	}
	return nil
}

func syncLocalTransition[T comparable](l *localLinks, name string, parent, dir *os.File, expected T, stage func() (T, error)) error {
	if err := syncShareDirectories(parent, dir); err != nil {
		return err
	}
	if err := checkLocalDirectory(l.realm, path.Dir(name), parent); err != nil {
		return err
	}
	if err := checkLocalDirectory(l.state, shareBackupDir, dir); err != nil {
		return err
	}
	return checkLocalPhase(name, expected, stage)
}

func checkLocalPhase[T comparable](name string, expected T, stage func() (T, error)) error {
	got, err := stage()
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("local realm recovery phase for %q changed; preserve the destination and retry", name)
	}
	return nil
}

func (l *localLinks) applyLocal(ctx context.Context, p *pendingLocal, approvedArtifacts []artifactAttestation) error {
	for _, change := range p.Changes {
		if change.Artifact != "" || artifactKind(change.Link.Path) != "" {
			if err := validateArtifactScope(l.loc, l.realm, change.Link.Path); err != nil {
				return err
			}
		}
		if !p.Unlink && !p.Cancel && change.Artifact != "" && !slices.ContainsFunc(approvedArtifacts, func(receipt artifactAttestation) bool {
			return receipt.Logical == change.Artifact && receipt.Link == change.Link
		}) {
			return fmt.Errorf("native link %q has no reviewed executable evidence; review and link again, or use realm unlink to recover it", change.Link.Path)
		}
		if p.Cancel {
			if _, err := l.cancelStage(ctx, change); err != nil {
				return fmt.Errorf("cannot unwind pending link at %q; preserve any replacement and retry unlink after resolving it: %w", change.Link.Path, err)
			}
		} else if p.Unlink {
			if _, err := l.unlinkStage(ctx, change); err != nil {
				return err
			}
		} else if _, err := l.linkStage(ctx, change); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := probeShareSymlinks(l.loc.physicalRealm); err != nil {
		return err
	}
	backups, err := openShareBackups(l.state)
	if err != nil {
		return err
	}
	defer backups.Close()
	dir, err := openShareDirectory(backups, ".")
	if err != nil {
		return err
	}
	defer dir.Close()
	b := l.binding
	if b.PendingLocal == nil {
		if err := l.preflightLocalMoves(ctx, p, dir); err != nil {
			return err
		}
	}
	if b.PendingLocal != p {
		b.PendingLocal = p
		if err := l.loc.saveBinding(b); err != nil {
			return err
		}
		l.binding.PendingLocal = p
	} else if err := l.syncBindingDirectory(); err != nil {
		return err
	}
	if err := checkLocalDirectory(l.state, shareBackupDir, dir); err != nil {
		return err
	}
	verb := "link"
	if p.Unlink || p.Cancel {
		verb = "unlink"
	}
	failed := func(err error) error {
		return fmt.Errorf("local realm %s did not finish; retained work is at %q; repeat the recorded %s after resolving the issue: %w", verb, backups.Name(), verb, err)
	}
	if p.Unlink {
		if err := l.removeInstructions(); err != nil {
			return failed(err)
		}
		b = l.binding
	}
	if !p.Cancel {
		b.Restorations = slices.Clone(b.Restorations)
	}
	for index := range p.Changes {
		if p.Cancel {
			index = len(p.Changes) - 1 - index
		}
		change := p.Changes[index]
		if err := ctx.Err(); err != nil {
			return failed(err)
		}
		if p.Cancel {
			if err := l.cancelLocalChange(ctx, backups, dir, change); err != nil {
				return failed(err)
			}
			continue
		}
		name := change.Link.Path
		if !p.Unlink && change.Artifact != "" {
			backing := findArtifactBacking(l.binding, p.Changes[:index], change.Artifact, change.Link.Source)
			if _, err := localLinkWitness(l.realm, l.state, l.binding, backing); err != nil {
				return failed(fmt.Errorf("native artifact %q requires its witnessed logical link: %w", name, err))
			}
		}
		parentName := path.Dir(name)
		if !p.Unlink {
			if err := ensureLocalParent(l.realm, parentName); err != nil {
				return failed(err)
			}
		}
		parent, err := openShareDirectory(l.realm, parentName)
		if err != nil {
			return failed(err)
		}
		if p.Unlink {
			err = l.removeLocalLink(ctx, backups, dir, parent, change)
		} else {
			err = l.installLocalLink(ctx, backups, dir, parent, change)
		}
		err = errors.Join(err, parent.Close())
		if err != nil {
			return failed(err)
		}
		for i, r := range b.Restorations {
			if r.Path == name {
				r.Restored = p.Unlink
				if p.Unlink {
					r.Identity = p.Changes[index].RestoreID
				} else {
					r.Identity = ""
					r.Backup = path.Join(shareBackupDir, change.Token)
				}
				b.Restorations[i] = r
				break
			}
		}
		if !p.Unlink {
			if change.Artifact != "" {
				b.Artifacts = append(b.Artifacts, artifactReceipt{Logical: change.Artifact, Link: change.Link})
			} else {
				b.Links = append(b.Links, change.Link)
			}
		}
	}
	if p.Unlink {
		b.Links = nil
		b.Artifacts = nil
		b.NativeVersions = nil
		b.Instructions = nil
	} else if !p.Cancel {
		b.TrustedDigest = p.Definition
		if _, err := l.recordArtifactVersions(&b, approvedArtifacts); err != nil {
			return failed(err)
		}
	}
	b.PendingLocal = nil
	if err := l.loc.saveBinding(b); err != nil {
		return failed(err)
	}
	l.binding = b
	if p.Unlink || p.Cancel {
		for _, c := range p.Changes {
			park := c.Token + ".park"
			proof := path.Base(localWitnessPath(l.binding, c.Link))
			if _, owned, err := inspectShareParking(backups, park, proof); err == nil && owned {
				_ = backups.Remove(park)
			}
		}
	}
	return nil
}

func (l *localLinks) cancelLocalLink(ctx context.Context, p *pendingLocal) error {
	if p.Cancel {
		return l.applyLocal(ctx, p, nil)
	}
	cancel := *p
	cancel.Cancel = true
	return l.applyLocal(ctx, &cancel, nil)
}

func (l *localLinks) cancelStage(ctx context.Context, c localChange) (localLinkStage, error) {
	present, err := l.localParking(c)
	if err != nil {
		return 0, err
	}
	if !present {
		return l.linkStage(ctx, c)
	}
	name := c.Link.Path
	r, retained := restorationFor(l.binding, name)
	backup := path.Join(shareBackupDir, c.Token)
	_, err = lstatBelow(l.realm, name)
	if err == nil {
		if retained {
			if _, err := lstatBelow(l.state, backup); os.IsNotExist(err) {
				return localCanceledRestored, checkLocalRestoration(ctx, l.realm, name, r, c.RestoreID)
			} else if err != nil {
				return 0, err
			}
		}
		return 0, fmt.Errorf("destination %q is occupied after the owned link was parked; preserve the replacement before retrying unlink", name)
	}
	if !os.IsNotExist(err) {
		return 0, err
	}
	if !retained {
		return localCanceledParked, nil
	}
	return localCanceledRetained, checkLocalRestoration(ctx, l.state, backup, r, c.RestoreID)
}

func (l *localLinks) cancelLocalChange(ctx context.Context, backups *os.Root, dir *os.File, c localChange) error {
	stage, err := l.cancelStage(ctx, c)
	if err != nil {
		return err
	}
	name := c.Link.Path
	_, retained := restorationFor(l.binding, name)
	if stage == localDestinationReady && !retained {
		return nil
	}
	parent, err := openShareDirectory(l.realm, path.Dir(name))
	if err != nil {
		return err
	}
	defer parent.Close()
	check := func() (localLinkStage, error) { return l.cancelStage(ctx, c) }
	if stage != localLinkInstalled {
		if err := syncLocalTransition(l, name, parent, dir, stage, check); err != nil {
			return err
		}
	} else {
		if err := l.parkLocalLink(backups, dir, parent, c); err != nil {
			return err
		}
		stage = localCanceledParked
		if retained {
			stage = localCanceledRetained
		}
		if err := checkLocalPhase(name, stage, check); err != nil {
			return err
		}
	}
	if stage != localOriginalRetained && stage != localCanceledRetained {
		return nil
	}
	if err := renameShareNoReplace(dir, c.Token, parent, path.Base(name)); err != nil {
		return fmt.Errorf("cannot restore %q without replacing another entry; preserve the destination and repeat unlink: %w", name, err)
	}
	expected := localDestinationReady
	if stage == localCanceledRetained {
		expected = localCanceledRestored
	}
	return syncLocalTransition(l, name, parent, dir, expected, check)
}

func (l *localLinks) installLocalLink(ctx context.Context, backups *os.Root, dir, parent *os.File, c localChange) error {
	stage, err := l.linkStage(ctx, c)
	if err != nil {
		return err
	}
	name := c.Link.Path
	if stage != localDestinationReady {
		check := func() (localLinkStage, error) { return l.linkStage(ctx, c) }
		if err := syncLocalTransition(l, name, parent, dir, stage, check); err != nil || stage == localLinkInstalled {
			return err
		}
	}
	base := path.Base(name)
	staged := c.Token + ".stage"
	target := filepath.Join(l.binding.ClonePath, filepath.FromSlash(c.Link.Source))
	witness, err := stageShareLink(backups, dir, path.Base(c.Link.Witness), staged, target)
	if err != nil {
		return err
	}
	if r, retained := restorationFor(l.binding, name); retained && stage == localDestinationReady {
		original, err := lstatBelow(l.realm, name)
		if err != nil {
			return err
		}
		if err := checkLocalRestoration(ctx, l.realm, name, r, c.RestoreID); err != nil {
			return err
		}
		if err := renameShareNoReplace(parent, base, dir, c.Token); err != nil {
			return err
		}
		moved, err := backups.Lstat(c.Token)
		if err == nil && !os.SameFile(original, moved) {
			err = fmt.Errorf("destination %q changed before its original could be retained", name)
		}
		if err == nil {
			err = checkLocalRestoration(ctx, backups, c.Token, r, c.RestoreID)
		}
		if err != nil {
			return errors.Join(err, renameShareNoReplace(dir, c.Token, parent, base), syncShareDirectories(parent, dir))
		}
		if err := syncShareDirectories(parent, dir); err != nil {
			return err
		}
	}
	if err := renameShareNoReplace(dir, staged, parent, base); err != nil {
		return err
	}
	if err := syncShareDirectories(parent, dir); err != nil {
		return err
	}
	return checkShareLink(l.realm, name, target, witness)
}

func (l *localLinks) removeLocalLink(ctx context.Context, backups *os.Root, dir, parent *os.File, c localChange) error {
	stage, err := l.unlinkStage(ctx, c)
	if err != nil {
		return err
	}
	name := c.Link.Path
	check := func() (localUnlinkStage, error) { return l.unlinkStage(ctx, c) }
	if stage != localLinkLive || c.RestoreStage != "" {
		if err := syncLocalTransition(l, name, parent, dir, stage, check); err != nil || stage == localOriginalRestored {
			return err
		}
	}
	base := path.Base(name)
	r, retained := restorationFor(l.binding, name)
	if retained && c.RestoreStage == "" {
		c, err = l.prepareUnlinkRestoration(ctx, backups, dir, c, r)
		if err != nil {
			return err
		}
	}
	if stage == localLinkLive {
		if err := l.parkLocalLink(backups, dir, parent, c); err != nil {
			return err
		}
		if err := checkLocalPhase(name, localLinkParked, check); err != nil {
			return err
		}
	}
	if !retained {
		return nil
	}
	if err := renameShareNoReplace(dir, c.RestoreStage, parent, base); err != nil {
		return err
	}
	return syncLocalTransition(l, name, parent, dir, localOriginalRestored, check)
}

func (l *localLinks) parkLocalLink(backups *os.Root, dir, parent *os.File, c localChange) error {
	witness, err := localLinkWitness(l.realm, l.state, l.binding, c.Link)
	if err != nil {
		return err
	}
	base, park := path.Base(c.Link.Path), c.Token+".park"
	if err := renameShareNoReplace(parent, base, dir, park); err != nil {
		return err
	}
	moved, err := backups.Lstat(park)
	if err == nil && !os.SameFile(moved, witness) {
		err = fmt.Errorf("destination %q changed before its owned link could be removed", c.Link.Path)
	}
	if err != nil {
		return errors.Join(err, renameShareNoReplace(dir, park, parent, base), syncShareDirectories(parent, dir))
	}
	return syncShareDirectories(parent, dir)
}

func (l *localLinks) prepareUnlinkRestoration(ctx context.Context, backups *os.Root, dir *os.File, c localChange, r RestorationReceipt) (localChange, error) {
	if c.RestoreStage != "" {
		return c, checkLocalRestoration(ctx, backups, c.RestoreStage, r, c.RestoreID)
	}
	p := l.binding.PendingLocal
	if p == nil || !p.Unlink {
		return c, fmt.Errorf("no pending unlink receipt for %q", c.Link.Path)
	}
	i := slices.IndexFunc(p.Changes, func(change localChange) bool { return change.Token == c.Token })
	if i < 0 {
		return c, fmt.Errorf("no pending unlink receipt for %q", c.Link.Path)
	}
	// A crash before saving the receipt must not let a retry adopt an unrecorded copy.
	c.RestoreStage = c.Token + ".restore-" + rand.Text()
	identity, err := prepareLocalRestoration(ctx, l.state, backups, dir, c.RestoreStage, r)
	if err != nil {
		return c, err
	}
	c.RestoreID = identity
	p.Changes[i] = c
	return c, l.loc.saveBinding(l.binding)
}

func prepareLocalRestoration(ctx context.Context, state, backups *os.Root, dir *os.File, ready string, r RestorationReceipt) (string, error) {
	temp, err := os.MkdirTemp(backups.Name(), ".restore-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	work, err := backups.OpenRoot(filepath.Base(temp))
	if err != nil {
		return "", err
	}
	defer work.Close()
	from, err := openShareDirectory(work, ".")
	if err != nil {
		return "", err
	}
	defer from.Close()
	if err := copyLocalOriginal(ctx, state, work, r.Backup); err != nil {
		return "", err
	}
	const original = "original"
	if err := checkLocalOriginal(ctx, work, original, r); err != nil {
		return "", err
	}
	identity, err := localRestorationIdentity(work, original)
	if err != nil {
		return "", err
	}
	if err := renameShareNoReplace(from, original, dir, ready); err != nil {
		return "", err
	}
	return identity, syncShareDirectories(from, dir)
}

func copyLocalOriginal(ctx context.Context, source, dest *os.Root, name string) error {
	type directoryMode struct {
		name string
		mode fs.FileMode
	}
	var directories []directoryMode
	err := fs.WalkDir(source.FS(), name, func(entry string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		to := "original" + strings.TrimPrefix(entry, name)
		switch mode := info.Mode(); {
		case mode.IsDir():
			directories = append(directories, directoryMode{to, mode})
			return dest.Mkdir(to, 0o700)
		case mode&fs.ModeSymlink != 0:
			target, err := source.Readlink(entry)
			if err != nil {
				return err
			}
			return dest.Symlink(target, to)
		case mode.IsRegular():
			in, err := openRegular(source, entry)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := dest.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, in)
			if err == nil {
				err = out.Chmod(mode)
			}
			if err == nil {
				err = out.Sync()
			}
			return errors.Join(err, out.Close())
		default:
			return fmt.Errorf("cannot restore special filesystem entry %q", entry)
		}
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		d := directories[i]
		dir, err := openShareDirectory(dest, d.name)
		if err != nil {
			return err
		}
		err = dir.Chmod(d.mode)
		if err == nil {
			err = dir.Sync()
		}
		if err := errors.Join(err, dir.Close()); err != nil {
			return err
		}
	}
	return nil
}
