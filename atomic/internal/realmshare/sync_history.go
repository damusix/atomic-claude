package realmshare

import (
	"context"
	"fmt"
	"strings"
)

type syncHistory struct {
	git            shareGit
	realmID, state string
	trees          map[string]struct{}
}

func newSyncHistory(git shareGit, realmID, state string) syncHistory {
	// A shallow boundary must not hide commits, and missing objects must not trigger a fetch.
	git.configEnv = append(git.configEnv[:len(git.configEnv):len(git.configEnv)], "GIT_SHALLOW_FILE=", "GIT_NO_LAZY_FETCH=1")
	return syncHistory{git: git, realmID: realmID, state: state, trees: make(map[string]struct{})}
}

func (h syncHistory) publishedCommit(ctx context.Context, head, published string) string {
	if len(published) != len(head) || !gitObjectID(published) {
		return ""
	}
	out, err := h.git.run(ctx, nil, "cat-file", "-t", published)
	if err != nil || string(out) != "commit\n" {
		return ""
	}
	return published
}

func (h syncHistory) treeOID(ctx context.Context, commit string) (string, error) {
	out, err := h.git.run(ctx, nil, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSuffix(string(out), "\n")
	if len(tree) != len(commit) || !gitObjectID(tree) {
		return "", fmt.Errorf("commit %s did not resolve to a valid Git tree", commit)
	}
	return tree, nil
}

func (h syncHistory) manifest(ctx context.Context, tree string, artifacts ...*syncArtifactGuard) (Manifest, error) {
	manifest, err := syncTreeManifest(ctx, h.git, tree, h.realmID, h.state, artifacts...)
	if err == nil {
		h.trees[tree] = struct{}{}
	}
	return manifest, err
}

func (h syncHistory) validateTree(ctx context.Context, tree string) error {
	if _, checked := h.trees[tree]; checked {
		return nil
	}
	_, err := h.manifest(ctx, tree)
	return err
}

func validateSyncHistory(ctx context.Context, git shareGit, head, published, realmID, state string) error {
	history := newSyncHistory(git, realmID, state)
	return history.validate(ctx, head, history.publishedCommit(ctx, head, published))
}

func (h syncHistory) validate(ctx context.Context, head, published string) error {
	if !gitObjectID(head) {
		return fmt.Errorf("history validation requires a full commit ID")
	}
	args := []string{"rev-list", "--no-commit-header", "--format=%H%x20%T", head}
	if published != "" {
		args = append(args, "^"+published)
	}
	out, err := h.git.run(ctx, nil, append(args, "--")...)
	if err != nil {
		return fmt.Errorf("cannot enumerate outgoing history: %w", err)
	}
	tipChecked := false
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" {
			continue
		}
		commit, tree, _ := strings.Cut(line, " ")
		if len(commit) != len(head) || len(tree) != len(head) || !gitObjectID(commit) || !gitObjectID(tree) {
			return fmt.Errorf("invalid commit or tree in outgoing history")
		}
		if err := h.validateTree(ctx, tree); err != nil {
			return fmt.Errorf("outgoing commit %s has an invalid snapshot: %w", commit, err)
		}
		tipChecked = tipChecked || commit == head
	}
	if !tipChecked {
		if head != published && h.publishedCommit(ctx, head, head) == "" {
			return fmt.Errorf("outgoing head %s is not a commit", head)
		}
		tree, err := h.treeOID(ctx, head)
		if err != nil {
			return err
		}
		if err := h.validateTree(ctx, tree); err != nil {
			return fmt.Errorf("outgoing commit %s has an invalid snapshot: %w", head, err)
		}
	}
	return nil
}

func (h syncHistory) commitManifest(ctx context.Context, commit string, artifacts ...*syncArtifactGuard) (Manifest, error) {
	tree, err := h.treeOID(ctx, commit)
	if err != nil {
		return Manifest{}, err
	}
	return h.manifest(ctx, tree, artifacts...)
}

func (h syncHistory) ancestor(ctx context.Context, earlier, later string) (bool, error) {
	_, err := h.git.run(ctx, nil, "merge-base", "--is-ancestor", earlier, later)
	if gitExit(err, 1) {
		return false, nil
	}
	return err == nil, err
}

func (h syncHistory) validatePending(ctx context.Context, head, published string, pending *PendingShare) error {
	if head == pending.Commit {
		return nil
	}
	if published == "" {
		return fmt.Errorf("no verified remote history for a continuation of %s", pending.Commit)
	}
	out, err := h.git.run(ctx, nil, "rev-list", "--first-parent", "--no-commit-header", "--format=%H%x20%T%x20%P", head, "^"+pending.Commit, "^"+published, "--")
	if err != nil {
		return err
	}
	base := head
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" {
			continue
		}
		commit, rest, _ := strings.Cut(line, " ")
		tree, parents, _ := strings.Cut(rest, " ")
		first, second, _ := strings.Cut(parents, " ")
		if commit != base || len(tree) != len(head) || len(first) != len(head) || len(second) != len(head) ||
			!gitObjectID(tree) || !gitObjectID(first) || !gitObjectID(second) {
			return fmt.Errorf("commit %s is not a two-parent merge continuing the recorded share", base)
		}
		verified, err := h.ancestor(ctx, second, published)
		if err != nil {
			return err
		}
		if !verified {
			return fmt.Errorf("merge %s has a second parent outside verified remote history", commit)
		}
		alreadyIntegrated, err := h.ancestor(ctx, second, first)
		if err != nil {
			return err
		}
		if alreadyIntegrated {
			return fmt.Errorf("merge %s does not resolve divergent history", commit)
		}
		fastForward, err := h.ancestor(ctx, first, second)
		if err != nil {
			return err
		}
		if fastForward {
			return fmt.Errorf("merge %s does not resolve divergent history", commit)
		}
		manifest, err := h.manifest(ctx, tree)
		if err != nil {
			return fmt.Errorf("resolution commit %s has an invalid snapshot: %w", commit, err)
		}
		if err := verifyPendingMappings(manifest, pending); err != nil {
			return fmt.Errorf("resolution commit %s: %w", commit, err)
		}
		base = first
	}
	if base != pending.Commit {
		containsShare, err := h.ancestor(ctx, pending.Commit, base)
		if err != nil {
			return err
		}
		if !containsShare {
			return fmt.Errorf("continuation does not start from the recorded share %s or a published descendant", pending.Commit)
		}
	}
	return nil
}
