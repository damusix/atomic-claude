// Package extraction provides the parser-pool runtime over tsbinding
// (tree-sitter via wazero). The binding sits behind a Go interface so extractors
// never import wazero or tsbinding, leaving a swap to a cgo binding a build-tag
// flip.
//
// Concurrency: one wazero module instance per in-flight file, drawn from a
// bounded pool (cap ≈ GOMAXPROCS); instances recycle every RecycleInterval
// parses to release wazero's grow-only linear memory.
package extraction

import (
	"context"
	"errors"
	"io"

	sitter "github.com/malivvan/tree-sitter"
)

// treeRooter gives WalkNamed the root node without putting sitter.Node in the
// public Tree interface.
type treeRooter interface {
	rootNode(ctx context.Context) (sitter.Node, error)
}

// --- Language constants: aliases so callers don't import tsbinding ---

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
	LangElixir          // Elixir
	LangErlang          // Erlang
)

// --- Tree / Node interfaces ---

// NodeInfo is a value type so walker callbacks stay decoupled from the binding.
type NodeInfo struct {
	Kind      string
	StartByte uint64
	EndByte   uint64
}

// Tree is the parse tree from Instance.ParseString. Traverse it with WalkNamed;
// any future root handle must surface as NodeInfo, never sitter.Node.
type Tree interface {
	// Unexported, so callers outside this package cannot reach sitter.Node.
	treeRooter
}

// --- Instance interface ---

// Instance is one parsing unit — its own wazero runtime+module and parser.
// Borrow one per goroutine; sharing one races.
type Instance interface {
	// ID is unique within the pool; tests use it to detect double-lending.
	ID() int

	// SetLanguage persists until the next call.
	SetLanguage(ctx context.Context, lang Lang) error

	ParseString(ctx context.Context, src string) (Tree, error)
}

// --- WalkNamed ---

// WalkNamed parses src with inst (advancing its parse counter) and visits every
// named node in DFS pre-order. A non-nil error from fn stops the walk and is
// returned; the iterator's io.EOF is swallowed as the natural end of tree.
//
// Each node costs NamedChildCount + N×NamedChild WASM crossings. tree-sitter's
// ts_tree_cursor_* would cut that, but tsbinding exposes no cursor API.
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
