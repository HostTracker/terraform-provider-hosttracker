package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const windowMonitorID = "4e49d7a2-4ab5-45e2-b9f8-1d59f505ad45"

func suppressObject(t *testing.T, alerts, stats bool) types.Object {
	t.Helper()
	object, diags := types.ObjectValue(suppressAttrTypes, map[string]attr.Value{
		"alerts": types.BoolValue(alerts),
		"stats":  types.BoolValue(stats),
	})
	if diags.HasError() {
		t.Fatalf("building the suppression: %v", diags)
	}
	return object
}

func baseWindowModel(t *testing.T) maintenanceModel {
	t.Helper()
	return maintenanceModel{
		ID:          types.StringValue("2c9a5f31-7b64-4e08-91d2-5a3c6e7b8f40"),
		Name:        types.StringValue("Weekly database maintenance"),
		From:        types.Int64Value(1785712608),
		To:          types.Int64Value(1785716208),
		DurationSec: types.Int64Value(3600),
		Timezone:    types.StringValue("Europe/Berlin"),
		Recurrence:  types.ObjectNull(recurrenceAttrTypes),
		Enabled:     types.BoolValue(true),
		MonitorIDs:  stringSetValue(t, windowMonitorID),
		Suppress:    suppressObject(t, true, true),
		Monitors:    types.SetNull(types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes}),
	}
}

func TestEncodeMaintenanceSendsOneSpellingOfTheLength(t *testing.T) {
	ctx := context.Background()
	model := baseWindowModel(t)

	body, diags := encodeMaintenance(ctx, &model, maintenanceShape{})
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if _, present := body["durationSec"]; present {
		t.Fatalf("expected only `to` on the wire, got %#v", body)
	}
	if body["to"] != int64(1785716208) {
		t.Fatalf("expected the end instant, got %v", body["to"])
	}

	body, diags = encodeMaintenance(ctx, &model, maintenanceShape{useDuration: true})
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if _, present := body["to"]; present {
		t.Fatalf("expected only `durationSec` on the wire, got %#v", body)
	}
	if body["durationSec"] != int64(3600) {
		t.Fatalf("expected the length, got %v", body["durationSec"])
	}
}

func TestEncodeMaintenanceBuildsTheUniformCoverage(t *testing.T) {
	ctx := context.Background()
	model := baseWindowModel(t)

	body, diags := encodeMaintenance(ctx, &model, maintenanceShape{})
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}

	want := map[string]any{
		"name":       "Weekly database maintenance",
		"from":       float64(1785712608),
		"to":         float64(1785716208),
		"timezone":   "Europe/Berlin",
		"enabled":    true,
		"monitorIds": []any{windowMonitorID},
		"suppress":   map[string]any{"alerts": true, "stats": true},
	}
	if got := mustNormalize(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("wire body mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEncodeMaintenanceBuildsThePerMonitorCoverage(t *testing.T) {
	ctx := context.Background()
	model := baseWindowModel(t)

	first, diags := types.ObjectValue(maintenanceMonitorAttrTypes, map[string]attr.Value{
		"monitor_id": types.StringValue(windowMonitorID),
		"suppress":   suppressObject(t, true, false),
	})
	if diags.HasError() {
		t.Fatalf("building the entry: %v", diags)
	}
	second, diags := types.ObjectValue(maintenanceMonitorAttrTypes, map[string]attr.Value{
		"monitor_id": types.StringValue("0b1f6c07-2d5e-41a9-9f3b-0c7d21e4a655"),
		"suppress":   suppressObject(t, false, true),
	})
	if diags.HasError() {
		t.Fatalf("building the entry: %v", diags)
	}
	monitors, setDiags := types.SetValue(types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes}, []attr.Value{first, second})
	if setDiags.HasError() {
		t.Fatalf("building the coverage: %v", setDiags)
	}
	model.Monitors = monitors

	body, encodeDiags := encodeMaintenance(ctx, &model, maintenanceShape{perMonitor: true})
	if encodeDiags.HasError() {
		t.Fatalf("encoding: %v", encodeDiags)
	}
	for _, refused := range []string{"monitorIds", "suppress"} {
		if _, present := body[refused]; present {
			t.Fatalf("expected no %q beside the per-monitor coverage, got %#v", refused, body)
		}
	}
	entries, ok := body["monitors"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("expected both entries on the wire, got %#v", body["monitors"])
	}
	// Sorted by monitor id, so that two equal coverages compare equal.
	if entries[0].(map[string]any)["monitorId"] != "0b1f6c07-2d5e-41a9-9f3b-0c7d21e4a655" {
		t.Fatalf("expected the coverage to be sorted, got %#v", entries)
	}
}

