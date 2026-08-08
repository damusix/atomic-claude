package bus

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReadAction_JSONAndHuman_FullTextNeverCollapsed(t *testing.T) {
	home := testBusHome(t)
	// 30 lines — well past TailLine's collapse threshold; read must never
	// elide, that is the verb's entire reason to exist.
	longText := strings.TrimSuffix(strings.Repeat("line of the payload\n", 30), "\n")
	env := Envelope{
		ID: "m-full", Room: "exp", From: "gui-fe", FromKind: KindAgent,
		To: []string{"api-be"}, ReplyTo: "m-prior", Ts: time.Unix(1786200000, 0), Text: longText,
	}
	if err := Append(home, "exp", env); err != nil {
		t.Fatalf("append: %v", err)
	}

	var out bytes.Buffer
	if code := readAction([]string{"exp", "m-full", "--json"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var got Envelope
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json output unparseable: %v", err)
	}
	if got.Text != longText || got.ID != "m-full" {
		t.Fatalf("json envelope mismatch: id=%q textLen=%d", got.ID, len(got.Text))
	}

	out.Reset()
	if code := readAction([]string{"exp", "m-full"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want 0", code)
	}
	rendered := out.String()
	if strings.Count(rendered, "line of the payload") != 30 {
		t.Fatalf("human output collapsed: %d occurrences, want 30", strings.Count(rendered, "line of the payload"))
	}
	for _, want := range []string{"gui-fe", "to api-be", "[m-full]", "reply-to m-prior"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("human header missing %q in %q", want, strings.SplitN(rendered, "\n", 2)[0])
		}
	}
}

func TestReadAction_Failures(t *testing.T) {
	home := testBusHome(t)
	if err := Append(home, "exp", Envelope{ID: "m-1", Room: "exp", From: "a", FromKind: KindAgent, Ts: time.Unix(1, 0), Text: "x"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	var out bytes.Buffer
	if code := readAction([]string{"exp", "m-missing"}, home, &out); code != int(ExitHard) {
		t.Fatalf("missing id: exit = %d, want %d (ExitHard)", code, ExitHard)
	}
	if code := readAction([]string{"never-existed", "m-1"}, home, &out); code != int(ExitNoRoom) {
		t.Fatalf("missing log: exit = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
	if code := readAction([]string{"../escape", "m-1"}, home, &out); code != int(ExitUsage) {
		t.Fatalf("traversal room: exit = %d, want %d (ExitUsage)", code, ExitUsage)
	}
	if code := readAction([]string{"exp"}, home, &out); code != int(ExitUsage) {
		t.Fatalf("missing id arg: exit = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}
