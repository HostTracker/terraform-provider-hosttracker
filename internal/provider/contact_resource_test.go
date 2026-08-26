package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestContactResourceSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	for _, build := range []func() fwresource.Resource{
		NewContactResource,
		NewContactGroupResource,
		NewMaintenanceResource,
	} {
		resp := &fwresource.SchemaResponse{}
		build().Schema(ctx, fwresource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("building the schema: %v", resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("the schema is not implementable: %v", diags)
		}
	}
}

func TestContactDataSourceSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	for _, build := range []func() fwdatasource.DataSource{
		NewContactDataSource,
		NewContactsDataSource,
		NewContactGroupDataSource,
		NewContactTypesDataSource,
		NewMaintenanceWindowsDataSource,
	} {
		resp := &fwdatasource.SchemaResponse{}
		build().Schema(ctx, fwdatasource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("building the schema: %v", resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("the schema is not implementable: %v", diags)
		}
	}
}

func baseContactModel() contactModel {
	return contactModel{
		ID:                   types.StringValue("2c9a5f31-7b64-4e08-91d2-5a3c6e7b8f40"),
		Type:                 types.StringValue("email"),
		Address:              types.StringValue("oncall@example.com"),
		Name:                 types.StringValue("On-call"),
		AlertDelay:           types.Int64Value(5),
		Language:             types.StringValue("en"),
		Gateway:              types.StringNull(),
		GroupedAlerts:        types.BoolValue(true),
		BillingNotifications: types.BoolNull(),
		SendNews:             types.BoolNull(),
		MimeType:             types.StringNull(),
		HTTPHeaders:          types.ListNull(types.ObjectType{AttrTypes: contactHeaderAttrTypes}),
		Templates:            types.ListNull(types.ObjectType{AttrTypes: contactTemplateAttrTypes}),
		ActivePeriod:         types.ObjectNull(activePeriodAttrTypes),
		SendConfirmation:     types.BoolValue(false),
	}
}

func activePeriodObject(t *testing.T, start, end string, days ...string) types.Object {
	t.Helper()
	dayValue := types.SetNull(types.StringType)
	if len(days) > 0 {
		dayValue = stringSetValue(t, days...)
	}
	object, diags := types.ObjectValue(activePeriodAttrTypes, map[string]attr.Value{
		"start":    types.StringValue(start),
		"end":      types.StringValue(end),
		"days":     dayValue,
		"timezone": types.StringValue("W. Europe Standard Time"),
	})
	if diags.HasError() {
		t.Fatalf("building the active period: %v", diags)
	}
	return object
}

