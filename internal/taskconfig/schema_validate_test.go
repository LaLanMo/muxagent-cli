package taskconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateValueRejectsFractionalInteger(t *testing.T) {
	deny := false
	schema := &JSONSchema{
		Type:                 "object",
		AdditionalProperties: &deny,
		Required:             []string{"count"},
		Properties: map[string]*JSONSchema{
			"count": {Type: "integer"},
		},
	}

	err := ValidateValue(schema, map[string]interface{}{"count": 1.5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$.count must be an integer")
}

func TestValidateValueNormalizesNumericEnums(t *testing.T) {
	deny := false
	schema := &JSONSchema{
		Type:                 "object",
		AdditionalProperties: &deny,
		Required:             []string{"count"},
		Properties: map[string]*JSONSchema{
			"count": {Type: "number", Enum: []interface{}{1}},
		},
	}

	err := ValidateValue(schema, map[string]interface{}{"count": 1.0})
	require.NoError(t, err)
}

func TestValuesEqualDistinguishesStringAndBoolean(t *testing.T) {
	assert.False(t, ValuesEqual("false", false))
	assert.True(t, ValuesEqual(1, 1.0))
}
