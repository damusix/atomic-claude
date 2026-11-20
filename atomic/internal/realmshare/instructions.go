package realmshare

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	privateInstructionsPath = "AGENTS.local.md"
	claudeRealmLoaderPath   = "CLAUDE.md"
	claudeLoaderStart       = "<!-- atomic:realm-private:start -->"
	claudeLoaderEnd         = "<!-- atomic:realm-private:end -->"
	claudePrivateImport     = "@" + privateInstructionsPath
	claudeLoaderBlock       = "\n\n" + claudeLoaderStart + "\n" + claudePrivateImport + "\n" + claudeLoaderEnd + "\n"
	instructionStagePrefix  = ".atomic-realm-instructions-"
	instructionStageDir     = ".claude"
	instructionCanceled     = "cancel"
	instructionInstalled    = "installed"
	instructionOwnerTag     = ":owner="
)

var claudeLoaderDigest = localDigest([]byte(claudeLoaderBlock))

var claudeLoaderParser = goldmark.DefaultParser()

// InstructionActivation records a reviewed team source and exact local Claude ownership.
type InstructionActivation struct {
	Source       string                   `toml:"source"`
	ClaudeMember string                   `toml:"claude_member,omitempty"`
	ClaudeLoader string                   `toml:"claude_loader,omitempty"`
	Pending      *pendingInstructionWrite `toml:"pending,omitempty"`
}

type instructionReview struct {
	Previous      *InstructionActivation `json:",omitempty"`
	CreatePrivate bool
	ClaudeTarget  string
	Resume        bool
	Changes       []instructionChange
	Notes         []string
}

type instructionChange struct {
	Path   string
	Before string
	After  string
	Member string
	Owner  string
	data   []byte
	mode   os.FileMode
}

type pendingInstructionWrite struct {
	Path     string `toml:"path"`
	Stage    string `toml:"stage"`
	Identity string `toml:"identity"`
	Before   string `toml:"before,omitempty"`
	After    string `toml:"after"`
	Member   string `toml:"member,omitempty"`
	Phase    string `toml:"phase,omitempty"`
}

func validateInstructionActivation(b Binding) error {
	a := b.Instructions
	if a == nil {
		return nil
	}
	if a.Source != sharedInstructionsPath || b.TrustedDigest == "" || !slices.ContainsFunc(b.Links, instructionLink) {
		return fmt.Errorf("invalid approved realm instruction source %q", a.Source)
	}
	if p := a.Pending; p != nil && ((p.Path != claudeRealmLoaderPath && p.Path != claude.RealmSettingsPath) ||
		!portablePath(p.Stage) || path.Base(p.Stage) != p.Stage || !strings.HasPrefix(p.Stage, instructionStagePrefix) ||
		p.Identity == "" || p.After == "" ||
		(p.Phase != "" && p.Phase != instructionCanceled && (p.Phase != instructionInstalled || p.Before == ""))) {
		return fmt.Errorf("invalid pending realm instruction write for %q", p.Path)
	}
	return nil
}

func instructionLink(link LinkReceipt) bool {
	return link.Path == sharedInstructionsPath && link.Source == sharedInstructionsPath
}

func claudeRealmLoaderImportsPrivate(data []byte) bool {
	return claudeLoaderImportsPrivateAt(data, -1)
}

func claudeLoaderImportsPrivateAt(data []byte, marker int) bool {
	document := claudeLoaderParser.Parse(text.NewReader(data))
	markerSeen := marker < 0
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		switch block := node.(type) {
		case *ast.HTMLBlock:
			if block.HTMLBlockType == ast.HTMLBlockType2 && block.Lines().Len() == 1 && block.Lines().At(0).Start == marker {
				markerSeen = true
			}
		case *ast.Paragraph:
			for inline := block.FirstChild(); inline != nil; inline = inline.NextSibling() {
				prose, ok := inline.(*ast.Text)
				if !ok {
					continue
				}
				start, end := prose.Segment.Start, prose.Segment.Stop
				if bytes.Contains(data[start:end], []byte("<!--")) {
					return false
				}
				if start == end || data[start] != '@' || !markerSeen || marker >= 0 && start != marker+len(claudeLoaderStart)+1 {
					continue
				}
				lineStart := bytes.LastIndexByte(data[:start], '\n') + 1
				if start-lineStart > 3 || len(bytes.Trim(data[lineStart:start], " ")) != 0 {
					continue
				}
				line, _, _ := bytes.Cut(data[start:], []byte("\n"))
				line = bytes.TrimRight(line, " \t\r")
				if start+len(line) <= end &&
					(bytes.Equal(line, []byte(claudePrivateImport)) || bytes.Equal(line, []byte("@./"+privateInstructionsPath))) {
					return true
				}
			}
		}
	}
	return false
}

