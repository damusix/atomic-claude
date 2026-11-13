package claude

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Scope is one repository or realm steering directory whose AGENTS.md and
// CLAUDE.md pair migration manages, shaped by the steering mode.
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
	// NativeRoot is the global Claude artifact root. A scope resolving to it is
	// refused, since its loader would sit beside the global steering.
	NativeRoot  string
	Target      string
	OperationID string
	Scope       Scope
	// Mode shapes the pair; empty means loader.
	Mode bundlespec.SteeringMode
	Now  func() time.Time
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
	// Notes name files the operation deliberately left as they were.
	Notes []string `json:"notes,omitempty"`
}

// MigrateScope converges one scope's steering pair. It observes the scope only
// after taking the lifecycle lock and recovering, like every lifecycle verb.
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

	return runScope(req, false)
}

// ConvertScope reshapes dir's pair to mode through the MigrateScope lock, journal,
// and ledger path, returning the paths changed and notes on files it left alone.
func ConvertScope(home, nativeRoot, dir string, mode bundlespec.SteeringMode) (changed, notes []string, err error) {
	if home == "" {
		return nil, nil, fmt.Errorf("claude: convert scope: no home")
	}
	if strings.TrimSpace(dir) == "" {
		return nil, nil, fmt.Errorf("claude: convert scope: no scope directory")
	}
	result, err := runScope(ScopeRequest{Home: home, NativeRoot: nativeRoot, Scope: Scope{Dir: dir}, Mode: mode}, true)
	return result.Applied, result.Notes, err
}

// IsNativeRoot reports whether dir is the configured Claude root or an enrolled
// Claude native root, where a steering pair would sit beside the global steering.
func IsNativeRoot(home, dir string) bool {
	if sameResolvedDir(dir, DefaultConfigDir(home)) {
		return true
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return false
	}
	return enrolledNativeRoot(ledger, dir)
}

func enrolledNativeRoot(ledger *installstate.Ledger, dir string) bool {
	for _, t := range ledger.Targets {
		if t.Harness == "claude" && sameResolvedDir(dir, t.NativeRoot) {
			return true
		}
	}
	return false
}

