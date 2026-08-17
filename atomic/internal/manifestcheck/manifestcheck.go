// Package manifestcheck compares the committed embedded manifest against what
// bundlemirror would generate from the live repo root, writing nothing.
package manifestcheck

import (
	"github.com/damusix/atomic-claude/atomic/internal/bundlemirror"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// DriftEntry is an artifact whose SHA256 differs between manifest and disk.
type DriftEntry struct {
	Target       string
	CommittedSHA string
	GeneratedSHA string
}

// Result is the output of Compare. Missing is committed-but-not-on-disk, Extra
// the reverse; OK is true only when all three lists are empty.
type Result struct {
	OK      bool
	Missing []string
	Extra   []string
	Drifted []DriftEntry
}

// Compare reuses bundlemirror.Enumerate, so the inclusion rules cannot drift
// from what the mirror actually writes. Writes nothing, spawns nothing.
func Compare(repoRoot string, committed []embedded.Artifact) (Result, error) {
	generated, err := bundlemirror.Enumerate(repoRoot)
	if err != nil {
		return Result{}, err
	}

	committedIdx := make(map[string]string, len(committed)) // target → sha256
	for _, a := range committed {
		committedIdx[a.Target] = a.SHA256
	}

	generatedIdx := make(map[string]string, len(generated)) // target → sha256
	for _, a := range generated {
		generatedIdx[a.Target] = a.SHA256
	}

	var res Result

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

	for _, c := range committed {
		if _, inGenerated := generatedIdx[c.Target]; !inGenerated {
			res.Missing = append(res.Missing, c.Target)
		}
	}

	res.OK = len(res.Missing) == 0 && len(res.Extra) == 0 && len(res.Drifted) == 0
	return res, nil
}
