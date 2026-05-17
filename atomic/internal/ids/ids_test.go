package ids_test

import (
	"regexp"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/ids"
)

func TestSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"UPPER CASE", "upper-case"},
		{"multiple   spaces", "multiple-spaces"},
		{"café au lait", "cafe-au-lait"},
		{"foo_bar", "foobar"},
		{"foo!@#$%bar", "foobar"},
		{"foo-bar", "foo-bar"},
		{"123 numbers", "123-numbers"},
		{"already-kebab", "already-kebab"},
		{"", ""},
		{"---", ""},
		{"Fix the auth race in middleware", "fix-the-auth-race-in-middleware"},
	}
	for _, tc := range tests {
		got := ids.Slug(tc.input)
		if got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestShortID_ValidPrefix(t *testing.T) {
	re := regexp.MustCompile(`^r-[0-9a-f]{4}$`)

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, err := ids.ShortID("r")
		if err != nil {
			t.Fatalf("ShortID(\"r\") error: %v", err)
		}
		if !re.MatchString(id) {
			t.Errorf("ShortID(\"r\") = %q, does not match r-[0-9a-f]{4}", id)
		}
		seen[id] = true
	}

	// With 4 hex chars = 65536 possibilities, 50 samples should yield >1 unique.
	if len(seen) < 2 {
		t.Errorf("ShortID(\"r\") produced only %d unique values in 50 calls — likely not random", len(seen))
	}
}

func TestShortID_EmptyPrefix(t *testing.T) {
	_, err := ids.ShortID("")
	if err == nil {
		t.Fatal("ShortID(\"\") expected error, got nil")
	}
}

func TestShortID_InvalidPrefix(t *testing.T) {
	cases := []string{"R", "1abc", "foo-bar", "foo bar", "FOO", "_prefix"}
	for _, prefix := range cases {
		_, err := ids.ShortID(prefix)
		if err == nil {
			t.Errorf("ShortID(%q) expected error for invalid prefix, got nil", prefix)
		}
	}
}

func TestShortID_PrefixFormat(t *testing.T) {
	// prefix "r" must produce "r-XXXX"
	re := regexp.MustCompile(`^r-[0-9a-f]{4}$`)
	id, err := ids.ShortID("r")
	if err != nil {
		t.Fatalf("ShortID(\"r\") error: %v", err)
	}
	if !re.MatchString(id) {
		t.Errorf("ShortID(\"r\") = %q, want r-[0-9a-f]{4}", id)
	}

	// prefix "abc123" must produce "abc123-XXXX"
	re2 := regexp.MustCompile(`^abc123-[0-9a-f]{4}$`)
	id2, err := ids.ShortID("abc123")
	if err != nil {
		t.Fatalf("ShortID(\"abc123\") error: %v", err)
	}
	if !re2.MatchString(id2) {
		t.Errorf("ShortID(\"abc123\") = %q, want abc123-[0-9a-f]{4}", id2)
	}
}
