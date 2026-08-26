package client

import (
	"bytes"
	"encoding/json"
)

// Clear is the value to put in a PATCH body for a member that should be
// cleared. It marshals to JSON null, which is what the API reads as "unset
// this", as opposed to an absent member, which it reads as "leave alone".
var Clear = clearValue{}

type clearValue struct{}

// MarshalJSON writes null.
func (clearValue) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// Diff builds a PATCH body out of the members that changed.
//
// have is the resource as the last read left it, want is the resource the
// plan asks for; both are wire-shaped maps. A member of want that equals
// its counterpart in have is left out entirely, because the API reads an
// absent member as "unchanged" and refuses members that are not part of
// the update vocabulary. A member of want holding Clear is always sent, as
// JSON null.
//
// Nested objects are compared and sent whole: `locations` and `settings`
// carry members that only make sense together, and the API merges a nested
// object member-wise onto what it already stores.
func Diff(have, want map[string]any) map[string]any {
	body := make(map[string]any, len(want))
	for key, wanted := range want {
		if _, isClear := wanted.(clearValue); isClear {
			body[key] = wanted
			continue
		}
		current, present := have[key]
		if present && equalJSON(current, wanted) {
			continue
		}
		if !present && isEmpty(wanted) {
			// The member was absent before and carries nothing now.
			continue
		}
		body[key] = wanted
	}
	return body
}

// equalJSON compares two wire values by their JSON encoding, which is what
// makes an int64 from a plan and a float64 from a decoded response compare
// equal when they carry the same number.
func equalJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	if bytes.Equal(left, right) {
		return true
	}
	// Re-decode so that map member order and numeric spelling stop
	// mattering, then compare the canonical forms.
	var leftAny, rightAny any
	if json.Unmarshal(left, &leftAny) != nil || json.Unmarshal(right, &rightAny) != nil {
		return false
	}
	leftCanonical, err := json.Marshal(leftAny)
	if err != nil {
		return false
	}
	rightCanonical, err := json.Marshal(rightAny)
	if err != nil {
		return false
	}
	return bytes.Equal(leftCanonical, rightCanonical)
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	default:
		return false
	}
}
