package installstate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// State is the read-only classification of one observed installation. It
// describes structure, never verified ownership: a structurally complete legacy
// install still needs per-resource ownership decisions before adoption.
type State string

const (
	// StateAbsent means no proven Atomic ownership was observed.
	StateAbsent State = "absent"
	// StateLegacyComplete means every legacy-declared resource is present and no
	// proposal is pending.
	StateLegacyComplete State = "legacy-complete"
	// StateLegacyPartial means listed files are missing, a proposal or merge is
	// pending, the snapshot is corrupt, or a resource block is ambiguous.
	StateLegacyPartial State = "legacy-partial"
	// StateV2Clean means the ledger and native generation agree with no
	// unresolved journal.
	StateV2Clean State = "v2-clean"
	// StateV2InFlight means an unresolved journal owns staged or applied work.
	StateV2InFlight State = "v2-in-flight"
	// StateV2Orphaned means generated state has no ledger or journal owner.
	StateV2Orphaned State = "v2-orphaned"
	// StateMixed means legacy and v2 ownership evidence overlap or disagree.
	StateMixed State = "mixed"
)

// Valid reports whether s is one of the seven migration states.
func (s State) Valid() bool {
	switch s {
	case StateAbsent, StateLegacyComplete, StateLegacyPartial, StateV2Clean, StateV2InFlight, StateV2Orphaned, StateMixed:
		return true
	}
	return false
}

// WithJournalsRecovered returns the classification as it would read once the
// named unresolved journals have been recovered, so a dry run can present an
// advisory post-recovery plan without touching the filesystem.
func (c Classification) WithJournalsRecovered() Classification {
	out := c
	out.V2.Journals = nil
	// V2.inflightPaths survives deliberately: recovery reconciles the rows a
	// recovered journal owns, so they must not read as drift against the
	// pre-recovery ledger.
	out.State, out.Detail, out.Conflicts = decide(out.Legacy, out.V2, out.Resources)
	return out
}

// Resource is one inventoried native resource: a selected-generation claim
// judged against the current bytes, plus whether the legacy [install] section
// named it.
type Resource struct {
	Assessment managedfile.Assessment `json:"assessment"`
	Listed     bool                   `json:"listed,omitempty"`
}

// LegacyInventory is the read-only legacy evidence the classifier observed.
type LegacyInventory struct {
	InstallVersion string            `json:"install_version,omitempty"`
	Listed         []string          `json:"listed,omitempty"`
	ListedMissing  []string          `json:"listed_missing,omitempty"`
	Snapshot       SnapshotInventory `json:"snapshot"`
	Backups        []string          `json:"backups,omitempty"`
	Proposed       bool              `json:"proposed,omitempty"`
	Merged         bool              `json:"merged,omitempty"`
	SteeringBlock  bool              `json:"steering_block,omitempty"`
	Profile        bool              `json:"profile,omitempty"`
	Wikis          bool              `json:"wikis,omitempty"`
}

// Present reports whether any live legacy evidence exists.
func (l LegacyInventory) Present() bool {
	return l.InstallVersion != "" || len(l.Listed) > 0 || l.Snapshot.State != claudeinstall.SnapshotMissing ||
		l.Proposed || l.Merged || len(l.Backups) > 0 || l.SteeringBlock
}

// SnapshotInventory is the structural health of the preserved legacy snapshot.
type SnapshotInventory struct {
	State         claudeinstall.SnapshotState `json:"state"`
	Files         int                         `json:"files,omitempty"`
	MissingCopies []string                    `json:"missing_copies,omitempty"`
	Detail        string                      `json:"detail,omitempty"`
}

// V2Inventory is the read-only v2 operational-state evidence.
type V2Inventory struct {
	LedgerPresent     bool     `json:"ledger_present,omitempty"`
	Rows              int      `json:"rows,omitempty"`
	Targets           []string `json:"targets,omitempty"`
	Journals          []string `json:"journals,omitempty"` // unresolved, oldest-first
	CompletedJournals []string `json:"completed_journals,omitempty"`
	Transactions      []string `json:"transactions,omitempty"`
	Packages          []string `json:"packages,omitempty"`
	Selections        []string `json:"selections,omitempty"`
	Orphans           []string `json:"orphans,omitempty"`
	// Unreadable names every journal file — or the journal directory itself —
	// that exists but cannot be read. It is evidence, not noise: a journal this
	// binary cannot parse could still own native bytes, so its transaction
	// directory is not an unjournaled remnant and the state is never reported
	// absent or clean.
	Unreadable []string `json:"unreadable,omitempty"`

	// rows is the loaded ledger's ownership records, kept unexported so the
	// inventory's JSON shape stays the summary the classifier reports.
	rows []Row
	// inflightPaths is the native path set the unresolved journals intend to
	// mutate. A ledger row for one of those paths is not drift while its
	// journal is unresolved: recovery is expected to reconcile it.
	inflightPaths map[string]bool
}

