package sitter

import (
	"context"
	"fmt"
)

type Tree struct {
	ts TreeSitter
	t  uint64
}

func newTree(ts TreeSitter, t uint64) Tree {
	return Tree{ts, t}
}

func (t Tree) RootNode(ctx context.Context) (Node, error) {
	// allocate tsnode 24 bytes
	nodePtr, err := t.ts.malloc.Call(ctx, uint64(24))
	if err != nil {
		return Node{}, fmt.Errorf("allocating node: %w", err)
	}

	_, err = t.ts.treeRootNode.Call(ctx, nodePtr[0], t.t)
	if err != nil {
		return Node{}, fmt.Errorf("getting tree root node: %w", err)
	}
	return newNode(t.ts, nodePtr[0]), nil
}
