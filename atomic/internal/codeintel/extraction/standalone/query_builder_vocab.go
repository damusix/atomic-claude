package standalone

// query_builder_vocab.go — C4: query-builder vocabulary.
//
// QueryBuilderCallees is the shared, exported set of bare callee names that
// resolution pass A (C2) checks a sql_string ref's CalleeExpr against to
// decide confidence tier: a hit upgrades medium to high. Membership alone
// never creates an edge — the object-name match in C2 is always required.

import "strings"

// QueryBuilderCallees is the flat set of bare callee names recognized as
// query-builder / ORM entry points (Kysely, Knex, and similar). Keys are
// lowercase; compare via IsQueryBuilderCallee for case-insensitive lookup.
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

// IsQueryBuilderCallee reports whether name (case-insensitive) is in the C4
// vocabulary.
func IsQueryBuilderCallee(name string) bool {
	if name == "" {
		return false
	}
	return QueryBuilderCallees[strings.ToLower(name)]
}
