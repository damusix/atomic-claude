// The orchestrator turns a project directory into a populated DB: scan files,
// route each extension to a tree-sitter or standalone extractor, and store the
// result. See docs/spec/code-intel-engine.md.

package indexer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// extToLanguage maps lower-case extensions (dot included) to types.Language.
// An extension absent here is skipped entirely by the indexer.
var extToLanguage = map[string]types.Language{
	// TypeScript
	".ts":  types.LanguageTypeScript,
	".mts": types.LanguageTypeScript, // ESM TypeScript module
	".cts": types.LanguageTypeScript, // CJS TypeScript module
	// TSX
	".tsx": types.LanguageTSX,
	// JavaScript
	".js":      types.LanguageJavaScript,
	".mjs":     types.LanguageJavaScript, // ESM module
	".cjs":     types.LanguageJavaScript, // CJS module
	".xsjs":    types.LanguageJavaScript, // SAP XSJS
	".xsjslib": types.LanguageJavaScript, // SAP XSJS library
	// JSX
	".jsx": types.LanguageJSX,
	// Python
	".py":  types.LanguagePython,
	".pyw": types.LanguagePython,
	// Go
	".go": types.LanguageGo,
	// Rust
	".rs": types.LanguageRust,
	// Java
	".java": types.LanguageJava,
	// C — .h defaults to C; promoting to cpp/objc by content is unimplemented.
	".c": types.LanguageC,
	".h": types.LanguageC,
	// C++
	".cpp": types.LanguageCpp,
	".cc":  types.LanguageCpp,
	".cxx": types.LanguageCpp,
	".hpp": types.LanguageCpp,
	".hxx": types.LanguageCpp,
	// C#
	".cs": types.LanguageCSharp,
	// PHP
	".php":     types.LanguagePHP,
	".phtml":   types.LanguagePHP,
	".module":  types.LanguagePHP, // Drupal module
	".install": types.LanguagePHP, // Drupal install
	".theme":   types.LanguagePHP, // Drupal theme
	".inc":     types.LanguagePHP, // PHP include
	// Ruby
	".rb":   types.LanguageRuby,
	".rake": types.LanguageRuby, // Rakefile tasks
	// Swift
	".swift": types.LanguageSwift,
	// Kotlin
	".kt":  types.LanguageKotlin,
	".kts": types.LanguageKotlin, // Kotlin script
	// Dart
	".dart": types.LanguageDart,
	// Scala
	".scala": types.LanguageScala,
	".sc":    types.LanguageScala, // Scala script / worksheet
	// Lua
	".lua": types.LanguageLua,
	// Luau
	".luau": types.LanguageLuau,
	// Objective-C
	".m":  types.LanguageObjC,
	".mm": types.LanguageObjC,
	// Elixir
	".ex":  types.LanguageElixir,
	".exs": types.LanguageElixir, // Elixir script (mix.exs, config.exs, test files)
	// Erlang
	".erl": types.LanguageErlang,
	".hrl": types.LanguageErlang, // Erlang header
	// Pascal / Delphi
	".pas": types.LanguagePascal,
	".dpr": types.LanguagePascal, // Delphi project
	".dpk": types.LanguagePascal, // Delphi package
	".lpr": types.LanguagePascal, // Lazarus project
	".dfm": types.LanguagePascal, // Delphi form (standalone extractor)
	".fmx": types.LanguagePascal, // FireMonkey form (standalone extractor)
	// Svelte
	".svelte": types.LanguageSvelte,
	// Vue
	".vue": types.LanguageVue,
	// Liquid
	".liquid": types.LanguageLiquid,
	// XML (MyBatis mapper)
	".xml": types.LanguageXML,
	// SQL extensions are populated in init() from standalone.SQLExtensions.
	// File-level only (no symbol extraction)
	".yaml":       types.LanguageYAML,
	".yml":        types.LanguageYAML,
	".twig":       types.LanguageTwig,
	".properties": types.LanguageProperties,
}

// fileLevelOnly languages get a file record but no symbol extraction.
var fileLevelOnly = map[types.Language]bool{
	types.LanguageYAML:       true,
	types.LanguageTwig:       true,
	types.LanguageProperties: true,
}

// standaloneExts route to regex-based extractors instead of tree-sitter.
// SQL extensions are populated in init() from standalone.SQLExtensions.
var standaloneExts = map[string]bool{
	".vue":    true,
	".svelte": true,
	".liquid": true,
	".dfm":    true,
	".fmx":    true,
	".xml":    true,
}

