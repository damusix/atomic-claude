// Package extraction — orchestrator (CP10).
//
// IndexAll turns a project directory into a populated DB:
//
//  1. Scan files: git ls-files fast path; WalkDir fallback when not a git repo.
//  2. Map each file's extension to a types.Language via the ext→language table
//     (appendix D). Skip files with no known extension.
//  3. File-level-only languages (yaml, twig, properties): write a file record,
//     no symbol extraction.
//  4. Tree-sitter languages → languages.Registry.For; standalone formats →
//     standalone.Registry.For. Both produce ExtractionResult.
//  5. storeExtractionResult: content-hash dedup (skip unchanged files);
//     DELETE file's nodes + cascade before re-insert (R-E invariant).
//  6. Pool borrow/return wires per-instance recycle (spike A3).

package indexer

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Extension → language map (appendix D, COPY)
// ---------------------------------------------------------------------------

// extToLanguage maps lower-case file extensions (including the dot) to the
// canonical types.Language value. Non-obvious entries documented inline.
//
// File-level-only languages (yaml, twig, properties) are included: the
// orchestrator writes a file record for them but skips symbol extraction.
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
	// C — .h defaults to c; promote to cpp/objc by content is a future heuristic
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

// fileLevelOnly is the set of languages that produce a file record but no
// symbol extraction.
var fileLevelOnly = map[types.Language]bool{
	types.LanguageYAML:       true,
	types.LanguageTwig:       true,
	types.LanguageProperties: true,
}

// standaloneExts is the set of extensions routed to the standalone registry.
// These extensions are a subset of extToLanguage but handled by regex-based
// extractors rather than tree-sitter.
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
	// Populate the SQL entries in extToLanguage and standaloneExts from the
	// canonical list in the standalone package so the two maps never diverge
	// from standalone.NewRegistry's SQL routing.
	for _, ext := range standalone.SQLExtensions {
		extToLanguage[ext] = types.LanguageSQL
		standaloneExts[ext] = true
	}
}

// ---------------------------------------------------------------------------
// Orchestrator
// ---------------------------------------------------------------------------

// Orchestrator wires the file scanner, extension→language router, parser pool,
// language/standalone registries, and the database into a single indexing pipeline.
type Orchestrator struct {
	db         *db.DB
	pool       *extraction.Pool
	langReg    *languages.Registry
	standalone *standalone.Registry
	// sqlExt is the SQLExtractor used by embeddedSQLPostPass for DDL and DML
	// extraction from host-language string literals. It is stateless and safe
	// for concurrent use across goroutines (no parser pool involved).
	sqlExt *standalone.SQLExtractor
}

// NewOrchestrator creates an Orchestrator. pool must be non-nil and already
// initialised; its recycle cadence enforces bounded memory (spike A3).
func NewOrchestrator(database *db.DB, pool *extraction.Pool) *Orchestrator {
	return &Orchestrator{
		db:         database,
		pool:       pool,
		langReg:    languages.NewRegistry(),
		standalone: standalone.NewRegistry(pool),
		sqlExt:     standalone.NewSQLExtractor(),
	}
}

// IndexAll scans projectRoot for source files and indexes them all into the DB.
// Files are processed concurrently (bounded by the pool size). Errors from
// individual files are recorded in the DB but do not abort the run.
func (o *Orchestrator) IndexAll(ctx context.Context, projectRoot string) error {
	files, err := scanFiles(projectRoot)
	if err != nil {
		return fmt.Errorf("orchestrator: scan: %w", err)
	}

	return o.indexFiles(ctx, projectRoot, files)
}

// IndexPaths indexes exactly the files in paths. Each path must be absolute.
// Only paths with a known extension are processed; unknown-extension paths are
// silently skipped (consistent with IndexAll behaviour). This is the real
// selective-indexing path that Engine.IndexFiles delegates to (F-56 fix).
func (o *Orchestrator) IndexPaths(ctx context.Context, projectRoot string, paths []string) error {
	return o.indexFiles(ctx, projectRoot, paths)
}

