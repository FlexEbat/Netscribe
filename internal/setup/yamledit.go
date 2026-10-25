package setup

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlDoc edits a config file as a tree, so the comments and the order of the keys of the
// person's file stay as they were.
type yamlDoc struct {
	root yaml.Node
}

func parseYAML(data []byte) (*yamlDoc, error) {
	d := &yamlDoc{}
	if err := yaml.Unmarshal(data, &d.root); err != nil {
		return nil, err
	}
	if d.root.Kind == 0 { // an empty file
		d.root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if d.root.Kind != yaml.DocumentNode || len(d.root.Content) != 1 || d.root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the config is not a mapping of keys")
	}
	return d, nil
}

func (d *yamlDoc) top() *yaml.Node { return d.root.Content[0] }

// child returns the value node of key in a mapping, or nil.
func child(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func (d *yamlDoc) find(path []string) *yaml.Node {
	n := d.top()
	for _, k := range path {
		if n = child(n, k); n == nil {
			return nil
		}
	}
	return n
}

// set puts value at path and creates the mappings on the way. A value that already exists
// keeps its comments.
func (d *yamlDoc) set(path []string, value *yaml.Node) error {
	n := d.top()
	for i, k := range path {
		last := i == len(path)-1
		next := child(n, k)
		if next == nil {
			key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			if last {
				next = value
			} else {
				next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			n.Content = append(n.Content, key, next)
		} else if last {
			value.HeadComment, value.LineComment, value.FootComment = next.HeadComment, next.LineComment, next.FootComment
			*next = *value
		}
		if !last && next.Kind != yaml.MappingNode {
			return fmt.Errorf("%s is not a section", strings.Join(path[:i+1], "."))
		}
		n = next
	}
	return nil
}

func (d *yamlDoc) remove(path []string) {
	parent := d.top()
	if len(path) > 1 {
		parent = d.find(path[:len(path)-1])
	}
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}
	last := path[len(path)-1]
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == last {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return
		}
	}
}

func (d *yamlDoc) bytes() ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&d.root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// value renders a node for a person: a scalar as it is, a list joined with commas.
func value(n *yaml.Node) string {
	switch {
	case n == nil:
		return ""
	case n.Kind == yaml.SequenceNode:
		var parts []string
		for _, c := range n.Content {
			parts = append(parts, c.Value)
		}
		return strings.Join(parts, ", ")
	default:
		return n.Value
	}
}

func count(n *yaml.Node) int {
	if n == nil || n.Kind != yaml.SequenceNode {
		return 0
	}
	return len(n.Content)
}

func scalarNode(tag, v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v}
}

func strNode(v string) *yaml.Node { return scalarNode("!!str", v) }

func boolNode(b bool) *yaml.Node {
	if b {
		return scalarNode("!!bool", "true")
	}
	return scalarNode("!!bool", "false")
}

// listNode makes a list on one line, like [a, b]. An item of digits only stays a number
// when asNumbers is set.
func listNode(items []string, asNumbers bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, it := range items {
		if asNumbers {
			n.Content = append(n.Content, scalarNode("!!int", it))
		} else {
			n.Content = append(n.Content, strNode(it))
		}
	}
	return n
}
