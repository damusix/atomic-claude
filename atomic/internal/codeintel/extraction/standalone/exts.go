package standalone

import "strings"

// SQLExtensions is the canonical SQL extension list. Registry wiring, indexer
// routing, and resolution's direct-SQL guard all reference this slice rather
// than re-typing the literals.
var SQLExtensions = []string{
	".sql",
	".ddl",
	".pgsql",
	".mysql",
	".sql.jinja",
}

// IsSQLExt reports whether filePath ends in a canonical SQL extension,
// case-insensitively.
func IsSQLExt(filePath string) bool {
	lower := strings.ToLower(filePath)
	for _, ext := range SQLExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
