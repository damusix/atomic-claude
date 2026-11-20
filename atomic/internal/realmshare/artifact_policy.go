package realmshare

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const artifactSourceLimit = 64 << 10

var (
	errArtifactSourceUnavailable = errors.New("artifact source unavailable")
	errUnverifiedArtifactOption  = errors.New("has no verified native behavior")
)

// ArtifactReview describes the authored source and the effects of linking it.
type ArtifactReview struct {
	SourceFile string
	Text       string
	Notes      []string
	Native     []ArtifactActivation

	digests         map[string]string
	identityMissing bool
}

func artifactKind(name string) MappingKind {
	switch {
	case artifactUnit(name, ".agents/skills/", ""):
		return KindSkill
	case artifactUnit(name, ".agents/commands/", ".md"):
		return KindCommand
	case artifactUnit(name, ".agents/agents/", ".md"):
		return KindAgent
	default:
		return ""
	}
}

func reviewArtifactSource(root *os.Root, name string, kind MappingKind) (*ArtifactReview, error) {
	review, err := reviewArtifact(name, kind, true, liveArtifactReader(root))
	if err != nil || review.SourceFile == "" {
		return nil, err
	}
	return &review, nil
}

func liveArtifactReader(root *os.Root) func(string, int64) ([]byte, error) {
	return func(name string, limit int64) ([]byte, error) {
		if limit == 0 {
			_, err := lstatBelow(root, name)
			return nil, err
		}
		file, err := openRegular(root, name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, limit+1))
	}
}

func reviewArtifact(name string, kind MappingKind, preview bool, read func(string, int64) ([]byte, error)) (ArtifactReview, error) {
	primary := name
	switch kind {
	case KindSkill:
		primary += "/SKILL.md"
	case KindAgent, KindCommand:
	default:
		return ArtifactReview{}, nil
	}
	var review ArtifactReview
	if preview {
		review = ArtifactReview{SourceFile: primary, digests: make(map[string]string)}
		review.Notes = append(review.Notes, "Native invocation can use tools or run supporting scripts with your existing permissions. Linking trusts subsequent content updates from the bound remote.")
	}
	text, err := readArtifactFile(primary, false, read)
	if err != nil {
		return ArtifactReview{}, fmt.Errorf("%w: %w", errArtifactSourceUnavailable, err)
	}
	frontmatter, body, err := artifactFrontmatter(text)
	if err != nil {
		return ArtifactReview{}, fmt.Errorf("artifact %q: %w", primary, err)
	}
	node, err := artifactMetadata(frontmatter)
	if err == nil {
		review.identityMissing, err = validateArtifactFrontmatter(node, name, kind)
	}
	if err != nil {
		return ArtifactReview{}, fmt.Errorf("artifact %q: %w", primary, err)
	}
	if preview {
		review.Text = string(text)
		review.digests[primary] = fmt.Sprintf("%x", sha256.Sum256(text))
		if bytes.Contains(body, []byte("!`")) || bytes.Contains(body, []byte("```!")) {
			review.Notes = append(review.Notes, "This source contains dynamic command expansion syntax. A supporting harness may run it at invocation under existing permissions.")
		}
	}
	if kind != KindSkill {
		return review, nil
	}
	plugin := name + "/.claude-plugin"
	if _, err := read(plugin, 0); err == nil {
		return ArtifactReview{}, fmt.Errorf("artifact %q: native plugin activation is prohibited", plugin)
	} else if !os.IsNotExist(err) {
		return ArtifactReview{}, fmt.Errorf("artifact %q: %w", plugin, err)
	}
	for _, suffix := range []string{"/agents/openai.yaml", "/SKILL.json"} {
		sidecar := name + suffix
		data, err := readArtifactFile(sidecar, true, read)
		if err != nil {
			return ArtifactReview{}, err
		}
		if data == nil {
			continue
		}
		if suffix == "/SKILL.json" && !json.Valid(data) {
			return ArtifactReview{}, fmt.Errorf("artifact %q: malformed JSON metadata", sidecar)
		}
		implicitDisabled, err := reviewArtifactSidecar(data, suffix == "/agents/openai.yaml")
		if err != nil {
			return ArtifactReview{}, fmt.Errorf("artifact %q: %w", sidecar, err)
		}
		if preview {
			review.digests[sidecar] = fmt.Sprintf("%x", sha256.Sum256(data))
			if implicitDisabled {
				review.Notes = append(review.Notes, fmt.Sprintf("Codex metadata in %q: policy.allow_implicit_invocation: false.", sidecar))
			}
		}
	}
	return review, nil
}

