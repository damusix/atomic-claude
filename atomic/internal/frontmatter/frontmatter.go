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

// Parse splits a markdown document into its YAML frontmatter and body.
//
// Rules:
//   - If the document does not start with "---\n", it has no frontmatter:
//     meta is nil, body is the full input.
//   - If a closing "---\n" (or "---" at EOF) is missing, an error is returned.
//   - An empty YAML block ("---\n---\n") returns nil meta and the remainder as body.
//   - Invalid YAML returns an error.
func Parse(input string) (meta map[string]any, body string, err error) {
	const open = delimiter + "\n"
	if !strings.HasPrefix(input, open) {
		return nil, input, nil
	}

	rest := input[len(open):]

	// The closing delimiter may appear at position 0 (empty block) or after "\n".
	var yamlBlock string
	var afterClose string

	if strings.HasPrefix(rest, delimiter+"\n") {
		// Empty frontmatter block: ---\n---\n
		yamlBlock = ""
		afterClose = rest[len(delimiter)+1:]
	} else if strings.HasPrefix(rest, delimiter) && len(rest) == len(delimiter) {
		// Closing delimiter at EOF, no newline.
		yamlBlock = ""
		afterClose = ""
	} else {
		// Normal case: search for \n--- within rest.
		idx := strings.Index(rest, "\n"+delimiter)
		if idx < 0 {
			return nil, "", fmt.Errorf("frontmatter: missing closing delimiter '---'")
		}
		yamlBlock = rest[:idx]
		tail := rest[idx+1+len(delimiter):]
		if strings.HasPrefix(tail, "\n") {
			tail = tail[1:]
		}
		afterClose = tail
	}

	body = afterClose

	if strings.TrimSpace(yamlBlock) == "" {
		return nil, body, nil
	}

	// Decode via yaml.Node to avoid implicit type coercion (e.g. date strings
	// becoming time.Time). nodeToMap walks the mapping and returns raw scalars.
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

// nodeToMap converts a yaml.Node (document or mapping) into map[string]any
// using the raw Value of scalar nodes to avoid implicit type coercion.
func nodeToMap(n *yaml.Node) (map[string]any, error) {
	// Unwrap document node.
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

// nodeToValue converts a scalar, sequence, or mapping node to a Go value.
// Scalars are always returned as their raw string Value to avoid coercion.
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

// Emit serializes meta and body back into a frontmatter markdown document.
// If meta is nil or empty, only the body is returned (no frontmatter block).
// The output round-trips with Parse when the input was produced by Parse.
//
// Key order is deterministic (sorted ascending) so that byte-identical input
// maps always produce byte-identical output.
func Emit(meta map[string]any, body string) (string, error) {
	if len(meta) == 0 {
		return body, nil
	}

	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build a yaml.MappingNode with keys in sorted order so that Marshal
	// produces deterministic output regardless of map iteration order.
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range keys {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
		valNode, err := anyToNode(meta[k])
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

// anyToNode converts a Go value (as returned by Parse) to a yaml.Node.
func anyToNode(v any) (*yaml.Node, error) {
	switch val := v.(type) {
	case string:
		// No explicit tag: yaml.v3 emits plain scalars without quoting for
		// normal strings. When the value is ambiguous (looks like a date,
		// boolean, or number), yaml.v3 will quote it automatically. Using
		// "!!str" here causes yaml.v3 to always quote date-like values such
		// as "2026-05-16", which breaks human-readable frontmatter.
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
