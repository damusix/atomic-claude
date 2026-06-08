package sitter

import (
	"context"
	"fmt"
	"io"
)

type IterMode int

const (
	DFSMode IterMode = iota
	BFSMode
)

type Iterator struct {
	named        bool
	mode         IterMode
	nodesToVisit []Node
}

func (t TreeSitter) NewIterator(n Node, mode IterMode) Iterator {
	return Iterator{
		named:        false,
		mode:         mode,
		nodesToVisit: []Node{n},
	}
}

func NewNamedIterator(n Node, mode IterMode) Iterator {
	return Iterator{
		named:        true,
		mode:         mode,
		nodesToVisit: []Node{n},
	}
}

func (iter *Iterator) Next(ctx context.Context) (Node, error) {
	if len(iter.nodesToVisit) == 0 {
		return Node{}, io.EOF
	}

	var n Node
	n, iter.nodesToVisit = iter.nodesToVisit[0], iter.nodesToVisit[1:]

	var children []Node
	if iter.named {
		namedChildCount, err := n.NamedChildCount(ctx)
		if err != nil {
			return Node{}, fmt.Errorf("getting named child count: %w", err)
		}
		for i := uint64(0); i < namedChildCount; i++ {
			c, err := n.NamedChild(ctx, i)
			if err != nil {
				return Node{}, fmt.Errorf("getting named child: %w", err)
			}
			children = append(children, c)
		}
	} else {
		childCount, err := n.ChildCount(ctx)
		if err != nil {
			return Node{}, fmt.Errorf("getting child count: %w", err)
		}
		for i := uint64(0); i < childCount; i++ {
			c, err := n.Child(ctx, i)
			if err != nil {
				return Node{}, fmt.Errorf("getting child: %w", err)
			}
			children = append(children, c)
		}
	}

	switch iter.mode {
	case DFSMode:
		iter.nodesToVisit = append(children, iter.nodesToVisit...)
	case BFSMode:
		iter.nodesToVisit = append(iter.nodesToVisit, children...)
	default:
		panic("not implemented")
	}
	return n, nil
}

func (iter *Iterator) ForEach(ctx context.Context, fn func(Node) error) error {
	for {
		n, err := iter.Next(ctx)
		if err != nil {
			return err
		}
		err = fn(n)
		if err != nil {
			return err
		}
	}
}