func readArtifactFile(name string, optional bool, read func(string, int64) ([]byte, error)) ([]byte, error) {
	data, err := read(name, artifactSourceLimit)
	if optional && os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("artifact %q: %w", name, err)
	}
	if err := validateArtifactText(data); err != nil {
		return nil, fmt.Errorf("artifact %q: %w", name, err)
	}
	if data == nil {
		return []byte{}, nil
	}
	return data, nil
}

func validateArtifactText(data []byte) error {
	if len(data) > artifactSourceLimit {
		return fmt.Errorf("exceeds the %d-byte review limit", artifactSourceLimit)
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("must be UTF-8 text without NUL bytes")
	}
	return nil
}

func artifactFrontmatter(data []byte) (metadata, body []byte, err error) {
	text := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text = bytes.TrimLeft(text, "\r\n")
	line, rest, _ := bytes.Cut(text, []byte("\n"))
	if !bytes.Equal(bytes.TrimSpace(line), []byte("---")) {
		return nil, data, nil
	}
	if !bytes.Equal(bytes.TrimSuffix(line, []byte("\r")), []byte("---")) {
		return nil, nil, fmt.Errorf("frontmatter opening delimiter must be '---' on its own line")
	}
	remaining := rest
	for len(remaining) != 0 {
		begin := len(rest) - len(remaining)
		line, remaining, _ = bytes.Cut(remaining, []byte("\n"))
		if bytes.Equal(bytes.TrimSpace(line), []byte("---")) {
			if !bytes.Equal(bytes.TrimSuffix(line, []byte("\r")), []byte("---")) {
				return nil, nil, fmt.Errorf("frontmatter closing delimiter must be '---' on its own line")
			}
			return rest[:begin], remaining, nil
		}
	}
	return nil, nil, fmt.Errorf("frontmatter is missing its closing '---' delimiter")
}

func artifactMetadata(data []byte) (*yaml.Node, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("malformed metadata: %w", err)
	}
	if err := decoder.Decode(new(yaml.Node)); err != io.EOF {
		return nil, fmt.Errorf("metadata must contain exactly one document")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode || doc.Content[0].ShortTag() != "!!map" {
		return nil, fmt.Errorf("metadata must be a mapping")
	}
	return doc.Content[0], nil
}

func declaredArtifactName(data []byte) (string, error) {
	if err := validateArtifactText(data); err != nil {
		return "", err
	}
	metadata, _, err := artifactFrontmatter(data)
	if err != nil {
		return "", err
	}
	node, err := artifactMetadata(metadata)
	if err != nil {
		return "", err
	}
	return artifactName(node)
}

func artifactName(node *yaml.Node) (string, error) {
	if node == nil {
		return "", nil
	}
	var name string
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.ShortTag() == "!!merge" {
			return "", fmt.Errorf("metadata merges can change the declared name")
		}
		if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" || key.Value != "name" {
			continue
		}
		if name != "" {
			return "", fmt.Errorf("malformed field %q: duplicate key", "name")
		}
		if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" || strings.TrimSpace(value.Value) == "" {
			return "", fmt.Errorf("malformed field %q: expected a nonempty string", "name")
		}
		name = value.Value
	}
	return name, nil
}

