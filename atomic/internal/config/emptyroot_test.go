package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// An empty root yields a bare relative segment, wrong once a record, state.dir,
// or ATOMIC_STATE_DIR moves the state root; production passes the real root.
func TestNoProductionCallPassesAnEmptyRoot(t *testing.T) {
	call := regexp.MustCompile(`\b(?:ScratchpadDir|ProjectDir|FollowupsDir|IndexDir|IndexDBPath|RepoConfigPath|RepositoryStateDir|WorktreeDir)\(\s*""`)
	var offenders []string
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "embedded" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, loc := range call.FindAllIndex(data, -1) {
			line := 1 + strings.Count(string(data[:loc[0]]), "\n")
			offenders = append(offenders, path+":"+strconv.Itoa(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("production calls pass an empty root: %v", offenders)
	}
}
