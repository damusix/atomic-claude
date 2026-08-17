package standalone

import "strings"

// QueryBuilderCallees names query-builder / ORM entry points (Kysely, Knex, and
// similar). A SQL string-match ref whose CalleeExpr hits this set is upgraded
// from medium to high confidence; the hit alone never creates an edge, an
// object-name match is still required. Keys are lowercase — compare via
// IsQueryBuilderCallee.
var QueryBuilderCallees = map[string]bool{
	"selectfrom":    true,
	"insertinto":    true,
	"updatetable":   true,
	"deletefrom":    true,
	"replaceinto":   true,
	"mergeinto":     true,
	"from":          true,
	"into":          true,
	"table":         true,
	"join":          true,
	"innerjoin":     true,
	"leftjoin":      true,
	"rightjoin":     true,
	"fulljoin":      true,
	"crossjoin":     true,
	"joinraw":       true,
	"call":          true,
	"callproc":      true,
	"from_":         true,
	"table_":        true,
	"column":        true,
	"field":         true,
	"tablename":     true,
	"withtablename": true,
	"totable":       true,
	"hascolumnname": true,
	"hastablename":  true,
	"entityname":    true,
}

// IsQueryBuilderCallee reports whether name is in QueryBuilderCallees,
// case-insensitively.
func IsQueryBuilderCallee(name string) bool {
	if name == "" {
		return false
	}
	return QueryBuilderCallees[strings.ToLower(name)]
}
