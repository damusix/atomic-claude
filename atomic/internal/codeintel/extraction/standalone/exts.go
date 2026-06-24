package standalone

import "strings"

// SQLExtensions is the single canonical list of file extensions handled by
// the standalone SQL extractor. Any package that needs to recognise or route
// SQL files must reference this slice — not re-type the four literals.
//
// Consumers:
//   - standalone.NewRegistry (this package): wires each ext → SQLExtractor.
//   - indexer/orchestrator.go extToLanguage + standaloneExts: routes SQL files.
//   - resolution/pipeline.go isStandaloneSQLExt: guards direct-SQL resolution path.
var SQLExtensions = []string{
	".sql",
	".ddl",
	".pgsql",
	".mysql",
	".sql.jinja",
}

// IsSQLExt reports whether filePath has one of the canonical SQL file extensions.
// The match is case-insensitive (path is lowercased before comparison).
func IsSQLExt(filePath string) bool {
	lower := strings.ToLower(filePath)
	for _, ext := range SQLExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
