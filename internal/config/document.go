package config

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"go.yaml.in/yaml/v3"
)

func parseDocument(data []byte) (map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("configuration must contain one YAML mapping")
		}
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parse YAML: %w", err)
		}
		return nil, fmt.Errorf("configuration must contain only one YAML document")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("configuration must be a YAML mapping")
	}
	value, err := documentValue(document.Content[0])
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func documentValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.MappingNode:
		if node.ShortTag() != "!!map" {
			return nil, fmt.Errorf("line %d: unsupported YAML mapping tag", node.Line)
		}
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
				return nil, fmt.Errorf("line %d: mapping keys must be strings; YAML merge keys are unsupported", key.Line)
			}
			if _, exists := result[key.Value]; exists {
				return nil, fmt.Errorf("line %d: duplicate key %q", key.Line, key.Value)
			}
			value, err := documentValue(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			result[key.Value] = value
		}
		return result, nil
	case yaml.SequenceNode:
		if node.ShortTag() != "!!seq" {
			return nil, fmt.Errorf("line %d: unsupported YAML sequence tag", node.Line)
		}
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := documentValue(child)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str", "!!bool", "!!int", "!!float":
			var value any
			if err := node.Decode(&value); err != nil {
				return nil, fmt.Errorf("line %d: invalid YAML scalar", node.Line)
			}
			if number, ok := value.(float64); ok && (math.IsInf(number, 0) || math.IsNaN(number)) {
				return nil, fmt.Errorf("line %d: numbers must be finite", node.Line)
			}
			return value, nil
		case "!!null":
			return nil, fmt.Errorf("line %d: null is unsupported", node.Line)
		default:
			return nil, fmt.Errorf("line %d: unsupported YAML scalar tag", node.Line)
		}
	case yaml.AliasNode:
		return nil, fmt.Errorf("line %d: YAML aliases are unsupported", node.Line)
	default:
		return nil, fmt.Errorf("line %d: unsupported YAML value", node.Line)
	}
}
