package extraction

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"

	sitter "github.com/malivvan/tree-sitter"
)

// RecycleInterval is the parse count after which a pooled instance is dropped
// and recreated. Wazero's linear memory is grow-only; recycling at this cadence
// holds RSS flat at ~1 GB on a large repo instead of growing unbounded.
const RecycleInterval = 500

// Pool hands out independent tree-sitter parsing instances. Each owns its own
// wazero runtime+module+parser and must never cross goroutines; borrow/return
// enforces that, and all pool state lives in the buffered channel.
//
// The channel holds exactly size tokens, each a live *tsInstance or nil. A nil
// token is an unbooted permit — the right to instantiate on first borrow —
// which bounds concurrency at size while deferring the expensive wazero-runtime
// + grammar compile until a parse needs it.
type Pool struct {
	ch           chan *tsInstance
	recycleCount atomic.Int64
	nextID       atomic.Int64
}

// PoolOptions configures a Pool. Zero-value fields use defaults.
type PoolOptions struct {
	// Size is the number of independent instances; <= 0 means GOMAXPROCS.
	Size int
}

// NewPool fills the pool with unbooted permits; no wazero runtime is booted
// until a Borrow demands one. That defers the ~0.5 s per-instance grammar
// compile to actual parse demand, so a sync where every file is unchanged boots
// nothing.
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
		ch <- nil // unbooted permit; Borrow instantiates on first use
	}
	return &Pool{ch: ch}, nil
}

// Borrow blocks until a token is available or ctx is cancelled. The caller must
// Return it — a dropped Instance leaks a pool slot. The Instance defaults to the
// Go grammar; call SetLanguage for anything else. Borrowing an unbooted permit
// instantiates here, so Borrow can fail with an instantiation error as well as
// a ctx error.
func (p *Pool) Borrow(ctx context.Context) (Instance, error) {
	select {
	case inst := <-p.ch:
		if inst != nil {
			return inst, nil
		}
		created, err := newTSInstance(ctx, int(p.nextID.Add(1)-1))
		if err != nil {
			p.ch <- nil // restore the permit; the slot must not vanish
			return nil, fmt.Errorf("extraction.Pool.Borrow: instantiate: %w", err)
		}
		return created, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("extraction.Pool.Borrow: %w", ctx.Err())
	}
}

// Return puts an instance back, recycling it first at RecycleInterval parses.
// Must be the exact value from Borrow — a foreign Instance corrupts pool state.
func (p *Pool) Return(inst Instance) {
	ti := inst.(*tsInstance) //nolint:forcetypeassert // only *tsInstance enters the pool
	if ti.parseCount >= RecycleInterval {
		p.recycle(ti)
	} else {
		p.ch <- ti
	}
}

// recycle swaps in a fresh instance with the same ID.
func (p *Pool) recycle(old *tsInstance) {
	ctx := context.Background()
	id := old.id
	old.close(ctx)
	old = nil

	// First pass collects the wazero runtime; the second runs the finalizers
	// that free its mmap'd module pages.
	runtime.GC()
	runtime.GC()

	fresh, err := newTSInstance(ctx, id)
	if err != nil {
		// Losing this slot would deadlock every future Borrow.
		panic(fmt.Sprintf("extraction.Pool.recycle: failed to recreate instance %d: %v", id, err))
	}
	p.recycleCount.Add(1)
	p.ch <- fresh
}

// RecycleCount reports recycles since creation; tests assert the cadence.
func (p *Pool) RecycleCount() int {
	return int(p.recycleCount.Load())
}

// Close drains and shuts down every available instance. Instances still
// borrowed are left to the caller — Return them first.
func (p *Pool) Close() {
	ctx := context.Background()
	for len(p.ch) > 0 {
		if inst := <-p.ch; inst != nil {
			inst.close(ctx)
		}
	}
}

// ChannelLen reports available instances; tests assert drain completeness.
func (p *Pool) ChannelLen() int {
	return len(p.ch)
}

// --- tsInstance: the concrete pooling unit ---

// tsInstance implements Instance. Never share one across goroutines — the
// wazero module behind it is not concurrency-safe.
type tsInstance struct {
	id         int
	ts         sitter.TreeSitter
	parser     sitter.Parser
	lang       sitter.Language
	parseCount int
}

// newTSInstance defaults to the Go grammar; id lets tests detect double-lending.
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

// close is for the pool's recycle and Close paths only.
func (ti *tsInstance) close(ctx context.Context) {
	// Frees ts_parser_t inside WASM; the module itself goes when GC collects
	// the wazero runtime, which the binding exposes no explicit close for.
	_ = ti.parser.Close(ctx)
}

func (ti *tsInstance) ID() int { return ti.id }

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

// ParseString advances the parse counter the pool reads on Return.
func (ti *tsInstance) ParseString(ctx context.Context, src string) (Tree, error) {
	tree, err := ti.parser.ParseString(ctx, src)
	if err != nil {
		return nil, err
	}
	ti.parseCount++
	return &tsTree{ts: ti.ts, t: tree}, nil
}

// resolveLanguage loads the grammar from this instance's own wazero module.
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

// --- tsTree: concrete Tree implementation ---

type tsTree struct {
	ts sitter.TreeSitter
	t  sitter.Tree
}

// rootNode is unexported so sitter.Node stays out of the public Tree interface.
func (tr *tsTree) rootNode(ctx context.Context) (sitter.Node, error) {
	return tr.t.RootNode(ctx)
}
