package clash

import "gopkg.in/yaml.v3"

// Ordered is a YAML mapping that marshals in insertion order, so generated
// configs read like hand-written ones (name first, then type, then options).
type Ordered struct {
	keys []string
	vals []any
}

func NewOrdered() *Ordered { return &Ordered{} }

func (o *Ordered) Set(key string, value any) *Ordered {
	for i, k := range o.keys {
		if k == key {
			o.vals[i] = value
			return o
		}
	}
	o.keys = append(o.keys, key)
	o.vals = append(o.vals, value)
	return o
}

func (o *Ordered) Get(key string) (any, bool) {
	for i, k := range o.keys {
		if k == key {
			return o.vals[i], true
		}
	}
	return nil, false
}

func (o *Ordered) Keys() []string { return o.keys }

func (o *Ordered) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for i, key := range o.keys {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		valNode := &yaml.Node{}
		if err := valNode.Encode(o.vals[i]); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, keyNode, valNode)
	}
	return node, nil
}
