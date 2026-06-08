// Package extraction provides the parser-pool runtime that drives the
// tsbinding (tree-sitter via wazero). The binding sits behind a Go interface
// so later extractors never import wazero or tsbinding directly — an A→C swap
// (wazero → cgo) is a build-tag flip without touching extractor code.
//
// Concurrency model: one independent wazero module instance per in-flight
// file. A bounded pool of instances (cap ≈ GOMAXPROCS) is shared via
// borrow/return. Instances are recycled every RecycleInterval parses to
// release wazero's grow-only linear memory.
package extraction

import (
	"context"
	"errors"
	"io"

	sitter "github.com/malivvan/tree-sitter"
)

// treeRooter is a package-internal interface used by WalkNamed to obtain the
// root node from a Tree implementation without exposing sitter.Node in the
// public Tree interface.
type treeRooter interface {
	rootNode(ctx context.Context) (sitter.Node, error)
}

// ---------------------------------------------------------------------------
// Language constants — named aliases so callers don't import tsbinding.
// ---------------------------------------------------------------------------

// Lang enumerates the supported tree-sitter grammars.
type Lang int

const (
	LangC          Lang = iota
	LangCpp             // C++
	LangCSharp          // C#
	LangJava            // Java
	LangJavaScript      // JavaScript
	LangGo              // Go
	LangKotlin          // Kotlin
	LangLua             // Lua
	LangPHP             // PHP
	LangPython          // Python
	LangRuby            // Ruby
	LangRust            // Rust
	LangScala           // Scala
	LangSwift           // Swift
	LangTypeScript      // TypeScript
	LangTSX             // TSX / JSX
	LangDart            // Dart
	LangLuau            // Luau
	LangObjC            // Objective-C
	LangPascal          // Pascal
)

// ---------------------------------------------------------------------------
// Tree / Node interfaces
// ---------------------------------------------------------------------------

// NodeInfo carries the fields the walker callback receives for each named node.
// Using a value type (not Node) keeps callers decoupled from the binding.
type NodeInfo struct {
	Kind      string
	StartByte uint64
	EndByte   uint64
}

// Tree is the parse tree returned by Instance.ParseString. The public
// traversal API is WalkNamed — callers do not need to inspect the root node
// directly. Exposing sitter.Node here would couple callers to the binding;
// if a root handle is needed in the future, expose it as NodeInfo, not
// sitter.Node.
type Tree interface {
	// treeRooter is satisfied by the concrete tsTree; WalkNamed uses it
	// internally. The method is unexported so callers outside this package
	// cannot reach sitter.Node.
	treeRooter
}

// ---------------------------------------------------------------------------
// Instance interface
// ---------------------------------------------------------------------------

// Instance is one independent parsing unit: its own wazero runtime+module,
// its own parser, with the language set. Borrow one per goroutine; never
// share across goroutines (data race — proven in spike A2).
//
// Callers hold Instance, not *tsInstance, so they have no wazero dependency.
type Instance interface {
	// ID returns a stable integer that uniquely identifies this instance
	// within its pool. Used in tests to detect double-lending.
	ID() int

	// SetLanguage configures the parser for the given language.
	// The language persists until the next SetLanguage call.
	SetLanguage(ctx context.Context, lang Lang) error

	// ParseString parses src and returns the parse tree.
	ParseString(ctx context.Context, src string) (Tree, error)
}

// ---------------------------------------------------------------------------
// WalkNamed — low-round-trip named traversal
// ---------------------------------------------------------------------------

// WalkNamed parses src with inst (advancing its parse counter) and visits
// every named node in DFS pre-order, calling fn for each.
//
// Implementation note: the underlying tsbinding Iterator uses
// NamedChildCount + N×NamedChild per node — each call crosses the WASM
// boundary. The tree-sitter C library exposes ts_tree_cursor_* for
// lower-round-trip traversal, but the current Go binding (tsbinding/) does
// not expose a cursor API. Cursor-based reimplementation is deferred to
// FOLLOWUPS F-3 (design open-Q#4: in-WASM bulk-serialize). The present
// implementation is correct and covers all extractor use cases; it trades
// per-node WASM overhead for simplicity until F-3 is addressed.
//
// fn returning a non-nil error stops the walk immediately and WalkNamed
// returns that error. io.EOF from the iterator signals natural end of tree
// and is swallowed.
func WalkNamed(ctx context.Context, inst Instance, src string, fn func(NodeInfo) error) error {
	tree, err := inst.ParseString(ctx, src)
	if err != nil {
		return err
	}
	root, err := tree.rootNode(ctx)
	if err != nil {
		return err
	}

	iter := sitter.NewNamedIterator(root, sitter.DFSMode)
	for {
		n, iterErr := iter.Next(ctx)
		if errors.Is(iterErr, io.EOF) {
			return nil
		}
		if iterErr != nil {
			return iterErr
		}

		kind, err := n.Kind(ctx)
		if err != nil {
			return err
		}
		start, err := n.StartByte(ctx)
		if err != nil {
			return err
		}
		end, err := n.EndByte(ctx)
		if err != nil {
			return err
		}

		if err := fn(NodeInfo{Kind: kind, StartByte: start, EndByte: end}); err != nil {
			return err
		}
	}
}
