package repl

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "plain assignments",
			body: "TOKEN=abc123\nREGION=us-east-1\n",
			want: []string{"TOKEN=abc123", "REGION=us-east-1"},
		},
		{
			name: "comments and blank lines are skipped",
			body: "# leading comment\n\nTOKEN=abc\n\n   # indented comment\nREGION=us\n",
			want: []string{"TOKEN=abc", "REGION=us"},
		},
		{
			name: "surrounding whitespace is trimmed",
			body: "  TOKEN  =  abc  \n",
			want: []string{"TOKEN=abc"},
		},
		{
			name: "double quotes are stripped",
			body: `TOKEN="a b c"` + "\n",
			want: []string{"TOKEN=a b c"},
		},
		{
			name: "single quotes are stripped",
			body: "TOKEN='a b c'\n",
			want: []string{"TOKEN=a b c"},
		},
		{
			name: "no variable expansion",
			body: "HOME_COPY=$HOME\nNESTED=${TOKEN}\n",
			want: []string{"HOME_COPY=$HOME", "NESTED=${TOKEN}"},
		},
		{
			name: "a hash inside a value is not a comment",
			body: "COLOR=#ff0000\nQUOTED=\"a # b\"\n",
			want: []string{"COLOR=#ff0000", "QUOTED=a # b"},
		},
		{
			name: "the first equals splits, later ones are value",
			body: "DSN=postgres://u:p@h/db?opt=1\n",
			want: []string{"DSN=postgres://u:p@h/db?opt=1"},
		},
		{
			name: "an empty value is a real assignment",
			body: "EMPTY=\nQUOTED_EMPTY=\"\"\n",
			want: []string{"EMPTY=", "QUOTED_EMPTY="},
		},
		{
			name: "an unmatched quote is literal",
			body: `TOKEN="abc` + "\n",
			want: []string{`TOKEN="abc`},
		},
		{
			name: "file order is preserved so a repeat wins last",
			body: "TOKEN=first\nTOKEN=second\n",
			want: []string{"TOKEN=first", "TOKEN=second"},
		},
		{
			name: "an empty file yields nothing",
			body: "\n# only a comment\n",
			want: nil,
		},
		{
			name: "a file with no trailing newline still parses",
			body: "TOKEN=abc",
			want: []string{"TOKEN=abc"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEnvFile(writeEnvFile(t, tc.body))
			if err != nil {
				t.Fatalf("ParseEnvFile: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseEnvFile = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseEnvFile_MalformedLineNamesTheLineNumber(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"no equals sign", "TOKEN=abc\nJUST_A_WORD\n"},
		{"empty key", "TOKEN=abc\n=orphan\n"},
		{"key with a space", "TOKEN=abc\nTWO WORDS=x\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A typo in an env file must not silently produce a session
			// missing the variable the code under eval depends on.
			_, err := ParseEnvFile(writeEnvFile(t, tc.body))
			if err == nil {
				t.Fatal("ParseEnvFile accepted a malformed line")
			}
			if !strings.Contains(err.Error(), "line 2") {
				t.Errorf("error %q does not name the offending line", err)
			}
		})
	}
}

func TestParseEnvFile_AbsentIsAnIsNotExistError(t *testing.T) {
	_, err := ParseEnvFile(filepath.Join(t.TempDir(), "nope.env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ParseEnvFile on an absent file = %v, want an os.ErrNotExist error", err)
	}
}
