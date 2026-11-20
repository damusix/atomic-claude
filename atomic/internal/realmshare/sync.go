package realmshare

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SyncResult reports the local Git tip and what changed during synchronization.
type SyncResult struct {
	Head      string
	Committed bool
	Received  bool
	Published bool
}

// Sync exchanges declared content through the bound clone without changing realm links.
func Sync(ctx context.Context, realmRoot string) (SyncResult, error) {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return SyncResult{}, err
	}
	lock, err := loc.lockShare()
	if err != nil {
		return SyncResult{}, err
	}
	defer lock.Release()
	b, err := loc.loadBinding()
	if err != nil {
		return SyncResult{}, fmt.Errorf("cannot sync realm %q without a valid bound checkout; finish any active Git merge or rebase in the clone before retrying: %w", loc.realm, err)
	}
	result, err := syncBoundClone(ctx, loc, b)
	if err != nil {
		return result, fmt.Errorf("sync in clone %q: %w", b.ClonePath, err)
	}
	return result, nil
}

func syncBoundClone(ctx context.Context, loc bindingLocation, b Binding) (SyncResult, error) {
	var result SyncResult
	if pending := b.PendingLocal; pending != nil {
		verb := "link"
		if pending.Unlink {
			verb = "unlink"
		}
		return result, fmt.Errorf("pending local %s; repeat the corresponding atomic realm %s operation before syncing", verb, verb)
	}
	pending := b.PendingShare
	if pending != nil && (pending.Commit == "" || len(pending.Selections) == 0) {
		return result, fmt.Errorf("pending share has no prepared publication; repeat the same approved atomic realm share")
	}
	clone, err := os.OpenRoot(b.ClonePath)
	if err != nil {
		return result, err
	}
	defer clone.Close()
	artifacts, err := newSyncArtifactGuard(loc, clone, b)
	if err != nil {
		return result, err
	}
	git := syncLocalGit(b, artifacts != nil)
	branch := shareBranchRef(b.Branch)
	if err := guardSyncOperations(clone); err != nil {
		return result, err
	}
	head, err := readSyncHead(ctx, clone, git, branch)
	if err != nil {
		return result, err
	}
	result.Head = head
	if err := probeShareSymlinks(b.ClonePath); err != nil {
		return result, fmt.Errorf("cannot sync: %w", err)
	}
	state, _ := relativeTo(loc.physicalRealm, loc.physicalState)
	history := newSyncHistory(git, b.RealmID, state)
	var published string
	if b.Sync != nil {
		published = history.publishedCommit(ctx, head, b.Sync.RemoteHead)
	}
	live, err := syncLiveManifest(ctx, clone, git, b.RealmID, state, artifacts)
	if err != nil {
		return result, err
	}
	if err := checkSyncIndex(ctx, git, live); err != nil {
		return result, err
	}
	headTree, err := history.treeOID(ctx, head)
	if err != nil {
		return result, err
	}
	local, err := history.manifest(ctx, headTree)
	if err != nil {
		return result, err
	}
	if pending != nil {
		if err := verifyShareParent(ctx, history.git, pending); err != nil {
			return result, err
		}
		if err := history.validatePending(ctx, head, published, pending); err != nil {
			return result, fmt.Errorf("cannot sync pending share; preserve unrelated clone work separately and repeat the same approved atomic realm share: %w", err)
		}
		if err := verifyPendingMappings(local, pending); err != nil {
			return result, err
		}
		if err := shareClean(ctx, git, head); err != nil {
			return result, fmt.Errorf("cannot sync pending share with unrelated clone work; preserve it separately and repeat the same approved atomic realm share: %w", err)
		}
	} else {
		if err := checkCommittedSyncMappings(live, local); err != nil {
			return result, err
		}
		if err := verifySyncCheckout(ctx, clone, git, branch, head); err != nil {
			return result, err
		}
		if _, err := git.run(ctx, strings.NewReader(syncPathspec(live)), "add", "--all", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return result, fmt.Errorf("cannot stage declared paths: %w", err)
		}
		tree, err := syncIndexTree(ctx, git)
		if err != nil {
			return result, err
		}
		if tree != headTree {
			local, err = history.manifest(ctx, tree, artifacts)
			if err != nil {
				return result, fmt.Errorf("invalid staged snapshot; inspect the clone index: %w", err)
			}
			if !slices.Equal(local.Mappings, live.Mappings) {
				return result, fmt.Errorf("shared selections changed during staging; inspect realm.toml and the clone index before retrying")
			}
			if err := guardSyncOperations(clone); err != nil {
				return result, err
			}
			result.Head, err = commitSyncTree(ctx, clone, git, branch, tree, head, "Synchronize realm content", "")
			if err != nil {
				return result, err
			}
			result.Committed = true
		}
	}

	if err := history.validate(ctx, result.Head, published); err != nil {
		return result, fmt.Errorf("refusing publication; inspect retained clone history before retrying atomic realm sync: %w", err)
	}
	if err := verifySyncCheckout(ctx, clone, git, branch, result.Head); err != nil {
		return result, err
	}
	updated, advanced, err := syncPush(ctx, b, result.Head)
	if err != nil && !advanced {
		return result, fmt.Errorf("push did not finish; commit %s is retained; retry atomic realm sync after resolving: %w", result.Head, err)
	}
	if advanced {
		remote, err := syncFetch(ctx, b, result.Head)
		if err != nil {
			return result, fmt.Errorf("cannot fetch bound branch %q; commit %s is retained: %w", b.Branch, result.Head, err)
		}
		if !gitObjectID(remote) || len(remote) != len(result.Head) {
			return result, fmt.Errorf("fetch returned an invalid commit for branch %q", b.Branch)
		}
		incoming, err := history.commitManifest(ctx, remote, artifacts)
		if err != nil {
			return result, fmt.Errorf("refusing invalid received snapshot %s: %w", remote, err)
		}
		if err := verifySyncCheckout(ctx, clone, git, branch, result.Head); err != nil {
			return result, err
		}
		if err := recordSync(ctx, loc, &b, git, result.Head, remote, false); err != nil {
			return result, err
		}
		if err := requireSyncClean(ctx, git, local, incoming); err != nil {
			return result, fmt.Errorf("cannot integrate received work; local changes are retained; retry atomic realm sync: %w", err)
		}
		if err := verifySyncCheckout(ctx, clone, git, branch, result.Head); err != nil {
			return result, err
		}
		merged, manifest, err := mergeSyncHead(ctx, clone, git, b, loc, result.Head, remote, local, incoming, artifacts)
		if err != nil {
			return result, err
		}
		result.Received, result.Head, local = merged != result.Head, merged, manifest
		if err := verifySyncCheckout(ctx, clone, git, branch, result.Head); err != nil {
			return result, err
		}
		if err := requireSyncClean(ctx, git, local); err != nil {
			return result, fmt.Errorf("received work is retained; retry atomic realm sync after handling clone edits: %w", err)
		}
		if err := history.validate(ctx, result.Head, remote); err != nil {
			return result, fmt.Errorf("refusing publication after integration; inspect retained clone history before retrying atomic realm sync: %w", err)
		}
		if err := verifySyncCheckout(ctx, clone, git, branch, result.Head); err != nil {
			return result, err
		}
		updated, advanced, err = syncPush(ctx, b, result.Head)
		if err != nil {
			if advanced {
				err = fmt.Errorf("remote branch %q advanced again; commit %s is retained, retry atomic realm sync: %w", b.Branch, result.Head, err)
			} else {
				err = fmt.Errorf("push after integration did not finish; commit %s is retained: %w", result.Head, err)
			}
			return result, errors.Join(err, recordSync(ctx, loc, &b, git, result.Head, remote, false))
		}
	}
	result.Published = updated
	cleanErr := verifySyncCheckout(ctx, clone, git, branch, result.Head)
	if cleanErr == nil {
		cleanErr = requireSyncClean(ctx, git, local)
	}
	if cleanErr != nil {
		cleanErr = fmt.Errorf("exchange reached %s but new clone work remains; preserve it and retry atomic realm sync: %w", result.Head, cleanErr)
	}
	if pending != nil {
		pending.Published = true
	}
	return result, errors.Join(cleanErr, recordSync(ctx, loc, &b, git, result.Head, result.Head, cleanErr == nil))
}

