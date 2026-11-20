package realmshare

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"golang.org/x/text/unicode/norm"
)

func validateArtifactCloneScope(loc bindingLocation, clonePath string) error {
	return validateArtifactCloneStage(loc, clonePath, "")
}

func validateArtifactCloneStage(loc bindingLocation, clonePath, stage string) error {
	proofs := filepath.Join(config.Dir(loc.physicalHome), cloneProofDir)
	witness := filepath.Join(proofs, filepath.Base(clonePath))
	paths := []string{clonePath, proofs, witness}
	if stage != "" {
		paths = append(paths, stage)
	}
	namespaces := make([]artifactNamespace, len(paths))
	for i, name := range paths {
		namespace, err := readArtifactNamespace(name)
		if err != nil {
			return fmt.Errorf("cannot resolve managed clone namespace %q: %w", name, err)
		}
		namespaces[i] = namespace
	}
	profiles := []string{
		config.ClaudeConfigDir(loc.home),
		filepath.Join(loc.home, ".claude"),
		filepath.Join(loc.home, codex.DefaultHomeDir),
		filepath.Join(loc.home, filepath.Dir(filepath.FromSlash(omp.DefaultAgentDir))),
		filepath.Join(loc.home, ".agent"),
		filepath.Join(loc.home, ".agents"),
	}
	if dir, err := codex.DefaultHome(loc.home); err == nil {
		profiles = append(profiles, dir)
		if value := strings.TrimSpace(os.Getenv(codex.HomeEnv)); !filepath.IsAbs(value) {
			profiles = append(profiles, value)
		}
	}
	if dir := os.Getenv("PI_CONFIG_DIR"); dir != "" {
		profiles = append(profiles, filepath.Join(loc.home, dir))
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		for _, key := range []string{"XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
			if dir := os.Getenv(key); dir != "" {
				profiles = append(profiles, filepath.Join(dir, "omp"))
			}
		}
	}
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		profiles = append(profiles, dir)
	}
	for _, profile := range profiles {
		absolute, err := filepath.Abs(profile)
		if err != nil {
			return err
		}
		global, err := readArtifactNamespace(absolute)
		if err != nil {
			return fmt.Errorf("cannot resolve global harness profile %q: %w", absolute, err)
		}
		for i, namespace := range namespaces {
			prefix := ""
			if i == 1 {
				prefix = cloneStagingPrefix
			}
			if global.contains(namespace, "") || namespace.contains(global, prefix) {
				return fmt.Errorf("managed clone %q overlaps the global harness profile %q (checkout namespace %q)", clonePath, absolute, paths[i])
			}
		}
	}
	return nil
}

type artifactNamespace [2][]artifactNamespaceNode

type artifactNamespaceNode struct {
	name string
	info os.FileInfo
}

func readArtifactNamespace(name string) (artifactNamespace, error) {
	var namespace artifactNamespace
	absolute, err := filepath.Abs(name)
	if err != nil {
		return namespace, err
	}
	resolved, err := artifactClonePath(absolute, 0)
	if err != nil {
		return namespace, err
	}
	for i, view := range [...]string{absolute, resolved} {
		if i == 1 && view == absolute {
			break
		}
		for {
			info, err := os.Lstat(view)
			parent := filepath.Dir(view)
			if err != nil && (!os.IsNotExist(err) || view == parent) {
				return namespace, err
			}
			if info != nil && info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Stat(view)
				if err == nil {
					info = target
				} else if !os.IsNotExist(err) {
					return namespace, err
				}
			}
			namespace[i] = append(namespace[i], artifactNamespaceNode{filepath.Base(view), info})
			if view == parent {
				break
			}
			view = parent
		}
	}
	return namespace, nil
}

func (parent artifactNamespace) contains(child artifactNamespace, firstChildPrefix string) bool {
	for _, p := range parent {
		for _, c := range child {
			if artifactAncestryContains(p, c, firstChildPrefix) {
				return true
			}
		}
	}
	return false
}

func artifactAncestryContains(parent, child []artifactNamespaceNode, firstChildPrefix string) bool {
	for i, p := range parent {
		if p.info == nil {
			continue
		}
		for j := i; j < len(child); j++ {
			c := child[j]
			if c.info == nil || !os.SameFile(p.info, c.info) {
				continue
			}
			extra := j - i
			if firstChildPrefix != "" && extra > 0 && !strings.HasPrefix(strings.ToLower(child[extra-1].name), firstChildPrefix) {
				continue
			}
			matched := true
			// Compare spelling only where an entry is missing beneath a shared filesystem object.
			for k := i - 1; k >= 0; k-- {
				a, b := parent[k], child[extra+k]
				if a.info != nil && b.info != nil {
					matched = os.SameFile(a.info, b.info)
				} else {
					matched = strings.EqualFold(norm.NFC.String(a.name), norm.NFC.String(b.name))
				}
				if !matched {
					break
				}
			}
			if matched {
				return true
			}
		}
	}
	return false
}

// Resolve existing ancestors and dangling symlink targets without creating the missing tail.
func artifactClonePath(name string, links int) (string, error) {
	if links == 255 {
		return "", fmt.Errorf("too many symbolic links at %q", name)
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err == nil || !os.IsNotExist(err) || filepath.Dir(name) == name {
		return resolved, err
	}
	parent, err := artifactClonePath(filepath.Dir(name), links)
	if err != nil {
		return "", err
	}
	name = filepath.Join(parent, filepath.Base(name))
	target, err := os.Readlink(name)
	if os.IsNotExist(err) {
		return name, nil
	}
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(parent, target)
	}
	return artifactClonePath(target, links+1)
}
