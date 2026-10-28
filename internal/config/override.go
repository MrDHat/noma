package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type selectorPart struct {
	key     string
	index   int
	indexed bool
}

func applyOverride(document map[string]any, assignment string) error {
	delimiter := -1
	quoted := false
	for i := 0; i < len(assignment); i++ {
		if quoted && assignment[i] == '\\' {
			i++
			continue
		}
		if assignment[i] == '"' {
			quoted = !quoted
		}
		if assignment[i] == '=' && !quoted {
			delimiter = i
			break
		}
	}
	if delimiter < 0 {
		return fmt.Errorf("--set requires a selector=value assignment")
	}
	selector, raw := assignment[:delimiter], assignment[delimiter+1:]
	parts, err := parseSelector(selector)
	if err != nil {
		return fmt.Errorf("--set selector %q: %w", selector, err)
	}
	var value any
	if json.Valid([]byte(raw)) {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		value, err = jsonValue(decoder)
		if err != nil {
			return fmt.Errorf("--set %q: %w", selector, err)
		}
	} else {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, `"`) {
			return fmt.Errorf("--set %q: malformed JSON value", selector)
		}
		value = raw
	}
	if err := assignValue(document, parts, value); err != nil {
		return fmt.Errorf("--set %q: %w", selector, err)
	}
	return nil
}

func jsonValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, fmt.Errorf("null is unsupported")
	}
	delimiter, collection := token.(json.Delim)
	if !collection {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key := token.(string)
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate key %q", key)
			}
			value, err := jsonValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		items := make([]any, 0)
		for decoder.More() {
			value, err := jsonValue(decoder)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter")
	}
}

func parseSelector(selector string) ([]selectorPart, error) {
	if selector == "" {
		return nil, fmt.Errorf("selector must not be empty")
	}
	var parts []selectorPart
	for position := 0; position < len(selector); {
		if selector[position] == '[' {
			position++
			if position == len(selector) {
				return nil, fmt.Errorf("unclosed bracket")
			}
			if selector[position] == '"' {
				start := position
				position++
				for position < len(selector) && selector[position] != '"' {
					if selector[position] == '\\' {
						position++
					}
					position++
				}
				if position >= len(selector) {
					return nil, fmt.Errorf("unclosed quoted key")
				}
				position++
				var key string
				if err := json.Unmarshal([]byte(selector[start:position]), &key); err != nil {
					return nil, fmt.Errorf("invalid JSON-quoted key")
				}
				parts = append(parts, selectorPart{key: key})
			} else {
				start := position
				for position < len(selector) && selector[position] >= '0' && selector[position] <= '9' {
					position++
				}
				index, err := strconv.Atoi(selector[start:position])
				if err != nil || len(parts) == 0 {
					return nil, fmt.Errorf("array index must be a nonnegative integer after a key")
				}
				parts = append(parts, selectorPart{index: index, indexed: true})
			}
			if position >= len(selector) || selector[position] != ']' {
				return nil, fmt.Errorf("expected closing bracket")
			}
			position++
		} else {
			start := position
			for position < len(selector) && !strings.ContainsRune(".[]", rune(selector[position])) {
				position++
			}
			key := selector[start:position]
			if key == "" || strings.ContainsAny(key, "\"'") || strings.ContainsFunc(key, unicode.IsSpace) {
				return nil, fmt.Errorf("expected a key; quote special keys inside brackets")
			}
			parts = append(parts, selectorPart{key: key})
		}
		if position == len(selector) {
			break
		}
		switch selector[position] {
		case '.':
			position++
			if position == len(selector) || strings.ContainsRune(".[", rune(selector[position])) {
				return nil, fmt.Errorf("expected a key after dot")
			}
		case '[':
		default:
			return nil, fmt.Errorf("expected dot or bracket")
		}
	}
	return parts, nil
}

func assignValue(container any, parts []selectorPart, value any) error {
	part := parts[0]
	var child any
	if part.indexed {
		items, ok := container.([]any)
		if !ok {
			return fmt.Errorf("array index requires an existing array")
		}
		if part.index >= len(items) {
			return fmt.Errorf("array index %d is out of range", part.index)
		}
		if len(parts) == 1 {
			items[part.index] = value
			return nil
		}
		child = items[part.index]
	} else {
		object, ok := container.(map[string]any)
		if !ok {
			return fmt.Errorf("key requires an object")
		}
		if len(parts) == 1 {
			object[part.key] = value
			return nil
		}
		var exists bool
		child, exists = object[part.key]
		if !exists {
			if parts[1].indexed {
				return fmt.Errorf("array index requires an existing array")
			}
			child = map[string]any{}
			object[part.key] = child
		}
	}
	return assignValue(child, parts[1:], value)
}