func guardSyncOperations(clone *os.Root) error {
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "REBASE_HEAD", "rebase-apply", "rebase-merge", "sequencer", "BISECT_START"} {
		if _, err := clone.Lstat(".git/" + name); !os.IsNotExist(err) {
			if err != nil {
				return err
			}
			return fmt.Errorf("unfinished Git operation (%s); inspect git -C %q status and resolve the operation before syncing", name, clone.Name())
		}
	}
	return nil
}

func readSyncHead(ctx context.Context, clone *os.Root, git shareGit, branch string) (string, error) {
	head, err := readSyncCommitRef(ctx, git, branch)
	if err != nil {
		return "", err
	}
	out, err := clone.ReadFile(".git/HEAD")
	if err != nil {
		return "", err
	}
	if strings.TrimSuffix(string(out), "\n") != "ref: "+branch {
		return "", fmt.Errorf("expected checkout on %q; inspect git -C %q status", branch, git.dir)
	}
	if head == "" {
		return "", fmt.Errorf("bound branch %q has no commit; inspect git -C %q status or finish pending share", branch, git.dir)
	}
	return head, nil
}

func readSyncCommitRef(ctx context.Context, git shareGit, ref string) (string, error) {
	out, err := git.run(ctx, nil, "for-each-ref", "--format=%(refname)%09%(objectname)%09%(objecttype)%09%(symref)", "--", ref)
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		name, rest, _ := strings.Cut(line, "\t")
		if name != ref {
			continue
		}
		oid, metadata, _ := strings.Cut(rest, "\t")
		if !gitObjectID(oid) || metadata != "commit\t" {
			return "", fmt.Errorf("Git ref %q must directly reference a commit; inspect git -C %q status", ref, git.dir)
		}
		return oid, nil
	}
	return "", nil
}

