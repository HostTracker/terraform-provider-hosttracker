package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	firstContactID  = "2c9a5f31-7b64-4e08-91d2-5a3c6e7b8f40"
	secondContactID = "0b1f6c07-2d5e-41a9-9f3b-0c7d21e4a655"
)

func groupItem(t *testing.T, contactID string, events ...string) attr.Value {
	t.Helper()
	object, diags := types.ObjectValue(contactGroupItemAttrTypes, map[string]attr.Value{
		"contact_id": types.StringValue(contactID),
		"events":     stringSetValue(t, events...),
	})
	if diags.HasError() {
		t.Fatalf("building the membership entry: %v", diags)
	}
	return object
}

func groupModel(t *testing.T, items ...attr.Value) contactGroupModel {
	t.Helper()
	set, diags := types.SetValue(types.ObjectType{AttrTypes: contactGroupItemAttrTypes}, items)
	if diags.HasError() {
		t.Fatalf("building the membership: %v", diags)
	}
	return contactGroupModel{
		ID:    types.StringValue("7d1b3e55-2a90-4c11-8f36-4b1d2e6a9c83"),
		Name:  types.StringValue("Ops"),
		Items: set,
	}
}

func TestEncodeContactGroupSortsTheMembership(t *testing.T) {
	ctx := context.Background()
	// The set is written in one order and the wire body must come out in
	// another, so that two equal memberships compare equal.
	model := groupModel(t,
		groupItem(t, secondContactID, "weekly"),
		groupItem(t, firstContactID, "up", "down"),
	)

	body, diags := encodeContactGroup(ctx, &model)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}

	want := map[string]any{
		"name": "Ops",
		"items": []any{
			map[string]any{"contact": secondContactID, "events": []any{"weekly"}},
			map[string]any{"contact": firstContactID, "events": []any{"down", "up"}},
		},
	}
	if got := mustNormalize(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("wire body mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestContactGroupPatchIsEmptyForAReorderedSet(t *testing.T) {
	ctx := context.Background()
	state := groupModel(t, groupItem(t, firstContactID, "down", "up"), groupItem(t, secondContactID, "weekly"))
	plan := groupModel(t, groupItem(t, secondContactID, "weekly"), groupItem(t, firstContactID, "up", "down"))

	have, diags := encodeContactGroup(ctx, &state)
	if diags.HasError() {
		t.Fatalf("encoding the state: %v", diags)
	}
	want, diags := encodeContactGroup(ctx, &plan)
	if diags.HasError() {
		t.Fatalf("encoding the plan: %v", diags)
	}

	if body := client.Diff(have, want); len(body) != 0 {
		t.Fatalf("expected no change on the wire, got %#v", body)
	}
}

func TestContactGroupPatchSendsTheWholeMembership(t *testing.T) {
	ctx := context.Background()
	state := groupModel(t, groupItem(t, firstContactID, "down"))
	plan := groupModel(t, groupItem(t, firstContactID, "down"), groupItem(t, secondContactID, "down"))

	have, diags := encodeContactGroup(ctx, &state)
	if diags.HasError() {
		t.Fatalf("encoding the state: %v", diags)
	}
	want, diags := encodeContactGroup(ctx, &plan)
	if diags.HasError() {
		t.Fatalf("encoding the plan: %v", diags)
	}

	body := client.Diff(have, want)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected the whole membership on the wire, got %#v", body["items"])
	}
}

func TestContactGroupToStateReadsTheMemberProjection(t *testing.T) {
	ctx := context.Background()
	name := "Ops"
	group := &client.ContactGroup{
		ID:      "7d1b3e55-2a90-4c11-8f36-4b1d2e6a9c83",
		Name:    &name,
		Created: 1785670783,
		Items: []client.ContactGroupItem{{
			Contact: json.RawMessage(`{"id":"` + firstContactID + `","type":"email","name":"On-call"}`),
			Events:  []string{"down", "up"},
		}},
	}

	state, diags := contactGroupToState(ctx, group)
	if diags.HasError() {
		t.Fatalf("rendering the state: %v", diags)
	}
	if len(state.Items.Elements()) != 1 {
		t.Fatalf("expected one member, got %d", len(state.Items.Elements()))
	}
	entry := state.Items.Elements()[0].(types.Object)
	if got := entry.Attributes()["contact_id"].(types.String).ValueString(); got != firstContactID {
		t.Fatalf("expected the member's id, got %q", got)
	}
}

func TestContactGroupPointerMapperPlacesFailures(t *testing.T) {
	if attribute, ok := contactGroupPointerMapper("/items/0/contact"); !ok || attribute.String() != "items" {
		t.Fatalf("expected a membership failure to place on `items`, got %v (%v)", attribute, ok)
	}
	if attribute, ok := contactGroupPointerMapper("/name"); !ok || attribute.String() != "name" {
		t.Fatalf("expected a name failure to place on `name`, got %v (%v)", attribute, ok)
	}
}
