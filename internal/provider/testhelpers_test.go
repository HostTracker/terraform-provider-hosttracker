package provider

import (
	"encoding/json"
	"testing"
)

// mustJSONRound encodes and decodes a wire map, so that a decoder under
// test sees exactly what a real answer would give it: float64 numbers and
// []any lists.
func mustJSONRound(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return out
}

// mustNormalize renders a wire map through JSON so that two maps built
// from different Go types ([]string against []any, int64 against float64)
// compare by what they would put on the wire.
func mustNormalize(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}
