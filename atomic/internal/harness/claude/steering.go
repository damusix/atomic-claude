package claude

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Scope is one repository or realm steering directory whose loader pair — a
// shared AGENTS.md carrying the Atomic-owned block and an adjacent thin Claude
// CLAUDE.md — migration manages.
type Scope struct {
	// Dir is the scope directory: a repository root, a realm root, or a nested
	// realm wiki directory.
	Dir string
	// Guidance is the Atomic-owned steering body placed inside AGENTS.md's
	// managed block.
	Guidance []byte
}

// ScopeRequest is the caller's intent for one scope.
type ScopeRequest struct {
	Home string
	// NativeRoot is the global Claude artifact root. A scope whose directory
	// resolves to it is refused: writing a scope loader pair there would put a
	// second CLAUDE.md loader beside the global steering.
	NativeRoot  string
	Target      string
	OperationID string
	Scope       Scope
	// Relocate approves moving unowned CLAUDE.md prose into AGENTS.md. Without
	// it, unowned prose stays byte-identical in CLAUDE.md and only the loader
	// block is added beside it. Either way the prose keeps its directory, so a
	// relative reference resolves to the same file.
	Relocate bool
	Now      func() time.Time
}

// ScopeStatus is the disposition of one scope migration.
type ScopeStatus string

const (
	// ScopeCreated means the pair was written where no CLAUDE.md existed.
	ScopeCreated ScopeStatus = "created"
	// ScopeUpdated means an existing pair's owned blocks were replaced and its
	// unowned prose preserved.
	ScopeUpdated ScopeStatus = "updated"
	// ScopeConverged means the scope already holds exactly the intended bytes,
	// so the operation writes no journal and takes no backup.
	ScopeConverged ScopeStatus = "converged"
)

// ScopeResult reports what one scope migration observed and wrote.
type ScopeResult struct {
	Status      ScopeStatus `json:"status"`
	AgentsPath  string      `json:"agents_path"`
	ClaudePath  string      `json:"claude_path"`
	Applied     []string    `json:"applied,omitempty"`
	JournalPath string      `json:"journal_path,omitempty"`
}