func init() {
	// Derived from the standalone package's canonical list so these maps cannot
	// diverge from standalone.NewRegistry's SQL routing.
	for _, ext := range standalone.SQLExtensions {
		extToLanguage[ext] = types.LanguageSQL
		standaloneExts[ext] = true
	}
}

// compoundExt is filepath.Ext plus the one two-level extension the router
// needs: filepath.Ext("stg.sql.jinja") returns ".jinja", which no map keys, so
// the dbt template would be silently skipped.
func compoundExt(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".sql.jinja") {
		return ".sql.jinja"
	}
	return filepath.Ext(path)
}

// extractorVersion must be bumped by hand whenever a change under extraction/
// would yield different nodes, edges, or refs for a file that is already
// indexed. Without the bump the content-hash dedup skips that file forever and
// the semantic drift never surfaces; a mismatch forces one full re-extraction.
const extractorVersion = "5"

const extractorVersionMetadataKey = "extractor_version"

// checkExtractorVersion reports whether this run must treat every file as
// changed (forceFull) and whether the stamp needs rewriting (needStamp). An
// absent key with file rows present is an index predating the mechanism, so it
// migrates; an absent key on an empty index only stamps.
func (o *Orchestrator) checkExtractorVersion(ctx context.Context) (forceFull, needStamp bool, err error) {
	stored, ok, err := o.getMetadata(ctx, extractorVersionMetadataKey)
	if err != nil {
		return false, false, err
	}
	if ok {
		if stored == extractorVersion {
			return false, false, nil
		}
		return true, true, nil
	}

	files, err := o.db.GetAllFiles(ctx)
	if err != nil {
		return false, false, err
	}
	return len(files) > 0, true, nil
}

// stampExtractorVersion runs only after a successful pass: a crash
// mid-migration must leave the mismatch in place so the next run retries,
// rather than recording a migration that never finished.
func (o *Orchestrator) stampExtractorVersion(ctx context.Context) error {
	return o.setMetadata(ctx, extractorVersionMetadataKey, extractorVersion)
}

