package taskconfig

import (
	"encoding/json"
	"fmt"
)

func ValidateValue(schema *JSONSchema, value interface{}) error {
	return validateValue(schema, value, "$")
}

func validateValue(schema *JSONSchema, value interface{}, path string) error {
	if schema == nil {
		return fmt.Errorf("%s has no schema", path)
	}
	if len(schema.OneOf) > 0 {
		var errs []error
		for _, inner := range schema.OneOf {
			if err := validateValue(inner, value, path); err == nil {
				return nil
			} else {
				errs = append(errs, err)
			}
		}
		return fmt.Errorf("%s does not match any allowed schema", path)
	}
	switch schema.Type {
	case "object":
		obj, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		required := map[string]struct{}{}
		for _, key := range schema.Required {
			required[key] = struct{}{}
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("%s.%s is required", path, key)
			}
		}
		if schema.Properties == nil {
			schema.Properties = map[string]*JSONSchema{}
		}
		for key, v := range obj {
			child, ok := schema.Properties[key]
			if !ok {
				if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
				continue
			}
			if err := validateValue(child, v, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if schema.MinItems != nil && len(items) < *schema.MinItems {
			return fmt.Errorf("%s must contain at least %d items", path, *schema.MinItems)
		}
		if schema.MaxItems != nil && len(items) > *schema.MaxItems {
			return fmt.Errorf("%s must contain at most %d items", path, *schema.MaxItems)
		}
		for idx, item := range items {
			if err := validateValue(schema.Items, item, fmt.Sprintf("%s[%d]", path, idx)); err != nil {
				return err
			}
		}
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "integer":
		if !isIntegerValue(value) {
			return fmt.Errorf("%s must be an integer", path)
		}
	case "number":
		if !isNumberValue(value) {
			return fmt.Errorf("%s must be a number", path)
		}
	default:
		return fmt.Errorf("%s has unsupported type %q", path, schema.Type)
	}
	if len(schema.Enum) > 0 {
		for _, candidate := range schema.Enum {
			if ValuesEqual(candidate, value) {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %v", path, schema.Enum)
	}
	return nil
}

func ValuesEqual(left, right interface{}) bool {
	leftKey, leftErr := comparableValueKey(left)
	rightKey, rightErr := comparableValueKey(right)
	if leftErr == nil && rightErr == nil {
		return leftKey == rightKey
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr == nil && rightErr == nil {
		return string(leftJSON) == string(rightJSON)
	}
	return false
}

func NormalizeJSONMap(raw []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}
