package acpprotocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanUpdateRequiresEntries(t *testing.T) {
	var update PlanUpdate
	err := json.Unmarshal([]byte(`{"sessionUpdate":"plan"}`), &update)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"entries"`)
}

func TestUsageUpdateRequiresUsedAndSize(t *testing.T) {
	var update UsageUpdate
	err := json.Unmarshal([]byte(`{"sessionUpdate":"usage_update","size":200000}`), &update)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"used"`)
}