// Present reports whether any v2 evidence exists.
func (v V2Inventory) Present() bool {
	return v.LedgerPresent || len(v.Journals) > 0 || len(v.CompletedJournals) > 0 ||
		len(v.Transactions) > 0 || len(v.Packages) > 0 || len(v.Unreadable) > 0
}

// SettingsInventory is the read-only Claude settings, hook, and style evidence.
type SettingsInventory struct {
	Present        bool `json:"present,omitempty"`
	OutputStyle    bool `json:"output_style,omitempty"`
	HooksInstalled bool `json:"hooks_installed,omitempty"`
	HooksDrifted   bool `json:"hooks_drifted,omitempty"`
}

// Classification is the complete read-only verdict for one installation.
type Classification struct {
	State     State             `json:"state"`
	Detail    []string          `json:"detail,omitempty"`
	Conflicts []string          `json:"conflicts,omitempty"`
	Drift     []string          `json:"drift,omitempty"`
	Resources []Resource        `json:"resources,omitempty"`
	Legacy    LegacyInventory   `json:"legacy"`
	V2        V2Inventory       `json:"v2"`
	Settings  SettingsInventory `json:"settings"`
}

// ClassifyRequest is the read-only input to Classify. NativeRoot is the legacy
// Claude artifact root (usually ~/.claude); Claims are the selected
// generation's ownership claims, which the harness layer produces from its
// adapter and this layer only judges. Target keys the ledger rows the claims
// are judged against, so Atomic's own recorded write is recognizable as
// ownership evidence.
type ClassifyRequest struct {
	Home       string
	NativeRoot string
	Target     string
	Claims     []managedfile.Claim
}

// Classify inventories every legacy and v2 evidence input and classifies the
// observed installation into one of the seven migration states. It never
// creates a file, lock, journal, backup, or enrollment; a newer unreadable
// schema is refused rather than guessed at.
func Classify(req ClassifyRequest) (Classification, error) {
	if req.Home == "" {
		return Classification{}, fmt.Errorf("installstate: classify: no home")
	}

	c := Classification{}

	legacy, err := inventoryLegacy(req)
	if err != nil {
		return Classification{}, err
	}
	c.Legacy = legacy
	c.Drift = append(c.Drift, legacy.ListedMissing...)

	v2, err := inventoryV2(req.Home)
	if err != nil {
		return Classification{}, err
	}
	c.V2 = v2
	// Settings drift and row drift are repairable rather than mixed ownership
	// evidence, so they are reported here instead of through the state verdict.
	c.Drift = append(c.Drift, rowDrift(v2)...)
	c.Drift = append(c.Drift, settingsDrift(v2)...)

	settings, err := inventorySettings(req.NativeRoot)
	if err != nil {
		return Classification{}, err
	}
	c.Settings = settings

	resources, err := assessResources(req.NativeRoot, req.Target, v2.rows, req.Claims, legacy.Listed)
	if err != nil {
		return Classification{}, err
	}
	c.Resources = resources

	c.State, c.Detail, c.Conflicts = decide(legacy, v2, resources)
	return c, nil
}

// decide resolves the seven-state table from the observed inventories.
func decide(legacy LegacyInventory, v2 V2Inventory, resources []Resource) (State, []string, []string) {
	var detail, conflicts []string

	switch {
	case !legacy.Present() && !v2.Present():
		return StateAbsent, []string{"no legacy or v2 evidence observed"}, nil

	case legacy.Present() && !v2.Present():
		return legacyState(legacy, resources)

	case !legacy.Present() && v2.Present():
		return v2State(v2)

	default:
		if len(v2.Unreadable) > 0 {
			conflicts = append(conflicts, "unreadable v2 evidence cannot be reconciled with the observed bytes: "+strings.Join(v2.Unreadable, ", "))
			return StateMixed, detail, conflicts
		}
		if len(v2.Orphans) > 0 {
			conflicts = append(conflicts, "unjournaled v2 remnant(s) overlap legacy evidence: "+strings.Join(v2.Orphans, ", "))
			return StateMixed, detail, conflicts
		}
		if v2.Rows == 0 && len(v2.Journals) == 0 {
			// A ledger with no ownership rows and no in-flight work claims nothing,
			// so the legacy classification still governs.
			state, legacyDetail, legacyConflicts := legacyState(legacy, resources)
			detail = append(detail, legacyDetail...)
			return state, append(detail, "v2 evidence makes no ownership claim"), legacyConflicts
		}
		state, v2Detail, v2Conflicts := v2State(v2)
		detail = append(detail, v2Detail...)
		if state == StateV2Clean {
			detail = append(detail, "v2 evidence agrees with the observed bytes; remaining legacy [install] artifacts converge under adoption")
		}
		return state, detail, v2Conflicts
	}
}

