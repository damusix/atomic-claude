package standalone

import "testing"

func TestIsQueryBuilderCallee(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"selectFrom", true},
		{"SELECTFROM", true},
		{"innerJoin", true},
		{"INNERJOIN", true},
		{"from_", true},
		{"table_", true},
		{"callproc", true},
		{"column", true},
		{"field", true},
		{"tableName", true},
		{"withTableName", true},
		{"toTable", true},
		{"hasColumnName", true},
		{"hasTableName", true},
		{"entityName", true},
		{"fetchUser", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsQueryBuilderCallee(c.name); got != c.want {
			t.Errorf("IsQueryBuilderCallee(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestQueryBuilderCalleesCount(t *testing.T) {
	if got := len(QueryBuilderCallees); got != 28 {
		t.Errorf("len(QueryBuilderCallees) = %d, want 28", got)
	}
}
