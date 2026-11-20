package realmshare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type shareGit struct {
	dir, workTree, index string
	objectDir            string
	configEnv            []string
	userConfig           bool
	outside              bool
	noRedirects          bool
}

func localGit(dir string) shareGit { return shareGit{dir: dir} }

func (g shareGit) run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	flags := []string{"-c", "core.fsmonitor=false"}
	if g.noRedirects {
		flags = append(flags, "-c", "http.followRedirects=false")
	}
	if args[0] == "checkout" {
		flags = append(flags, "-c", "core.hooksPath="+os.DevNull, "-c", "core.symlinks=true",
			"-c", "core.protectHFS=true", "-c", "core.protectNTFS=true")
	}
	if g.outside {
		flags = append(flags, "--git-dir="+os.DevNull)
	}
	if !g.outside && args[0] != "init" {
		if g.workTree == "" {
			g.workTree = g.dir
		}
		flags = append(flags, "--git-dir="+filepath.Join(g.dir, ".git"), "--work-tree="+g.workTree)
	}
	cmd := exec.CommandContext(ctx, "git", append(flags, args...)...)
	cmd.Dir, cmd.Stdin = g.dir, stdin
	if g.workTree != "" {
		cmd.Dir = g.workTree
	}
	for _, setting := range os.Environ() {
		key, _, _ := strings.Cut(setting, "=")
		switch strings.ToUpper(key) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_NAMESPACE", "GIT_PREFIX", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
			"GIT_CEILING_DIRECTORIES", "GIT_SHALLOW_FILE", "GIT_GRAFT_FILE", "GIT_REPLACE_REF_BASE", "GIT_QUARANTINE_PATH", "GIT_TEMPLATE_DIR",
			"GIT_EXEC_PATH", "GIT_ALLOW_PROTOCOL", "GIT_PROTOCOL_FROM_USER", "GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "SSH_ASKPASS",
			"SSH_ASKPASS_REQUIRE", "GIT_OPTIONAL_LOCKS", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_SSH_VARIANT", "GCM_INTERACTIVE":
			continue
		case "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM":
			if !g.userConfig {
				continue
			}
		}
		cmd.Env = append(cmd.Env, setting)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS_REQUIRE=never", "GCM_INTERACTIVE=never",
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_GRAFT_FILE="+os.DevNull, "GIT_ALLOW_PROTOCOL=file:https:ssh")
	if !g.userConfig {
		cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	}
	if g.index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+g.index)
	}
	if g.objectDir != "" {
		cmd.Env = append(cmd.Env, "GIT_OBJECT_DIRECTORY="+g.objectDir)
	}
	cmd.Env = append(cmd.Env, g.configEnv...)
	cmd.WaitDelay = 2 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if detail := strings.TrimSpace(stderr.String() + "\n" + string(out)); detail != "" {
			return out, fmt.Errorf("git %s: %w: %s", args[0], err, detail)
		}
		return out, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

func gitExit(err error, code int) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}

func validateShareRemote(remote string) error {
	if strings.TrimSpace(remote) == remote && remote != "" && !strings.ContainsAny(remote, "\x00\r\n") {
		switch {
		case filepath.IsAbs(remote):
			return nil
		case strings.Contains(remote, "://"):
			u, err := url.Parse(remote)
			if err == nil && u.RawQuery == "" && u.Fragment == "" && u.Path != "" {
				switch u.Scheme {
				case "https":
					if u.Host != "" && u.User == nil {
						return nil
					}
				case "ssh":
					_, password := u.User.Password()
					if u.Host != "" && !password {
						return nil
					}
				case "file":
					if (u.Host == "" || u.Host == "localhost") && u.User == nil && strings.HasPrefix(u.Path, "/") {
						return nil
					}
				}
			}
		default:
			host, name, ok := strings.Cut(remote, ":")
			if ok && host != "" && !strings.HasPrefix(host, "-") && !strings.ContainsAny(host, "/\\: \t") && name != "" && !strings.HasPrefix(name, ":") {
				return nil
			}
		}
	}
	return fmt.Errorf("provide an SSH or HTTPS Git URL without credentials, or an absolute repository path")
}