func validateArtifactFrontmatter(node *yaml.Node, unit string, kind MappingKind) (identityMissing bool, err error) {
	if node != nil {
		if err := validateArtifactFields(node, "", false, 0); err != nil {
			return false, err
		}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			switch key {
			case "name":
			case "description", "license", "compatibility":
				if value.ShortTag() != "!!str" {
					return false, fmt.Errorf("malformed field %q: expected a string", key)
				}
			case "metadata":
				if value.Kind != yaml.MappingNode {
					return false, fmt.Errorf("malformed field %q: expected a mapping of strings", key)
				}
				for j := 0; j < len(value.Content); j += 2 {
					if value.Content[j+1].ShortTag() != "!!str" {
						return false, fmt.Errorf("malformed field %q: expected a string", key+"."+value.Content[j].Value)
					}
				}
			default:
				return false, fmt.Errorf("field %q %w", key, errUnverifiedArtifactOption)
			}
		}
	}
	name, err := artifactName(node)
	if err != nil {
		return false, err
	}
	if kind == KindSkill || kind == KindAgent {
		if name == "" {
			return true, nil
		}
		expected := path.Base(unit)
		if kind == KindAgent {
			expected = strings.TrimSuffix(expected, ".md")
		}
		if name != expected {
			return false, fmt.Errorf("field %q must match the canonical unit name %q", "name", expected)
		}
	}
	return false, nil
}

func reviewArtifactSidecar(data []byte, openAI bool) (bool, error) {
	node, err := artifactMetadata(data)
	if err != nil {
		return false, err
	}
	if node == nil {
		return false, fmt.Errorf("metadata must be a mapping")
	}
	if err := validateArtifactFields(node, "", true, 0); err != nil {
		return false, err
	}
	if !openAI {
		return false, fmt.Errorf("sidecar schema %w", errUnverifiedArtifactOption)
	}
	implicitDisabled := false
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if key != "policy" {
			return false, fmt.Errorf("field %q %w", key, errUnverifiedArtifactOption)
		}
		if value.Kind != yaml.MappingNode {
			return false, fmt.Errorf("malformed field %q: expected a mapping", key)
		}
		for j := 0; j < len(value.Content); j += 2 {
			key, option := value.Content[j].Value, value.Content[j+1]
			field := "policy." + key
			if key != "allow_implicit_invocation" {
				return false, fmt.Errorf("field %q %w", field, errUnverifiedArtifactOption)
			}
			if option.ShortTag() != "!!bool" {
				return false, fmt.Errorf("malformed field %q: expected a boolean", field)
			}
			if option.Value != "false" {
				return false, fmt.Errorf("field %q: values other than false %w", field, errUnverifiedArtifactOption)
			}
			implicitDisabled = true
		}
	}
	return implicitDisabled, nil
}

func validateArtifactFields(node *yaml.Node, field string, sidecar bool, depth int) error {
	if depth > 32 {
		return fmt.Errorf("malformed field %q: metadata nesting exceeds 32 levels", field)
	}
	switch node.Kind {
	case yaml.MappingNode:
		if node.ShortTag() != "!!map" {
			return fmt.Errorf("malformed field %q: unsupported tag %q", field, node.ShortTag())
		}
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			name := key.Value
			if field != "" {
				name = field + "." + name
			}
			if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" || key.Value == "" {
				return fmt.Errorf("malformed field %q: metadata keys must be nonempty strings", name)
			}
			if seen[key.Value] {
				return fmt.Errorf("malformed field %q: duplicate key", name)
			}
			seen[key.Value] = true
			if depth == 0 || sidecar {
				if reason := prohibitedArtifactField(key.Value); reason != "" {
					return fmt.Errorf("field %q is prohibited: %s", name, reason)
				}
			}
			if err := validateArtifactFields(value, name, sidecar, depth+1); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		if node.ShortTag() != "!!seq" {
			return fmt.Errorf("malformed field %q: unsupported tag %q", field, node.ShortTag())
		}
		for _, child := range node.Content {
			if err := validateArtifactFields(child, field, sidecar, depth+1); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str", "!!int", "!!float", "!!bool", "!!null", "!!timestamp":
		default:
			return fmt.Errorf("malformed field %q: unsupported tag %q", field, node.ShortTag())
		}
	default:
		return fmt.Errorf("malformed field %q: YAML aliases are not supported", field)
	}
	return nil
}

func prohibitedArtifactField(key string) string {
	key = strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(key), "-", ""), "_", "")
	switch key {
	case "allowedtools", "permissions", "permissionmode", "tools", "tooldependencies":
		return "native tool grants or dependencies are not supported"
	case "hooks":
		return "native hooks are not supported"
	case "mcp", "mcpconfig", "mcpservers":
		return "native MCP configuration is not supported"
	case "plugin", "plugins", "enabledplugins", "additionalplugins":
		return "native plugin configuration is not supported"
	default:
		return ""
	}
}
