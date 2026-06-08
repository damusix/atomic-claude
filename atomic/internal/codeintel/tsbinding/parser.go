package sitter

import (
	"context"
	"fmt"
)

type Parser struct {
	t TreeSitter
	p uint64
}

func (t TreeSitter) NewParser(ctx context.Context) (Parser, error) {
	p, err := t.parserNew.Call(ctx)
	if err != nil {
		return Parser{}, fmt.Errorf("creating parser: %w", err)
	}

	return Parser{
		t: t,
		p: p[0],
	}, nil
}

func (p Parser) Close(ctx context.Context) error {
	_, err := p.t.parserDelete.Call(ctx, p.p)
	if err != nil {
		return fmt.Errorf("closing parser: %w", err)
	}
	return nil
}

func (p Parser) SetLanguage(ctx context.Context, l Language) error {
	ok, err := p.t.parserSetLanguage.Call(ctx, p.p, l.l)
	if err != nil {
		return fmt.Errorf("setting language: %w", err)
	}
	if ok[0] == 0 {
		v, err := p.GetLanguageVersion(ctx, l)
		if err != nil {
			return err
		}
		return LanguageError{v}
	}
	return nil
}

func (p Parser) GetLanguageVersion(ctx context.Context, l Language) (uint64, error) {
	v, err := p.t.languageVersion.Call(ctx, l.l)
	if err != nil {
		return 0, fmt.Errorf("getting language version: %w", err)
	}
	return v[0], nil
}

func (p Parser) ParseString(ctx context.Context, str string) (Tree, error) {
	strPtr, strSize, freeStr, err := p.t.allocateString(ctx, str)
	if err != nil {
		return Tree{}, fmt.Errorf("allocating parse string: %w", err)
	}
	defer freeStr()

	tree, err := p.t.parserParseString.Call(ctx, p.p, uint64(0), strPtr, strSize)
	if err != nil {
		return Tree{}, fmt.Errorf("calling ts_parser_parse_string: %w", err)
	}
	return newTree(p.t, tree[0]), nil
}