func shareBranchRef(branch string) string { return "refs/heads/" + branch }

func shareRemoteRefs(ctx context.Context, home, remote string) (map[string]string, error) {
	g, err := literalRemoteGit(ctx, home, remote)
	if err != nil {
		return nil, err
	}
	out, err := g.run(ctx, nil, "ls-remote", "--", remote)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if oid, ref, ok := strings.Cut(line, "\t"); ok && gitObjectID(oid) {
			refs[ref] = oid
		} else if line != "" {
			return nil, fmt.Errorf("unexpected Git remote reference: %q", line)
		}
	}
	return refs, nil
}

func literalRemoteGit(ctx context.Context, dir, remote string) (shareGit, error) {
	g := shareGit{dir: dir, outside: true, noRedirects: true}
	if strings.HasPrefix(remote, "https://") {
		env, err := transportHTTPConfig(ctx, dir)
		if err != nil {
			return shareGit{}, err
		}
		g.configEnv = env
	}
	return g, nil
}

const joinRemoteHeads = "refs/remotes/origin/"

func transportHTTPConfig(ctx context.Context, dir string) ([]string, error) {
	// Resolve includes outside any checkout, then carry only HTTPS and credential settings.
	config := shareGit{dir: dir, outside: true, userConfig: true}
	out, err := config.run(ctx, nil, "config", "--includes", "--null", "--get-regexp", `^(http|credential)\.`)
	if gitExit(err, 1) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var env []string
	count := 0
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		key, value, explicit := strings.Cut(record, "\n")
		if strings.HasPrefix(key, "http.") {
			switch strings.ToLower(key[strings.LastIndexByte(key, '.')+1:]) {
			case "followredirects":
				continue
			case "extraheader":
				if !explicit || (value != "" && !safeTransportHeader(value)) {
					continue
				}
			}
		}
		if !explicit {
			value = "true"
		}
		index := strconv.Itoa(count)
		env = append(env, "GIT_CONFIG_KEY_"+index+"="+key, "GIT_CONFIG_VALUE_"+index+"="+value)
		count++
	}
	return append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(count)), nil
}