// legacyState classifies a legacy-only installation.
func legacyState(legacy LegacyInventory, resources []Resource) (State, []string, []string) {
	var detail, conflicts []string

	if legacy.Proposed {
		conflicts = append(conflicts, "a proposed CLAUDE.md is pending; finish, discard, or explicitly supersede it")
	}
	if legacy.Merged {
		conflicts = append(conflicts, "a CLAUDE.md.atomic-merged candidate is pending")
	}
	if len(legacy.ListedMissing) > 0 {
		conflicts = append(conflicts, fmt.Sprintf("%d listed file(s) are missing: %s", len(legacy.ListedMissing), strings.Join(legacy.ListedMissing, ", ")))
	}
	if legacy.Snapshot.State == claudeinstall.SnapshotCorrupt {
		conflicts = append(conflicts, "the legacy pre-install snapshot is corrupt: "+legacy.Snapshot.Detail)
	}
	for _, r := range resources {
		if r.Assessment.Evidence == managedfile.EvidenceConflict {
			conflicts = append(conflicts, fmt.Sprintf("%s carries an ambiguous managed block", r.Assessment.Claim.Path))
		}
	}
	if legacy.Snapshot.State == claudeinstall.SnapshotMissing {
		detail = append(detail, "no historical restoration evidence; adoption must acknowledge that limit")
	}
	if len(conflicts) > 0 {
		return StateLegacyPartial, detail, conflicts
	}
	return StateLegacyComplete, detail, conflicts
}

// v2State classifies a v2-only installation. The ledger is consulted first: an
// unreadable journal is decisive evidence, and reporting in-flight or orphaned
// work would otherwise hide it. A row whose bytes no longer match is ordinary
// per-resource drift, not a state: the resource itself carries the verdict, so
// repair can replace it and uninstall can clear it. A row the unresolved
// journals themselves intend to rewrite is not drift — recovery re-observes and
// reconciles it before any new plan.
func v2State(v2 V2Inventory) (State, []string, []string) {
	if len(v2.Unreadable) > 0 {
		return StateMixed, nil, []string{"unreadable v2 evidence: " + strings.Join(v2.Unreadable, ", ")}
	}
	switch {
	case len(v2.Journals) > 0:
		return StateV2InFlight, []string{fmt.Sprintf("%d unresolved journal(s); recover oldest-first", len(v2.Journals))}, nil
	case len(v2.Orphans) > 0:
		return StateV2Orphaned, nil, []string{"unjournaled v2 remnant(s): " + strings.Join(v2.Orphans, ", ")}
	default:
		return StateV2Clean, nil, nil
	}
}

// ObserveApplied observes the native bytes one ledger row records, dispatching
// on the applied kind. A settings row owns named JSON members of a Claude
// settings file rather than the whole file, so its observation comes from the
// settings layer; every other kind reads the path directly.
func ObserveApplied(a AppliedValue) (managedfile.Observation, error) {
	if a.Kind == managedfile.KindSettings {
		return hooks.ObserveSettingsOwnedInDir(filepath.Dir(a.Path))
	}
	return managedfile.Observe(a.Path, a.Kind)
}

// rowDrift reports every ledger row whose recorded applied value no longer
// matches the bytes on disk. A row that agrees is proof of adopted work; a row
// that disagrees — a changed file, a deleted file, or a row with no recorded
// digest — is per-resource drift the owning resource's verdict carries: a
// changed file needs a replace-or-leave-unowned decision, an absent one is
// recreated, and neither makes the install mixed. A row an unresolved journal
// intends to rewrite is excluded: its journal owns the difference and recovery
// reconciles it.
//
// A settings row is exempt from the verdict: convergence re-applies the members
// it owns on every run, so a missing or edited member is drift the next converge
// repairs, never evidence of a competing owner. It is reported through Drift
// instead, which is why settingsDrift exists.
func rowDrift(v2 V2Inventory) []string {
	var out []string
	for _, row := range v2.rows {
		if row.Applied.Kind == managedfile.KindSettings {
			continue
		}
		if disagreement, ok := ledgerRowDisagreement(row, v2.inflightPaths); ok {
			out = append(out, disagreement)
		}
	}
	return out
}

