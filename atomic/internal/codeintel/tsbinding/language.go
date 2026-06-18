package sitter

import (
	"context"
	"fmt"
)

type (
	Language struct {
		t TreeSitter
		l uint64
	}

	LanguageError struct {
		version uint64
	}
)

func (l LanguageError) Error() string {
	return fmt.Sprintf("Incompatible language version %d", l.version)
}

func NewLanguage(l uint64, t TreeSitter) Language {
	return Language{l: l, t: t}
}

func (t TreeSitter) LanguageC(ctx context.Context) (Language, error) {
	p, err := t.languageC.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating c language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageCpp(ctx context.Context) (Language, error) {
	p, err := t.languageCpp.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating cpp language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageCSharp(ctx context.Context) (Language, error) {
	p, err := t.languageCSharp.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating csharp language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageJava(ctx context.Context) (Language, error) {
	p, err := t.languageJava.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating java language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageJavaScript(ctx context.Context) (Language, error) {
	p, err := t.languageJavaScript.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating javascript language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageGo(ctx context.Context) (Language, error) {
	p, err := t.languageGo.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating go language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageKotlin(ctx context.Context) (Language, error) {
	p, err := t.languageKotlin.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating kotlin language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageLua(ctx context.Context) (Language, error) {
	p, err := t.languageLua.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating lua language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguagePHP(ctx context.Context) (Language, error) {
	p, err := t.languagePHP.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating php language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguagePython(ctx context.Context) (Language, error) {
	p, err := t.languagePython.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating python language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageRuby(ctx context.Context) (Language, error) {
	p, err := t.languageRuby.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating ruby language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageRust(ctx context.Context) (Language, error) {
	p, err := t.languageRust.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating rust language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageScala(ctx context.Context) (Language, error) {
	p, err := t.languageScala.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating scala language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageSwift(ctx context.Context) (Language, error) {
	p, err := t.languageSwift.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating swift language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageTypescript(ctx context.Context) (Language, error) {
	p, err := t.languageTypescript.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating typescript language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

// LanguageTSX loads the TSX grammar (also used for JSX).
func (t TreeSitter) LanguageTSX(ctx context.Context) (Language, error) {
	p, err := t.languageTSX.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating tsx language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageDart(ctx context.Context) (Language, error) {
	p, err := t.languageDart.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating dart language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageLuau(ctx context.Context) (Language, error) {
	p, err := t.languageLuau.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating luau language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageObjC(ctx context.Context) (Language, error) {
	p, err := t.languageObjC.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating objc language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguagePascal(ctx context.Context) (Language, error) {
	p, err := t.languagePascal.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating pascal language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageElixir(ctx context.Context) (Language, error) {
	p, err := t.languageElixir.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating elixir language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}

func (t TreeSitter) LanguageErlang(ctx context.Context) (Language, error) {
	p, err := t.languageErlang.Call(ctx)
	if err != nil {
		return Language{}, fmt.Errorf("initiating erlang language: %w", err)
	}
	return NewLanguage(p[0], t), nil
}