func safeTransportHeader(header string) bool {
	name, value, ok := strings.Cut(header, ":")
	if !ok || name == "" || strings.EqualFold(name, "Host") {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
			strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0) {
			return false
		}
	}
	for i := range len(value) {
		c := value[i]
		if (c < ' ' && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

type joinAdvertisement struct {
	headTarget string
	headOID    string
	branches   map[string]string
}

func readJoinAdvertisement(ctx context.Context, transport shareGit, remote string) (joinAdvertisement, error) {
	out, err := transport.run(ctx, nil, "ls-remote", "--quiet", "--symref", "--", remote, "HEAD", "refs/heads/*")
	if err != nil {
		return joinAdvertisement{}, err
	}
	ad := joinAdvertisement{branches: make(map[string]string)}
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		value, ref, ok := strings.Cut(line, "\t")
		target, isSymref := strings.CutPrefix(value, "ref: ")
		branch, isBranch := strings.CutPrefix(ref, "refs/heads/")
		switch {
		case ok && ref == "HEAD" && isSymref && target != "" && ad.headTarget == "":
			ad.headTarget = target
		case ok && ref == "HEAD" && gitObjectID(value) && ad.headOID == "":
			ad.headOID = value
		case ok && isBranch && branch != "" && gitObjectID(value) && ad.branches[branch] == "":
			ad.branches[branch] = value
		case ok && !isBranch && strings.HasPrefix(ref, "refs/") && gitObjectID(value):
			continue
		default:
			return joinAdvertisement{}, fmt.Errorf("unexpected Git join advertisement: %q", line)
		}
	}
	return ad, nil
}

func downloadJoin(ctx context.Context, home, dir, remote string) (string, string, error) {
	transport, err := literalRemoteGit(ctx, home, remote)
	if err != nil {
		return "", "", err
	}
	before, err := readJoinAdvertisement(ctx, transport, remote)
	if err != nil {
		return "", "", err
	}
	objectFormat := "sha1"
	if len(before.headOID) == 64 {
		objectFormat = "sha256"
	}
	for _, oid := range before.branches {
		if len(oid) == 64 {
			objectFormat = "sha256"
			break
		}
	}

	git := localGit(dir)
	if _, err := git.run(ctx, nil, "init", "--quiet", "--template=", "--object-format="+objectFormat); err != nil {
		return "", "", err
	}
	// Configure origin only after the literal URL has been fetched without remotes or rewrites.
	fetch := transport
	fetch.dir, fetch.outside = dir, false
	if _, err := fetch.run(ctx, nil, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules",
		"--no-write-fetch-head", "--", remote, "+refs/heads/*:"+joinRemoteHeads+"*"); err != nil {
		return "", "", err
	}
	after, err := readJoinAdvertisement(ctx, transport, remote)
	if err != nil {
		return "", "", err
	}
	if before.headTarget != after.headTarget || before.headOID != after.headOID || !maps.Equal(before.branches, after.branches) {
		return "", "", errJoinSnapshotChanged
	}
	branch, err := findJoinBranch(ctx, git, after)
	if err != nil {
		return "", "", err
	}
	head, err := readSyncCommitRef(ctx, git, joinRemoteHeads+branch)
	if err != nil {
		return "", "", err
	}
	if head != after.branches[branch] {
		return "", "", errJoinSnapshotChanged
	}
	if _, err := git.run(ctx, nil, "remote", "add", "--no-tags", "--", "origin", remote); err != nil {
		return "", "", err
	}
	return branch, head, nil
}

func checkoutJoin(ctx context.Context, git shareGit, branch, head string) error {
	if _, err := git.run(ctx, nil, "checkout", "--quiet", "--no-recurse-submodules",
		"-B", branch, head, "--"); err != nil {
		return err
	}
	_, err := git.run(ctx, nil, "branch", "--quiet", "--set-upstream-to="+joinRemoteHeads+branch, "--", branch)
	return err
}

var errJoinSnapshotChanged = errors.New("remote HEAD or branches changed during download; retry atomic realm join")

func findJoinBranch(ctx context.Context, git shareGit, ad joinAdvertisement) (string, error) {
	out, err := git.run(ctx, nil, "for-each-ref", "--format=%(refname)%09%(objectname)%09%(symref)", joinRemoteHeads)
	if err != nil {
		return "", err
	}
	count := 0
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		ref, fields, _ := strings.Cut(line, "\t")
		branch, isBranch := strings.CutPrefix(ref, joinRemoteHeads)
		oid, symref, ok := strings.Cut(fields, "\t")
		if !ok || !isBranch || symref != "" || oid == "" || ad.branches[branch] != oid {
			return "", errJoinSnapshotChanged
		}
		count++
	}
	if count != len(ad.branches) {
		return "", errJoinSnapshotChanged
	}

	defaultBranch, _ := strings.CutPrefix(ad.headTarget, "refs/heads/")
	if oid := ad.branches[defaultBranch]; oid != "" {
		if oid != ad.headOID {
			return "", errJoinSnapshotChanged
		}
		hasManifest, err := joinHasManifest(ctx, git, oid)
		if err != nil {
			return "", err
		}
		if hasManifest {
			return defaultBranch, nil
		}
	}
	var selected string
	for branch, oid := range ad.branches {
		if branch == defaultBranch {
			continue
		}
		hasManifest, err := joinHasManifest(ctx, git, oid)
		if err != nil {
			return "", err
		}
		if !hasManifest {
			continue
		}
		if selected != "" {
			return "", fmt.Errorf("multiple branches contain %s; set the remote's default branch to the shared realm branch", manifestName)
		}
		selected = branch
	}
	if selected == "" {
		return "", fmt.Errorf("no downloaded branch contains %s", manifestName)
	}
	return selected, nil
}

