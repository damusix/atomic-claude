package harness

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The climb must never take the root, a user's symlink, or an occupied
// directory, since it runs on every target's native root.
func TestPruneEmptyContainers(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, root, outside string) string
		wantPruned []string
		wantKept   []string
	}{
		{
			name: "root survives a fully empty chain",
			setup: func(t *testing.T, root, _ string) string {
				mkdir(t, filepath.Join(root, "rules", "python"))
				return filepath.Join(root, "rules", "python", "x.md")
			},
			wantPruned: []string{"rules/python", "rules"},
			wantKept:   []string{"."},
		},
		{
			name: "occupied ancestor stops the climb",
			setup: func(t *testing.T, root, _ string) string {
				mkdir(t, filepath.Join(root, "rules", "python"))
				writeFile(t, filepath.Join(root, "rules", "user.md"), "user\n")
				return filepath.Join(root, "rules", "python", "x.md")
			},
			wantPruned: []string{"rules/python"},
			wantKept:   []string{"rules", "rules/user.md"},
		},
		{
			name: "symlinked ancestor stops the climb",
			setup: func(t *testing.T, root, outside string) string {
				mkdir(t, filepath.Join(outside, "python"))
				if err := os.Symlink(outside, filepath.Join(root, "rules")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(root, "rules", "python", "x.md")
			},
			wantPruned: []string{"rules/python"},
			wantKept:   []string{"rules"},
		},
		{
			name: "path outside the root prunes nothing",
			setup: func(t *testing.T, _, outside string) string {
				mkdir(t, filepath.Join(outside, "rules", "python"))
				return filepath.Join(outside, "rules", "python", "x.md")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".claude")
			mkdir(t, root)
			outside := t.TempDir()
			path := tc.setup(t, root, outside)

			var want []string
			for _, rel := range tc.wantPruned {
				want = append(want, filepath.Join(root, filepath.FromSlash(rel)))
			}
			if got := PruneEmptyContainers(path, root); !slices.Equal(got, want) {
				t.Errorf("pruned = %v, want %v", got, want)
			}
			for _, rel := range tc.wantKept {
				if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Errorf("%s did not survive: %v", rel, err)
				}
			}
			if tc.wantPruned == nil {
				if _, err := os.Stat(filepath.Dir(path)); err != nil {
					t.Errorf("a directory outside the root was removed: %v", err)
				}
			}
		})
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