func TestEncodeContactBuildsTheWireBody(t *testing.T) {
	ctx := context.Background()
	model := baseContactModel()
	model.ActivePeriod = activePeriodObject(t, "09:00:00", "18:00:00", "Monday", "Friday")

	body, diags := encodeContact(ctx, &model)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}

	want := map[string]any{
		"type":          "email",
		"address":       "oncall@example.com",
		"name":          "On-call",
		"alertDelay":    float64(5),
		"language":      "en",
		"groupedAlerts": true,
		"activePeriod": map[string]any{
			"start":    "09:00:00",
			"end":      "18:00:00",
			"days":     []any{"Friday", "Monday"},
			"timezone": "W. Europe Standard Time",
		},
	}
	if got := mustNormalize(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("wire body mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEncodeContactRenamesTheHeaderMember(t *testing.T) {
	ctx := context.Background()
	model := baseContactModel()
	header, diags := types.ObjectValue(contactHeaderAttrTypes, map[string]attr.Value{
		"name":  types.StringValue("X-Token"),
		"value": types.StringValue("secret"),
	})
	if diags.HasError() {
		t.Fatalf("building the header: %v", diags)
	}
	list, listDiags := types.ListValue(types.ObjectType{AttrTypes: contactHeaderAttrTypes}, []attr.Value{header})
	if listDiags.HasError() {
		t.Fatalf("building the header list: %v", listDiags)
	}
	model.HTTPHeaders = list

	body, encodeDiags := encodeContact(ctx, &model)
	if encodeDiags.HasError() {
		t.Fatalf("encoding: %v", encodeDiags)
	}
	headers, ok := body["httpHeaders"].([]any)
	if !ok || len(headers) != 1 {
		t.Fatalf("expected one header on the wire, got %#v", body["httpHeaders"])
	}
	entry := headers[0].(map[string]any)
	if entry["header"] != "X-Token" || entry["value"] != "secret" {
		t.Fatalf("expected the header to travel as `header`/`value`, got %#v", entry)
	}
}

func TestContactPatchSendsOnlyTheChangedMembers(t *testing.T) {
	ctx := context.Background()
	state := baseContactModel()
	plan := baseContactModel()
	plan.Name = types.StringValue("On-call, EU")

	have, diags := encodeContact(ctx, &state)
	if diags.HasError() {
		t.Fatalf("encoding the state: %v", diags)
	}
	want, diags := encodeContact(ctx, &plan)
	if diags.HasError() {
		t.Fatalf("encoding the plan: %v", diags)
	}
	delete(have, "type")
	delete(want, "type")

	body := client.Diff(have, want)
	if len(body) != 1 || body["name"] != "On-call, EU" {
		t.Fatalf("expected only the rename on the wire, got %#v", body)
	}
}

func TestContactToStateKeepsTheConfiguredClockSpelling(t *testing.T) {
	ctx := context.Background()
	prior := baseContactModel()
	prior.ActivePeriod = activePeriodObject(t, "09:00", "18:00", "Monday")

	address := "oncall@example.com"
	start, end, zone := "09:00:00", "18:00:00", "Europe/Berlin"
	contact := &client.Contact{
		ID:      prior.ID.ValueString(),
		Type:    "email",
		Address: &address,
		ActivePeriod: &client.ContactActivePeriod{
			Start: &start, End: &end, Days: []string{"Monday"}, Timezone: &zone,
		},
	}

	state, diags := contactToState(ctx, &prior, contact)
	if diags.HasError() {
		t.Fatalf("rendering the state: %v", diags)
	}
	attrs := state.ActivePeriod.Attributes()
	if got := attrs["start"].(types.String).ValueString(); got != "09:00" {
		t.Fatalf("expected the configured spelling to survive, got %q", got)
	}
	// The stored zone answers to several ids, so the configured one is
	// kept rather than reported as a change.
	if got := attrs["timezone"].(types.String).ValueString(); got != "W. Europe Standard Time" {
		t.Fatalf("expected the configured zone to survive, got %q", got)
	}
}

func TestContactToStateAdoptsARealClockChange(t *testing.T) {
	ctx := context.Background()
	prior := baseContactModel()
	prior.ActivePeriod = activePeriodObject(t, "09:00", "18:00", "Monday")

	start, end := "10:00:00", "18:00:00"
	contact := &client.Contact{
		ID:           prior.ID.ValueString(),
		Type:         "email",
		ActivePeriod: &client.ContactActivePeriod{Start: &start, End: &end},
	}

	state, diags := contactToState(ctx, &prior, contact)
	if diags.HasError() {
		t.Fatalf("rendering the state: %v", diags)
	}
	if got := state.ActivePeriod.Attributes()["start"].(types.String).ValueString(); got != "10:00:00" {
		t.Fatalf("expected the change to be reported, got %q", got)
	}
}

func TestContactToStateKeepsTheWriteOnlyConfirmationFlag(t *testing.T) {
	ctx := context.Background()
	prior := baseContactModel()
	prior.SendConfirmation = types.BoolValue(true)

	state, diags := contactToState(ctx, &prior, &client.Contact{ID: prior.ID.ValueString(), Type: "email"})
	if diags.HasError() {
		t.Fatalf("rendering the state: %v", diags)
	}
	if !state.SendConfirmation.ValueBool() {
		t.Fatal("expected the flag to survive a read that never publishes it")
	}
}

func TestRefuseContactTypeNamesTheWayToGetOne(t *testing.T) {
	for _, c := range []struct {
		contactType string
		refused     bool
		mentions    string
	}{
		{"email", false, ""},
		{"sms", false, ""},
		{"voiceCall", false, ""},
		{"http", true, "hosttracker_webhook"},
		{"webPush", true, "browser"},
		{"telegram", true, "bot"},
	} {
		_, detail, refused := refuseContactType(c.contactType)
		if refused != c.refused {
			t.Fatalf("refuseContactType(%q) refused = %v, want %v", c.contactType, refused, c.refused)
		}
		if refused && !strings.Contains(detail, c.mentions) {
			t.Fatalf("expected the refusal of %q to mention %q, got %q", c.contactType, c.mentions, detail)
		}
	}
}

func TestContactPointerMapperPlacesFailures(t *testing.T) {
	for pointer, want := range map[string]string{
		"/address":              "address",
		"/alertDelay":           "alert_delay",
		"/groupedAlerts":        "grouped_alerts",
		"/httpHeaders/0/header": "http_headers",
		"/activePeriod/start":   "active_period.start",
	} {
		attribute, ok := contactPointerMapper(pointer)
		if !ok {
			t.Fatalf("expected %q to place on an attribute", pointer)
		}
		if got := attribute.String(); got != want {
			t.Fatalf("%q placed on %q, want %q", pointer, got, want)
		}
	}
	if _, ok := contactPointerMapper("/somethingElse"); ok {
		t.Fatal("expected an unknown pointer to place nowhere")
	}
}
