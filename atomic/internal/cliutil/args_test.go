package cliutil

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

type testFlags struct {
	fs     *flag.FlagSet
	reason string
	json   bool
}

func newTestFlags() *testFlags {
	tf := &testFlags{fs: flag.NewFlagSet("test", flag.ContinueOnError)}
	tf.fs.SetOutput(io.Discard)
	tf.fs.StringVar(&tf.reason, "reason", "", "reason")
	tf.fs.BoolVar(&tf.json, "json", false, "json")
	return tf
}

func TestParseInterspersed_FlagPositions(t *testing.T) {
	cases := []struct {
		name string
		args []string
		pos  []string
	}{
		{"before", []string{"--reason", "r", "--json", "a", "b"}, []string{"a", "b"}},
		{"after", []string{"a", "b", "--reason", "r", "--json"}, []string{"a", "b"}},
		{"between", []string{"a", "--reason", "r", "b", "--json"}, []string{"a", "b"}},
		{"equals and single-dash forms", []string{"a", "--reason=r", "-json"}, []string{"a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tf := newTestFlags()
			pos, err := ParseInterspersed(tf.fs, c.args)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if !reflect.DeepEqual(pos, c.pos) {
				t.Errorf("positionals=%q, want %q", pos, c.pos)
			}
			if tf.reason != "r" || !tf.json {
				t.Errorf("reason=%q json=%v, want flags honored wherever they sit", tf.reason, tf.json)
			}
		})
	}
}

func TestParseInterspersed_TerminatorKeepsLaterFlagsPositional(t *testing.T) {
	tf := newTestFlags()
	pos, err := ParseInterspersed(tf.fs, []string{"a", "--", "b", "--reason", "x", "--"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	want := []string{"a", "b", "--reason", "x", "--"}
	if !reflect.DeepEqual(pos, want) {
		t.Errorf("positionals=%q, want %q", pos, want)
	}
	if tf.reason != "" {
		t.Errorf("reason=%q, want flag after -- left unparsed", tf.reason)
	}
}

func TestParseInterspersed_BoolFlagDoesNotSwallowNextToken(t *testing.T) {
	tf := newTestFlags()
	pos, err := ParseInterspersed(tf.fs, []string{"--json", "a"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(pos, []string{"a"}) || !tf.json {
		t.Errorf("positionals=%q json=%v", pos, tf.json)
	}
}

func TestParseInterspersed_ValueFlagTakesDashLeadingValueVerbatim(t *testing.T) {
	tf := newTestFlags()
	pos, err := ParseInterspersed(tf.fs, []string{"a", "--reason", "--json"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if tf.reason != "--json" || tf.json {
		t.Errorf("reason=%q json=%v, want value token consumed verbatim", tf.reason, tf.json)
	}
	if !reflect.DeepEqual(pos, []string{"a"}) {
		t.Errorf("positionals=%q", pos)
	}
}

func TestParseInterspersed_UnknownFlagAfterPositionalErrors(t *testing.T) {
	tf := newTestFlags()
	if _, err := ParseInterspersed(tf.fs, []string{"a", "--bogus"}); err == nil {
		t.Error("err=nil, want unknown flag rejected rather than kept as a positional")
	}
}

func TestUnexpectedArgs_NamesExtrasThenUsage(t *testing.T) {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	var out bytes.Buffer
	fs.SetOutput(&out)
	SetUsage(fs, "atomic demo")

	err := UnexpectedArgs(fs, "demo", []string{"x", "--y"})
	if err == nil {
		t.Fatal("err=nil, want an error the caller can return")
	}
	want := "atomic demo: unexpected argument(s): [\"x\" \"--y\"]\nUsage: atomic demo\n"
	if !strings.HasPrefix(out.String(), want) {
		t.Errorf("output=%q, want prefix %q: message names the strays, then usage follows", out.String(), want)
	}
}

func TestParseInterspersed_HelpReturnsErrHelp(t *testing.T) {
	tf := newTestFlags()
	if _, err := ParseInterspersed(tf.fs, []string{"a", "-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err=%v, want flag.ErrHelp so callers can tell help from misuse", err)
	}
}
