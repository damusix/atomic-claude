package claude

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ErrCardLinkConflict reports an entry at .claude/rules/atomic that Atomic
// cannot prove is its own. It is never replaced.
var ErrCardLinkConflict = errors.New("claude: .claude/rules/atomic is not Atomic's card link")

// CardLinkPath is the repository-relative entry Claude reads the cards through.
const CardLinkPath = ".claude/rules/atomic"

// CardLinkAction is what EnsureCardLink did.
type CardLinkAction string

const (
	// CardLinkNone: nothing to serve, or the state root is .claude, whose
	// rules/wiki Claude already reads.
	CardLinkNone CardLinkAction = "none"
	// CardLinkPresent: the expected link was already there.
	CardLinkPresent CardLinkAction = "present"
	// CardLinkCreated: the link was written.
	CardLinkCreated CardLinkAction = "created"
	// CardLinkAdopted: an expected link with no ledger row was recorded, as a
	// teammate's clone of the committed link has.
	CardLinkAdopted CardLinkAction = "adopted"
	// CardLinkCopied: the state root is outside the repository, so the cards
	// were copied to an ignored .claude/rules/atomic/wiki/.
	CardLinkCopied CardLinkAction = "copied"
)

// CardLinkRequest is one repository's card link convergence.
type CardLinkRequest struct {
	Home       string
	RepoRoot   string
	StateRoot  string
	ProjectKey string
	// Targets are the enrolled Claude target keys that record ownership.
	Targets []string
	// Stage adds the link and any amended ignore file to the Git index.
	Stage bool
}

// CardLinkResult reports one convergence. Path is the link or copy root.
type CardLinkResult struct {
	Action CardLinkAction `json:"action"`
	Path   string         `json:"path"`
	// Negated is the repository ignore file a negation was appended to.
	Negated string `json:"negated,omitempty"`
	// IgnoredBy is an ignore file outside the repository, or one a negation
	// cannot override, that still hides the link.
	IgnoredBy string `json:"ignored_by,omitempty"`
	// Retired and Kept are legacy card trees removed or left in place.
	Retired []string `json:"retired,omitempty"`
	Kept    []string `json:"kept,omitempty"`
}

// EnsureCardLink makes the cards readable as .claude/rules/atomic/wiki/*.md.
// Contract: docs/spec/omp-plugin-compatibility.md.
func EnsureCardLink(req CardLinkRequest) (CardLinkResult, error) {
	repo, err := resolvedAbs(req.RepoRoot)
	if err != nil {
		return CardLinkResult{}, err
	}
	state, err := resolvedAbs(req.StateRoot)
	if err != nil {
		return CardLinkResult{}, err
	}
	link := filepath.Join(repo, filepath.FromSlash(CardLinkPath))
	result := CardLinkResult{Action: CardLinkNone, Path: link}

	ledger, release, err := lockedLedger(req.Home)
	if err != nil {
		return result, err
	}
	defer release()
	if err := retireLegacyCards(req.Home, ledger, repo, &result); err != nil {
		return result, err
	}

	rel, inside := config.RepoRelative(repo, state)
	cards := filepath.Join(state, "rules", "wiki")
	hasCards, err := isDir(cards)
	if err != nil || !hasCards || inside && rel == ".claude" || !inside && len(req.Targets) == 0 {
		return result, err
	}

	var applied installstate.AppliedValue
	if inside {
		applied, err = ensureLink(req, ledger, link, filepath.Join(state, "rules"), &result)
	} else {
		applied, err = ensureCopy(ledger, link, cards, &result)
	}
	if err != nil {
		return result, err
	}
	for _, target := range req.Targets {
		ledger.Upsert(installstate.Row{Target: target, Resource: applied.Path, Consumer: req.ProjectKey, Applied: applied})
	}
	if err := saveLedger(req.Home, ledger); err != nil {
		return result, err
	}
	if inside {
		err = exposeLink(repo, req.Stage, &result)
	}
	return result, err
}

// resolvedAbs is path made absolute with symlinks resolved, because Git
// reports resolved paths; a path that does not exist stays merely absolute.
func resolvedAbs(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func saveLedger(home string, ledger *installstate.Ledger) error {
	if home == "" {
		return nil
	}
	return ledger.Save(config.LedgerPath(home))
}

func retireLegacyCards(home string, ledger *installstate.Ledger, repo string, result *CardLinkResult) error {
	legacy := filepath.Join(repo, filepath.FromSlash(harness.LegacyClaudeCards))
	removed, kept, err := harness.RetireTree(ledger, legacy)
	if err != nil {
		return err
	}
	if kept {
		result.Kept = append(result.Kept, legacy)
	}
	if !removed {
		return nil
	}
	result.Retired = append(result.Retired, legacy)
	return saveLedger(home, ledger)
}

// lockedLedger loads the ledger under the lifecycle lock. Without a home there
// is no ledger to record into, so an empty one stands in.
func lockedLedger(home string) (*installstate.Ledger, func(), error) {
	if home == "" {
		return &installstate.Ledger{}, func() {}, nil
	}
	id := fmt.Sprintf("claude-cards-%d", time.Now().UnixNano())
	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: id})
	if err != nil {
		return nil, nil, err
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		lock.Release()
		return nil, nil, err
	}
	return ledger, func() { lock.Release() }, nil
}

