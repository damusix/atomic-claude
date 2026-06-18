package extraction

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"

	sitter "github.com/malivvan/tree-sitter"
)

// RecycleInterval is the number of parses after which a pooled instance is
// dropped and recreated. Wazero's linear memory is grow-only; this reclaim
// strategy keeps RSS flat at ~1 GB for a large repo (spike A3: unbounded
// without recycle vs ~1 GB flat at 500-parse cadence).
const RecycleInterval = 500

// Pool is a bounded pool of independent tree-sitter parsing instances.
// Each instance owns its own wazero runtime+module+parser and must never be
// shared across goroutines. The pool enforces this via borrow/return.
//
// Pool is safe for concurrent use (all state is accessed via a buffered
// channel; no mutexes needed).
type Pool struct {
	ch           chan *tsInstance
	recycleCount atomic.Int64
}

// PoolOptions configures a Pool. Zero-value fields use defaults.
type PoolOptions struct {
	// Size is the number of independent instances. Defaults to GOMAXPROCS.
	// Must be >= 1.
	Size int
}

// NewPool creates and fills a pool. All instances are initialised eagerly so
// the first borrow does not pay instantiation cost.
func NewPool(ctx context.Context, opts PoolOptions) (*Pool, error) {
	size := opts.Size
	if size <= 0 {
		size = runtime.GOMAXPROCS(0)
		if size < 1 {
			size = 1
		}
	}

	ch := make(chan *tsInstance, size)
	for i := 0; i < size; i++ {
		inst, err := newTSInstance(ctx, i)
		if err != nil {
			// Drain and return any already-created instances before failing.
			close(ch)
			for inst := range ch {
				inst.close(ctx)
			}
			return nil, fmt.Errorf("initialising pool instance %d: %w", i, err)
		}
		ch <- inst
	}
	return &Pool{ch: ch}, nil
}

// Borrow takes an instance from the pool. It blocks until one is available
// or ctx is cancelled. The caller must call Return when done — failure to
// return leaks the pool. The returned Instance is ready to parse (language
// defaults to Go; callers may call SetLanguage before use).
//
// ctx is used only for the blocking wait, not for the lifetime of the
// instance. A cancelled ctx returns a wrapped ctx error so callers can
// handle graceful shutdown.
func (p *Pool) Borrow(ctx context.Context) (Instance, error) {
	select {
	case inst := <-p.ch:
		return inst, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("extraction.Pool.Borrow: %w", ctx.Err())
	}
}

// Return puts an instance back into the pool, recycling it first if its
// parse counter has reached RecycleInterval. The caller must pass the exact
// value obtained from Borrow — passing a different Instance or returning an
// Instance to the wrong pool will corrupt pool state.
func (p *Pool) Return(inst Instance) {
	ti := inst.(*tsInstance) //nolint:forcetypeassert // only *tsInstance enters the pool
	if ti.parseCount >= RecycleInterval {
		p.recycle(ti)
	} else {
		p.ch <- ti
	}
}

// recycle drops the given instance, runs GC to release wazero's mmap'd
// linear memory, and creates a fresh replacement with the same ID.
func (p *Pool) recycle(old *tsInstance) {
	ctx := context.Background()
	id := old.id
	old.close(ctx)
	old = nil

	// Two GC passes: first collects the wazero runtime reference, second
	// runs any finalizers that free mmap'd module pages (spike A3 pattern).
	runtime.GC()
	runtime.GC()

	fresh, err := newTSInstance(ctx, id)
	if err != nil {
		// Recycling failure is fatal: without this slot the pool blocks.
		// Panic rather than silently deadlock.
		panic(fmt.Sprintf("extraction.Pool.recycle: failed to recreate instance %d: %v", id, err))
	}
	p.recycleCount.Add(1)
	p.ch <- fresh
}

// RecycleCount returns the total number of recycle operations performed since
// the pool was created. Used in tests to verify recycle cadence.
func (p *Pool) RecycleCount() int {
	return int(p.recycleCount.Load())
}

// Close drains the pool and shuts down all available instances. It must be
// called only after all outstanding borrows have been returned. Any instances
// still borrowed at Close time are the caller's responsibility.
func (p *Pool) Close() {
	ctx := context.Background()
	for len(p.ch) > 0 {
		(<-p.ch).close(ctx)
	}
}

// ChannelLen returns the number of instances currently available in the pool.
// Used in tests to verify drain completeness after Close.
func (p *Pool) ChannelLen() int {
	return len(p.ch)
}

// ---------------------------------------------------------------------------
// tsInstance — the concrete pooling unit
// ---------------------------------------------------------------------------