func claudeRealmLoaderOwned(data []byte, digest string) bool {
	return digest == claudeLoaderDigest && bytes.Count(data, []byte(claudeLoaderBlock)) == 1 &&
		claudeLoaderImportsPrivateAt(data, bytes.Index(data, []byte(claudeLoaderBlock))+2)
}

func projectClaudeLoader(data []byte, owned string, enable bool) ([]byte, string, error) {
	if owned != "" {
		if enable {
			if !claudeRealmLoaderOwned(data, owned) {
				return nil, "", fmt.Errorf("owned private import in %s is altered or ineffective; preserve it and restore the exact active block", claudeRealmLoaderPath)
			}
			return data, owned, nil
		}
		if owned != claudeLoaderDigest || bytes.Count(data, []byte(claudeLoaderBlock)) != 1 {
			return nil, "", fmt.Errorf("owned private import in %s was edited or replaced; preserve it and restore the exact managed block", claudeRealmLoaderPath)
		}
		return bytes.Replace(data, []byte(claudeLoaderBlock), nil, 1), "", nil
	}
	if !enable {
		return data, "", nil
	}
	if bytes.Contains(data, []byte(claudeLoaderStart)) || bytes.Contains(data, []byte(claudeLoaderEnd)) {
		return nil, "", fmt.Errorf("%s contains an unowned private import marker; preserve it and resolve the collision", claudeRealmLoaderPath)
	}
	if claudeRealmLoaderImportsPrivate(data) {
		return data, "", nil
	}
	next := append(append(make([]byte, 0, len(data)+len(claudeLoaderBlock)), data...), claudeLoaderBlock...)
	if !claudeLoaderImportsPrivateAt(next, len(data)+2) {
		return nil, "", fmt.Errorf("cannot add an effective private import to %s; close any unfinished Markdown fence or comment and review again", claudeRealmLoaderPath)
	}
	return next, claudeLoaderDigest, nil
}

func instructionFile(root *os.Root, name string) ([]byte, os.FileMode, string, error) {
	info, err := lstatBelow(root, name)
	if os.IsNotExist(err) {
		return nil, 0, "", nil
	}
	if err != nil {
		return nil, 0, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, "", fmt.Errorf("local instruction file %q must be a regular file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, 0, "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, 0, "", err
	}
	if !os.SameFile(info, opened) {
		return nil, 0, "", fmt.Errorf("local instruction file %q changed while opening it", name)
	}
	identity, err := restorationIdentity(file)
	if err != nil {
		return nil, 0, "", err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, "", err
	}
	owner, err := instructionAccess(file, opened, name, false)
	if err != nil {
		return nil, 0, "", err
	}
	current, err := lstatBelow(root, name)
	if err != nil {
		return nil, 0, "", err
	}
	if !os.SameFile(info, current) || info.Mode() != current.Mode() ||
		int64(len(data)) != info.Size() || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) ||
		instructionOwnership(current, false) != owner {
		return nil, 0, "", fmt.Errorf("local instruction file %q changed while reading it", name)
	}
	mode := instructionPermissions(info)
	return data, mode, instructionFileDigest(data, mode) + ":" + identity + instructionOwnerTag + owner, nil
}

