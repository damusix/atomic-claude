package sitter

import (
	"context"
	"fmt"
)

type Node struct {
	t TreeSitter
	n uint64
}

func newNode(t TreeSitter, n uint64) Node {
	return Node{t, n}
}

// tsnodeSize is the size in bytes of a TSNode struct as defined by the
// tree-sitter ABI (v14). The struct holds: context[4]uint32 (16 bytes) +
// id *void (8 bytes) = 24 bytes on wasm32 (pointers are 4 bytes, but the
// field is padded to 8 in the union layout). See tree_sitter/api.h TSNode.
const tsnodeSize = 24

func (t TreeSitter) allocateNode(ctx context.Context) (uint64, error) {
	nodePtr, err := t.malloc.Call(ctx, uint64(tsnodeSize))
	if err != nil {
		return 0, fmt.Errorf("allocating node: %w", err)
	}
	return nodePtr[0], nil
}

func (n Node) Kind(ctx context.Context) (string, error) {
	nodeTypeStrPtr, err := n.t.nodeType.Call(ctx, n.n)
	if err != nil {
		return "", fmt.Errorf("getting node type: %w", err)
	}
	return n.t.readString(ctx, nodeTypeStrPtr[0])
}

func (n Node) Child(ctx context.Context, index uint64) (Node, error) {
	nodePtr, err := n.t.allocateNode(ctx)
	if err != nil {
		return Node{}, err
	}
	_, err = n.t.nodeChild.Call(ctx, nodePtr, n.n, index)
	if err != nil {
		return Node{}, fmt.Errorf("getting node child: %w", err)
	}
	return newNode(n.t, nodePtr), nil
}

func (n Node) NamedChild(ctx context.Context, index uint64) (Node, error) {
	nodePtr, err := n.t.allocateNode(ctx)
	if err != nil {
		return Node{}, err
	}
	_, err = n.t.nodeNamedChild.Call(ctx, nodePtr, n.n, index)
	if err != nil {
		return Node{}, fmt.Errorf("getting node child: %w", err)
	}
	return newNode(n.t, nodePtr), nil
}

func (n Node) IsError(ctx context.Context) (bool, error) {
	res, err := n.t.nodeIsError.Call(ctx, n.n)
	if err != nil {
		return false, fmt.Errorf("getting node is error: %w", err)
	}
	return res[0] == 1, nil
}

func (n Node) StartByte(ctx context.Context) (uint64, error) {
	res, err := n.t.nodeStartByte.Call(ctx, n.n)
	if err != nil {
		return 0, fmt.Errorf("getting node start byte: %w", err)
	}
	return res[0], nil
}

func (n Node) EndByte(ctx context.Context) (uint64, error) {
	res, err := n.t.nodeEndByte.Call(ctx, n.n)
	if err != nil {
		return 0, fmt.Errorf("getting node end byte: %w", err)
	}
	return res[0], nil
}

func (n Node) ChildCount(ctx context.Context) (uint64, error) {
	res, err := n.t.nodeChildCount.Call(ctx, n.n)
	if err != nil {
		return 0, fmt.Errorf("getting node child count: %w", err)
	}
	return res[0], nil
}

func (n Node) NamedChildCount(ctx context.Context) (uint64, error) {
	res, err := n.t.nodeNamedChildCount.Call(ctx, n.n)
	if err != nil {
		return 0, fmt.Errorf("getting node named child count: %w", err)
	}
	return res[0], nil
}

func (n Node) String(ctx context.Context) (string, error) {
	strPtr, err := n.t.nodeString.Call(ctx, n.n)
	if err != nil {
		return "", fmt.Errorf("getting node string: %w", err)
	}
	return n.t.readString(ctx, strPtr[0])
}

// IsNull reports whether the node is the null/zero node returned by tree-sitter
// when a field or sibling does not exist. Callers should check IsNull before
// using any other method on a node returned by ChildByFieldName or PrevNamedSibling.
func (n Node) IsNull(ctx context.Context) (bool, error) {
	res, err := n.t.nodeIsNull.Call(ctx, n.n)
	if err != nil {
		return false, fmt.Errorf("getting node is null: %w", err)
	}
	return res[0] == 1, nil
}

// ChildByFieldName returns the child node for the given grammar field name.
// If no child exists for that field the returned node is a null node —
// callers must check IsNull before using it.
func (n Node) ChildByFieldName(ctx context.Context, name string) (Node, error) {
	namePtr, nameLen, freeName, err := n.t.allocateString(ctx, name)
	if err != nil {
		return Node{}, fmt.Errorf("allocating field name: %w", err)
	}
	defer freeName()

	nodePtr, err := n.t.allocateNode(ctx)
	if err != nil {
		return Node{}, err
	}
	_, err = n.t.nodeChildByFieldName.Call(ctx, nodePtr, n.n, namePtr, nameLen)
	if err != nil {
		return Node{}, fmt.Errorf("getting child by field name %q: %w", name, err)
	}
	return newNode(n.t, nodePtr), nil
}

// PrevNamedSibling returns the previous named sibling of this node.
// If there is no such sibling the returned node is a null node —
// callers must check IsNull before using it.
func (n Node) PrevNamedSibling(ctx context.Context) (Node, error) {
	nodePtr, err := n.t.allocateNode(ctx)
	if err != nil {
		return Node{}, err
	}
	_, err = n.t.nodePrevNamedSibling.Call(ctx, nodePtr, n.n)
	if err != nil {
		return Node{}, fmt.Errorf("getting prev named sibling: %w", err)
	}
	return newNode(n.t, nodePtr), nil
}
