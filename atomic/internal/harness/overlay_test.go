package harness

import (
	"bytes"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/pelletier/go-toml/v2"
)

const projectedMarkdown = "---\nname: atomic-reviewer\ndescription: Reviews diffs.\nskills: atomic-verify\n---\n\nBody stays byte-identical.\n"

func projectedCodex(t *testing.T) []byte {
	t.Helper()
	data, err := toml.Marshal(CodexAgentDoc{Name: "atomic-reviewer", Description: "Reviews diffs.", DeveloperInstructions: "Body.\n"})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// No adapter may supply a default model or effort: with no stored override
// the projected bytes pass through, so the harness inherits the session's.
func TestOverlayEmptyOverrideWritesNoKey(t *testing.T) {
	for _, kind := range []Kind{KindClaude, KindOMP} {
		got, err := Overlay(kind, []byte(projectedMarkdown), config.AgentOverride{})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if string(got) != projectedMarkdown {
			t.Errorf("%s: empty override changed bytes:\n%s", kind, got)
		}
	}
	codex := projectedCodex(t)
	got, err := Overlay(KindCodex, codex, config.AgentOverride{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, codex) {
		t.Errorf("codex: empty override changed bytes:\n%s", got)
	}
}

func TestOverlayWritesNativeKeysPerHarness(t *testing.T) {
	ov := config.AgentOverride{Model: "m-1", Effort: "high"}
	for kind, effortKey := range map[Kind]string{KindClaude: "effort", KindOMP: "thinking"} {
		got, err := Overlay(kind, []byte(projectedMarkdown), ov)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		meta, body, err := frontmatter.Parse(string(got))
		if err != nil {
			t.Fatal(err)
		}
		if meta["model"] != "m-1" || meta[effortKey] != "high" {
			t.Errorf("%s frontmatter = %v, want model and %s", kind, meta, effortKey)
		}
		if meta["skills"] != "atomic-verify" || !strings.Contains(body, "Body stays byte-identical.") {
			t.Errorf("%s lost projected content:\n%s", kind, got)
		}
	}

	got, err := Overlay(KindCodex, projectedCodex(t), ov)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{`model = 'm-1'`, `model_reasoning_effort = 'high'`, `developer_instructions = `} {
		if !strings.Contains(string(got), line) {
			t.Errorf("codex document lacks %q:\n%s", line, got)
		}
	}
	doc, err := DecodeCodexAgent(got)
	if err != nil || doc.DeveloperInstructions != "Body.\n" {
		t.Errorf("codex instructions lost: %+v %v", doc, err)
	}
}

// One field set leaves the other key absent, never written empty.
func TestOverlaySingleFieldWritesOneKey(t *testing.T) {
	got, err := Overlay(KindClaude, []byte(projectedMarkdown), config.AgentOverride{Effort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	meta, _, _ := frontmatter.Parse(string(got))
	if _, ok := meta["model"]; ok {
		t.Errorf("effort-only override wrote a model key:\n%s", got)
	}
	got, err = Overlay(KindCodex, projectedCodex(t), config.AgentOverride{Model: "gpt-5.5"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "model_reasoning_effort") {
		t.Errorf("model-only override wrote an effort key:\n%s", got)
	}
}

// A key the Codex projection never writes would be dropped by a re-encode, so
// it is refused.
func TestOverlayRefusesAnUnknownCodexKey(t *testing.T) {
	doc := append(projectedCodex(t), []byte("sandbox_mode = 'read-only'\n")...)
	if _, err := Overlay(KindCodex, doc, config.AgentOverride{Model: "gpt-5.5"}); err == nil {
		t.Error("an unknown Codex key was silently dropped")
	}
}

func TestOverlayIsDeterministicAndLeavesItsInputUnchanged(t *testing.T) {
	in := []byte(projectedMarkdown)
	ov := config.AgentOverride{Model: "opus", Effort: "max"}
	first, err := Overlay(KindClaude, in, ov)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Overlay(KindClaude, in, ov)
	if !bytes.Equal(first, second) || string(in) != projectedMarkdown {
		t.Error("overlay is not deterministic or mutated its input")
	}
}

func TestOverlayRefusesUnknownKindAndMissingFrontmatter(t *testing.T) {
	ov := config.AgentOverride{Model: "opus"}
	if _, err := Overlay(Kind("pi"), []byte(projectedMarkdown), ov); err == nil {
		t.Error("unknown kind overlaid")
	}
	if _, err := Overlay(KindClaude, []byte("no frontmatter\n"), ov); err == nil {
		t.Error("document without frontmatter overlaid")
	}
}
