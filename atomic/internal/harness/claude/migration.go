package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// MigrationRequest is one Claude instruction migration: the global steering
// projection plus any repository or realm scopes the caller asked to converge.
type MigrationRequest struct {
	Home       string
	NativeRoot string
	// Target keys the ledger rows the migration records ("harness:instance").
	Target     string
	Consumer   string
	Generation string
	Tier       string
	// OperationID names the journal and transaction directories. Empty derives a
	// unique id from the clock.
	OperationID string
	// BatchDecision applies to every older-version artifact the selected
	// generation cannot prove; Decisions overrides it per artifact ID.
	BatchDecision installstate.Decision
	Decisions     map[string]installstate.Decision
	// AcknowledgeSnapshot accepts an absent or corrupt legacy snapshot, whose
	// historical restoration evidence cannot be verified.
	AcknowledgeSnapshot bool
	// Scopes are the repository and realm loader pairs to converge after the
	// global migration commits.
	Scopes []Scope
	Now    func() time.Time
}

// MigrationResult reports what the engine planned and committed.
type MigrationResult struct {
	Plan     installstate.AdoptionPlan   `json:"plan"`
	Adoption installstate.AdoptionResult `json:"adoption"`
	// Import names the mutable authorities a one-time legacy import created.
	Import MutableImport `json:"import"`
	Scopes []ScopeResult `json:"scopes,omitempty"`
}

// MutableImport names the authoritative mutable files a legacy import created.
type MutableImport struct {
	Imported []string `json:"imported,omitempty"`
}

// legacyMutableDir is where a Claude-only install kept the mutable profile and
// wiki registry before they moved to ~/.atomic.
func legacyMutableDir(nativeRoot string) string {
	return filepath.Join(nativeRoot, ".atomic")
}

// PlanMigration is the read-only migration plan. It opens no lock and writes
// nothing.
func PlanMigration(req MigrationRequest) (installstate.AdoptionPlan, error) {
	artifacts, err := GlobalArtifacts(req.Home, req.NativeRoot)
	if err != nil {
		return installstate.AdoptionPlan{}, err
	}
	plan, err := installstate.PlanAdoption(adoptionRequest(req, artifacts))
	if err != nil {
		return plan, err
	}
	// ApplyOwnedSettings runs after Migrate, so a shape it would refuse has to
	// block here, before any caller rewrites the tree.
	if err := hooks.SettingsShapeError(hooks.SettingsPathInDir(req.NativeRoot)); err != nil {
		if plan.Status != installstate.StatusBlocked && plan.Status != installstate.StatusBlockedOnRecovery {
			plan.Status = installstate.StatusBlocked
		}
		plan.Blockers = append(plan.Blockers, err.Error())
	}
	return plan, nil
}

// Migrate adopts the global install through installstate.Adopt, then converges
// each scope's loader pair as its own operation.
func Migrate(req MigrationRequest) (MigrationResult, error) {
	var result MigrationResult
	if req.Home == "" {
		return result, fmt.Errorf("claude: migrate: no home")
	}
	if req.NativeRoot == "" {
		return result, fmt.Errorf("claude: migrate: no native root")
	}
	if req.Target == "" {
		return result, fmt.Errorf("claude: migrate: no target key")
	}

	// A scope equal to the global root would put a loader pair beside the global
	// steering, so it is refused before any write.
	for _, scope := range req.Scopes {
		if sameResolvedDir(scope.Dir, req.NativeRoot) {
			return result, fmt.Errorf("claude: migrate: scope %s is the global native root", scope.Dir)
		}
	}

	artifacts, err := GlobalArtifacts(req.Home, req.NativeRoot)
	if err != nil {
		return result, err
	}
	adoption := adoptionRequest(req, artifacts)
	// The import runs inside Adopt's locked section after recovery, so a refused
	// adoption can never have written an authority.
	adoption.ImportMutable = func() ([]string, error) {
		imported, importErr := ImportMutable(req.Home, req.NativeRoot)
		return imported.Imported, importErr
	}

	plan, err := installstate.PlanAdoption(adoption)
	if err != nil {
		return result, err
	}
	result.Plan = plan

	adopted, err := installstate.Adopt(adoption)
	result.Adoption = adopted
	if err != nil {
		return result, err
	}
	result.Import = MutableImport{Imported: adopted.Imported}

	// A shared operation id would let each scope overwrite the previous scope's
	// journal, so the caller's id is suffixed per scope.
	for i, scope := range req.Scopes {
		operationID := ""
		if req.OperationID != "" {
			operationID = fmt.Sprintf("%s-scope-%d", req.OperationID, i)
		}
		mode, err := config.ResolveSteeringMode(req.Home, scope.Dir)
		if err != nil {
			return result, fmt.Errorf("claude: migrate: scope %s: %w", scope.Dir, err)
		}
		scopeResult, err := MigrateScope(ScopeRequest{
			Home:        req.Home,
			NativeRoot:  req.NativeRoot,
			Target:      req.Target,
			OperationID: operationID,
			Scope:       scope,
			Mode:        mode,
			Now:         req.Now,
		})
		if err != nil {
			return result, err
		}
		result.Scopes = append(result.Scopes, scopeResult)
	}
	return result, nil
}

