package managedfile

import "testing"

// Removing the block must not disturb a byte of the user's text around it.
func TestRemoveBlockKeepsSurroundingText(t *testing.T) {
	in := "before\n<atomic>\nowned\n</atomic>\nafter\n"
	got, err := RemoveBlock([]byte(in))
	if err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}
	if string(got) != "before\nafter\n" {
		t.Errorf("RemoveBlock = %q", got)
	}
}

func TestRemoveBlockRefusesAmbiguousOrAbsentBlock(t *testing.T) {
	for _, in := range []string{"no block\n", "<atomic>\nunclosed\n", "<atomic>\n</atomic>\n<atomic>\n</atomic>\n"} {
		if _, err := RemoveBlock([]byte(in)); err == nil {
			t.Errorf("RemoveBlock(%q) succeeded", in)
		}
	}
}

// AppendBlock then RemoveBlock returns the user's bytes exactly when the text
// ended in a newline. Text with no final newline gains the one AppendBlock added.
func TestRemoveBlockUndoesAppendBlock(t *testing.T) {
	block := BlockDocument([]byte("owned"))
	for _, user := range []string{"", "text\n", "text\n\n", "# h\n\npara\n"} {
		got, err := RemoveBlock(AppendBlock([]byte(user), block))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != user {
			t.Errorf("round trip of %q = %q", user, got)
		}
	}
	got, err := RemoveBlock(AppendBlock([]byte("text"), block))
	if err != nil || string(got) != "text\n" {
		t.Errorf("round trip of text with no final newline = %q, %v; want the one added newline", got, err)
	}
}

func TestRemoveBlockKeepsABlankLineBeforeABlock(t *testing.T) {
	got, err := RemoveBlock([]byte("a\n\n<atomic>\nowned\n</atomic>\n"))
	if err != nil || string(got) != "a\n\n" {
		t.Errorf("RemoveBlock = %q, %v; want a\\n\\n", got, err)
	}
}