// Sync re-indexes files in projectRoot that have changed since the last index.
// It uses size+mtime as a pre-filter, then confirms with a content hash.
// Files that have not changed are skipped (dedup). Changed files have their old
// nodes deleted (cascade clears edges) before re-extraction (R-E invariant).
func (o *Orchestrator) Sync(ctx context.Context, projectRoot string) error {
	files, err := scanFiles(projectRoot)
	if err != nil {
		return fmt.Errorf("orchestrator: sync scan: %w", err)
	}

	return o.indexFiles(ctx, projectRoot, files)
}

// indexFiles processes a list of absolute file paths. It is the shared inner
// loop for IndexAll and Sync.
func (o *Orchestrator) indexFiles(ctx context.Context, projectRoot string, filePaths []string) error {
	// Filter to files with a known extension.
	var toIndex []string
	for _, p := range filePaths {
		ext := strings.ToLower(filepath.Ext(p))
		if _, ok := extToLanguage[ext]; ok {
			toIndex = append(toIndex, p)
		}
	}

	// Bounded concurrency: one goroutine per pool slot. We launch one worker
	// per file but the pool's Borrow call serialises at most pool.Size()
	// simultaneous parsers. File-level-only and standalone-ext files don't
	// borrow from the pool; they only hold the mutex briefly for DB writes.
	var wg sync.WaitGroup
	// errCh collects fatal per-file errors (not recorded in DB). In practice
	// storeExtractionResult records per-file errors in the DB and never
	// returns fatal errors from extraction — only DB write errors surface here.
	errCh := make(chan error, len(toIndex))

	for _, path := range toIndex {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := o.indexOneFile(ctx, projectRoot, p); err != nil {
				errCh <- err
			}
		}(path)
	}

	wg.Wait()
	close(errCh)

	// Collect any non-nil errors.
	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// indexOneFile processes a single file: reads it, extracts, stores.