func verifySyncCheckout(ctx context.Context, clone *os.Root, git shareGit, branch, expected string) error {
	if err := verifySyncHead(ctx, clone, git, branch, expected); err != nil {
		return err
	}
	return guardSyncOperations(clone)
}

func verifySyncHead(ctx context.Context, clone *os.Root, git shareGit, branch, expected string) error {
	head, err := readSyncHead(ctx, clone, git, branch)
	if err != nil {
		return err
	}
	if head != expected {
		return fmt.Errorf("bound branch changed from %s to %s; inspect clone before retrying", expected, head)
	}
	return nil
}

func checkCommittedSyncMappings(live, committed Manifest) error {
	for _, mapping := range live.Mappings {
		if !slices.Contains(committed.Mappings, mapping) {
			return fmt.Errorf("new or changed selection %q must be published through atomic realm share", mapping.Destination)
		}
	}
	return nil
}

func syncIndexTree(ctx context.Context, git shareGit) (string, error) {
	out, err := git.run(ctx, nil, "write-tree")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSuffix(string(out), "\n")
	if !gitObjectID(tree) {
		return "", fmt.Errorf("Git did not return a valid staged tree")
	}
	return tree, nil
}

func checkSyncIndex(ctx context.Context, git shareGit, manifest Manifest) error {
	out, err := git.run(ctx, nil, "ls-files", "--stage", "-v", "-z")
	if err != nil {
		return err
	}
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		if !ok || len(meta) < 10 || !strings.HasSuffix(meta, " 0") {
			return fmt.Errorf("unmerged or invalid index entry %q; resolve staging before syncing", name)
		}
		if strings.HasPrefix(meta[2:], "160000 ") {
			return fmt.Errorf("nested Git repository staged at %q", name)
		}
		if meta[0] == 'S' || meta[0] >= 'a' && meta[0] <= 'z' {
			return fmt.Errorf("index hides worktree changes at %q; clear skip-worktree or assume-unchanged before syncing", name)
		}
	}
	out, err = git.run(ctx, nil, "diff", "--cached", "--name-only", "--no-renames", "-z", "--")
	if err != nil {
		return err
	}
	for name := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if name != "" && name != manifestName {
			if _, ok := syncMapping(manifest, name); !ok {
				return fmt.Errorf("unexpected staged path %q; handle it in the clone before syncing", name)
			}
		}
	}
	return nil
}

