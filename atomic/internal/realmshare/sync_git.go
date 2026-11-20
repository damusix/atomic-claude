package realmshare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func syncLocalGit(b Binding, protected bool) shareGit {
	git := localGit(b.ClonePath)
	settings := [][2]string{
		{"core.hooksPath", os.DevNull},
		{"core.protectHFS", "true"},
		{"core.protectNTFS", "true"},
		{"submodule.recurse", "false"},
		{"rerere.enabled", "false"},
		{"core.symlinks", "true"},
		{"branch." + b.Branch + ".mergeOptions", ""},
	}
	git.configEnv = []string{"GIT_LITERAL_PATHSPECS=1", "GIT_GLOB_PATHSPECS=0", "GIT_NOGLOB_PATHSPECS=0", "GIT_ICASE_PATHSPECS=0"}
	if protected {
		git.configEnv = append(git.configEnv, "GIT_ATTR_NOSYSTEM=1")
		settings = append(settings,
			[2]string{"core.attributesFile", os.DevNull},
			[2]string{"core.autocrlf", "false"},
			[2]string{"core.eol", "lf"},
			[2]string{"core.safecrlf", "false"},
			[2]string{"core.splitIndex", "false"},
			[2]string{"core.sparseCheckout", "false"},
			[2]string{"merge.renormalize", "false"},
		)
	}
	git.configEnv = append(git.configEnv, "GIT_CONFIG_COUNT="+strconv.Itoa(len(settings)))
	for i, setting := range settings {
		n := strconv.Itoa(i)
		git.configEnv = append(git.configEnv, "GIT_CONFIG_KEY_"+n+"="+setting[0], "GIT_CONFIG_VALUE_"+n+"="+setting[1])
	}
	return git
}

func checkSyncAttributeFiles(clone *os.Root) error {
	if _, present := os.LookupEnv("GIT_ATTR_SOURCE"); present {
		return fmt.Errorf("unset GIT_ATTR_SOURCE before syncing linked artifacts so Git checks the actual worktree and received attributes")
	}
	if _, err := clone.Lstat(".git/commondir"); err == nil {
		return fmt.Errorf("cannot validate Git attributes for linked artifacts with redirected .git/commondir")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := checkDirectory(clone, ".git/info"); err != nil {
		return err
	}
	return checkSyncAttributeFile(clone, ".git/info/attributes")
}

func checkSyncAttributeFile(clone *os.Root, name string) error {
	info, err := lstatBelow(clone, name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git attributes file %q must be a regular file", name)
	}
	return nil
}

func checkSyncAttributes(ctx context.Context, git shareGit, tree string, paths map[string]struct{}) error {
	if len(paths) == 0 {
		return nil
	}
	args := []string{"check-attr", "--all", "--stdin", "-z"}
	if tree != "" {
		temp, err := os.MkdirTemp("", "atomic-realm-sync-index-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		git.index = filepath.Join(temp, "index")
		if _, err := git.run(ctx, nil, "read-tree", "--no-sparse-checkout", tree); err != nil {
			return err
		}
		args = append(args, "--cached")
	}
	return visitGitAttributes(ctx, git, args, paths, func(name, attr, value string) error {
		switch attr {
		case "text", "crlf", "eol", "working-tree-encoding", "ident", "filter", "merge":
			return fmt.Errorf("Git attribute %s=%q at %q can alter protected artifact bytes; remove the conversion attributes before syncing, or unlink the affected artifact, sync and review it again before linking", attr, value, name)
		}
		return nil
	})
}

func visitGitAttributes(ctx context.Context, git shareGit, args []string, paths map[string]struct{}, visit func(name, attr, value string) error) error {
	var input strings.Builder
	for name := range paths {
		input.WriteString(name)
		input.WriteByte(0)
	}
	out, err := git.run(ctx, strings.NewReader(input.String()), args...)
	if err != nil {
		return err
	}
	for rest := string(out); rest != ""; {
		name, tail, nameOK := strings.Cut(rest, "\x00")
		attr, tail, attrOK := strings.Cut(tail, "\x00")
		value, tail, valueOK := strings.Cut(tail, "\x00")
		if _, ok := paths[name]; !ok || !nameOK || !attrOK || !valueOK {
			return fmt.Errorf("invalid Git check-attr output for %q", name)
		}
		if err := visit(name, attr, value); err != nil {
			return err
		}
		rest = tail
	}
	return nil
}

func syncPush(ctx context.Context, b Binding, head string) (updated, advanced bool, err error) {
	transport, cleanup, err := isolatedTransportGit(ctx, b, head)
	if err != nil {
		return false, false, err
	}
	defer cleanup()

	transport.configEnv = append(transport.configEnv, "LC_ALL=C")
	ref := shareBranchRef(b.Branch)
	refspec := head + "^{commit}:" + ref
	out, err := transport.run(ctx, nil, "push", "--porcelain", "--no-verify", "--no-follow-tags",
		"--recurse-submodules=no", "--", b.Remote, refspec)
	flag, summary := pushStatus(out, refspec)
	if err != nil {
		advanced := gitExit(err, 1) && flag == '!' &&
			(summary == "[rejected] (fetch first)" || summary == "[rejected] (non-fast-forward)")
		return false, advanced, err
	}
	switch flag {
	case '=', ' ', '*':
		return flag != '=', false, nil
	default:
		return false, false, fmt.Errorf("Git push did not report the expected status of %q", ref)
	}
}

func syncFetch(ctx context.Context, b Binding, head string) (remoteHead string, err error) {
	transport, cleanup, err := isolatedTransportGit(ctx, b, head)
	if err != nil {
		return "", err
	}
	defer cleanup()

	const received = "refs/atomic/sync"
	ref := shareBranchRef(b.Branch)
	if _, err := transport.run(ctx, nil, "fetch", "--no-tags", "--no-recurse-submodules",
		"--no-write-fetch-head", "--no-auto-maintenance", "--negotiation-tip="+head, "--refmap=", "--", b.Remote, ref+":"+received); err != nil {
		return "", err
	}
	out, err := transport.run(ctx, nil, "for-each-ref", "--format=%(objectname) %(objecttype)", received)
	if err != nil {
		return "", err
	}
	oid, kind, _ := strings.Cut(strings.TrimSuffix(string(out), "\n"), " ")
	if kind != "commit" || len(oid) != len(head) || !gitObjectID(oid) {
		return "", fmt.Errorf("Git fetch did not resolve %q to a commit", ref)
	}
	return oid, nil
}
