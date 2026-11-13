package managedfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind classifies a managed resource by the shape of the bytes Atomic owns.
type Kind string

const (
	// KindAbsent is an unobserved resource: nothing on disk to own.
	KindAbsent Kind = "absent"
	// KindFile is a whole-file resource.
	KindFile Kind = "file"
	// KindBlock is a file whose owned region is a single managed block.
	KindBlock Kind = "block"
	// KindTree is a generated directory published as one unit.
	KindTree Kind = "tree"
	// KindSettings is a JSON settings file whose owned region is the members
	// Atomic wrote. The hooks package observes it; only it knows which are owned.
	KindSettings Kind = "settings"
	// KindSymlink is a symbolic link whose owned content is its target text.
	KindSymlink Kind = "symlink"
)

// Conflict names a structural reason an observation cannot be treated as
// ownership evidence. It is empty when the observation is well-formed.
type Conflict string

const (
	ConflictNone Conflict = ""
	// ConflictMalformedBlock marks a block resource whose file does not carry
	// exactly one parseable block.
	ConflictMalformedBlock Conflict = "malformed-block"
	// ConflictMalformedSettings marks a settings resource whose file does not
	// parse, or whose hooks/SessionStart shape cannot be edited safely.
	ConflictMalformedSettings Conflict = "malformed-settings"
)

// Observation is the current native state of one managed resource. Ownership is
// never inferred from a name, only from a matching digest.
type Observation struct {
	Path   string `json:"path"`
	Kind   Kind   `json:"kind"`
	Digest string `json:"digest,omitempty"`
	// Bytes is never serialized: journals persist digests, and recovery
	// re-observes native bytes rather than trusting an old copy.
	Bytes     []byte   `json:"-"`
	Mode      uint32   `json:"mode,omitempty"`
	Ownership string   `json:"ownership"`
	Conflict  Conflict `json:"conflict,omitempty"`
}

// Ownership values recorded on an observation.
const (
	OwnershipUnowned = "unowned"
	OwnershipOwned   = "owned"
)

// Observe reads path as the given kind. A missing path is an absent observation,
// not an error.
func Observe(path string, kind Kind) (Observation, error) {
	if kind == KindSettings {
		return Observation{}, fmt.Errorf("managedfile: observe %s: a settings resource is observed member-wise by the settings layer, not as whole-file bytes", path)
	}
	obs := Observation{Path: path, Kind: kind, Ownership: OwnershipUnowned}

	if kind == KindTree {
		digest, _, err := TreeDigest(path)
		if errors.Is(err, fs.ErrNotExist) {
			obs.Kind = KindAbsent
			return obs, nil
		}
		if err != nil {
			return Observation{}, err
		}
		obs.Digest = digest
		return obs, nil
	}

	if kind == KindSymlink {
		fi, err := os.Lstat(path)
		if os.IsNotExist(err) {
			obs.Kind = KindAbsent
			return obs, nil
		}
		if err != nil {
			return Observation{}, fmt.Errorf("managedfile: observe %s: %w", path, err)
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			return Observation{}, fmt.Errorf("managedfile: observe %s: not a symlink", path)
		}
		target, err := os.Readlink(path)
		if err != nil {
			return Observation{}, fmt.Errorf("managedfile: observe %s: %w", path, err)
		}
		obs.Bytes = []byte(target)
		obs.Digest = Digest(obs.Bytes)
		return obs, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		obs.Kind = KindAbsent
		return obs, nil
	}
	if err != nil {
		return Observation{}, fmt.Errorf("managedfile: observe %s: %w", path, err)
	}
	obs.Bytes = data
	obs.Digest = Digest(data)
	if fi, statErr := os.Stat(path); statErr == nil {
		obs.Mode = uint32(fi.Mode().Perm())
	}

	if kind == KindBlock {
		block, blockErr := ManagedBlock(data)
		if blockErr != nil {
			obs.Digest = ""
			obs.Conflict = ConflictMalformedBlock
			return obs, nil
		}
		obs.Digest = Digest(block)
	}
	return obs, nil
}

// Verify reports whether the observation's digest matches expected, and marks
// ownership accordingly. An empty expected digest proves nothing.
func (o Observation) Verify(expected string) (Observation, bool) {
	if expected == "" || o.Digest == "" || o.Digest != expected {
		o.Ownership = OwnershipUnowned
		return o, false
	}
	o.Ownership = OwnershipOwned
	return o, true
}

// Exists reports whether the resource is present on disk.
func (o Observation) Exists() bool {
	return o.Kind != KindAbsent
}

// TreeEntry is one node of a digested tree, recorded in stable path order.
type TreeEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	Digest string `json:"digest,omitempty"`
	Link   string `json:"link,omitempty"`
}

// TreeDigest digests every entry under root in lexical order. The root's own mode
// is excluded: MkdirTemp and a umask-masked MkdirAll give identical trees different roots.
func TreeDigest(root string) (string, []TreeEntry, error) {
	var entries []TreeEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := TreeEntry{Path: rel, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), Size: info.Size()}
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			e.Link = target
			e.Size = 0
		case d.IsDir():
			e.Size = 0
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			e.Digest = Digest(data)
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("managedfile: digest tree %s: %w", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	h := sha256.New()
	for _, e := range entries {
		mode := e.Mode
		if e.Path == "." {
			mode = ""
		}
		io.WriteString(h, strings.Join([]string{e.Path, mode, fmt.Sprint(e.Size), e.Digest, e.Link}, "\x00"))
		io.WriteString(h, "\n")
	}
	return hex.EncodeToString(h.Sum(nil)), entries, nil
}

// DigestResourceBytes computes the ownership digest of in-memory bytes for a
// kind. KindTree is an error: its bytes are not a single buffer.
func DigestResourceBytes(data []byte, kind Kind) (string, error) {
	switch kind {
	case KindBlock:
		block, err := ManagedBlock(data)
		if err != nil {
			return "", fmt.Errorf("managedfile: block digest: %w", err)
		}
		return Digest(block), nil
	case KindTree:
		return "", fmt.Errorf("managedfile: tree bytes need TreeDigest")
	default:
		return Digest(data), nil
	}
}
