package realmshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func statusHead(ctx context.Context, clone *os.Root, git shareGit, b Binding) (string, error) {
	out, err := git.run(ctx, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if gitExit(err, 1) && b.PendingShare != nil && b.PendingShare.Base == "" && !b.PendingShare.Published {
		branch := shareBranchRef(b.Branch)
		raw, readErr := clone.ReadFile(".git/HEAD")
		if readErr != nil {
			return "", readErr
		}
		if strings.TrimSuffix(string(raw), "\n") == "ref: "+branch {
			_, refErr := lstatBelow(clone, ".git/"+branch)
			if os.IsNotExist(refErr) {
				head, refErr := readSyncCommitRef(ctx, git, branch)
				if refErr != nil || head == "" {
					return head, refErr
				}
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("cannot read checkout HEAD in %q: %w", git.dir, err)
	}
	head := strings.TrimSpace(string(out))
	if !gitObjectID(head) {
		return "", fmt.Errorf("invalid checkout HEAD in %q", git.dir)
	}
	return head, nil
}

func statusChanges(ctx context.Context, git shareGit, head string, manifest Manifest, links []LinkReceipt, loc bindingLocation) ([]StatusChange, []StatusConflict, error) {
	probe, closeProbe, err := openStatusGit(ctx, git, head, loc)
	if err != nil {
		return nil, nil, err
	}
	defer closeProbe()
	return readStatusChanges(ctx, probe, manifest, links)
}

func readStatusChanges(ctx context.Context, git shareGit, manifest Manifest, links []LinkReceipt) ([]StatusChange, []StatusConflict, error) {
	out, err := git.run(ctx, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--ignore-submodules=all")
	if err != nil {
		return nil, nil, err
	}
	changes := make([]StatusChange, 0)
	conflicts := make([]StatusConflict, 0)
	if len(out) == 0 {
		return changes, conflicts, nil
	}
	if out[len(out)-1] != 0 {
		return nil, nil, fmt.Errorf("unterminated Git status record in %q", git.dir)
	}
	for record := range strings.SplitSeq(string(out[:len(out)-1]), "\x00") {
		if len(record) < 4 || record[2] != ' ' {
			return nil, nil, fmt.Errorf("invalid Git status record in %q", git.dir)
		}
		name := record[3:]
		change := StatusChange{Path: name}
		if record[0] != ' ' && record[0] != '?' {
			change.Index = record[:1]
		}
		if record[1] != ' ' {
			change.Worktree = record[1:2]
		}
		changes = append(changes, change)
		switch record[:2] {
		case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
			conflicts = append(conflicts, StatusConflict{Source: name, Path: statusConflictPath(manifest, links, name)})
		}
	}
	return changes, conflicts, nil
}

func statusManifestUnmerged(ctx context.Context, git shareGit, head string, loc bindingLocation) (bool, error) {
	out, err := runStatusGit(ctx, git, head, loc, "ls-files", "--unmerged", "-z", "--", ":(top,literal)"+manifestName)
	if err != nil {
		return false, err
	}
	for record := range strings.SplitSeq(string(out), "\x00") {
		if _, name, ok := strings.Cut(record, "\t"); ok && name == manifestName {
			return true, nil
		}
	}
	return false, nil
}

func runStatusGit(ctx context.Context, git shareGit, head string, loc bindingLocation, args ...string) ([]byte, error) {
	probe, closeProbe, err := openStatusGit(ctx, git, head, loc)
	if err != nil {
		return nil, err
	}
	defer closeProbe()
	return probe.run(ctx, nil, args...)
}

func openStatusGit(ctx context.Context, git shareGit, head string, loc bindingLocation) (shareGit, func(), error) {
	temp, err := physicalDirectory(os.TempDir())
	if err != nil {
		return shareGit{}, nil, fmt.Errorf("cannot resolve Git status temporary directory: %w", err)
	}
	roots := [...]string{git.dir, loc.physicalRealm, loc.physicalState}
	var protected [len(roots)]os.FileInfo
	for i, root := range roots {
		protected[i], err = statusDirectoryIdentity(root)
		if err != nil {
			return shareGit{}, nil, err
		}
	}
	for ancestor := temp; ; {
		info, err := statusDirectoryIdentity(ancestor)
		if err != nil {
			return shareGit{}, nil, err
		}
		for i, root := range roots {
			if os.SameFile(info, protected[i]) {
				return shareGit{}, nil, fmt.Errorf("Git status temporary directory %q is inside %q; use a temporary directory outside the managed checkout, realm and state", temp, root)
			}
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	scratch, err := os.MkdirTemp(temp, "atomic-realm-status-")
	if err != nil {
		return shareGit{}, nil, err
	}
	closeProbe := func() { os.RemoveAll(scratch) }
	probe, err := prepareStatusGit(ctx, git, head, scratch)
	if err != nil {
		closeProbe()
		return shareGit{}, nil, err
	}
	return probe, closeProbe, nil
}

func statusDirectoryIdentity(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot verify Git status directory identity: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cannot verify Git status directory %q: not a directory", path)
	}
	// On Windows, Stat can defer identity lookup until SameFile.
	if !os.SameFile(info, info) {
		return nil, fmt.Errorf("cannot verify Git status directory identity for %q", path)
	}
	return info, nil
}

func prepareStatusGit(ctx context.Context, git shareGit, head, scratch string) (shareGit, error) {
	clone, err := os.OpenRoot(git.dir)
	if err != nil {
		return shareGit{}, err
	}
	defer clone.Close()
	info, err := clone.Lstat(".git")
	if err != nil {
		return shareGit{}, err
	}
	if !info.IsDir() {
		return shareGit{}, fmt.Errorf("Git metadata in %q must be a real directory", git.dir)
	}
	real, err := clone.OpenRoot(".git")
	if err != nil {
		return shareGit{}, err
	}
	defer real.Close()
	common, err := shareGitCommonDir(clone)
	if err != nil {
		return shareGit{}, err
	}
	defer common.Close()

	sha256 := len(head) == 64
	if head == "" {
		f, err := openRegular(real, "HEAD")
		if err != nil {
			return shareGit{}, err
		}
		raw, err := io.ReadAll(f)
		if err := errors.Join(err, f.Close()); err != nil {
			return shareGit{}, err
		}
		head = strings.TrimSuffix(string(raw), "\n")
		ref, ok := strings.CutPrefix(head, "ref: ")
		if !ok || !strings.HasPrefix(ref, "refs/heads/") {
			return shareGit{}, fmt.Errorf("expected an unborn branch in %q", git.dir)
		}
		if _, err := git.run(ctx, nil, "check-ref-format", ref); err != nil {
			return shareGit{}, err
		}
		out, err := git.run(ctx, nil, "rev-parse", "--show-object-format")
		if err != nil {
			return shareGit{}, err
		}
		switch format := strings.TrimSpace(string(out)); format {
		case "sha256":
			sha256 = true
		case "sha1", "--show-object-format": // Git versions before SHA-256 echo the unknown option.
		default:
			return shareGit{}, fmt.Errorf("unsupported Git object format %q in %q", format, git.dir)
		}
	} else if !gitObjectID(head) {
		return shareGit{}, fmt.Errorf("invalid checkout HEAD in %q", git.dir)
	}

	config, err := readStatusGitConfig(ctx, git, sha256)
	if err != nil {
		return shareGit{}, err
	}
	objects, err := common.Lstat("objects")
	if err != nil {
		return shareGit{}, err
	}
	if !objects.IsDir() {
		return shareGit{}, fmt.Errorf("Git object storage in %q must be a real directory", common.Name())
	}
	objectPath := filepath.Join(common.Name(), "objects")
	if strings.ContainsRune(objectPath, '\n') {
		return shareGit{}, fmt.Errorf("Git object storage path %q contains a newline", objectPath)
	}

	gitDir := filepath.Join(scratch, ".git")
	for _, name := range []string{"refs", "objects/info", "info"} {
		if err := os.MkdirAll(filepath.Join(gitDir, name), 0o700); err != nil {
			return shareGit{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head+"\n"), 0o600); err != nil {
		return shareGit{}, err
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o600); err != nil {
		return shareGit{}, err
	}
	// Alternates let Git read live objects without making their directory a write target.
	if err := os.WriteFile(filepath.Join(gitDir, "objects", "info", "alternates"), []byte(objectPath+"\n"), 0o600); err != nil {
		return shareGit{}, err
	}
	for _, entry := range []struct {
		root *os.Root
		name string
	}{
		{common, "info/exclude"},
		{common, "info/attributes"},
		{real, "info/sparse-checkout"},
	} {
		if err := copyStatusFile(entry.root, entry.name, filepath.Join(gitDir, entry.name), false); err != nil && !os.IsNotExist(err) {
			return shareGit{}, err
		}
	}

	dir, err := real.Open(".")
	if err != nil {
		return shareGit{}, err
	}
	names, err := dir.Readdirnames(-1)
	if err := errors.Join(err, dir.Close()); err != nil {
		return shareGit{}, err
	}
	// Git freshens split-index bases even when optional locks are disabled.
	for _, name := range names {
		if oid, ok := strings.CutPrefix(name, "sharedindex."); ok && gitObjectID(oid) {
			if err := copyStatusFile(real, name, filepath.Join(gitDir, name), false); err != nil && !os.IsNotExist(err) {
				return shareGit{}, err
			}
		}
	}

	index := filepath.Join(gitDir, "index")
	info, err = real.Lstat("index")
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return shareGit{}, err
	case !info.Mode().IsRegular():
		return shareGit{}, fmt.Errorf("Git index in %q must be a regular file", real.Name())
	default:
		// For read-only probes, prefer an alias to preserve the original racy-stat timestamp.
		if os.Symlink(filepath.Join(real.Name(), "index"), index) != nil {
			if err := copyStatusFile(real, "index", index, true); err != nil {
				return shareGit{}, err
			}
		}
	}
	git.dir, git.workTree, git.index = scratch, git.dir, index
	return git, nil
}

func copyStatusFile(real *os.Root, name, dest string, preserveTime bool) error {
	src, err := openRegular(real, name)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if err := errors.Join(err, dst.Close()); err != nil {
		return err
	}
	if preserveTime {
		info, err := src.Stat()
		if err != nil {
			return err
		}
		return os.Chtimes(dest, info.ModTime(), info.ModTime())
	}
	return nil
}

func readStatusGitConfig(ctx context.Context, git shareGit, sha256 bool) (string, error) {
	out, err := git.run(ctx, nil, "config", "--includes", "--null", "--list")
	if err != nil {
		return "", err
	}
	raw := string(out)
	if raw != "" && !strings.HasSuffix(raw, "\x00") {
		return "", fmt.Errorf("unterminated Git config record in %q", git.dir)
	}
	var config strings.Builder
	quote := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\b", `\b`)
	config.WriteString("[core]\n\tuntrackedCache = false\n\thooksPath = \"")
	_, _ = quote.WriteString(&config, os.DevNull)
	config.WriteString("\"\n")
	if sha256 {
		config.WriteString("\trepositoryFormatVersion = 1\n[extensions]\n\tobjectFormat = sha256\n")
	}
	// Keep path values unchanged: both Git commands run from the real worktree.
	for record := range strings.SplitSeq(strings.TrimSuffix(raw, "\x00"), "\x00") {
		key, value, hasValue := strings.Cut(record, "\n")
		switch key {
		case "core.excludesfile", "core.attributesfile",
			"core.filemode", "core.trustctime", "core.checkstat", "core.ignorestat",
			"core.ignorecase", "core.symlinks", "core.precomposeunicode",
			"core.autocrlf", "core.eol", "core.safecrlf", "core.checkroundtripencoding",
			"core.sparsecheckout", "core.sparsecheckoutcone", "index.sparse":
			section, name, _ := strings.Cut(key, ".")
			config.WriteString("[" + section + "]\n\t" + name)
			if hasValue {
				config.WriteString(` = "`)
				_, _ = quote.WriteString(&config, value)
				config.WriteByte('"')
			}
			config.WriteByte('\n')
		}
	}
	return config.String(), nil
}

func statusRemote(ctx context.Context, git shareGit, head string, observed *SyncState) *StatusRemote {
	if observed == nil {
		return nil
	}
	cache := &StatusRemote{Head: observed.RemoteHead, LastSuccess: observed.LastSuccess}
	if head == "" || cache.Head == "" {
		return cache
	}
	kind, err := git.run(ctx, nil, "cat-file", "-t", cache.Head)
	if err != nil || strings.TrimSpace(string(kind)) != "commit" {
		return cache
	}
	if head == cache.Head {
		cache.DivergenceKnown = true
		return cache
	}
	// Do not let a shallow boundary hide missing parents from the traversal.
	git.configEnv = append(git.configEnv[:len(git.configEnv):len(git.configEnv)], "GIT_SHALLOW_FILE=", "GIT_NO_LAZY_FETCH=1")
	out, err := git.run(ctx, nil, "rev-list", "--left-right", "--count", head+"..."+cache.Head)
	if err != nil {
		return cache
	}
	counts := strings.Fields(string(out))
	if len(counts) != 2 {
		return cache
	}
	ahead, aheadErr := strconv.Atoi(counts[0])
	behind, behindErr := strconv.Atoi(counts[1])
	if aheadErr == nil && behindErr == nil && ahead >= 0 && behind >= 0 {
		cache.Ahead, cache.Behind, cache.DivergenceKnown = ahead, behind, true
	}
	return cache
}