// MigrateScope creates or converges one scope's loader pair through the shared
// transaction engine: the journal and backups precede any native write, the
// owned block is spliced into observed bytes so unowned prose survives, and the
// ledger commit waits for effective-content verification. An ambiguous managed
// block on either file refuses the scope instead of guessing at a boundary.
func MigrateScope(req ScopeRequest) (ScopeResult, error) {
	if req.Home == "" {
		return ScopeResult{}, fmt.Errorf("claude: migrate scope: no home")
	}
	if req.Target == "" {
		return ScopeResult{}, fmt.Errorf("claude: migrate scope: no target key")
	}
	if strings.TrimSpace(req.Scope.Dir) == "" {
		return ScopeResult{}, fmt.Errorf("claude: migrate scope: no scope directory")
	}
	if req.NativeRoot != "" && sameResolvedDir(req.Scope.Dir, req.NativeRoot) {
		return ScopeResult{}, fmt.Errorf("claude: migrate scope: %s is the global native root; refusing to write a scope loader pair there", req.Scope.Dir)
	}

	agentsPath := filepath.Join(req.Scope.Dir, bundlespec.ScopeSteering.Source)
	claudePath := filepath.Join(req.Scope.Dir, bundlespec.ScopeSteering.ClaudeTarget)
	result := ScopeResult{AgentsPath: agentsPath, ClaudePath: claudePath}

	agents, agentsExists, err := readIfExists(agentsPath)
	if err != nil {
		return result, err
	}
	claude, claudeExists, err := readIfExists(claudePath)
	if err != nil {
		return result, err
	}

	agentsShape, err := classifyShape(agentsPath, agents, agentsExists)
	if err != nil {
		return result, err
	}
	claudeShape, err := classifyShape(claudePath, claude, claudeExists)
	if err != nil {
		return result, err
	}

	guidanceBlock := managedfile.BlockDocument(req.Scope.Guidance)
	loaderBlock := loaderDocument([]byte(bundlespec.ScopeSteering.LoaderBody()))

	agentsIntended, agentsKind, err := mergeOwned(agentsPath, agents, agentsShape, guidanceBlock)
	if err != nil {
		return result, err
	}

	var claudeIntended []byte
	claudeKind := managedfile.KindBlock
	if req.Relocate {
		prose := unownedProse(claude, claudeShape)
		if len(bytes.TrimSpace(prose)) > 0 {
			// The prose keeps the scope directory, so a relative reference keeps
			// resolving to the same file; it just lives in the shared document now.
			agentsIntended = append(append(append([]byte{}, prose...), '\n'), agentsIntended...)
			agentsKind = managedfile.KindFile
		}
		claudeIntended = loaderBlock
		// The whole file becomes the loader; the relocated prose now lives in
		// AGENTS.md, so CLAUDE.md is replaced rather than block-spliced.
		claudeKind = managedfile.KindFile
	} else {
		claudeIntended, claudeKind, err = mergeOwned(claudePath, claude, claudeShape, loaderBlock)
		if err != nil {
			return result, err
		}
	}

	type plan struct {
		path     string
		observed []byte
		exists   bool
		kind     managedfile.Kind
		intended []byte
	}
	plans := []plan{
		{path: agentsPath, observed: agents, exists: agentsExists, kind: agentsKind, intended: agentsIntended},
		{path: claudePath, observed: claude, exists: claudeExists, kind: claudeKind, intended: claudeIntended},
	}

	var mutations []installstate.Mutation
	for _, p := range plans {
		if p.exists && bytes.Equal(p.observed, p.intended) {
			continue
		}
		digest, err := managedfile.DigestResourceBytes(p.intended, p.kind)
		if err != nil {
			return result, err
		}
		unit, err := safeUnit(req.Scope.Dir, p.path)
		if err != nil {
			return result, err
		}
		mutations = append(mutations, installstate.Mutation{
			Unit:     unit,
			Resource: p.path,
			Target:   req.Target,
			Consumer: req.Target,
			Kind:     p.kind,
			Path:     p.path,
			Intended: digest,
		})
	}

	if len(mutations) == 0 {
		result.Status = ScopeConverged
		return result, nil
	}

	operationID := req.OperationID
	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if operationID == "" {
		operationID = fmt.Sprintf("scope-%d", now().UnixNano())
	}

	tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: mutations})
	if err != nil {
		return result, err
	}
	result.JournalPath = tx.JournalPath

	byPath := map[string]plan{agentsPath: plans[0], claudePath: plans[1]}
	for _, m := range mutations {
		p := byPath[m.Path]
		if _, err := tx.StageFile(m.Unit, p.intended, 0o644); err != nil {
			return result, err
		}
		if err := tx.PublishFile(m.Unit); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, m.Path)
	}
	if err := tx.Complete(); err != nil {
		return result, err
	}

	if err := verifyEffective(agentsPath, guidanceBlock, claudePath, loaderBlock); err != nil {
		return result, err
	}

	if !claudeExists {
		result.Status = ScopeCreated
	} else {
		result.Status = ScopeUpdated
	}
	return result, nil
}

// sameResolvedDir reports whether two paths name the same directory. Paths are
// compared as cleaned absolute paths, with symlinks resolved when both exist so
// a scope reached through a symlink cannot slip past the native-root refusal.
func sameResolvedDir(a, b string) bool {
	return resolvedDir(a) == resolvedDir(b)
}

// resolvedDir returns path as an absolute directory path, resolving symlinks
// when the path exists and falling back to the cleaned absolute form otherwise.
func resolvedDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// shape classifies an existing file's managed-block structure without guessing.
type fileShape int

const (
	shapeAbsent  fileShape = iota // nothing on disk
	shapeBlocked                  // exactly one parseable block
	shapePlain                    // present, no block tags at all
)