func joinHasManifest(ctx context.Context, git shareGit, ref string) (bool, error) {
	out, err := git.run(ctx, nil, "ls-tree", "--name-only", "--full-tree", ref, "--", manifestName)
	return len(out) != 0, err
}

func gitObjectID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, ch := range oid {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func shareHead(ctx context.Context, g shareGit, branch string) (string, error) {
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-apply", "rebase-merge", "sequencer"} {
		if _, err := os.Lstat(filepath.Join(g.dir, ".git", name)); !os.IsNotExist(err) {
			return "", fmt.Errorf("unfinished Git operation in %q (%s); resolve it before sharing", g.dir, name)
		}
	}
	out, err := g.run(ctx, nil, "symbolic-ref", "HEAD")
	if err != nil || strings.TrimSpace(string(out)) != shareBranchRef(branch) {
		return "", fmt.Errorf("checkout at %q must be on branch %q to share", g.dir, branch)
	}
	out, err = g.run(ctx, nil, "rev-parse", "--verify", "--quiet", "HEAD")
	if gitExit(err, 1) {
		return "", nil
	}
	return strings.TrimSpace(string(out)), err
}

func shareClean(ctx context.Context, g shareGit, revision string) error {
	if revision != "" {
		if _, err := g.run(ctx, nil, "diff-index", "--cached", "--quiet", revision, "--"); err != nil {
			if !gitExit(err, 1) {
				return err
			}
			return fmt.Errorf("unrelated staged changes in %q; handle them before sharing", g.dir)
		}
	}
	out, err := g.run(ctx, nil, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return err
	}
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		if revision == "" || len(record) < 3 || record[1] != ' ' {
			return fmt.Errorf("unrelated working-tree changes in %q; handle them before sharing", g.dir)
		}
	}
	return nil
}

func checkShareBaseTree(ctx context.Context, g shareGit, base string, selections []pendingPath) error {
	spellings := make(map[string]string)
	roots := make(map[string]bool, len(selections))
	for _, selected := range selections {
		source := selected.Mapping.Source
		if err := recordPathPrefixes(spellings, source); err != nil {
			return err
		}
		roots[source] = true
	}
	out, err := g.run(ctx, nil, "ls-tree", "-r", "-t", "-z", "--full-tree", base)
	if err != nil {
		return err
	}
	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		metadata, name, ok := strings.Cut(record, "\t")
		if !ok {
			return fmt.Errorf("invalid Git tree entry at reviewed base %s", base)
		}
		selected, ok := spellings[strings.ToLower(name)]
		if !ok {
			continue
		}
		if selected != name {
			return fmt.Errorf("selection path %q aliases %q in the reviewed Git base", selected, name)
		}
		if roots[name] || !strings.HasPrefix(metadata, "040000 tree ") {
			return fmt.Errorf("selection overlaps tracked path %q in the reviewed Git base", name)
		}
	}
	return nil
}