// runScope shapes one scope under the lifecycle lock. With reuse set it moves the
// guidance block already on disk instead of writing req.Scope.Guidance.
func runScope(req ScopeRequest, reuse bool) (ScopeResult, error) {
	if req.NativeRoot != "" && sameResolvedDir(req.Scope.Dir, req.NativeRoot) {
		return ScopeResult{}, fmt.Errorf("claude: scope %s is the global native root; refusing to write a steering pair there", req.Scope.Dir)
	}

	now := req.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	operationID := req.OperationID
	if operationID == "" {
		operationID = fmt.Sprintf("scope-%d", now().UnixNano())
	}

	lock, err := installstate.AcquireLock(req.Home, installstate.WriterIdentity{OperationID: operationID})
	if err != nil {
		return ScopeResult{}, err
	}
	defer lock.Release()

	recovery, err := installstate.RecoverJournals(req.Home)
	if err != nil {
		return ScopeResult{}, err
	}
	for _, action := range recovery {
		if action.Decision == installstate.DecisionConflict {
			return ScopeResult{}, installstate.JournalConflictError(action.Path, action.Detail)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(req.Home))
	if err != nil {
		return ScopeResult{}, err
	}
	if enrolledNativeRoot(ledger, req.Scope.Dir) {
		return ScopeResult{}, fmt.Errorf("claude: scope %s is an enrolled Claude native root; refusing to write a steering pair there", req.Scope.Dir)
	}

	result := ScopeResult{
		AgentsPath: filepath.Join(req.Scope.Dir, bundlespec.ScopeSteering.Source),
		ClaudePath: filepath.Join(req.Scope.Dir, bundlespec.ScopeSteering.ClaudeTarget),
	}
	mode := req.Mode
	if mode == "" {
		mode = bundlespec.SteeringLoader
	}
	target := req.Target
	owned := map[string]bool{}
	for _, row := range ledger.Rows {
		if row.Resource == result.AgentsPath || row.Resource == result.ClaudePath {
			owned[row.Resource] = true
			if target == "" {
				target = row.Target
			}
		}
	}
	if target == "" {
		for _, t := range ledger.Targets {
			if t.Harness == "claude" {
				target = t.Key()
				break
			}
		}
	}

	var guidanceBlock []byte
	if !reuse {
		guidanceBlock = managedfile.BlockDocument(req.Scope.Guidance)
	}
	pair, err := planPair(req.Scope.Dir, mode, guidanceBlock, owned)
	if err != nil {
		return result, err
	}
	if pair != nil {
		result.Notes = pair.notes
	}
	if pair == nil || len(pair.edits) == 0 {
		result.Status = ScopeConverged
		return result, nil
	}

	var mutations []installstate.Mutation
	byPath := map[string]fileEdit{}
	for _, e := range pair.edits {
		byPath[e.path] = e
		if !e.block || target == "" {
			continue
		}
		digest, err := managedfile.DigestResourceBytes(e.data, managedfile.KindBlock)
		if err != nil {
			return result, err
		}
		unit, err := safeUnit(req.Scope.Dir, e.path)
		if err != nil {
			return result, err
		}
		mutations = append(mutations, installstate.Mutation{
			Unit:     unit,
			Resource: e.path,
			Target:   target,
			Consumer: target,
			Kind:     managedfile.KindBlock,
			Path:     e.path,
			Intended: digest,
		})
	}

	if len(mutations) > 0 {
		tx, err := installstate.NewTransaction(req.Home, operationID, installstate.Plan{Mutations: mutations})
		if err != nil {
			return result, err
		}
		result.JournalPath = tx.JournalPath

		for _, m := range mutations {
			if _, err := tx.StageFile(m.Unit, byPath[m.Path].data, 0o644); err != nil {
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
		if _, err := installstate.CleanupOperation(req.Home, tx.ID); err != nil {
			return result, err
		}
	}

	// A file with no Atomic block left has no ledger row to keep.
	var released []string
	for _, e := range pair.edits {
		switch {
		case e.block && target != "":
			continue
		case e.remove:
			if err := os.Remove(e.path); err != nil {
				return result, fmt.Errorf("claude: remove %s: %w", e.path, err)
			}
		default:
			if err := managedfile.WriteFileAtomic(e.path, e.data, 0o644); err != nil {
				return result, fmt.Errorf("claude: write %s: %w", e.path, err)
			}
		}
		result.Applied = append(result.Applied, e.path)
		if !e.block {
			released = append(released, e.path)
		}
	}
	if len(released) > 0 {
		after, err := installstate.LoadLedger(config.LedgerPath(req.Home))
		if err != nil {
			return result, err
		}
		changed := false
		for _, path := range released {
			changed = after.RemoveRows(path) || changed
		}
		if changed {
			if err := after.Save(config.LedgerPath(req.Home)); err != nil {
				return result, err
			}
		}
	}

	if err := pair.verify(); err != nil {
		return result, err
	}

	if pair.headExisted {
		result.Status = ScopeUpdated
	} else {
		result.Status = ScopeCreated
	}
	return result, nil
}

// fileEdit is one change that brings a pair file to its mode: a managed-block
// write, or a block-less write or removal once the mode retires Atomic's block.
type fileEdit struct {
	path   string
	data   []byte
	block  bool
	remove bool
}

// pairPlan is the edits a mode needs plus what each file must hold afterwards.
type pairPlan struct {
	edits       []fileEdit
	agentsPath  string
	claudePath  string
	wantAgents  []byte // nil: Atomic's blocks must be gone
	wantClaude  []byte
	headExisted bool
	// kept lists files whose block stays because stripping it would leave an
	// empty file Atomic cannot prove it wrote; notes name them.
	kept  map[string]bool
	notes []string
}

// planPair returns the edits shaping dir's pair for mode; nil guidanceBlock reuses
// the one on disk, and only paths in owned may be removed.
func planPair(dir string, mode bundlespec.SteeringMode, guidanceBlock []byte, owned map[string]bool) (*pairPlan, error) {
	steering := bundlespec.ScopeSteering
	agentsPath := filepath.Join(dir, steering.Source)
	claudePath := filepath.Join(dir, steering.ClaudeTarget)

	agents, agentsExists, err := readIfExists(agentsPath)
	if err != nil {
		return nil, err
	}
	claude, claudeExists, err := readIfExists(claudePath)
	if err != nil {
		return nil, err
	}
	agentsShape, err := classifyShape(agentsPath, agents, agentsExists)
	if err != nil {
		return nil, err
	}
	claudeShape, err := classifyShape(claudePath, claude, claudeExists)
	if err != nil {
		return nil, err
	}

	loaderBlock := steering.LoaderDocument()
	if guidanceBlock == nil {
		switch {
		case agentsShape == shapeBlocked:
			guidanceBlock, _ = managedfile.ManagedBlock(agents)
		case claudeShape == shapeBlocked && !steering.OwnsLoader(claude):
			guidanceBlock, _ = managedfile.ManagedBlock(claude)
		default:
			return nil, nil
		}
		guidanceBlock = append([]byte{}, guidanceBlock...)
	}

	pair := &pairPlan{agentsPath: agentsPath, claudePath: claudePath}
	switch mode {
	case bundlespec.SteeringLoader:
		pair.wantAgents, pair.wantClaude = guidanceBlock, loaderBlock
		pair.headExisted = claudeExists
	case bundlespec.SteeringAgentsMD:
		pair.wantAgents = guidanceBlock
		pair.headExisted = agentsExists
	case bundlespec.SteeringClaudeMD:
		pair.wantClaude = guidanceBlock
		pair.headExisted = claudeExists
	default:
		return nil, fmt.Errorf("claude: unknown steering mode %q", mode)
	}

	for _, f := range []struct {
		path     string
		observed []byte
		shape    fileShape
		want     []byte
	}{
		{agentsPath, agents, agentsShape, pair.wantAgents},
		{claudePath, claude, claudeShape, pair.wantClaude},
	} {
		edit, changed, kept, err := settleFile(f.path, f.observed, f.shape, f.want, owned[f.path])
		if err != nil {
			return nil, err
		}
		if kept {
			if pair.kept == nil {
				pair.kept = map[string]bool{}
			}
			pair.kept[f.path] = true
			pair.notes = append(pair.notes, fmt.Sprintf("%s keeps its Atomic block: stripping it would leave an empty file Atomic cannot prove it wrote", f.path))
		}
		if changed {
			pair.edits = append(pair.edits, edit)
		}
	}
	return pair, nil
}

// settleFile makes a file hold want as its block, or none when nil. Stripping to
// an empty file removes it when owned and is skipped (kept) otherwise.
func settleFile(path string, observed []byte, shape fileShape, want []byte, owned bool) (edit fileEdit, changed, kept bool, err error) {
	if want != nil {
		merged, _, err := mergeOwned(path, observed, shape, want)
		if err != nil {
			return fileEdit{}, false, false, err
		}
		return fileEdit{path: path, data: merged, block: true}, shape == shapeAbsent || !bytes.Equal(observed, merged), false, nil
	}
	if shape != shapeBlocked {
		return fileEdit{}, false, false, nil
	}
	rest, err := managedfile.RemoveBlock(observed)
	if err != nil {
		return fileEdit{}, false, false, fmt.Errorf("claude: %s block: %w", path, err)
	}
	if len(bytes.TrimSpace(rest)) == 0 {
		if owned {
			return fileEdit{path: path, remove: true}, true, false, nil
		}
		return fileEdit{}, false, true, nil
	}
	return fileEdit{path: path, data: rest}, true, false, nil
}

// verify re-reads both files and proves the pair holds exactly the intended blocks.
func (p *pairPlan) verify() error {
	for _, f := range []struct {
		path string
		want []byte
	}{{p.agentsPath, p.wantAgents}, {p.claudePath, p.wantClaude}} {
		got, exists, err := readIfExists(f.path)
		if err != nil {
			return err
		}
		switch {
		case f.want != nil && !managedfile.BlocksEqual(got, f.want):
			return fmt.Errorf("claude: %s effective block does not match the intended steering", f.path)
		case f.want == nil && exists && !p.kept[f.path] && managedfile.HasBlock(got):
			return fmt.Errorf("claude: %s still carries an Atomic block the steering mode retires", f.path)
		}
	}
	return nil
}

// sameResolvedDir resolves symlinks so a scope reached through one cannot slip
// past the native-root refusal.
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

// mergeOwned always returns KindBlock so the ownership digest covers the block
// alone and a user prose edit is never read as an edit of Atomic's bytes.
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

// safeUnit hex-escapes a scope path into a single-segment unit name, so two
// scopes can never collide on one unit.
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