// settingsDrift reports every settings row whose owned members no longer match
// the ledger record. These are repairable drift — the next convergence rewrites
// the members — so they never classify the install mixed, but they stay visible
// to a caller reporting what changed.
func settingsDrift(v2 V2Inventory) []string {
	var out []string
	for _, row := range v2.rows {
		if row.Applied.Kind != managedfile.KindSettings {
			continue
		}
		if disagreement, ok := ledgerRowDisagreement(row, v2.inflightPaths); ok {
			out = append(out, disagreement)
		}
	}
	return out
}

// ledgerRowDisagreement reports the disagreement one ledger row carries, if any.
// A row with no applied path, or one an unresolved journal intends to rewrite,
// contributes nothing: the journal owns that difference.
func ledgerRowDisagreement(row Row, inflight map[string]bool) (string, bool) {
	if row.Applied.Path == "" || inflight[row.Applied.Path] {
		return "", false
	}
	obs, err := ObserveApplied(row.Applied)
	if err != nil {
		return fmt.Sprintf("%s is unreadable", row.Applied.Path), true
	}
	switch {
	case !obs.Exists():
		return fmt.Sprintf("%s recorded by the ledger is absent", row.Applied.Path), true
	case row.Applied.Digest == "":
		return fmt.Sprintf("%s carries no recorded applied digest", row.Applied.Path), true
	case obs.Digest != row.Applied.Digest:
		return fmt.Sprintf("%s (%s) no longer matches its ledger record", row.Applied.Path, obs.Digest), true
	}
	return "", false
}

// inventoryLegacy reads the legacy installer's evidence read-only.
func inventoryLegacy(req ClassifyRequest) (LegacyInventory, error) {
	var inv LegacyInventory

	rec, err := claudeinstall.ReadInstallRecord(req.Home)
	if err != nil {
		return inv, err
	}
	inv.InstallVersion = rec.Version
	inv.Listed = rec.Targets

	snap := claudeinstall.ReadSnapshot(req.Home)
	inv.Snapshot = SnapshotInventory{
		State:         snap.State,
		Files:         snap.Files,
		MissingCopies: snap.MissingCopies,
		Detail:        snap.Detail,
	}

	if stamps, err := config.BackupStamps(req.Home); err == nil {
		inv.Backups = stamps
	}

	if req.NativeRoot != "" {
		inv.Proposed = exists(claudeinstall.ProposedPath(req.Home))
		inv.Merged = exists(claudeinstall.MergedPath(req.NativeRoot))

		steering, err := managedfile.Observe(filepath.Join(req.NativeRoot, "CLAUDE.md"), managedfile.KindBlock)
		if err != nil {
			return inv, err
		}
		inv.SteeringBlock = steering.Exists() && steering.Conflict == managedfile.ConflictNone

		for _, target := range rec.Targets {
			if !exists(filepath.Join(req.NativeRoot, filepath.FromSlash(target))) {
				inv.ListedMissing = append(inv.ListedMissing, target)
			}
		}
	}

	inv.Profile = exists(config.ProfilePath(req.Home))
	inv.Wikis = exists(config.WikisPath(req.Home))
	return inv, nil
}