func classifyShape(path string, data []byte, exists bool) (fileShape, error) {
	if !exists {
		return shapeAbsent, nil
	}
	if managedfile.HasBlock(data) {
		return shapeBlocked, nil
	}
	if managedfile.HasBlockTags(data) {
		return 0, fmt.Errorf("claude: %s carries an ambiguous %s block; refusing to guess a boundary", path, managedfile.BlockOpen)
	}
	return shapePlain, nil
}

// loaderDocument renders the thin Claude loader as a managed block whose import
// is a top-level markdown paragraph. Claude's memory parser resolves an import
// through its markdown parse, and an import line immediately inside the block's
// tags is swallowed by the HTML block those tags open, so the body is bracketed
// by blank lines the way the authored global contract already is. Without the
// brackets the loader converges and verifies while delivering nothing.
func loaderDocument(body []byte) []byte {
	inner := strings.TrimRight(string(body), "\n")
	return []byte(managedfile.BlockOpen + "\n\n" + inner + "\n\n" + managedfile.BlockClose + "\n")
}

// mergeOwned derives the intended bytes for one file: a missing or blocked file
// takes the owned block (spliced in place when a block already exists), and a
// plain file keeps every byte and gains the block appended. Whenever the
// reconciled document carries a managed block the kind is KindBlock, so the
// ownership digest covers the block alone and a later edit to the user's prose
// is not mistaken for a derivative edit of Atomic's bytes.
func mergeOwned(path string, existing []byte, s fileShape, block []byte) ([]byte, managedfile.Kind, error) {
	switch s {
	case shapeAbsent:
		return block, managedfile.KindBlock, nil
	case shapeBlocked:
		merged, err := managedfile.ReplaceBlock(existing, block)
		if err != nil {
			return nil, "", fmt.Errorf("claude: %s block: %w", path, err)
		}
		return merged, managedfile.KindBlock, nil
	default:
		return managedfile.AppendBlock(existing, block), managedfile.KindBlock, nil
	}
}

// unownedProse returns a file's bytes with its managed block removed, so the
// prose keeps every byte the user owns and none of the bytes Atomic owns.
func unownedProse(data []byte, s fileShape) []byte {
	if s != shapeBlocked {
		return data
	}
	start, end, err := managedfile.BlockBoundaries(data)
	if err != nil {
		return data
	}
	out := append([]byte{}, data[:start]...)
	return append(out, data[end:]...)
}

// verifyEffective re-reads both files and proves the pair delivers the intended
// guidance exactly: AGENTS.md carries the guidance block, CLAUDE.md carries the
// loader block.
func verifyEffective(agentsPath string, guidanceBlock []byte, claudePath string, loaderBlock []byte) error {
	agents, err := os.ReadFile(agentsPath)
	if err != nil {
		return fmt.Errorf("claude: verify %s: %w", agentsPath, err)
	}
	if !managedfile.BlocksEqual(agents, guidanceBlock) {
		return fmt.Errorf("claude: %s effective block does not match the intended steering", agentsPath)
	}
	claude, err := os.ReadFile(claudePath)
	if err != nil {
		return fmt.Errorf("claude: verify %s: %w", claudePath, err)
	}
	if !managedfile.BlocksEqual(claude, loaderBlock) {
		return fmt.Errorf("claude: %s effective block is not the adjacent loader", claudePath)
	}
	return nil
}

// safeUnit derives a commit-unit name that survives the single-segment journal
// and staging paths: a scope path and file name with every disallowed byte
// hex-escaped, so two scopes can never collide on one unit name.
func safeUnit(dir, path string) (string, error) {
	var b strings.Builder
	for _, s := range []string{filepath.Clean(dir), filepath.Base(path)} {
		b.WriteByte('.')
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
				b.WriteByte(c)
			default:
				fmt.Fprintf(&b, ".%02x", c)
			}
		}
	}
	b.WriteByte('.')
	unit := strings.TrimLeft(b.String(), ".")
	if unit == "" || unit == "." || unit == ".." {
		return "", fmt.Errorf("claude: scope %s has no safe commit-unit name", dir)
	}
	return unit, nil
}

// readIfExists reads path, reporting whether it is present.
func readIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("claude: read %s: %w", path, err)
	}
	return data, true, nil
}
