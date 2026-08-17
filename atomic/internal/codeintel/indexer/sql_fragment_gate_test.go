package indexer

// sql_fragment_gate_test.go — C8 fragment gate + tokenizer unit tests.
// White-box (package indexer) so matchesSQLFragmentGate/tokenizeSQLFragment
// can be exercised directly without a full orchestrator index run.

import (
	"strings"
	"testing"
)

func TestMatchesSQLFragmentGate(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"161 chars rejected (over length cap)", strings.Repeat("a", 161) + " = 1", false},
		{"160 chars with discriminator accepted", strings.Repeat("a", 156) + " = 1", true},
		{"prose with no discriminator rejected", "hello world status", false},
		{"discriminator but no identifier rejected", "= = ?", false},
		{"comparison + identifier accepted", "name = ?", true},
		{"connective keyword accepted", "created_at DESC", true},
		{"comma list accepted", "isbn, out_of_print", true},
		{"named placeholder accepted", ":param", true},
		{"named placeholder with identifier accepted", "status = :param", true},
		{"dollar placeholder accepted", "user_id = $1", true},
		{"bare question mark accepted", "email = ?", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchesSQLFragmentGate(c.text)
			if got != c.want {
				t.Errorf("matchesSQLFragmentGate(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func TestTokenizeSQLFragment(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"qualified pair kept as one token", "orders.created_at = ?", []string{"orders.created_at"}},
		{"keyword stoplisted case-insensitively", "title LIKE ?", []string{"title"}},
		{"connective keyword dropped, surrounding identifiers survive", "amt AND qty", []string{"amt", "qty"}},
		{"comma list keeps both identifiers", "isbn, out_of_print", []string{"isbn", "out_of_print"}},
		{"order by desc keeps only identifier", "created_at DESC", []string{"created_at"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tokenizeSQLFragment(c.text)
			if len(got) != len(c.want) {
				t.Fatalf("tokenizeSQLFragment(%q) = %v, want %v", c.text, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("tokenizeSQLFragment(%q)[%d] = %q, want %q", c.text, i, got[i], c.want[i])
				}
			}
		})
	}
}