func ensureLink(req CardLinkRequest, ledger *installstate.Ledger, link, rulesDir string, result *CardLinkResult) (installstate.AppliedValue, error) {
	target, err := filepath.Rel(filepath.Dir(link), rulesDir)
	if err != nil {
		return installstate.AppliedValue{}, err
	}
	applied := installstate.AppliedValue{Path: link, Kind: managedfile.KindSymlink, Digest: managedfile.Digest([]byte(target))}

	fi, err := os.Lstat(link)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return applied, err
		}
		if err := os.Symlink(target, link); err != nil {
			return applied, fmt.Errorf("claude: write card link: %w", err)
		}
		result.Action = CardLinkCreated
		return applied, nil
	case err != nil:
		return applied, err
	case fi.Mode()&os.ModeSymlink == 0:
		return applied, fmt.Errorf("%w: %s is not a symlink; move it aside", ErrCardLinkConflict, link)
	}
	got, err := os.Readlink(link)
	if err != nil {
		return applied, err
	}
	if got != target {
		return applied, fmt.Errorf("%w: %s points to %s, want %s", ErrCardLinkConflict, link, got, target)
	}
	result.Action = CardLinkPresent
	if len(req.Targets) == 0 {
		return applied, nil
	}
	for _, key := range req.Targets {
		if _, ok := ledger.Find(key, link); ok {
			return applied, nil
		}
	}
	result.Action = CardLinkAdopted
	return applied, nil
}

// ensureCopy publishes the canonical cards as a generated tree. A tree whose
// bytes are neither the projection nor what the ledger recorded is the user's.
func ensureCopy(ledger *installstate.Ledger, link, cardsDir string, result *CardLinkResult) (installstate.AppliedValue, error) {
	dir := filepath.Join(link, "wiki")
	result.Path = dir
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return installstate.AppliedValue{}, fmt.Errorf("%w: %s is a symlink, but the state root is outside the repository", ErrCardLinkConflict, link)
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		return installstate.AppliedValue{}, err
	}
	// Claude loads .claude/rules/ recursively, so a crash must not strand
	// staged cards there.
	staging, err := os.MkdirTemp(filepath.Dir(filepath.Dir(link)), ".atomic-wiki-staging-")
	if err != nil {
		return installstate.AppliedValue{}, err
	}
	defer os.RemoveAll(staging)
	if err := copyCards(cardsDir, staging); err != nil {
		return installstate.AppliedValue{}, err
	}
	want, _, err := managedfile.TreeDigest(staging)
	if err != nil {
		return installstate.AppliedValue{}, err
	}
	applied := installstate.AppliedValue{Path: dir, Kind: managedfile.KindTree, Digest: want}
	obs, err := managedfile.Observe(dir, managedfile.KindTree)
	if err != nil {
		return applied, err
	}
	if obs.Exists() && obs.Digest != want && !recordedTree(ledger, dir, obs.Digest) {
		return applied, fmt.Errorf("%w: %s holds cards Atomic did not write; move it aside", ErrCardLinkConflict, dir)
	}
	if obs.Digest != want {
		if err := os.RemoveAll(dir); err != nil {
			return applied, err
		}
		if err := os.Rename(staging, dir); err != nil {
			return applied, err
		}
	}
	result.Action = CardLinkCopied
	ignore := filepath.Join(filepath.Dir(filepath.Dir(link)), ".gitignore")
	return applied, config.AppendIgnoreRule(ignore, "/rules/atomic/")
}

func recordedTree(ledger *installstate.Ledger, path, digest string) bool {
	for _, row := range ledger.Rows {
		if row.Applied.Path == path && row.Applied.Digest == digest {
			return true
		}
	}
	return false
}

func copyCards(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// exposeLink negates a repository ignore rule that hides the link. One outside
// the repository is only reported, and an ignored link is never force-added.
func exposeLink(repo string, stage bool, result *CardLinkResult) error {
	if !isGitRepo(repo) {
		return nil
	}
	source, err := ignoreSource(repo)
	if err != nil {
		return err
	}
	var amended string
	if source != "" {
		amended, err = negate(repo, source, result)
		if err != nil || amended == "" {
			return err
		}
	}
	if !stage {
		return nil
	}
	args := []string{"add", "--", CardLinkPath}
	if amended != "" {
		args = append(args, amended)
	}
	return gitRun(repo, args...)
}

// negate undoes its append when the link stays ignored: Git cannot re-include
// a file whose parent directory is ignored.
func negate(repo, source string, result *CardLinkResult) (string, error) {
	base, inside := negationBase(repo, source)
	if !inside {
		result.IgnoredBy = source
		return "", nil
	}
	rel, err := filepath.Rel(base, filepath.Join(repo, filepath.FromSlash(CardLinkPath)))
	if err != nil {
		return "", err
	}
	before, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err := config.AppendIgnoreRule(source, "!/"+filepath.ToSlash(rel)); err != nil {
		return "", err
	}
	still, err := ignoreSource(repo)
	if err != nil {
		return "", err
	}
	if still != "" {
		result.IgnoredBy = still
		return "", os.WriteFile(source, before, 0o644)
	}
	result.Negated = source
	return source, nil
}

func isGitRepo(repo string) bool {
	return gitRun(repo, "rev-parse", "--git-dir") == nil
}

// ignoreSource returns the absolute ignore file that hides the link, or "".
func ignoreSource(repo string) (string, error) {
	source, ok, err := config.GitIgnoreSource(repo, CardLinkPath)
	if err != nil || !ok {
		return "", err
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(repo, source)
	}
	return source, nil
}

// negationBase is the directory a negation in source is relative to, and
// whether source belongs to the repository.
func negationBase(repo, source string) (string, bool) {
	if source == filepath.Join(repo, ".git", "info", "exclude") {
		return repo, true
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		resolved = source
	}
	if _, inside := config.RepoRelative(repo, resolved); !inside {
		return "", false
	}
	return filepath.Dir(source), true
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
