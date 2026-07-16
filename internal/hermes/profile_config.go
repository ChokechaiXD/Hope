package hermes

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func renderActivatedConfig(home string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	document := &yaml.Node{Kind: yaml.DocumentNode}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, document); err != nil {
			return nil, fmt.Errorf("decode Hermes config: %w", err)
		}
	}
	if len(document.Content) == 0 {
		document.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("Hermes config root must be a mapping")
	}
	memory := mappingValue(root, "memory")
	if memory == nil {
		memory = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendMapping(root, "memory", memory)
	} else if memory.Kind != yaml.MappingNode {
		memory.Kind = yaml.MappingNode
		memory.Tag = "!!map"
		memory.Value = ""
		memory.Content = nil
	}
	setScalar(memory, "provider", "cortex")
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode Hermes config: %w", err)
	}
	return encoded, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func appendMapping(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value,
	)
}

func setScalar(mapping *yaml.Node, key, value string) {
	if existing := mappingValue(mapping, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		existing.Content = nil
		return
	}
	appendMapping(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}