func requireSyncClean(ctx context.Context, git shareGit, manifests ...Manifest) error {
	out, err := git.run(ctx, nil, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return err
	}
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return fmt.Errorf("invalid Git status output")
		}
		name := strings.TrimSuffix(record[3:], "/")
		if record[:2] != "??" || name == manifestName {
			return fmt.Errorf("working tree or index has changes at %q", name)
		}
		for _, manifest := range manifests {
			if _, ok := syncMapping(manifest, name); ok {
				return fmt.Errorf("working tree has an untracked declared path %q", name)
			}
		}
	}
	return nil
}

func commitSyncTree(ctx context.Context, clone *os.Root, git shareGit, branch, tree, parent, message, mergeParent string) (string, error) {
	commit, err := writeSyncCommit(ctx, git, tree, parent, message, mergeParent)
	if err != nil {
		return parent, err
	}
	if mergeParent == "" {
		if err := guardSyncOperations(clone); err != nil {
			return parent, err
		}
	} else if err := verifySyncMergeHead(clone, mergeParent); err != nil {
		return parent, err
	}
	if err := verifySyncHead(ctx, clone, git, branch, parent); err != nil {
		return parent, err
	}
	return installSyncCommit(ctx, git, branch, parent, commit, message)
}

func writeSyncCommit(ctx context.Context, git shareGit, tree, parent, message, mergeParent string) (string, error) {
	args := []string{"commit-tree", tree, "--no-gpg-sign", "-m", message, "-p", parent}
	if mergeParent != "" {
		args = append(args, "-p", mergeParent)
	}
	git.userConfig = true
	out, err := git.run(ctx, nil, args...)
	if err != nil {
		return "", fmt.Errorf("cannot commit validated snapshot: %w", err)
	}
	commit := strings.TrimSuffix(string(out), "\n")
	if !gitObjectID(commit) || len(commit) != len(parent) {
		return "", fmt.Errorf("Git did not return a valid snapshot commit")
	}
	return commit, nil
}

func installSyncCommit(ctx context.Context, git shareGit, branch, parent, commit, message string) (string, error) {
	if _, err := git.run(ctx, nil, "update-ref", "--no-deref", "-m", message, branch, commit, parent); err != nil {
		return parent, fmt.Errorf("could not install snapshot commit %s; inspect the clone: %w", commit, err)
	}
	return commit, nil
}