func instructionPermissions(info os.FileInfo) os.FileMode {
	return info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

func instructionReceiptOwner(receipt string) string {
	_, owner, _ := strings.Cut(receipt, instructionOwnerTag)
	return owner
}

func instructionAccess(file *os.File, info os.FileInfo, name string, directory bool) (string, error) {
	if err := instructionACL(file, directory); err != nil {
		return "", fmt.Errorf("cannot safely update %q: access metadata of %q: %w; configure Claude manually", name, file.Name(), err)
	}
	owner := instructionOwnership(info, directory)
	if owner == "" {
		return "", fmt.Errorf("cannot safely update %q: owner/group metadata of %q is unavailable; configure Claude manually", name, file.Name())
	}
	return owner, nil
}

func checkInstructionOwner(name, expected, observed string) error {
	if expected == "" || observed != expected {
		return fmt.Errorf("cannot safely update %q without changing its owner/group; preserve it and configure Claude manually", name)
	}
	return nil
}

func instructionWriteOwner(stage, destination *os.File, name, before string) (string, error) {
	dirOwner := func(dir *os.File) (string, error) {
		info, err := dir.Stat()
		if err != nil {
			return "", err
		}
		return instructionAccess(dir, info, name, true)
	}
	stageOwner, err := dirOwner(stage)
	if err != nil {
		return "", err
	}
	owner := stageOwner
	if destination != stage {
		owner, err = dirOwner(destination)
		if err != nil {
			return "", err
		}
	}
	if before != "" {
		owner = instructionReceiptOwner(before)
	}
	return owner, checkInstructionOwner(name, owner, stageOwner)
}

func instructionReviewOwner(root *os.Root, name, before string) (string, error) {
	stage, err := openShareDirectory(root, instructionStageDir)
	missing := os.IsNotExist(err)
	if missing {
		stage, err = openShareDirectory(root, ".")
	}
	if err != nil {
		return "", err
	}
	defer stage.Close()
	destination := stage
	if !missing && path.Dir(name) != instructionStageDir {
		destination, err = openShareDirectory(root, path.Dir(name))
		if err != nil {
			return "", err
		}
		defer destination.Close()
	}
	return instructionWriteOwner(stage, destination, name, before)
}

func instructionFileDigest(data []byte, mode os.FileMode) string {
	return fmt.Sprintf("%s:%03o", localDigest(data), mode)
}

func checkInstructionBefore(root *os.Root, name, before string) error {
	present, err := instructionEntryPresent(root, name, before)
	if err == nil && !present && before != "" {
		return fmt.Errorf("recorded local instruction file %q is missing; recover it before retrying", name)
	}
	return err
}

func (l *localLinks) projectInstructionFile(a InstructionActivation, name string, enable bool) (instructionChange, error) {
	readName := name
	if p := a.Pending; p != nil && p.Path == name {
		state, err := l.inspectInstructionWrite(p, false)
		if err != nil {
			return instructionChange{}, err
		}
		if state.cancel {
			if state.parked {
				readName = p.parkedPath()
			}
		} else {
			a.recordClaudeWrite(p)
			if state.staged {
				owner, err := instructionReviewOwner(l.realm, name, p.Before)
				if err != nil {
					return instructionChange{}, err
				}
				if err := checkInstructionOwner(name, owner, instructionReceiptOwner(p.Identity)); err != nil {
					return instructionChange{}, err
				}
				readName = p.stagePath()
			}
		}
	}
	data, mode, before, err := instructionFile(l.realm, readName)
	if err != nil {
		return instructionChange{}, err
	}
	var next []byte
	var receipt string
	if name == claudeRealmLoaderPath {
		next, receipt, err = projectClaudeLoader(data, a.ClaudeLoader, enable)
	} else {
		if before != "" && len(data) == 0 {
			return instructionChange{}, fmt.Errorf("%s is empty and is not valid Claude settings; preserve it and resolve the collision", name)
		}
		next, receipt, err = claude.RealmSessionStart(data, a.ClaudeMember, enable)
	}
	if err != nil {
		return instructionChange{}, fmt.Errorf("%s: %w", name, err)
	}
	if bytes.Equal(data, next) {
		return instructionChange{}, nil
	}
	if before == "" {
		mode = 0o600
	}
	owner, err := instructionReviewOwner(l.realm, name, before)
	if err != nil {
		return instructionChange{}, err
	}
	return instructionChange{Path: name, Before: before, After: instructionFileDigest(next, mode), Member: receipt, Owner: owner, data: next, mode: mode}, nil
}

func (l *localLinks) previewInstructions(team string, teamErr error) *instructionReview {
	r := &instructionReview{Previous: l.binding.Instructions}
	info, err := lstatBelow(l.realm, privateInstructionsPath)
	safePrivate := err == nil && info.Mode().IsRegular()
	if os.IsNotExist(err) {
		r.CreatePrivate = true
		safePrivate = true
		r.Notes = append(r.Notes, "Create local "+privateInstructionsPath+" (never published).")
	}
	if !safePrivate {
		r.Notes = append(r.Notes, "Private realm guidance is unavailable: "+privateInstructionsPath+" must be a local regular file; existing entry is preserved.")
	}
	if teamErr != nil {
		r.Notes = append(r.Notes, "Realm startup instructions unavailable: "+teamErr.Error())
		return r
	}
	r.Notes = append(r.Notes, "OMP and Codex delivery is limited to enrolled primary startup; Codex requires trusted startup. Model receipt is not verified by linking.")
	ledger, err := installstate.LoadLedger(config.LedgerPath(l.loc.home))
	if err != nil {
		r.Notes = append(r.Notes, "Claude realm startup unavailable: cannot read enrollment ledger: "+err.Error())
		return r
	}
	target := claude.DefaultConfigDir(l.loc.home)
	if rec, ok := ledger.FindTarget(string(harness.KindClaude), target); !ok || (rec.NativeRoot != "" && rec.NativeRoot != target) {
		r.Notes = append(r.Notes, "Claude realm startup unavailable: the configured Claude instance is not enrolled.")
		return r
	}
	if _, err := exec.LookPath("claude"); err != nil {
		r.Notes = append(r.Notes, "Claude realm startup unavailable: claude is not on PATH; repeat the instruction link once installed.")
		return r
	}
	if !safePrivate {
		return r
	}
	if _, err := claudeRealmContext(team); err != nil {
		r.Notes = append(r.Notes, "Claude realm startup unavailable: "+err.Error())
		return r
	}
	r.ClaudeTarget = target
	a := InstructionActivation{}
	if previous := r.Previous; previous != nil {
		a = *previous
		r.Resume = a.Pending != nil
	}
	for _, name := range []string{claudeRealmLoaderPath, claude.RealmSettingsPath} {
		change, err := l.projectInstructionFile(a, name, true)
		if err != nil {
			r.ClaudeTarget = ""
			r.Resume = false
			r.Changes = nil
			r.Notes = append(r.Notes, "Claude realm startup unchanged: "+err.Error())
			return r
		}
		if change.Path != "" {
			r.Changes = append(r.Changes, change)
		}
	}
	if r.Resume {
		r.Notes = append(r.Notes, "Finish the recorded local realm instruction write to "+a.Pending.Path+".")
	}
	for _, change := range r.Changes {
		if change.Path == claude.RealmSettingsPath {
			r.Notes = append(r.Notes, "Register the Atomic SessionStart hook in "+claude.RealmSettingsPath+".")
		} else {
			r.Notes = append(r.Notes, "Add the managed private @"+privateInstructionsPath+" import to "+claudeRealmLoaderPath+".")
		}
	}
	if len(r.Changes) == 0 && !r.Resume {
		r.Notes = append(r.Notes, "Claude realm startup already configured: "+claude.RealmSettingsPath+" SessionStart and local private import.")
	}
	return r
}

func (l *localLinks) saveInstructions(a *InstructionActivation) error {
	b := l.binding
	b.Instructions = a
	if err := l.loc.saveBinding(b); err != nil {
		return err
	}
	l.binding = b
	return nil
}

func (a *InstructionActivation) recordClaudeWrite(p *pendingInstructionWrite) {
	if p.Path == claudeRealmLoaderPath {
		a.ClaudeLoader = p.Member
	} else {
		a.ClaudeMember = p.Member
	}
}

func reviewTeamInstructions(ctx context.Context, clone *os.Root, source string) (team string, limitation error, digest string, err error) {
	team, limitation = realmTeamText(clone, source)
	if limitation == nil {
		return team, nil, localDigest([]byte(team)), nil
	}
	digest, err = digestSharePath(ctx, clone, source, func(entry snapshotEntry) error {
		if !entry.mode.IsRegular() {
			return fmt.Errorf("shared instruction source %q must be a regular file", source)
		}
		return nil
	})
	return "", limitation, digest, err
}

func (l *localLinks) applyInstructions(ctx context.Context, review *instructionReview, definition, behavior string) error {
	i := slices.IndexFunc(l.binding.Links, instructionLink)
	if i < 0 || l.binding.TrustedDigest != definition {
		return fmt.Errorf("cannot activate unrecorded realm instruction selection")
	}
	if _, err := localLinkWitness(l.realm, l.state, l.binding, l.binding.Links[i]); err != nil {
		return err
	}
	name, err := managedCloneName(l.loc.home, l.loc.physicalHome, l.binding.ClonePath)
	if err != nil {
		return err
	}
	clone, id, err := openWitnessedClone(l.loc.physicalHome, name, l.binding)
	if err != nil {
		return err
	}
	defer clone.Close()
	if id != l.owner.CloneID {
		return fmt.Errorf("realm instruction checkout changed; review the selection again")
	}
	data, err := readLocalRegular(clone, manifestName)
	if err != nil {
		return err
	}
	if localDigest(data) != definition {
		return fmt.Errorf("realm instruction mapping changed; review the selection again")
	}
	_, _, digest, err := reviewTeamInstructions(ctx, clone, sharedInstructionsPath)
	if err != nil {
		return err
	}
	if digest != behavior {
		return fmt.Errorf("team instructions changed; review the selection again")
	}
	if review.ClaudeTarget != "" && (review.Resume || len(review.Changes) != 0) {
		if _, err := exec.LookPath("claude"); err != nil {
			return fmt.Errorf("claude is no longer on PATH; review the instruction link again")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if review.CreatePrivate {
		if err := createPrivateInstructions(l.realm); err != nil {
			return err
		}
	}
	if l.binding.Instructions == nil {
		if err := l.saveInstructions(&InstructionActivation{Source: sharedInstructionsPath}); err != nil {
			return err
		}
	}
	if review.ClaudeTarget == "" {
		return nil
	}
	if info, err := lstatBelow(l.realm, privateInstructionsPath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is no longer a local regular file; review the selection again", privateInstructionsPath)
	}
	if review.Resume {
		if err := l.finishInstructionWrite(false); err != nil {
			return err
		}
	}
	for _, change := range review.Changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := l.writeInstructionChange(change); err != nil {
			return err
		}
	}
	return nil
}

func createPrivateInstructions(root *os.Root) error {
	file, err := root.OpenFile(privateInstructionsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create local %s without replacing it: %w", privateInstructionsPath, err)
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return err
	}
	dir, err := openShareDirectory(root, ".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (l *localLinks) removeInstructions() error {
	if l.binding.Instructions == nil {
		return nil
	}
	if p := l.binding.Instructions.Pending; p != nil {
		if err := l.finishInstructionWrite(p.Member != ""); err != nil {
			return err
		}
	}
	a := *l.binding.Instructions
	changes := make([]instructionChange, 0, 2)
	for _, name := range []string{claude.RealmSettingsPath, claudeRealmLoaderPath} {
		if name == claudeRealmLoaderPath && a.ClaudeLoader == "" || name == claude.RealmSettingsPath && a.ClaudeMember == "" {
			continue
		}
		change, err := l.projectInstructionFile(a, name, false)
		if err != nil {
			return err
		}
		if change.Path == "" {
			return fmt.Errorf("owned realm instruction entry at %q could not be removed; preserve the replacement and restore the exact entry", name)
		}
		changes = append(changes, change)
	}
	for _, change := range changes {
		if err := l.writeInstructionChange(change); err != nil {
			return err
		}
	}
	return nil
}

func (p *pendingInstructionWrite) stagePath() string {
	return path.Join(instructionStageDir, p.Stage)
}

func (p *pendingInstructionWrite) parkedPath() string {
	return p.stagePath() + ".original"
}

func (p *pendingInstructionWrite) installedReceipt() string {
	return p.After + ":" + p.Identity
}

type instructionWriteState struct {
	staged bool
	parked bool
	cancel bool
}

func instructionEntryPresent(root *os.Root, name, receipt string) (bool, error) {
	if receipt == "" {
		if _, err := lstatBelow(root, name); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, fmt.Errorf("local instruction target %q is occupied; preserve it before retrying", name)
	}
	_, _, current, err := instructionFile(root, name)
	if err != nil || current == "" {
		return false, err
	}
	if current != receipt {
		return true, fmt.Errorf("local instruction target %q changed after review; preserve it and restore the reviewed state before retrying", name)
	}
	return true, nil
}

func (l *localLinks) inspectInstructionWrite(p *pendingInstructionWrite, cancel bool) (instructionWriteState, error) {
	var s instructionWriteState
	var err error
	s.staged, err = instructionEntryPresent(l.realm, p.stagePath(), p.installedReceipt())
	if err != nil {
		return s, err
	}
	s.parked, err = instructionEntryPresent(l.realm, p.parkedPath(), p.Before)
	if err != nil {
		return s, err
	}
	s.cancel = p.Phase == instructionCanceled || (cancel && s.staged && p.Phase == "")
	if s.cancel {
		if s.parked {
			err = checkInstructionBefore(l.realm, p.Path, "")
		}
		return s, err
	}
	expected := p.installedReceipt()
	if s.staged {
		if p.Phase == instructionInstalled {
			return s, fmt.Errorf("installed instruction write to %q unexpectedly still has a stage", p.Path)
		}
		expected = p.Before
		if s.parked {
			expected = ""
		}
	} else if p.Before != "" && !s.parked && p.Phase != instructionInstalled {
		return s, fmt.Errorf("recorded replacement stage or parked original for %q is missing; recover it before retrying", p.Path)
	}
	return s, checkInstructionBefore(l.realm, p.Path, expected)
}

func (l *localLinks) writeInstructionChange(c instructionChange) error {
	if err := checkInstructionBefore(l.realm, c.Path, c.Before); err != nil {
		return err
	}
	owner, err := instructionReviewOwner(l.realm, c.Path, c.Before)
	if err != nil {
		return err
	}
	if err := checkInstructionOwner(c.Path, c.Owner, owner); err != nil {
		return err
	}
	// Interrupted edits can contain private text; stage under the excluded .claude tree.
	if err := ensureLocalParent(l.realm, instructionStageDir); err != nil {
		return err
	}
	stageRoot, err := l.realm.OpenRoot(instructionStageDir)
	if err != nil {
		return err
	}
	defer stageRoot.Close()
	dir, err := openShareDirectory(stageRoot, ".")
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := checkLocalDirectory(l.realm, instructionStageDir, dir); err != nil {
		return err
	}
	stage := instructionStagePrefix + rand.Text()
	file, err := stageRoot.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	identity, err := restorationIdentity(file)
	checkStage := func(mode os.FileMode) error {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || instructionPermissions(info) != mode {
			return fmt.Errorf("cannot safely update %q: staged file access mode does not match %v", c.Path, mode)
		}
		owner, err := instructionAccess(file, info, c.Path, false)
		if err != nil {
			return err
		}
		return checkInstructionOwner(c.Path, c.Owner, owner)
	}
	if err == nil {
		err = checkStage(0o600)
	}
	if err == nil {
		_, err = file.Write(c.data)
	}
	if err == nil {
		err = file.Chmod(c.mode)
	}
	if err == nil {
		err = checkStage(c.mode)
	}
	if err := errors.Join(err, file.Sync(), file.Close(), dir.Sync()); err != nil {
		if current, identityErr := localRestorationIdentity(stageRoot, stage); identityErr == nil && current == identity {
			return errors.Join(err, stageRoot.Remove(stage), dir.Sync())
		}
		return fmt.Errorf("cannot stage instruction write at %q; preserve the entry for review: %w", path.Join(instructionStageDir, stage), err)
	}
	if err := checkLocalDirectory(l.realm, instructionStageDir, dir); err != nil {
		return err
	}
	a := *l.binding.Instructions
	a.Pending = &pendingInstructionWrite{Path: c.Path, Stage: stage, Identity: identity + instructionOwnerTag + c.Owner, Before: c.Before, After: c.After, Member: c.Member}
	if err := l.saveInstructions(&a); err != nil {
		return err
	}
	return l.finishInstructionWrite(false)
}

func syncInstructionDirectories(first, second *os.File) error {
	if first == second {
		return first.Sync()
	}
	return syncShareDirectories(first, second)
}

func (l *localLinks) finishInstructionWrite(cancel bool) (err error) {
	a := *l.binding.Instructions
	p := *a.Pending
	defer func() {
		if err != nil {
			err = fmt.Errorf("local realm instruction write to %q is pending; preserve the destination and any retained original at %q before retrying: %w", p.Path, p.parkedPath(), err)
		}
	}()
	if err := l.syncBindingDirectory(); err != nil {
		return err
	}
	s, err := l.inspectInstructionWrite(&p, cancel)
	if err != nil {
		return err
	}
	stageRoot, err := l.realm.OpenRoot(instructionStageDir)
	if err != nil {
		return err
	}
	defer stageRoot.Close()
	stageDir, err := openShareDirectory(stageRoot, ".")
	if err != nil {
		return err
	}
	defer stageDir.Close()
	parent := path.Dir(p.Path)
	dir := stageDir
	if parent != instructionStageDir {
		dir, err = openShareDirectory(l.realm, parent)
		if err != nil {
			return err
		}
		defer dir.Close()
	}
	checkDirs := func() error {
		if err := checkLocalDirectory(l.realm, instructionStageDir, stageDir); err != nil {
			return err
		}
		if dir != stageDir {
			return checkLocalDirectory(l.realm, parent, dir)
		}
		return nil
	}
	if err := checkDirs(); err != nil {
		return err
	}
	base := path.Base(p.Path)
	parked := path.Base(p.parkedPath())
	savePhase := func(phase string) error {
		p.Phase = phase
		updated := a
		receipt := p
		updated.Pending = &receipt
		return l.saveInstructions(&updated)
	}
	if s.cancel {
		if p.Phase != instructionCanceled {
			if err := savePhase(instructionCanceled); err != nil {
				return err
			}
			if err := checkDirs(); err != nil {
				return err
			}
		}
		if s.parked {
			if err := checkInstructionBefore(stageRoot, parked, p.Before); err != nil {
				return err
			}
			if err := renameShareNoReplace(stageDir, parked, dir, base); err != nil {
				return err
			}
			if err := checkInstructionBefore(l.realm, p.Path, p.Before); err != nil {
				return err
			}
		}
		if err := syncInstructionDirectories(dir, stageDir); err != nil {
			return err
		}
		if s.staged {
			if err := checkInstructionBefore(stageRoot, p.Stage, p.installedReceipt()); err != nil {
				return err
			}
			if err := stageRoot.Remove(p.Stage); err != nil {
				return err
			}
			if err := stageDir.Sync(); err != nil {
				return err
			}
		}
	} else {
		if s.staged {
			checkAccess := func() error {
				owner, err := instructionWriteOwner(stageDir, dir, p.Path, p.Before)
				if err != nil {
					return err
				}
				return checkInstructionOwner(p.Path, owner, instructionReceiptOwner(p.Identity))
			}
			if err := checkAccess(); err != nil {
				return err
			}
			if p.Before != "" {
				if !s.parked {
					if err := renameShareNoReplace(dir, base, stageDir, parked); err != nil {
						return err
					}
					s.parked = true
				}
				if err := syncInstructionDirectories(stageDir, dir); err != nil {
					return err
				}
				if err := checkInstructionBefore(stageRoot, parked, p.Before); err != nil {
					return err
				}
			}
			if err := checkDirs(); err != nil {
				return err
			}
			if err := checkAccess(); err != nil {
				return err
			}
			if err := checkInstructionBefore(stageRoot, p.Stage, p.installedReceipt()); err != nil {
				return err
			}
			if err := renameShareNoReplace(stageDir, p.Stage, dir, base); err != nil {
				return err
			}
		}
		if err := syncInstructionDirectories(dir, stageDir); err != nil {
			return err
		}
		if err := checkDirs(); err != nil {
			return err
		}
		if err := checkInstructionBefore(l.realm, p.Path, p.installedReceipt()); err != nil {
			return err
		}
		if s.parked {
			// The durable receipt distinguishes installation from a missing original after cleanup.
			if p.Phase != instructionInstalled {
				if err := savePhase(instructionInstalled); err != nil {
					return err
				}
				if err := checkInstructionBefore(l.realm, p.Path, p.installedReceipt()); err != nil {
					return err
				}
			}
			if err := checkInstructionBefore(stageRoot, parked, p.Before); err != nil {
				return err
			}
			if err := stageRoot.Remove(parked); err != nil {
				return err
			}
			if err := stageDir.Sync(); err != nil {
				return err
			}
		}
		a.recordClaudeWrite(&p)
	}
	if err := checkDirs(); err != nil {
		return err
	}
	a.Pending = nil
	return l.saveInstructions(&a)
}
