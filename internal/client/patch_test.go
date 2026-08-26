package client

import (
	"encoding/json"
	"testing"
)

func TestDiffLeavesUnchangedMembersOut(t *testing.T) {
	have := map[string]any{
		"name":     "Marketing site",
		"interval": int64(300),
		"enabled":  true,
	}
	want := map[string]any{
		"name":     "Marketing site",
		"interval": int64(60),
		"enabled":  true,
	}

	body := Diff(have, want)

	if len(body) != 1 {
		t.Fatalf("expected exactly the changed member, got %v", body)
	}
	if body["interval"] != int64(60) {
		t.Fatalf("expected interval 60, got %v", body["interval"])
	}
}

func TestDiffTreatsDecodedNumbersAsEqual(t *testing.T) {
	// A read decodes JSON numbers as float64; a plan carries int64. The
	// same number in both must not look like a change.
	have := map[string]any{"interval": float64(300)}
	want := map[string]any{"interval": int64(300)}

	if body := Diff(have, want); len(body) != 0 {
		t.Fatalf("expected no change, got %v", body)
	}
}

func TestDiffSendsNewMembers(t *testing.T) {
	body := Diff(map[string]any{}, map[string]any{"name": "Added"})

	if body["name"] != "Added" {
		t.Fatalf("expected the new member to be sent, got %v", body)
	}
}

func TestDiffSkipsMembersThatAreNewAndEmpty(t *testing.T) {
	body := Diff(map[string]any{}, map[string]any{
		"tags":     []string{},
		"settings": map[string]any{},
	})

	if len(body) != 0 {
		t.Fatalf("expected an empty body, got %v", body)
	}
}

func TestDiffSendsAnEmptyValueThatReplacesAFullOne(t *testing.T) {
	body := Diff(
		map[string]any{"tags": []string{"prod"}},
		map[string]any{"tags": []string{}},
	)

	raw, err := json.Marshal(body["tags"])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("expected an empty list to be sent, got %s", raw)
	}
}

func TestDiffSendsClearAsNull(t *testing.T) {
	body := Diff(
		map[string]any{"name": "Marketing site"},
		map[string]any{"name": Clear},
	)

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"name":null}` {
		t.Fatalf("expected an explicit null, got %s", raw)
	}
}

func TestDiffComparesNestedObjectsWhole(t *testing.T) {
	have := map[string]any{"locations": map[string]any{"pools": []any{"allworld"}}}
	same := map[string]any{"locations": map[string]any{"pools": []string{"allworld"}}}
	other := map[string]any{"locations": map[string]any{"pools": []string{"europe"}}}

	if body := Diff(have, same); len(body) != 0 {
		t.Fatalf("expected no change, got %v", body)
	}
	if body := Diff(have, other); len(body) != 1 {
		t.Fatalf("expected the nested object to be sent, got %v", body)
	}
}
