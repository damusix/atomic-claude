package repl

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed harness/python_harness.py harness/node_harness.js
var harnessFS embed.FS

// Canonical language identifiers. The CLI accepts aliases (python, node,
// javascript) and resolves them to these before anything here is called.
const (
	LangPython = "py"
	LangNode   = "js"
)

// harnessFile pairs an embedded harness with the name it must be written under
// when materialized to disk.
type harnessFile struct {
	embedded     string
	materialized string
}

var harnessFiles = map[string]harnessFile{
	LangPython: {embedded: "harness/python_harness.py", materialized: "python_harness.py"},
	// The Node harness is an ES module, and the extension is the only thing
	// that says so where it lands: a materialized script under ~/.atomic/repl
	// has no package.json of its own, and whichever one Node finds by walking
	// up from there is somebody else's. ".mjs" fixes the module system
	// regardless — ".js" would inherit that stranger's "type" field.
	LangNode: {embedded: "harness/node_harness.js", materialized: "node_harness.mjs"},
}

// HarnessScript returns the embedded harness source for lang, which must be a
// canonical identifier (LangPython or LangNode).
func HarnessScript(lang string) ([]byte, error) {
	file, ok := harnessFiles[lang]
	if !ok {
		return nil, unknownLangError(lang)
	}
	data, err := harnessFS.ReadFile(file.embedded)
	if err != nil {
		return nil, fmt.Errorf("repl: read embedded harness %s: %w", file.embedded, err)
	}
	return data, nil
}

// HarnessFilename returns the name a materialized harness for lang must be
// written under. It is not cosmetic — see the LangNode entry above.
func HarnessFilename(lang string) (string, error) {
	file, ok := harnessFiles[lang]
	if !ok {
		return "", unknownLangError(lang)
	}
	return file.materialized, nil
}

func unknownLangError(lang string) error {
	langs := make([]string, 0, len(harnessFiles))
	for name := range harnessFiles {
		langs = append(langs, name)
	}
	sort.Strings(langs)
	return fmt.Errorf("repl: unknown language %q; valid languages: %s", lang, strings.Join(langs, ", "))
}