// getMetadata goes through the exported *sql.DB handle because the db package
// has no project_metadata accessor; this mirrors migrations.go's raw SQL.
func (o *Orchestrator) getMetadata(ctx context.Context, key string) (value string, ok bool, err error) {
	err = o.db.DB().QueryRowContext(ctx,
		`SELECT value FROM project_metadata WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("orchestrator: read metadata %s: %w", key, err)
	}
	return value, true, nil
}

func (o *Orchestrator) setMetadata(ctx context.Context, key, value string) error {
	if _, err := o.db.DB().ExecContext(ctx,
		`INSERT OR REPLACE INTO project_metadata (key, value, updated_at)
		 VALUES (?, ?, strftime('%s','now'))`, key, value); err != nil {
		return fmt.Errorf("orchestrator: write metadata %s: %w", key, err)
	}
	return nil
}

// Orchestrator wires the file scanner, extension router, parser pool,
// extractor registries, and the database into one indexing pipeline.
type Orchestrator struct {
	db         *db.DB
	pool       *extraction.Pool
	langReg    *languages.Registry
	standalone *standalone.Registry
	// sqlExt is stateless and safe for concurrent use — no parser pool involved.
	sqlExt *standalone.SQLExtractor
	// skippedFiles counts unreadable or un-stat-able files from the last run.
	// Skipping one is not fatal, but the count is surfaced so it stays visible.
	skippedFiles atomic.Int64
	// ignore filters discovery output against .claude/atomic.toml globs.
	// Nil disables filtering.
	ignore *config.IgnoreMatcher
}

// SkippedFiles returns how many files the most recent run could not read or
// stat. Reset at the start of each run.
func (o *Orchestrator) SkippedFiles() int {
	return int(o.skippedFiles.Load())
}

// SetIgnoreMatcher filters the file lists IndexAll, Sync, IndexPaths, and
// ScanFiles produce. Nil, the default, disables filtering.
func (o *Orchestrator) SetIgnoreMatcher(m *config.IgnoreMatcher) {
	o.ignore = m
}

// filterIgnored is the single discovery-time filtering seam. IndexAll and Sync
// feed the same filtered list to indexFiles and pruneDeleted, so a newly
// ignored file simply stops appearing and pruneDeleted reclaims it as an
// orphan — no separate un-ignore mechanism is needed.
func (o *Orchestrator) filterIgnored(projectRoot string, paths []string) []string {
	if o.ignore == nil {
		return paths
	}
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(projectRoot, p)
		if err != nil {
			rel = p
		}
		if o.ignore.Match(filepath.ToSlash(rel)) {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// NewOrchestrator requires a non-nil, initialised pool; its recycle cadence is
// what bounds parser memory.
func NewOrchestrator(database *db.DB, pool *extraction.Pool) *Orchestrator {
	return &Orchestrator{
		db:         database,
		pool:       pool,
		langReg:    languages.NewRegistry(),
		standalone: standalone.NewRegistry(pool),
		sqlExt:     standalone.NewSQLExtractor(),
	}
}

// IndexAll indexes every source file under projectRoot, concurrently up to the
// pool size. A per-file error is recorded in the DB and never aborts the run.
func (o *Orchestrator) IndexAll(ctx context.Context, projectRoot string) error {
	return o.scanAndIndex(ctx, projectRoot, "orchestrator: scan")
}

// IndexPaths indexes exactly the given absolute paths, skipping unknown
// extensions and ignored paths. It deliberately does not prune: it is handed a
// subset, so pruning would delete every file outside it.
func (o *Orchestrator) IndexPaths(ctx context.Context, projectRoot string, paths []string) error {
	return o.indexFiles(ctx, projectRoot, o.filterIgnored(projectRoot, paths), false)
}

// Sync re-indexes only files whose content hash changed, and prunes files that
// vanished from disk. It runs the same extractor_version check as IndexAll:
// warm repos only ever call Sync, so the migration must not be inert here.
func (o *Orchestrator) Sync(ctx context.Context, projectRoot string) error {
	return o.scanAndIndex(ctx, projectRoot, "orchestrator: sync scan")
}

// scanAndIndex is the shared body of IndexAll and Sync: version check, scan,
// index (full pass on a mismatch), prune, stamp.
func (o *Orchestrator) scanAndIndex(ctx context.Context, projectRoot, scanErrPrefix string) error {
	forceFull, needStamp, err := o.checkExtractorVersion(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: extractor version check: %w", err)
	}

	files, err := scanFiles(projectRoot)
	if err != nil {
		return fmt.Errorf("%s: %w", scanErrPrefix, err)
	}
	files = o.filterIgnored(projectRoot, files)

	if err := o.indexFiles(ctx, projectRoot, files, forceFull); err != nil {
		return err
	}
	if err := o.pruneDeleted(ctx, projectRoot, files); err != nil {
		return err
	}

	if needStamp {
		if err := o.stampExtractorVersion(ctx); err != nil {
			return err
		}
	}
	return nil
}

// pruneDeleted reclaims rows for files still in the DB but gone from disk;
// without it a delete or rename leaves symbols and call edges that queries
// keep returning. onDisk is the full scanned list, pre-extension-filter, so
// any DB path missing from it is genuinely gone.
//
// Each orphan is pruned in its own transaction, so a crash never leaves a
// half-pruned file. Whole-tree callers only — IndexPaths must not prune.
func (o *Orchestrator) pruneDeleted(ctx context.Context, projectRoot string, onDisk []string) error {
	present := make(map[string]bool, len(onDisk))
	for _, p := range onDisk {
		rel, err := filepath.Rel(projectRoot, p)
		if err != nil {
			rel = p // mirrors indexOneFile's fallback when Rel fails
		}
		present[rel] = true
	}

	dbFiles, err := o.db.GetAllFiles(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: prune: list files: %w", err)
	}

	var errs []error
	for _, fr := range dbFiles {
		if present[fr.Path] {
			continue
		}
		if err := o.db.WithTx(ctx, func(tx *db.Tx) error {
			if err := tx.DeleteNodesByFile(ctx, fr.Path); err != nil {
				return err
			}
			if err := tx.DeleteUnresolvedRefsByFile(ctx, fr.Path); err != nil {
				return err
			}
			return tx.DeleteFile(ctx, fr.Path)
		}); err != nil {
			errs = append(errs, fmt.Errorf("orchestrator: prune %s: %w", fr.Path, err))
		}
	}
	return errors.Join(errs...)
}

// indexFiles is the shared inner loop for IndexAll, Sync, and IndexPaths.
// forceReindex bypasses indexOneFile's content-hash dedup for every file in
// this call, which is how the extractor_version migration forces a full pass.
func (o *Orchestrator) indexFiles(ctx context.Context, projectRoot string, filePaths []string, forceReindex bool) error {
	o.skippedFiles.Store(0)

	var toIndex []string
	for _, p := range filePaths {
		ext := strings.ToLower(compoundExt(p))
		if _, ok := extToLanguage[ext]; ok {
			toIndex = append(toIndex, p)
		}
	}

	// One goroutine per file, but Borrow caps live parsers at pool.Size().
	// File-level-only and standalone files never borrow at all.
	var wg sync.WaitGroup
	// Only DB write errors reach errCh: extraction errors are recorded in the
	// file row by storeExtractionResult and never returned.
	errCh := make(chan error, len(toIndex))

	for _, path := range toIndex {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := o.indexOneFile(ctx, projectRoot, p, forceReindex); err != nil {
				errCh <- err
			}
		}(path)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// indexOneFile reads, extracts, and stores a single file. The relative path is
// the canonical DB key.
func (o *Orchestrator) indexOneFile(ctx context.Context, projectRoot, filePath string, forceReindex bool) error {
	relPath, err := filepath.Rel(projectRoot, filePath)
	if err != nil {
		relPath = filePath
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		// A broken symlink, deleted-but-staged path, or permission error on one
		// file must not abort the index — the rest of the tree still needs to
		// reach the resolution phase.
		o.skippedFiles.Add(1)
		return nil
	}

	contentHash := hashContent(src)
	stat, err := os.Stat(filePath)
	if err != nil {
		o.skippedFiles.Add(1)
		return nil
	}

	ext := strings.ToLower(compoundExt(relPath))
	lang := extToLanguage[ext]

	if !forceReindex {
		if existing, err := o.db.GetFile(ctx, relPath); err == nil {
			if existing.ContentHash == contentHash {
				return nil // unchanged — skip
			}
		}
	}

	if fileLevelOnly[lang] {
		fr := types.FileRecord{
			Path:        relPath,
			ContentHash: contentHash,
			Language:    lang,
			Size:        stat.Size(),
			ModifiedAt:  stat.ModTime().UTC().Format(time.RFC3339),
			IndexedAt:   time.Now().UTC().Format(time.RFC3339),
			NodeCount:   0,
		}
		return o.db.UpsertFile(ctx, fr)
	}

	var result types.ExtractionResult

	if standaloneExts[ext] {
		ex := o.standalone.For(ext)
		if ex == nil {
			fr := types.FileRecord{
				Path:        relPath,
				ContentHash: contentHash,
				Language:    lang,
				Size:        stat.Size(),
				ModifiedAt:  stat.ModTime().UTC().Format(time.RFC3339),
				IndexedAt:   time.Now().UTC().Format(time.RFC3339),
			}
			return o.db.UpsertFile(ctx, fr)
		}
		result, err = ex.Extract(relPath, string(src))
		if err != nil {
			// Best-effort: record it in the file row and keep going.
			result.Errors = append(result.Errors, err.Error())
		}
	} else {
		cfg, tsLang, ok := o.langReg.For(lang)
		if !ok {
			// Known extension, no extractor config — file record only.
			fr := types.FileRecord{
				Path:        relPath,
				ContentHash: contentHash,
				Language:    lang,
				Size:        stat.Size(),
				ModifiedAt:  stat.ModTime().UTC().Format(time.RFC3339),
				IndexedAt:   time.Now().UTC().Format(time.RFC3339),
			}
			return o.db.UpsertFile(ctx, fr)
		}
		extractor := extraction.NewTreeSitterExtractor(o.pool, tsLang, cfg)
		result = extractor.Extract(ctx, relPath, string(src), lang)

		// Harvest SQL out of host-language string literals and merge it in
		// before the single store call.
		if embeddedSQLHostExts[ext] {
			embeddedSQLPostPass(ctx, relPath, string(src), &result, o.sqlExt, o.pool)
		}
	}

	return o.storeExtractionResult(ctx, relPath, contentHash, lang, stat, result)
}

// storeExtractionResult persists one file's extraction in a single
// transaction, so a crash or cancellation mid-store leaves no half-deleted
// file and no nodes without a file row.
//
// It deletes all the file's nodes before re-inserting because a node id embeds
// its line: a moved symbol gets a new id, and an in-place REPLACE would strand
// the old node with dangling edges.
func (o *Orchestrator) storeExtractionResult(
	ctx context.Context,
	relPath, contentHash string,
	lang types.Language,
	stat os.FileInfo,
	result types.ExtractionResult,
) error {
	now := time.Now()
	nowUnix := now.Unix()

	var errJSON []byte
	if len(result.Errors) > 0 {
		errJSON, _ = json.Marshal(result.Errors)
	}

	fr := types.FileRecord{
		Path:        relPath,
		ContentHash: contentHash,
		Language:    lang,
		Size:        stat.Size(),
		ModifiedAt:  stat.ModTime().UTC().Format(time.RFC3339),
		IndexedAt:   now.UTC().Format(time.RFC3339),
		NodeCount:   len(result.Nodes),
		Errors:      errJSON,
	}

	return o.db.WithTx(ctx, func(tx *db.Tx) error {
		// Cascade clears this file's edges too.
		if err := tx.DeleteNodesByFile(ctx, relPath); err != nil {
			return fmt.Errorf("storeExtractionResult: delete nodes: %w", err)
		}

		if err := tx.DeleteUnresolvedRefsByFile(ctx, relPath); err != nil {
			return fmt.Errorf("storeExtractionResult: delete unresolved refs: %w", err)
		}

		for _, n := range result.Nodes {
			if err := tx.UpsertNodeAt(ctx, n, nowUnix); err != nil {
				return fmt.Errorf("storeExtractionResult: upsert node %s: %w", n.ID, err)
			}
		}

		for _, e := range result.Edges {
			if _, err := tx.InsertEdge(ctx, e); err != nil {
				return fmt.Errorf("storeExtractionResult: insert edge: %w", err)
			}
		}

		// Language is only assigned when empty: embedded SQL refs already carry
		// Language==SQL, and createEdges keys Provenance:"embedded" off it.
		//
		// Owner guard: from_node_id has an FK to nodes(id), so one ref whose
		// owner never made it into result.Nodes would fail the whole file's
		// transaction and skip the resolution phase for the entire run. Record
		// the miss in the file's errors column and store the rest instead.
		localNodeIDs := make(map[string]bool, len(result.Nodes))
		for _, n := range result.Nodes {
			localNodeIDs[n.ID] = true
		}
		var skippedRefs []string
		for _, ref := range result.UnresolvedReferences {
			ref.FilePath = relPath
			if ref.Language == "" {
				ref.Language = lang
			}
			if !localNodeIDs[ref.FromNodeID] {
				exists, err := tx.NodeExists(ctx, ref.FromNodeID)
				if err != nil {
					return fmt.Errorf("storeExtractionResult: check owner node %s: %w", ref.FromNodeID, err)
				}
				if !exists {
					skippedRefs = append(skippedRefs, fmt.Sprintf(
						"skipped unresolved ref %s (%s): owner node %s not found", ref.ID, ref.ReferenceName, ref.FromNodeID))
					continue
				}
			}
			if err := tx.InsertUnresolvedRef(ctx, ref); err != nil {
				return fmt.Errorf("storeExtractionResult: insert unresolved ref %s: %w", ref.ID, err)
			}
		}

		if len(skippedRefs) > 0 {
			allErrors := append(append([]string{}, result.Errors...), skippedRefs...)
			if merged, err := json.Marshal(allErrors); err == nil {
				fr.Errors = merged
			}
		}

		// Last, because it records node_count.
		if err := tx.UpsertFile(ctx, fr); err != nil {
			return fmt.Errorf("storeExtractionResult: upsert file: %w", err)
		}

		return nil
	})
}

// ScanFiles returns dir's tracked files through o's ignore matcher. It is a
// method, not a free function, so ExtractFrameworkNodes sees the same filtered
// set IndexAll and Sync do — a second unfiltered scan would mint framework
// nodes for files otherwise invisible to `atomic code files`.
func (o *Orchestrator) ScanFiles(dir string) ([]string, error) {
	files, err := scanFiles(dir)
	if err != nil {
		return nil, err
	}
	return o.filterIgnored(dir, files), nil
}

// scanFiles prefers git ls-files, which honors .gitignore for free, and falls
// back to a WalkDir when git is unavailable or dir is not a repo.
func scanFiles(dir string) ([]string, error) {
	if paths, err := gitLsFiles(dir); err == nil {
		return paths, nil
	}
	return walkDirFallback(dir)
}

// gitLsFiles returns absolute paths for dir's tracked and
// untracked-but-not-ignored files.
func gitLsFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var paths []string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		rel := strings.TrimSpace(string(line))
		if rel == "" {
			continue
		}
		paths = append(paths, filepath.Join(dir, rel))
	}
	return paths, nil
}

// walkDirFallback walks dir recursively when git is unavailable. It knows
// nothing of .gitignore, so it skips a fixed set of directories instead.
func walkDirFallback(dir string) ([]string, error) {
	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		".svn":         true,
		".hg":          true,
		"vendor":       true,
	}

	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable entries
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	return paths, err
}

// hashContent produces the files.content_hash value that drives dedup.
func hashContent(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}
