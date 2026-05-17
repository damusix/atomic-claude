// Package manifestcheck compares the committed embedded manifest against what
// bundlemirror would generate from the live repo root, without writing any files.
// Used by check 5 in doctor and by atomic validate.
package manifestcheck

import (
	"github.com/damusix/atomic-claude/atomic/internal/bundlemirror"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// DriftEntry describes an artifact whose SHA256 differs between the committed
// manifest and the current state of the repo root.
type DriftEntry struct {
	Target       string
	CommittedSHA string
	GeneratedSHA string
}

// Result is the output of Compare.
type Result struct {
	// OK is true iff committed and generated match exactly (same set of targets,
	// all SHAs equal).
	OK bool

	// Missing contains targets present in committed but absent from disk.
	Missing []string

	// Extra contains targets present on disk but absent from committed.
	Extra []string

	// Drifted contains targets present in both but with different SHA256 values.
	Drifted []DriftEntry
}

// Compare walks repoRoot using the same inclusion rules as bundlemirror.Run
// (via bundlemirror.Enumerate) and compares the result against committed.
// No files are written. No external processes are spawned.
func Compare(repoRoot string, committed []embedded.Artifact) (Result, error) {
	generated, err := bundlemirror.Enumerate(repoRoot)
	if err != nil {
		return Result{}, err
	}

	// Index both slices by Target for O(1) lookup.
	committedIdx := make(map[string]string, len(committed)) // target → sha256
	for _, a := range committed {
		committedIdx[a.Target] = a.SHA256
	}

	generatedIdx := make(map[string]string, len(generated)) // target → sha256
	for _, a := range generated {
		generatedIdx[a.Target] = a.SHA256
	}

	var res Result

	// Find drifted and extra (in generated, check against committed).
	for _, g := range generated {
		csha, inCommitted := committedIdx[g.Target]
		if !inCommitted {
			res.Extra = append(res.Extra, g.Target)
		} else if csha != g.SHA256 {
			res.Drifted = append(res.Drifted, DriftEntry{
				Target:       g.Target,
				CommittedSHA: csha,
				GeneratedSHA: g.SHA256,
			})
		}
	}

	// Find missing (in committed, not in generated).
	for _, c := range committed {
		if _, inGenerated := generatedIdx[c.Target]; !inGenerated {
			res.Missing = append(res.Missing, c.Target)
		}
	}

	res.OK = len(res.Missing) == 0 && len(res.Extra) == 0 && len(res.Drifted) == 0
	return res, nil
}
