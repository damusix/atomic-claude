// Package frontmatter parses and emits YAML frontmatter in markdown files.
// Format: "---\n<yaml>\n---\n<body>". Body is preserved byte-for-byte.
//
// YAML scalars that look like dates (e.g. 2026-05-16) are kept as strings to
// prevent yaml.v3 from silently coercing them to time.Time values, which would
// break the round-trip guarantee.
package frontmatter

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const delimiter = "---"

// splitFrontmatter is shared by Parse and ParseOrdered so an edge-case fix
// reaches both. ok is false when there is no leading "---\n" at all, and
// callers must then return the input unchanged as body.
func splitFrontmatter(input string) (yamlBlock, body string, ok bool, err error) {
	const open = delimiter + "\n"
	if !strings.HasPrefix(input, open) {
		return "", input, false, nil
	}

	rest := input[len(open):]

	var afterClose string
	if strings.HasPrefix(rest, delimiter+"\n") {
		// Empty block: ---\n---\n
		yamlBlock = ""
		afterClose = rest[len(delimiter)+1:]
	} else if strings.HasPrefix(rest, delimiter) && len(rest) == len(delimiter) {
		// Closing delimiter at EOF, no newline.
		yamlBlock = ""
		afterClose = ""
	} else {
		idx := strings.Index(rest, "\n"+delimiter)
		if idx < 0 {
			return "", "", true, fmt.Errorf("frontmatter: missing closing delimiter '---'")
		}
		yamlBlock = rest[:idx]
		tail := rest[idx+1+len(delimiter):]
		if strings.HasPrefix(tail, "\n") {
			tail = tail[1:]
		}
		afterClose = tail
	}

	return yamlBlock, afterClose, true, nil
}

// Parse splits a markdown document into its YAML frontmatter and body. An
// absent or empty block yields nil meta; a missing closing delimiter or invalid
// YAML is an error.
func Parse(input string) (meta map[string]any, body string, err error) {
	yamlBlock, afterClose, ok, splitErr := splitFrontmatter(input)
	if !ok {
		return nil, input, nil
	}
	if splitErr != nil {
		return nil, "", splitErr
	}

	body = afterClose

	if strings.TrimSpace(yamlBlock) == "" {
		return nil, body, nil
	}

	// yaml.Node rather than a map: decoding straight to any coerces date-shaped
	// scalars to time.Time.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlBlock), &doc); err != nil {
		return nil, "", fmt.Errorf("frontmatter: invalid YAML: %w", err)
	}
	if doc.Kind == 0 {
		return nil, body, nil
	}

	m, err := nodeToMap(&doc)
	if err != nil {
		return nil, "", fmt.Errorf("frontmatter: %w", err)
	}
	if len(m) == 0 {
		return nil, body, nil
	}
	return m, body, nil
}

func nodeToMap(n *yaml.Node) (map[string]any, error) {
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeToMap(n.Content[0])
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node, got kind %v", n.Kind)
	}
	m := make(map[string]any, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		val, err := nodeToValue(n.Content[i+1])
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	return m, nil
}

// nodeToValue returns scalars as their raw string Value to avoid coercion.
func nodeToValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value, nil
	case yaml.MappingNode:
		return nodeToMap(n)
	case yaml.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			v, err := nodeToValue(child)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported node kind %v", n.Kind)
	}
}

// KV is a key-value pair for ordered frontmatter emission and ordered parsing.
type KV struct {
	Key   string
	Value any
}

// ParseOrdered is the order-preserving sibling of Parse: keys come back in
// YAML source order rather than a map's arbitrary order. Rules match Parse.
func ParseOrdered(input string) (kvs []KV, body string, err error) {
	yamlBlock, afterClose, ok, splitErr := splitFrontmatter(input)
	if !ok {
		return nil, input, nil
	}
	if splitErr != nil {
		return nil, "", splitErr
	}

	body = afterClose

	if strings.TrimSpace(yamlBlock) == "" {
		return nil, body, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlBlock), &doc); err != nil {
		return nil, "", fmt.Errorf("frontmatter: invalid YAML: %w", err)
	}
	if doc.Kind == 0 {
		return nil, body, nil
	}

	ordered, err := nodeToOrdered(&doc)
	if err != nil {
		return nil, "", fmt.Errorf("frontmatter: %w", err)
	}
	if len(ordered) == 0 {
		return nil, body, nil
	}
	return ordered, body, nil
}

func nodeToOrdered(n *yaml.Node) ([]KV, error) {
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeToOrdered(n.Content[0])
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node, got kind %v", n.Kind)
	}
	kvs := make([]KV, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		val, err := nodeToValue(n.Content[i+1])
		if err != nil {
			return nil, err
		}
		kvs = append(kvs, KV{Key: key, Value: val})
	}
	return kvs, nil
}

// Emit round-trips with Parse. Keys are sorted ascending so equal input maps
// always produce byte-identical output.
func Emit(meta map[string]any, body string) (string, error) {
	if len(meta) == 0 {
		return body, nil
	}

	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	kvs := make([]KV, 0, len(keys))
	for _, k := range keys {
		kvs = append(kvs, KV{Key: k, Value: meta[k]})
	}
	return EmitOrdered(kvs, body)
}

// EmitOrdered emits keys in exactly the order of kvs. Empty kvs yields the
// body with no frontmatter block.
func EmitOrdered(kvs []KV, body string) (string, error) {
	if len(kvs) == 0 {
		return body, nil
	}

	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, kv := range kvs {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv.Key}
		valNode, err := anyToNode(kv.Value)
		if err != nil {
			return "", fmt.Errorf("frontmatter: marshal error: %w", err)
		}
		mapping.Content = append(mapping.Content, keyNode, valNode)
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}

	yamlBytes, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("frontmatter: marshal error: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(delimiter)
	sb.WriteByte('\n')
	sb.Write(yamlBytes)
	sb.WriteString(delimiter)
	sb.WriteByte('\n')
	sb.WriteString(body)
	return sb.String(), nil
}

func anyToNode(v any) (*yaml.Node, error) {
	switch val := v.(type) {
	case string:
		// Deliberately untagged: a "!!str" tag makes yaml.v3 quote the scalar,
		// which mangles readable dates like 2026-05-16.
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val}, nil
	case map[string]any:
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			childNode, err := anyToNode(val[k])
			if err != nil {
				return nil, err
			}
			mapping.Content = append(mapping.Content, keyNode, childNode)
		}
		return mapping, nil
	case []any:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range val {
			child, err := anyToNode(item)
			if err != nil {
				return nil, err
			}
			seq.Content = append(seq.Content, child)
		}
		return seq, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}
