package realmshare

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

func portablePath(name string) bool {
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, `\:<>"|?*`) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		stem, _, _ := strings.Cut(part, ".")
		stem = strings.ToLower(strings.TrimRight(stem, " "))
		switch stem {
		case "aux", "con", "nul", "prn", "conin$", "conout$",
			"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "com¹", "com²", "com³",
			"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9", "lpt¹", "lpt²", "lpt³":
			return false
		}
	}
	return true
}

func containsPath(parent, name string) bool {
	return parent == "." || parent == name || strings.HasPrefix(name, parent+"/")
}

func pathsOverlap(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return containsPath(a, b) || containsPath(b, a)
}

func recordPathPrefixes(spellings map[string]string, name string) error {
	for {
		key := strings.ToLower(name)
		if previous, ok := spellings[key]; ok {
			if previous != name {
				return fmt.Errorf("case aliases %q and %q", previous, name)
			}
			return nil
		}
		spellings[key] = name
		slash := strings.LastIndexByte(name, '/')
		if slash < 0 {
			return nil
		}
		name = name[:slash]
	}
}

type directoryLinks map[string][]string

func (g directoryLinks) add(from, to string) {
	g.addDirectory(from)
	g.addDirectory(to)
	g[from] = append(g[from], to)
}

func (g directoryLinks) addDirectory(name string) {
	if _, ok := g[name]; ok {
		return
	}
	g[name] = nil
	if name != "." {
		parent := path.Dir(name)
		g.addDirectory(parent)
		g[parent] = append(g[parent], name)
	}
}

func (g directoryLinks) checkCycles() error {
	if len(g) == 0 {
		return nil
	}
	active := make(map[string]bool, len(g))
	var visit func(string) error
	visit = func(name string) error {
		if inPath, seen := active[name]; seen {
			if inPath {
				return fmt.Errorf("directory symlink cycle through %q", name)
			}
			return nil
		}
		active[name] = true
		for _, child := range g[name] {
			if err := visit(child); err != nil {
				return err
			}
		}
		active[name] = false
		return nil
	}
	return visit(".")
}

func relativeTo(base, name string) (string, bool) {
	rel, err := filepath.Rel(base, name)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func lstatBelow(root *os.Root, name string) (os.FileInfo, error) {
	for end := strings.IndexByte(name, '/'); end >= 0; {
		parent := name[:end]
		info, err := root.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: parent %q must be a real directory", root.Name(), parent)
		}
		next := strings.IndexByte(name[end+1:], '/')
		if next < 0 {
			break
		}
		end += next + 1
	}
	return root.Lstat(name)
}

func checkDirectory(root *os.Root, name string) error {
	info, err := lstatBelow(root, name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: %q must be a real directory", root.Name(), name)
	}
	return nil
}

func openRegular(root *os.Root, name string) (*os.File, error) {
	info, err := lstatBelow(root, name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %q must be a regular file", root.Name(), name)
	}
	return root.Open(name)
}

func physicalDirectory(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}