// adoptionRequest maps the migration intent onto the adoption contract.
func adoptionRequest(req MigrationRequest, artifacts []installstate.Artifact) installstate.AdoptionRequest {
	return installstate.AdoptionRequest{
		Home:                req.Home,
		NativeRoot:          req.NativeRoot,
		Target:              req.Target,
		Consumer:            req.Consumer,
		Generation:          req.Generation,
		Tier:                req.Tier,
		OperationID:         req.OperationID,
		Artifacts:           artifacts,
		BatchDecision:       req.BatchDecision,
		Decisions:           req.Decisions,
		AcknowledgeSnapshot: req.AcknowledgeSnapshot,
		Now:                 req.Now,
	}
}

// ImportMutable copies a legacy <nativeRoot>/.atomic profile or wikis file into
// an absent authority. An existing authority makes the import run only once.
func ImportMutable(home, nativeRoot string) (MutableImport, error) {
	var out MutableImport
	items := []struct{ authority, legacy string }{
		{config.ProfilePath(home), filepath.Join(legacyMutableDir(nativeRoot), "profile.md")},
		{config.WikisPath(home), filepath.Join(legacyMutableDir(nativeRoot), "wikis.md")},
	}
	for _, item := range items {
		if _, err := os.Stat(item.authority); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return out, fmt.Errorf("claude: import mutable: stat %s: %w", item.authority, err)
		}
		data, err := os.ReadFile(item.legacy)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return out, fmt.Errorf("claude: import mutable: read %s: %w", item.legacy, err)
		}
		if err := managedfile.WriteFileAtomic(item.authority, data, 0o644); err != nil {
			return out, fmt.Errorf("claude: import mutable: %w", err)
		}
		out.Imported = append(out.Imported, item.authority)
	}
	return out, nil
}

// Verification is the read-only post-adoption state of one Claude install.
type Verification struct {
	Classification installstate.Classification `json:"classification"`
	// Drift names listed resources the observed bytes no longer match, including
	// an older Claude-only binary's later writes.
	Drift []string `json:"drift,omitempty"`
	// Conflicts names derivative edits to owned or authoritative bytes. They are
	// reported, never overwritten and never imported.
	Conflicts []string `json:"conflicts,omitempty"`
}

// Verify reports the current migration state read-only, including divergent
// native profile and wiki copies.
func Verify(req MigrationRequest) (Verification, error) {
	var out Verification
	if req.Home == "" || req.NativeRoot == "" {
		return out, fmt.Errorf("claude: verify: home and native root are required")
	}

	claims, err := GlobalClaims(req.Home, req.NativeRoot)
	if err != nil {
		return out, err
	}
	c, err := installstate.Classify(installstate.ClassifyRequest{
		Home:       req.Home,
		NativeRoot: req.NativeRoot,
		Target:     req.Target,
		Claims:     claims,
	})
	if err != nil {
		return out, err
	}
	out.Classification = c
	out.Drift = append(out.Drift, c.Drift...)
	out.Conflicts = append(out.Conflicts, c.Conflicts...)

	if conflict, ok, err := profile.DetectNativeConflict(req.Home, filepath.Join(legacyMutableDir(req.NativeRoot), "profile.md")); err != nil {
		return out, err
	} else if ok {
		out.Conflicts = append(out.Conflicts, fmt.Sprintf("native profile copy %s diverges from %s", conflict.Native, conflict.Authority))
	}

	if ok, err := wiki.ProjectionConflict(GlobalSteeringPath(req.NativeRoot)); err != nil {
		return out, err
	} else if ok {
		out.Conflicts = append(out.Conflicts, fmt.Sprintf("%s <wikis> block diverges from %s", GlobalSteeringPath(req.NativeRoot), config.WikisPath(req.Home)))
	}

	return out, nil
}