// inventoryV2 reads the v2 operational state read-only. An unreadable ledger is
// refused; an unreadable journal is recorded as evidence rather than dropped,
// because classification must not report a state owning unreadable work as
// absent or clean.
func inventoryV2(home string) (V2Inventory, error) {
	var inv V2Inventory

	ledgerPath := config.LedgerPath(home)
	inv.LedgerPresent = exists(ledgerPath)
	if inv.LedgerPresent {
		led, err := LoadLedger(ledgerPath)
		if err != nil {
			return inv, err
		}
		inv.Rows = len(led.Rows)
		inv.rows = led.Rows
		for _, t := range led.Targets {
			inv.Targets = append(inv.Targets, t.Key())
		}
	}

	journalIDs := map[string]bool{}
	inv.inflightPaths = map[string]bool{}
	journalsDir := config.JournalsDir(home)
	entries, err := journalEntries(journalsDir)
	if err != nil {
		// The journal directory itself is unreadable, so its contents are
		// unknown: that is unreadable evidence, not an absent directory.
		inv.Unreadable = append(inv.Unreadable, journalsDir)
	}
	for _, entry := range entries {
		if entry.err != nil {
			inv.Unreadable = append(inv.Unreadable, entry.path)
			// A journal that cannot be parsed still names its transaction
			// directory, so that directory is not an unjournaled remnant.
			journalIDs[strings.TrimSuffix(filepath.Base(entry.path), ".json")] = true
			continue
		}
		j := entry.loaded
		journalIDs[j.OperationID] = true
		if j.Completed {
			inv.CompletedJournals = append(inv.CompletedJournals, entry.path)
			continue
		}
		inv.Journals = append(inv.Journals, entry.path)
		for _, m := range j.Mutations {
			if m.Path != "" {
				inv.inflightPaths[m.Path] = true
			}
		}
	}

	inv.Transactions = childDirs(config.TransactionsDir(home))
	for _, tx := range inv.Transactions {
		if !journalIDs[filepath.Base(tx)] {
			inv.Orphans = append(inv.Orphans, tx)
		}
	}

	inv.Packages = packageRoots(home)
	for _, pkg := range inv.Packages {
		harness := filepath.Base(filepath.Dir(pkg))
		if !ledgerNamesHarness(inv.Targets, harness) {
			inv.Orphans = append(inv.Orphans, pkg)
		}
	}

	inv.Selections = selectionRecords(home)
	return inv, nil
}

// inventorySettings reads the Claude settings, hook registration, and output
// style evidence read-only. nativeRoot is the Claude config directory itself,
// so settings resolve from it directly rather than from a sibling `.claude`.
func inventorySettings(nativeRoot string) (SettingsInventory, error) {
	var inv SettingsInventory
	if nativeRoot == "" {
		return inv, nil
	}
	sfPath := hooks.SettingsPathInDir(nativeRoot)
	inv.Present = exists(sfPath)
	if inv.Present {
		if _, present, err := hooks.ReadOutputStyle(sfPath); err == nil {
			inv.OutputStyle = present
		}
	}
	installed, drifted, err := hooks.IsInstalledInDir(nativeRoot)
	if err == nil {
		inv.HooksInstalled, inv.HooksDrifted = installed, drifted
	}
	return inv, nil
}

// assessResources judges every selected-generation claim and marks which the
// legacy [install] section named. Each claim is first enriched with the ledger's
// recorded digest for its (target, resource), so bytes Atomic itself wrote at an
// older generation are recognized as owned rather than batched for a decision.
func assessResources(nativeRoot, target string, rows []Row, claims []managedfile.Claim, listed []string) ([]Resource, error) {
	listedSet := make(map[string]bool, len(listed))
	for _, t := range listed {
		listedSet[filepath.FromSlash(t)] = true
	}

	recorded := map[string]string{}
	for _, row := range rows {
		if row.Target != target || row.Applied.Digest == "" {
			continue
		}
		recorded[row.Resource] = row.Applied.Digest
	}

	out := make([]Resource, 0, len(claims))
	for _, claim := range claims {
		if claim.RecordedDigest == "" {
			claim.RecordedDigest = recorded[claim.ID]
		}
		a, err := managedfile.Assess(claim)
		if err != nil {
			return nil, err
		}
		var rel string
		if nativeRoot != "" {
			if r, err := filepath.Rel(nativeRoot, claim.Path); err == nil {
				rel = r
			}
		}
		out = append(out, Resource{Assessment: a, Listed: listedSet[rel] || listedSet[claim.ID]})
	}
	return out, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// childDirs lists the immediate subdirectories of dir, sorted. A missing dir is
// an empty list.
func childDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// packageRoots lists every generated harness package root under ~/.atomic/packages.
func packageRoots(home string) []string {
	root := config.PackagesDir(home)
	var out []string
	for _, harnessDir := range childDirs(root) {
		for _, pkg := range childDirs(harnessDir) {
			out = append(out, pkg)
		}
	}
	sort.Strings(out)
	return out
}

// selectionRecords lists the persisted project-state selections under ~/.atomic.
func selectionRecords(home string) []string {
	entries, err := os.ReadDir(config.Dir(home))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(config.Dir(home), e.Name(), "state-location.json")
		if exists(path) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// ledgerNamesHarness reports whether an enrolled target names harness.
func ledgerNamesHarness(targets []string, harness string) bool {
	for _, key := range targets {
		if strings.HasPrefix(key, harness+":") {
			return true
		}
	}
	return false
}