func TestMaintenancePatchSendsOnlyTheChangedMembers(t *testing.T) {
	ctx := context.Background()
	state := baseWindowModel(t)
	plan := baseWindowModel(t)
	plan.Enabled = types.BoolValue(false)

	have, diags := encodeMaintenance(ctx, &state, maintenanceShape{})
	if diags.HasError() {
		t.Fatalf("encoding the state: %v", diags)
	}
	want, diags := encodeMaintenance(ctx, &plan, maintenanceShape{})
	if diags.HasError() {
		t.Fatalf("encoding the plan: %v", diags)
	}

	body := client.Diff(have, want)
	if len(body) != 1 || body["enabled"] != false {
		t.Fatalf("expected only the switch on the wire, got %#v", body)
	}
}

func TestMaintenanceToStateFillsBothSpellings(t *testing.T) {
	ctx := context.Background()
	name, zone, state := "Weekly database maintenance", "Europe/Berlin", "scheduled"
	window := &client.Maintenance{
		ID:          "2c9a5f31-7b64-4e08-91d2-5a3c6e7b8f40",
		Name:        &name,
		From:        1785712608,
		To:          1785716208,
		DurationSec: 3600,
		Timezone:    &zone,
		Enabled:     true,
		State:       &state,
		Recurrence:  &client.MaintenanceRecurrence{WeekDays: []string{"Sunday"}},
		MonitorIDs:  []string{windowMonitorID},
		Suppress:    &client.Suppress{Alerts: true, Stats: false},
		Monitors: []client.MaintenanceMonitor{{
			MonitorID: windowMonitorID,
			Suppress:  &client.Suppress{Alerts: true, Stats: false},
		}},
	}

	rendered, diags := maintenanceToState(ctx, nil, window)
	if diags.HasError() {
		t.Fatalf("rendering the state: %v", diags)
	}
	if rendered.To.ValueInt64() != 1785716208 || rendered.DurationSec.ValueInt64() != 3600 {
		t.Fatal("expected both spellings of the length in state")
	}
	if len(rendered.MonitorIDs.Elements()) != 1 || len(rendered.Monitors.Elements()) != 1 {
		t.Fatal("expected both spellings of the coverage in state")
	}
	if rendered.FromRFC3339.ValueString() != client.UnixToRFC3339(1785712608) {
		t.Fatalf("unexpected rendering %q", rendered.FromRFC3339.ValueString())
	}
	if rendered.Recurrence.IsNull() {
		t.Fatal("expected the recurrence to survive")
	}
}

func TestMaintenanceToStateKeepsTheConfiguredZone(t *testing.T) {
	ctx := context.Background()
	prior := baseWindowModel(t)
	prior.Timezone = types.StringValue("Europe/Rome")

	zone := "Europe/Berlin"
	rendered, diags := maintenanceToState(ctx, &prior, &client.Maintenance{
		ID: prior.ID.ValueString(), From: 1, To: 2, DurationSec: 1, Timezone: &zone,
	})
	if diags.HasError() {
		t.Fatalf("rendering the state: %v", diags)
	}
	if got := rendered.Timezone.ValueString(); got != "Europe/Rome" {
		t.Fatalf("expected the configured id to survive the group's representative, got %q", got)
	}
}

func TestMaintenanceChangedReportsAnEditedWindow(t *testing.T) {
	state := baseWindowModel(t)
	plan := baseWindowModel(t)
	config := baseWindowModel(t)
	if maintenanceChanged(&plan, &state, &config) {
		t.Fatal("expected an unedited window to report no change")
	}
	plan.Name = types.StringValue("Renamed")
	config.Name = types.StringValue("Renamed")
	if !maintenanceChanged(&plan, &state, &config) {
		t.Fatal("expected a rename to report a change")
	}
}

// TestMaintenanceChangedIgnoresWhatTheConfigurationOmits pins the reason the
// resource answers this question itself: Terraform proposes an omitted nested
// attribute as unknown rather than as the value the state holds, and reading
// that as an edit would plan an update on every run, forever.
func TestMaintenanceChangedIgnoresWhatTheConfigurationOmits(t *testing.T) {
	state := baseWindowModel(t)
	plan := baseWindowModel(t)
	config := baseWindowModel(t)

	config.Monitors = types.SetNull(types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes})
	plan.Monitors = types.SetUnknown(types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes})
	config.Recurrence = types.ObjectNull(recurrenceAttrTypes)
	plan.Recurrence = types.ObjectUnknown(recurrenceAttrTypes)

	if maintenanceChanged(&plan, &state, &config) {
		t.Fatal("expected an omitted member to carry no opinion")
	}
}

func TestMaintenancePointerMapperPlacesFailures(t *testing.T) {
	for pointer, want := range map[string]string{
		"/from":                 "from",
		"/durationSec":          "duration_sec",
		"/monitorIds":           "monitor_ids",
		"/recurrence/weekDays":  "recurrence.week_days",
		"/monitors/0/monitorId": "monitors",
	} {
		attribute, ok := maintenancePointerMapper(pointer)
		if !ok {
			t.Fatalf("expected %q to place on an attribute", pointer)
		}
		if got := attribute.String(); got != want {
			t.Fatalf("%q placed on %q, want %q", pointer, got, want)
		}
	}
}