func mergeSyncHead(ctx context.Context, clone *os.Root, git shareGit, b Binding, loc bindingLocation, head, remote string, local, incoming Manifest, guards ...*syncArtifactGuard) (string, Manifest, error) {
	branch := shareBranchRef(b.Branch)
	if _, err := git.run(ctx, nil, "merge-base", "--is-ancestor", remote, head); err == nil {
		return head, local, nil
	} else if !gitExit(err, 1) {
		return head, Manifest{}, err
	}
	var artifacts *syncArtifactGuard
	if len(guards) != 0 {
		artifacts = guards[0]
	}
	if b.PendingShare != nil || artifacts != nil {
		if _, err := git.run(ctx, nil, "merge-base", "--is-ancestor", head, remote); err == nil {
			if b.PendingShare != nil {
				if err := verifyPendingMappings(incoming, b.PendingShare); err != nil {
					return head, Manifest{}, err
				}
			}
			if artifacts != nil {
				commit, err := checkoutSyncTree(ctx, clone, git, branch, head, remote, remote, "Fast-forward received realm changes")
				return commit, incoming, err
			}
		} else if !gitExit(err, 1) {
			return head, Manifest{}, err
		} else if artifacts != nil {
			tree, err := syncArtifactMergeTree(ctx, git, head, remote, artifacts)
			if err != nil {
				return head, Manifest{}, err
			}
			if tree != "" {
				state, _ := relativeTo(loc.physicalRealm, loc.physicalState)
				merged, err := syncTreeManifest(ctx, git, tree, b.RealmID, state, artifacts)
				if err == nil && b.PendingShare != nil {
					err = verifyPendingMappings(merged, b.PendingShare)
				}
				if err != nil {
					return head, Manifest{}, fmt.Errorf("refusing invalid merged snapshot %s: %w", tree, err)
				}
				const message = "Merge received realm changes"
				commit, err := writeSyncCommit(ctx, git, tree, head, message, remote)
				if err != nil {
					return head, Manifest{}, err
				}
				if err := requireSyncClean(ctx, git, local, merged); err != nil {
					return head, Manifest{}, err
				}
				commit, err = checkoutSyncTree(ctx, clone, git, branch, head, tree, commit, message)
				return commit, merged, err
			}
		}
	}
	if err := verifySyncCheckout(ctx, clone, git, branch, head); err != nil {
		return head, Manifest{}, err
	}
	git.userConfig = true
	if _, err := git.run(ctx, nil, "merge", "--ff", "--no-commit", "--no-edit", "--no-stat", "--no-verify", "--no-autostash",
		"--no-overwrite-ignore", "--no-verify-signatures", "--no-squash", "--no-allow-unrelated-histories", "-s", "ort", "--", remote); err != nil {
		paths, listErr := git.run(ctx, nil, "diff", "--name-only", "--diff-filter=U", "--no-renames", "-z", "--")
		if listErr == nil && len(paths) != 0 {
			return head, Manifest{}, syncConflictError(git.dir, loc.physicalRealm, paths, local, incoming, err)
		}
		return head, Manifest{}, fmt.Errorf("could not integrate received commit %s; inspect git -C %q status: %w", remote, git.dir, err)
	}
	mergeHead, err := clone.ReadFile(".git/MERGE_HEAD")
	if os.IsNotExist(err) {
		if err := verifySyncHead(ctx, clone, git, branch, remote); err != nil {
			return head, Manifest{}, err
		}
		return remote, incoming, nil
	}
	if err != nil {
		return head, Manifest{}, err
	}
	if strings.TrimSuffix(string(mergeHead), "\n") != remote {
		return head, Manifest{}, fmt.Errorf("merge head changed; inspect git -C %q status before retrying", git.dir)
	}
	tree, err := syncIndexTree(ctx, git)
	if err != nil {
		return head, Manifest{}, err
	}
	state, _ := relativeTo(loc.physicalRealm, loc.physicalState)
	merged, err := syncTreeManifest(ctx, git, tree, b.RealmID, state, artifacts)
	if err == nil && b.PendingShare != nil {
		err = verifyPendingMappings(merged, b.PendingShare)
	}
	if err != nil {
		return head, Manifest{}, fmt.Errorf("invalid merged snapshot; merge left open in %q for review: %w", git.dir, err)
	}
	commit, err := commitSyncTree(ctx, clone, git, branch, tree, head, "Merge received realm changes", remote)
	if err != nil {
		return head, Manifest{}, err
	}
	// The validated merge commit is already installed; quit only clears Git's merge metadata.
	if _, err := git.run(ctx, nil, "merge", "--quit"); err != nil {
		return commit, merged, fmt.Errorf("merge commit %s is retained; inspect Git merge state: %w", commit, err)
	}
	return commit, merged, nil
}

