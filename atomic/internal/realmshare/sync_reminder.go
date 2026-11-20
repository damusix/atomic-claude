package realmshare

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaimSyncReminder claims the local day's notice if the bound checkout has unpublished work.
func ClaimSyncReminder(ctx context.Context, realmRoot string, now time.Time) bool {
	loc, err := locateBinding(realmRoot)
	if err != nil {
		return false
	}
	const dir = "realm-sync-reminders"
	marker := dir + "/" + now.Format(time.DateOnly)
	if _, err := os.Lstat(filepath.Join(loc.physicalState, marker)); !os.IsNotExist(err) {
		return false
	}
	links, err := openLocalLinkView(loc, true)
	if err != nil {
		return false
	}
	defer links.close()
	if !links.hasUnpublishedChanges(ctx) || ctx.Err() != nil {
		return false
	}
	state := links.state
	if err := state.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return false
	}
	if err := checkDirectory(state, dir); err != nil {
		return false
	}
	file, err := state.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	file.Close()
	return true
}

func (l *localLinks) hasUnpublishedChanges(ctx context.Context) bool {
	b := l.binding
	clone, err := os.OpenRoot(b.ClonePath)
	if err != nil {
		return false
	}
	defer clone.Close()
	git := statusGit(b.ClonePath)
	branch := shareBranchRef(b.Branch)
	head, err := readSyncCommitRef(ctx, git, branch)
	if err != nil || head == "" {
		return false
	}
	if cached := statusRemote(ctx, git, head, b.Sync); cached != nil && cached.DivergenceKnown && cached.Ahead > 0 {
		return true
	}
	if b.PendingLocal != nil || b.PendingShare != nil || guardSyncOperations(clone) != nil {
		return false
	}
	if ref, err := clone.ReadFile(".git/HEAD"); err != nil || strings.TrimSuffix(string(ref), "\n") != "ref: "+branch {
		return false
	}
	manifest, err := readManifest(clone)
	if err != nil || l.checkMappingDeclarations(manifest) != nil {
		return false
	}
	committed, err := l.committedManifest(ctx, git, head)
	if err != nil || len(manifest.Mappings) != len(committed.Mappings) || checkCommittedSyncMappings(manifest, committed) != nil {
		return false
	}
	probe, closeProbe, err := openStatusGit(ctx, git, head, l.loc)
	if err != nil {
		return false
	}
	defer closeProbe()
	if checkSyncIndex(ctx, probe, manifest) != nil {
		return false
	}
	baseline := probe
	baseline.index += ".head"
	if _, err := baseline.run(ctx, nil, "read-tree", head); err != nil {
		return false
	}
	for i, index := range [...]shareGit{baseline, probe} {
		changes, _, err := readStatusChanges(ctx, index, manifest, nil)
		if err != nil {
			return false
		}
		paths := make(map[string]struct{})
		for _, change := range changes {
			mapping, mapped := syncMapping(manifest, change.Path)
			if !mapped && change.Path != manifestName {
				continue
			}
			if i == 0 || change.Index == "A" && change.Worktree != "D" || change.Worktree == "A" ||
				change.Index == "D" && mapped && change.Path != mapping.Source {
				paths[change.Path] = struct{}{}
			}
		}
		if i != 0 {
			// Restaging re-adds non-ignored paths that are missing from the index.
			for _, change := range changes {
				if change.Worktree == "?" {
					delete(paths, change.Path)
				}
			}
		}
		if hasUnfilteredReminderPath(ctx, git, probe, paths) {
			artifacts, err := newSyncArtifactGuard(l.loc, clone, b)
			if err != nil {
				return false
			}
			preflight := probe
			if artifacts != nil {
				preflight.configEnv = append(syncLocalGit(b, true).configEnv, probe.configEnv...)
			}
			state, _ := relativeTo(l.loc.physicalRealm, l.loc.physicalState)
			live, err := syncLiveManifest(ctx, clone, preflight, b.RealmID, state, artifacts)
			return err == nil && reminderDirectoriesMaterialized(ctx, clone, probe, live)
		}
	}
	return false
}

func hasUnfilteredReminderPath(ctx context.Context, git, probe shareGit, paths map[string]struct{}) bool {
	if len(paths) == 0 {
		return false
	}
	out, err := git.run(ctx, nil, "config", "--includes", "--null", "--get-regexp", `^filter\..+\.(clean|process)$`)
	if gitExit(err, 1) {
		return true
	}
	if err != nil {
		return false
	}
	configured := make(map[string]bool)
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		configured[key] = value != ""
	}
	filtered := make(map[string]struct{})
	err = visitGitAttributes(ctx, probe, []string{"check-attr", "--all", "--stdin", "-z"}, paths, func(name, attr, value string) error {
		if attr == "filter" && (configured["filter."+value+".clean"] || configured["filter."+value+".process"]) {
			filtered[name] = struct{}{}
		}
		return nil
	})
	return err == nil && len(filtered) < len(paths)
}

func reminderDirectoriesMaterialized(ctx context.Context, clone *os.Root, probe shareGit, manifest Manifest) bool {
	remaining := make(map[string]struct{})
	for _, mapping := range manifest.Mappings {
		if mapping.Kind == KindDirectory || mapping.Kind == KindSkill {
			remaining[mapping.Source] = struct{}{}
		}
	}
	if len(remaining) == 0 {
		return true
	}
	out, err := probe.run(ctx, nil, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return false
	}
	for name := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		mapping, mapped := syncMapping(manifest, name)
		if !mapped {
			continue
		}
		if _, needed := remaining[mapping.Source]; needed {
			if info, err := lstatBelow(clone, name); err == nil && !info.IsDir() {
				delete(remaining, mapping.Source)
				if len(remaining) == 0 {
					return true
				}
			}
		}
	}
	return false
}