func (o *Orchestrator) indexOneFile(ctx context.Context, projectRoot, filePath string) error {
	// Relative path used as the canonical DB key (matches reference impl).
	relPath, err := filepath.Rel(projectRoot, filePath)
	if err != nil {
		relPath = filePath
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	contentHash := hashContent(src)
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filePath, err)
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	lang := extToLanguage[ext]

	// Dedup: if the file record exists with the same content hash, skip.
	if existing, err := o.db.GetFile(ctx, relPath); err == nil {
		if existing.ContentHash == contentHash {
			return nil // unchanged — skip
		}
	}

	// File-level-only: just write the file record, no symbol extraction.
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

	// Extract symbols.
	var result types.ExtractionResult

	if standaloneExts[ext] {
		// Standalone extractor (Vue, Svelte, Liquid, DFM, MyBatis XML).
		ex := o.standalone.For(ext)
		if ex == nil {
			// No extractor for this standalone ext — write file record only.
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
			// Best-effort: record the error in the file row, continue.
			result.Errors = append(result.Errors, err.Error())
		}
	} else {
		// Tree-sitter extractor via the pool.
		cfg, tsLang, ok := o.langReg.For(lang)
		if !ok {
			// Language registered in extToLanguage but no extractor config.
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

		// Embedded SQL post-pass: harvest string literals from supported host
		// languages (Go, Python; CP4 adds TypeScript) and merge any SQL
		// nodes/edges/refs into the result before the single store call.
		// embeddedSQLHostExts is a positive allowlist of registered host languages;
		// this branch is only reached for non-standalone extensions (outer else).
		if embeddedSQLHostExts[ext] {
			embeddedSQLPostPass(ctx, relPath, string(src), &result, o.sqlExt, o.pool)
		}
	}

	return o.storeExtractionResult(ctx, relPath, contentHash, lang, stat, result)
}

// storeExtractionResult persists one file's extraction results to the DB in a
// single transaction.
//
// Atomicity: the delete-nodes + insert-nodes + insert-edges + upsert-file
// sequence runs inside one BEGIN/COMMIT block. A crash or context cancellation
// mid-store rolls back the entire unit — no half-deleted file, no nodes without
// a file row, and no TOCTOU double-insert window.
//
// Sync invariant (R-E): node-id embeds line; a moved symbol gets a new id.
// An in-place REPLACE leaves the old-id node orphaned with dangling edges.
// Deleting all of the file's nodes (cascade clears their edges) before
// re-inserting guarantees no orphans regardless of what changed.
func (o *Orchestrator) storeExtractionResult(
	ctx context.Context,
	relPath, contentHash string,
	lang types.Language,
	stat os.FileInfo,
	result types.ExtractionResult,
) error {
	now := time.Now()
	nowUnix := now.Unix()

	// Encode per-file errors as JSON for the file record (outside tx; pure CPU).
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
		// DELETE all existing nodes for this file (cascade clears edges).
		if err := tx.DeleteNodesByFile(ctx, relPath); err != nil {
			return fmt.Errorf("storeExtractionResult: delete nodes: %w", err)
		}

		// DELETE all existing unresolved_refs for this file so re-index replaces
		// rather than duplicates them.
		if err := tx.DeleteUnresolvedRefsByFile(ctx, relPath); err != nil {
			return fmt.Errorf("storeExtractionResult: delete unresolved refs: %w", err)
		}

		// Insert nodes with updated_at stamped to now.
		for _, n := range result.Nodes {
			if err := tx.UpsertNodeAt(ctx, n, nowUnix); err != nil {
				return fmt.Errorf("storeExtractionResult: upsert node %s: %w", n.ID, err)
			}
		}

		// Insert edges.
		for _, e := range result.Edges {
			if _, err := tx.InsertEdge(ctx, e); err != nil {
				return fmt.Errorf("storeExtractionResult: insert edge: %w", err)
			}
		}

		// Insert unresolved references so CP13 can resolve them later.
		// Set file_path and language on each ref so resolution can scope matches.
		// Language preservation: embedded SQL refs already carry Language==SQL
		// (set by ExtractEmbeddedSQL / scanBodyEdges). We must not overwrite that
		// with the host-file language — the provenance seam in createEdges relies
		// on Language==SQL to stamp Provenance:"embedded" on resolved edges.
		// For all other refs (from normal host-language extraction), Language is
		// empty at this point, so the assignment sets it correctly.
		for _, ref := range result.UnresolvedReferences {
			ref.FilePath = relPath
			if ref.Language == "" {
				ref.Language = lang
			}
			if err := tx.InsertUnresolvedRef(ctx, ref); err != nil {
				return fmt.Errorf("storeExtractionResult: insert unresolved ref %s: %w", ref.ID, err)
			}
		}

		// Upsert the file record last — it records node_count so it must come
		// after nodes are inserted.
		if err := tx.UpsertFile(ctx, fr); err != nil {
			return fmt.Errorf("storeExtractionResult: upsert file: %w", err)
		}

		return nil
	})
}

// ---------------------------------------------------------------------------
// File scanner
// ---------------------------------------------------------------------------

// ScanFiles returns the list of tracked files in dir. It is exported so the
// engine facade can build the []FileInput slice for ExtractAndPersist without
// duplicating the git-ls-files / walkDir logic.
func ScanFiles(dir string) ([]string, error) {
	return scanFiles(dir)
}

// scanFiles returns the list of tracked files in dir.
//
// Fast path: if dir is inside a git repo, `git ls-files --cached --others
// --exclude-standard` is used. This respects .gitignore automatically and
// returns both tracked and untracked-but-not-ignored files.
//
// Fallback: filepath.WalkDir skipping .git and common ignored dirs when
// the git command fails (not a git repo, git not installed, etc.).
func scanFiles(dir string) ([]string, error) {
	if paths, err := gitLsFiles(dir); err == nil {
		return paths, nil
	}
	return walkDirFallback(dir)
}

// gitLsFiles runs `git ls-files --cached --others --exclude-standard` in dir
// and returns the absolute paths of all returned files.
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

// walkDirFallback walks dir recursively, skipping common ignored directories.
func walkDirFallback(dir string) ([]string, error) {
	// Directories to skip (not gitignored but commonly irrelevant).
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

// hashContent returns a hex-encoded SHA-256 of src. This is the content_hash
// stored in the files table for dedup.
func hashContent(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}
