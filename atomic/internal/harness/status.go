package harness

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// RowState is the read-only verdict for one ownership row against the native
// bytes currently on disk.
type RowState string

const (
	// RowApplied means the observed bytes are exactly what Atomic recorded
	// writing, and the desired generation is the same.
	RowApplied RowState = "applied"
	// RowStale means the observed bytes are Atomic's own recorded generation, but
	// the selected binary now projects a different one.
	RowStale RowState = "stale"
	// RowMissing means the resource the row owns is not on disk.
	RowMissing RowState = "missing"
	// RowConflict means the observed bytes match neither the recorded nor the
	// desired generation: a later edit Atomic must not overwrite.
	RowConflict RowState = "conflict"
	// RowUnverifiable means the row carries no applied digest, so nothing can be
	// proven about the bytes.
	RowUnverifiable RowState = "unverifiable"
)

// ResourceStatus is one ownership row compared against the current native
// bytes.
type ResourceStatus struct {
	Resource   string               `json:"resource"`
	Path       string               `json:"path"`
	Kind       managedfile.Kind     `json:"kind"`
	Target     string               `json:"target"`
	Generation string               `json:"generation,omitempty"`
	Tier       string               `json:"tier,omitempty"`
	Applied    string               `json:"applied,omitempty"`
	Observed   string               `json:"observed,omitempty"`
	State      RowState             `json:"state"`
	Conflict   managedfile.Conflict `json:"conflict,omitempty"`
}

// RowStatus compares one ownership row against the current native bytes. A
// stale verdict requires desired, the selected generation's digest.
func RowStatus(row installstate.Row, desired string) (ResourceStatus, error) {
	out := ResourceStatus{
		Resource:   row.Resource,
		Path:       row.Applied.Path,
		Kind:       row.Applied.Kind,
		Target:     row.Target,
		Generation: row.Generation,
		Tier:       row.Tier,
		Applied:    row.Applied.Digest,
	}
	obs, err := installstate.ObserveApplied(row.Applied)
	if err != nil {
		return out, err
	}
	out.Observed = obs.Digest
	out.Conflict = obs.Conflict

	switch {
	case obs.Conflict != managedfile.ConflictNone:
		out.State = RowConflict
	case !obs.Exists():
		out.State = RowMissing
	case row.Applied.Digest == "":
		out.State = RowUnverifiable
	case obs.Digest == row.Applied.Digest:
		if desired != "" && desired != row.Applied.Digest {
			out.State = RowStale
		} else {
			out.State = RowApplied
		}
	case desired != "" && obs.Digest == desired:
		out.State = RowStale
	default:
		out.State = RowConflict
	}
	return out, nil
}

// TargetStatus is one enrolled target with every row it owns, plus the enrolled
// record as the ledger holds it.
type TargetStatus struct {
	Target    Target           `json:"target"`
	Status    Status           `json:"status"`
	Resources []ResourceStatus `json:"resources,omitempty"`
	// Blockers are the observations the selected generation reports against this
	// target, empty when the target has no read-only projection.
	Blockers []string `json:"blockers,omitempty"`
	// Unproven names the native surfaces this target's projection cannot promise,
	// each as "surface: evidence".
	Unproven []string `json:"unproven,omitempty"`
	// Left names the user's files kept where Atomic would deliver a resource, on
	// any harness: a Claude artifact, an agent, or a Codex hooks.json.
	Left []string `json:"left,omitempty"`
}

// Resources merges ledger rows and discovered instances into one record per
// physical resource, keeping consumers separate from mere visibility.
func Resources(ledger *installstate.Ledger, instances []Instance, sharedRoots map[Kind]string) []Resource {
	byID := map[string]*Resource{}
	var order []string
	// Claude root-relative ids repeat across roots, so the path completes the key.
	for _, row := range ledger.Rows {
		key := row.Resource + "\x00" + row.Applied.Path
		r, ok := byID[key]
		if !ok {
			r = &Resource{ID: row.Resource, Kind: row.Applied.Kind, Path: row.Applied.Path}
			byID[key] = r
			order = append(order, key)
		}
		if r.Owner == "" {
			r.Owner = row.Target
		}
		r.Consumers = appendUnique(r.Consumers, row.Target)
	}

	for _, id := range order {
		r := byID[id]
		kind, _ := kindOfTarget(r.Owner)
		shared := underDir(r.Path, sharedRoots[kind])
		for _, inst := range instances {
			if !inst.Exists || inst.Kind != kind {
				// One harness's package is not visible to another harness's
				// instance, however close the paths look.
				continue
			}
			key := Target{Kind: inst.Kind, Instance: inst.ID}.Key()
			if contains(r.Consumers, key) {
				continue
			}
			if shared || underDir(r.Path, inst.NativeRoot) {
				r.VisibleTo = appendUnique(r.VisibleTo, key)
			}
		}
	}

	out := make([]Resource, 0, len(order))
	for _, id := range order {
		r := byID[id]
		sort.Strings(r.Consumers)
		sort.Strings(r.VisibleTo)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// SharedRoots is the generated-package root per harness kind. A resource under
// one of these is a shared physical resource several instances can read.
func SharedRoots(home string) map[Kind]string {
	return map[Kind]string{
		KindClaude: config.PackageRoot(home, string(KindClaude)),
		KindOMP:    config.PackageRoot(home, string(KindOMP)),
		KindCodex:  config.PackageRoot(home, string(KindCodex)),
	}
}

// kindOfTarget recovers the harness kind from a ledger target key.
func kindOfTarget(key string) (Kind, bool) {
	t, err := ParseKey(key)
	if err != nil {
		return "", false
	}
	return t.Kind, true
}

// underDir reports whether path is directory dir itself or lies inside it.
func underDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// appendUnique appends value when it is not already present.
func appendUnique(list []string, value string) []string {
	if contains(list, value) {
		return list
	}
	return append(list, value)
}

// contains reports whether list holds value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// DesiredDigests maps each claim's resource to its selected digest. A claim
// without one is omitted so its row can never read as stale.
func DesiredDigests(p Plan) map[string]string {
	out := map[string]string{}
	for _, c := range p.Claims {
		if c.SelectedDigest == "" {
			continue
		}
		out[c.ID] = c.SelectedDigest
	}
	return out
}
