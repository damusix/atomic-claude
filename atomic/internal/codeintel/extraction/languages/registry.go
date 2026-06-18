package languages

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Registry maps types.Language values to (LanguageExtractor, extraction.Lang) pairs.
// It is the single resolution point the orchestrator (CP10) will use to obtain the
// correct config for a given file language.
type Registry struct {
	entries map[types.Language]registryEntry
}

type registryEntry struct {
	cfg  extraction.LanguageExtractor
	lang extraction.Lang
}

// NewRegistry builds and returns a fully-initialised Registry containing the
// twenty-two language configs: Go, TypeScript, JavaScript, TSX, JSX, Python, Rust,
// Java, C, C++, C#, Swift, Kotlin, Scala, Ruby, PHP, Lua, Luau, Dart, ObjC, Pascal, Elixir.
func NewRegistry() *Registry {
	r := &Registry{
		entries: make(map[types.Language]registryEntry, 22),
	}
	r.entries[types.LanguageGo] = registryEntry{cfg: GoExtractor(), lang: extraction.LangGo}
	r.entries[types.LanguageTypeScript] = registryEntry{cfg: TypeScriptExtractor(), lang: extraction.LangTypeScript}
	r.entries[types.LanguageJavaScript] = registryEntry{cfg: JavaScriptExtractor(), lang: extraction.LangJavaScript}
	r.entries[types.LanguageTSX] = registryEntry{cfg: TSXExtractor(), lang: extraction.LangTSX}
	r.entries[types.LanguageJSX] = registryEntry{cfg: JSXExtractor(), lang: extraction.LangTSX}
	r.entries[types.LanguagePython] = registryEntry{cfg: PythonExtractor(), lang: extraction.LangPython}
	r.entries[types.LanguageRust] = registryEntry{cfg: RustExtractor(), lang: extraction.LangRust}
	r.entries[types.LanguageJava] = registryEntry{cfg: JavaExtractor(), lang: extraction.LangJava}
	r.entries[types.LanguageC] = registryEntry{cfg: CExtractor(), lang: extraction.LangC}
	r.entries[types.LanguageCpp] = registryEntry{cfg: CppExtractor(), lang: extraction.LangCpp}
	r.entries[types.LanguageCSharp] = registryEntry{cfg: CSharpExtractor(), lang: extraction.LangCSharp}
	r.entries[types.LanguageSwift] = registryEntry{cfg: SwiftExtractor(), lang: extraction.LangSwift}
	r.entries[types.LanguageKotlin] = registryEntry{cfg: KotlinExtractor(), lang: extraction.LangKotlin}
	r.entries[types.LanguageScala] = registryEntry{cfg: ScalaExtractor(), lang: extraction.LangScala}
	r.entries[types.LanguageRuby] = registryEntry{cfg: RubyExtractor(), lang: extraction.LangRuby}
	r.entries[types.LanguagePHP] = registryEntry{cfg: PHPExtractor(), lang: extraction.LangPHP}
	r.entries[types.LanguageLua] = registryEntry{cfg: LuaExtractor(), lang: extraction.LangLua}
	r.entries[types.LanguageLuau] = registryEntry{cfg: LuauExtractor(), lang: extraction.LangLuau}
	r.entries[types.LanguageDart] = registryEntry{cfg: DartExtractor(), lang: extraction.LangDart}
	r.entries[types.LanguageObjC] = registryEntry{cfg: ObjCExtractor(), lang: extraction.LangObjC}
	r.entries[types.LanguagePascal] = registryEntry{cfg: PascalExtractor(), lang: extraction.LangPascal}
	r.entries[types.LanguageElixir] = registryEntry{cfg: ElixirExtractor(), lang: extraction.LangElixir}
	return r
}

// For returns the LanguageExtractor config and extraction.Lang for the given
// types.Language. Returns (zero, 0, false) when the language is not registered.
func (r *Registry) For(lang types.Language) (extraction.LanguageExtractor, extraction.Lang, bool) {
	entry, ok := r.entries[lang]
	if !ok {
		return extraction.LanguageExtractor{}, 0, false
	}
	return entry.cfg, entry.lang, true
}