func isolatedTransportGit(ctx context.Context, binding Binding, head string) (shareGit, func(), error) {
	if !gitObjectID(head) {
		return shareGit{}, nil, fmt.Errorf("Git transport requires a full object ID")
	}
	if err := validateShareRemote(binding.Remote); err != nil {
		return shareGit{}, nil, err
	}
	dir, err := os.MkdirTemp("", "atomic-realm-transport-")
	if err != nil {
		return shareGit{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	transport, err := literalRemoteGit(ctx, dir, binding.Remote)
	if err != nil {
		cleanup()
		return shareGit{}, nil, err
	}
	objectFormat := "sha1"
	if len(head) == 64 {
		objectFormat = "sha256"
	}
	// Use the recorded objects without loading the clone's remotes or transport config.
	transport.outside = false
	if _, err := transport.run(ctx, nil, "init", "--quiet", "--template=", "--object-format="+objectFormat); err != nil {
		cleanup()
		return shareGit{}, nil, err
	}
	transport.objectDir = filepath.Join(binding.ClonePath, ".git", "objects")
	return transport, cleanup, nil
}

func sharePush(ctx context.Context, binding Binding) error {
	pending := binding.PendingShare
	if err := verifyShareParent(ctx, localGit(binding.ClonePath), pending); err != nil {
		return err
	}
	transport, cleanup, err := isolatedTransportGit(ctx, binding, pending.Commit)
	if err != nil {
		return err
	}
	defer cleanup()
	ref := shareBranchRef(binding.Branch)
	refspec := pending.Commit + "^{commit}:" + ref
	// The raw parent check makes the lease an expected-old check, never permission to rewrite history.
	out, err := transport.run(ctx, nil, "push", "--porcelain", "--no-verify", "--no-follow-tags", "--recurse-submodules=no",
		"--force-with-lease="+ref+":"+pending.Base, "--", binding.Remote, refspec)
	if err != nil {
		return err
	}
	flag := byte(' ')
	if pending.Base == "" {
		flag = '*'
	}
	if status, _ := pushStatus(out, refspec); status != flag {
		return fmt.Errorf("Git push did not report the expected update of %q", ref)
	}
	return nil
}

func pushStatus(out []byte, refspec string) (flag byte, summary string) {
	for line := range bytes.SplitSeq(out, []byte("\n")) {
		if len(line) < 2 || line[1] != '\t' {
			continue
		}
		refs, detail, ok := bytes.Cut(line[2:], []byte("\t"))
		if !ok || flag != 0 || string(refs) != refspec {
			return 0, ""
		}
		flag, summary = line[0], string(detail)
	}
	return flag, summary
}

func verifyShareParent(ctx context.Context, g shareGit, pending *PendingShare) error {
	if pending == nil || !gitObjectID(pending.Commit) || pending.Base != "" && !gitObjectID(pending.Base) {
		return fmt.Errorf("publication requires a recorded commit and Git base")
	}
	raw, err := g.run(ctx, strings.NewReader(pending.Commit+"\n"), "cat-file", "--batch=%(objecttype)")
	if err != nil {
		return err
	}
	kind, commit, _ := bytes.Cut(raw, []byte("\n"))
	headers, _, ok := bytes.Cut(commit, []byte("\n\n"))
	tree, rest, more := bytes.Cut(headers, []byte("\n"))
	treeID, hasTree := bytes.CutPrefix(tree, []byte("tree "))
	if string(kind) != "commit" || !ok || !more || !hasTree || len(treeID) != len(pending.Commit) || !gitObjectID(string(treeID)) {
		return fmt.Errorf("recorded publication %s is not a valid commit", pending.Commit)
	}
	parentPrefix := []byte("parent ")
	if pending.Base != "" {
		var parent []byte
		parent, rest, _ = bytes.Cut(rest, []byte("\n"))
		if string(parent) != "parent "+pending.Base {
			return fmt.Errorf("recorded publication %s is not a direct extension of reviewed base %s", pending.Commit, pending.Base)
		}
		baseKind, err := g.run(ctx, nil, "cat-file", "-t", pending.Base)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(baseKind)) != "commit" {
			return fmt.Errorf("reviewed base %s is not a commit", pending.Base)
		}
	}
	for line := range bytes.SplitSeq(rest, []byte("\n")) {
		if bytes.HasPrefix(line, parentPrefix) {
			return fmt.Errorf("recorded publication %s has an unexpected parent", pending.Commit)
		}
	}
	return nil
}

func shareIgnored(ctx context.Context, realm *os.Root, paths []string) (map[string]bool, error) {
	ignored := make(map[string]bool)
	if slices.ContainsFunc(paths, func(name string) bool { return containsPath("wiki", name) }) {
		info, err := lstatBelow(realm, "wiki/.git")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && info.IsDir() {
			wiki, err := realm.OpenRoot("wiki")
			if err != nil {
				return nil, err
			}
			defer wiki.Close()
			var wikiPaths, realmPaths []string
			for _, name := range paths {
				if containsPath("wiki", name) {
					wikiPaths = append(wikiPaths, name)
				} else {
					realmPaths = append(realmPaths, name)
				}
			}
			if err := checkShareIgnores(ctx, wiki, "wiki/", wikiPaths, ignored); err != nil {
				return nil, err
			}
			paths = realmPaths
		}
	}
	return ignored, checkShareIgnores(ctx, realm, "", paths, ignored)
}

func checkShareIgnores(ctx context.Context, workTree *os.Root, prefix string, paths []string, ignored map[string]bool) error {
	if len(paths) == 0 {
		return nil
	}
	common, err := shareGitCommonDir(workTree)
	if err != nil {
		return err
	}
	if common != nil {
		defer common.Close()
	}
	temp, err := os.MkdirTemp("", "atomic-realm-ignore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	// Git evaluates patterns with disposable metadata so it never loads the source repository's config.
	gitDir := filepath.Join(temp, ".git")
	for _, name := range []string{"objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(gitDir, name), 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		return err
	}
	if common != nil {
		if err := copyShareIgnoreMetadata(ctx, common, gitDir); err != nil {
			return err
		}
	}
	var input strings.Builder
	for _, name := range paths {
		input.WriteString(strings.TrimPrefix(name, prefix))
		input.WriteByte(0)
	}
	git := shareGit{dir: temp, workTree: workTree.Name(), userConfig: true}
	out, err := git.run(ctx, strings.NewReader(input.String()), "check-ignore", "--no-index", "--stdin", "-z")
	if err != nil && !gitExit(err, 1) {
		return err
	}
	for name := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if name != "" {
			ignored[prefix+name] = true
		}
	}
	return nil
}

func copyShareIgnoreMetadata(ctx context.Context, common *os.Root, gitDir string) error {
	config, err := openRegular(common, "config")
	if err == nil {
		defer config.Close()
		parser := shareGit{dir: filepath.Dir(gitDir), outside: true}
		out, err := parser.run(ctx, config, "config", "--file", "-", "--no-includes", "--type=bool", "--get", "core.ignoreCase")
		switch {
		case err == nil:
			value := strings.TrimSuffix(string(out), "\n")
			if value != "true" && value != "false" {
				return fmt.Errorf("invalid core.ignoreCase in Git config at %q", common.Name())
			}
			if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n\tignoreCase = "+value+"\n"), 0o600); err != nil {
				return err
			}
		case !gitExit(err, 1):
			return fmt.Errorf("read core.ignoreCase from Git config at %q: %w", common.Name(), err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	exclude, err := openRegular(common, "info/exclude")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer exclude.Close()
	info := filepath.Join(gitDir, "info")
	if err := os.Mkdir(info, 0o700); err != nil {
		return err
	}
	dst, err := os.OpenFile(filepath.Join(info, "exclude"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, exclude)
	return errors.Join(err, dst.Close())
}

func shareGitCommonDir(workTree *os.Root) (*os.Root, error) {
	info, err := workTree.Lstat(".git")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	gitDir := filepath.Join(workTree.Name(), ".git")
	switch {
	case info.IsDir():
	case info.Mode().IsRegular():
		raw, err := workTree.ReadFile(".git")
		if err != nil {
			return nil, err
		}
		target, ok := strings.CutPrefix(strings.TrimRight(string(raw), "\r\n"), "gitdir: ")
		if !ok || target == "" || strings.ContainsAny(target, "\r\n\x00") {
			return nil, fmt.Errorf("invalid Git directory pointer at %q", gitDir)
		}
		gitDir = target
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(workTree.Name(), gitDir)
		}
	default:
		return nil, fmt.Errorf("Git metadata at %q must be a real directory or file", gitDir)
	}
	root, err := os.OpenRoot(gitDir)
	if err != nil {
		return nil, err
	}
	commonFile, err := openRegular(root, "commondir")
	if os.IsNotExist(err) {
		return root, nil
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	data, err := io.ReadAll(commonFile)
	if err := errors.Join(err, commonFile.Close(), root.Close()); err != nil {
		return nil, err
	}
	common := strings.TrimRight(string(data), "\r\n")
	if common == "" || strings.ContainsAny(common, "\r\n\x00") {
		return nil, fmt.Errorf("invalid Git common directory at %q", gitDir)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return os.OpenRoot(common)
}