func checkoutSyncTree(ctx context.Context, clone *os.Root, git shareGit, branch, head, tree, commit, message string) (string, error) {
	if err := verifySyncCheckout(ctx, clone, git, branch, head); err != nil {
		return head, err
	}
	if _, err := git.run(ctx, nil, "read-tree", "-m", "-u", head, tree); err != nil {
		return head, fmt.Errorf("could not install validated snapshot %s; inspect the clone: %w", tree, err)
	}
	if err := verifySyncCheckout(ctx, clone, git, branch, head); err != nil {
		return head, err
	}
	return installSyncCommit(ctx, git, branch, head, commit, message)
}

func verifySyncMergeHead(clone *os.Root, expected string) error {
	out, err := clone.ReadFile(".git/MERGE_HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSuffix(string(out), "\n") != expected {
		return fmt.Errorf("merge head changed; inspect git -C %q status before retrying", clone.Name())
	}
	return nil
}

func syncConflictError(clone, realm string, paths []byte, local, incoming Manifest, cause error) error {
	var details strings.Builder
	for name := range strings.SplitSeq(strings.TrimSuffix(string(paths), "\x00"), "\x00") {
		fmt.Fprintf(&details, "\n  clone path %q", name)
		var previous string
		for _, manifest := range []Manifest{local, incoming} {
			if mapping, ok := syncMapping(manifest, name); ok {
				mapped := mapping.Destination + strings.TrimPrefix(name, mapping.Source)
				if mapped != previous {
					fmt.Fprintf(&details, ", realm path %q", filepath.Join(realm, filepath.FromSlash(mapped)))
					previous = mapped
				}
			}
		}
		if previous == "" {
			details.WriteString(", no realm-local mapping")
		}
	}
	quotedClone := "'" + strings.ReplaceAll(clone, "'", `'\''`) + "'"
	return fmt.Errorf("merge conflict; Git merge state is preserved:%s\nResolve the files, stage the resolved clone paths with git -C %s add -- <paths>, then run git -C %s commit and retry atomic realm sync: %w", details.String(), quotedClone, quotedClone, cause)
}

func recordSync(ctx context.Context, loc bindingLocation, b *Binding, git shareGit, head, remote string, success bool) error {
	var ahead, behind int
	if head != remote {
		out, err := git.run(ctx, nil, "rev-list", "--left-right", "--count", head+"..."+remote)
		if err != nil {
			return err
		}
		left, right, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
		var leftErr, rightErr error
		ahead, leftErr = strconv.Atoi(left)
		behind, rightErr = strconv.Atoi(right)
		if leftErr != nil || rightErr != nil || ahead < 0 || behind < 0 {
			return fmt.Errorf("cannot read Git branch divergence")
		}
	}
	tracking := joinRemoteHeads + b.Branch
	old, err := readSyncCommitRef(ctx, git, tracking)
	if err != nil {
		return err
	}
	if old != remote {
		if old == "" {
			old = strings.Repeat("0", len(remote))
		}
		if _, err := git.run(ctx, nil, "update-ref", "--no-deref", tracking, remote, old); err != nil {
			return fmt.Errorf("cannot record the observed remote tip %s: %w", remote, err)
		}
	}
	if b.Sync == nil {
		b.Sync = &SyncState{}
	}
	b.Sync.Ahead, b.Sync.Behind, b.Sync.RemoteHead = ahead, behind, remote
	if success {
		b.Sync.LastSuccess = time.Now().UTC()
	}
	return loc.saveBinding(*b)
}