// tsInstance is one fully-independent parsing unit: its own wazero
// runtime+module (TreeSitter), its own Parser, with the current language set.
// This is the concrete type that implements Instance.
//
// It must never be shared across goroutines (data race — proven in spike A2).
type tsInstance struct {
	id         int
	ts         sitter.TreeSitter
	parser     sitter.Parser
	lang       sitter.Language
	parseCount int
}

// newTSInstance creates a fully-initialised instance with Go as the default
// language. id is used to detect double-lending in tests.
func newTSInstance(ctx context.Context, id int) (*tsInstance, error) {
	ts, err := sitter.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("sitter.New: %w", err)
	}
	p, err := ts.NewParser(ctx)
	if err != nil {
		return nil, fmt.Errorf("ts.NewParser: %w", err)
	}
	lang, err := ts.LanguageGo(ctx)
	if err != nil {
		return nil, fmt.Errorf("ts.LanguageGo: %w", err)
	}
	if err := p.SetLanguage(ctx, lang); err != nil {
		return nil, fmt.Errorf("parser.SetLanguage(go): %w", err)
	}
	return &tsInstance{id: id, ts: ts, parser: p, lang: lang}, nil
}

// close shuts down the wazero module. Called only by the pool's recycle and
// Close paths — never by external callers.
func (ti *tsInstance) close(ctx context.Context) {
	// Parser.Close frees the ts_parser_t inside WASM; the module itself is
	// released when the GC collects the wazero runtime (no explicit close
	// on the runtime API at the version we use).
	_ = ti.parser.Close(ctx)
}

// ID implements Instance.
func (ti *tsInstance) ID() int { return ti.id }

// SetLanguage implements Instance. It changes the parser's grammar.
func (ti *tsInstance) SetLanguage(ctx context.Context, lang Lang) error {
	sl, err := ti.resolveLanguage(ctx, lang)
	if err != nil {
		return err
	}
	if err := ti.parser.SetLanguage(ctx, sl); err != nil {
		return fmt.Errorf("SetLanguage(%d): %w", lang, err)
	}
	ti.lang = sl
	return nil
}

// ParseString implements Instance. It increments the parse counter (used for
// recycle cadence — checked by the pool on Return).
func (ti *tsInstance) ParseString(ctx context.Context, src string) (Tree, error) {
	tree, err := ti.parser.ParseString(ctx, src)
	if err != nil {
		return nil, err
	}
	ti.parseCount++
	return &tsTree{ts: ti.ts, t: tree}, nil
}

// resolveLanguage maps a Lang constant to a sitter.Language loaded from the
// wazero module embedded in this instance.
func (ti *tsInstance) resolveLanguage(ctx context.Context, lang Lang) (sitter.Language, error) {
	ts := ti.ts
	switch lang {
	case LangC:
		return ts.LanguageC(ctx)
	case LangCpp:
		return ts.LanguageCpp(ctx)
	case LangCSharp:
		return ts.LanguageCSharp(ctx)
	case LangJava:
		return ts.LanguageJava(ctx)
	case LangJavaScript:
		return ts.LanguageJavaScript(ctx)
	case LangGo:
		return ts.LanguageGo(ctx)
	case LangKotlin:
		return ts.LanguageKotlin(ctx)
	case LangLua:
		return ts.LanguageLua(ctx)
	case LangPHP:
		return ts.LanguagePHP(ctx)
	case LangPython:
		return ts.LanguagePython(ctx)
	case LangRuby:
		return ts.LanguageRuby(ctx)
	case LangRust:
		return ts.LanguageRust(ctx)
	case LangScala:
		return ts.LanguageScala(ctx)
	case LangSwift:
		return ts.LanguageSwift(ctx)
	case LangTypeScript:
		return ts.LanguageTypescript(ctx)
	case LangTSX:
		return ts.LanguageTSX(ctx)
	case LangDart:
		return ts.LanguageDart(ctx)
	case LangLuau:
		return ts.LanguageLuau(ctx)
	case LangObjC:
		return ts.LanguageObjC(ctx)
	case LangPascal:
		return ts.LanguagePascal(ctx)
	case LangElixir:
		return ts.LanguageElixir(ctx)
	case LangErlang:
		return ts.LanguageErlang(ctx)
	default:
		return sitter.Language{}, fmt.Errorf("unknown language %d", lang)
	}
}

// ---------------------------------------------------------------------------
// tsTree — concrete Tree implementation
// ---------------------------------------------------------------------------

type tsTree struct {
	ts sitter.TreeSitter
	t  sitter.Tree
}

// rootNode satisfies treeRooter. It is package-internal so sitter.Node does
// not appear in any public interface (callers use WalkNamed instead).
func (tr *tsTree) rootNode(ctx context.Context) (sitter.Node, error) {
	return tr.t.RootNode(ctx)
}
