package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMonitorResourceSchemaIsValid(t *testing.T) {
	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewMonitorResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("building the schema: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("the schema is not implementable: %v", diags)
	}
}

func TestDataSourceSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	for _, build := range []func() fwdatasource.DataSource{
		NewMonitorDataSource,
		NewMonitorsDataSource,
		NewMonitorTypesDataSource,
		NewLocationsDataSource,
		NewAccountDataSource,
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

func baseModel(t *testing.T) monitorModel {
	t.Helper()
	return monitorModel{
		ID:           types.StringValue("8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e"),
		Type:         types.StringValue("http"),
		URL:          types.StringValue("https://example.com"),
		Name:         types.StringValue("Marketing site"),
		Interval:     types.Int64Value(300),
		Enabled:      types.BoolValue(true),
		Tags:         stringSetValue(t, "prod", "eu"),
		CronSchedule: types.StringNull(),
		FullLog:      types.BoolNull(),
		OpenStat:     types.BoolNull(),
		SLATarget:    types.Float64Null(),
		OnOverlimit:  types.StringNull(),
		Locations:    types.ObjectNull(locationsAttrTypes),
		Recheck:      types.ObjectNull(recheckAttrTypes),
		Settings:     types.ObjectNull(settingsAttrTypes()),
		SettingsJSON: types.StringNull(),
	}
}

func TestEncodeMonitorBuildsTheWireBody(t *testing.T) {
	ctx := context.Background()
	model := baseModel(t)

	locations, diags := types.ObjectValue(locationsAttrTypes, map[string]attr.Value{
		"pools":           stringSetValue(t, "allworld"),
		"fallback":        types.StringValue("world"),
		"excluded_agents": types.SetNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("building locations: %v", diags)
	}
	model.Locations = locations

	recheck, diags := types.ObjectValue(recheckAttrTypes, map[string]attr.Value{
		"strategy":     types.StringValue("minNumDown"),
		"min_num_down": types.Int64Value(2),
	})
	if diags.HasError() {
		t.Fatalf("building recheck: %v", diags)
	}
	model.Recheck = recheck
	model.Settings = settingsObject(t, "http", map[string]attr.Value{"keywords": types.StringValue("Sign in")})

	body, encodeDiags := encodeMonitor(ctx, &model)
	if encodeDiags.HasError() {
		t.Fatalf("encoding: %v", encodeDiags)
	}

	want := map[string]any{
		"type":      "http",
		"url":       "https://example.com",
		"name":      "Marketing site",
		"interval":  float64(300),
		"enabled":   true,
		"tags":      []any{"eu", "prod"},
		"locations": map[string]any{"pools": []any{"allworld"}, "fallback": "world"},
		"recheck":   map[string]any{"strategy": "minNumDown", "minNumDown": float64(2)},
		"settings":  map[string]any{"keywords": "Sign in"},
	}
	if got := mustNormalize(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("wire body mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEncodeMonitorResolvesTheTypeAlias(t *testing.T) {
	ctx := context.Background()
	model := baseModel(t)
	model.Type = types.StringValue("pageSpeed")

	body, diags := encodeMonitor(ctx, &model)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if body["type"] != "waterfall" {
		t.Fatalf("expected the alias to be resolved on the wire, got %v", body["type"])
	}
}

func TestEncodeMonitorTakesSettingsJSONVerbatim(t *testing.T) {
	ctx := context.Background()
	model := baseModel(t)
	model.Type = types.StringValue("database")
	model.SettingsJSON = types.StringValue(`{"query":"select 1","timeout":5000}`)

	body, diags := encodeMonitor(ctx, &model)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	settings, ok := body["settings"].(map[string]any)
	if !ok || settings["query"] != "select 1" {
		t.Fatalf("expected the raw settings to reach the wire, got %#v", body["settings"])
	}
}

func TestEncodeMonitorRefusesMalformedSettingsJSON(t *testing.T) {
	ctx := context.Background()
	model := baseModel(t)
	model.SettingsJSON = types.StringValue("not json")

	_, diags := encodeMonitor(ctx, &model)
	if !diags.HasError() {
		t.Fatal("expected a diagnostic for settings JSON that does not parse")
	}
}

func TestMonitorToStateFallsBackToTheRawURL(t *testing.T) {
	ctx := context.Background()
	prior := baseModel(t)
	monitor := &client.Monitor{
		ID: "8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e", Type: "http",
		URL: "https://example.com", Enabled: true, Since: 1, Updated: 2,
	}

	state, diags := monitorToState(ctx, &prior, monitor)
	if diags.HasError() {
		t.Fatalf("rendering state: %v", diags)
	}
	// The API omits effectiveUrl whenever it agrees with the raw address.
	if state.EffectiveURL.ValueString() != "https://example.com" {
		t.Fatalf("expected the raw url as the effective one, got %v", state.EffectiveURL)
	}
}

func TestMonitorToStateKeepsTheWrittenTypeSpelling(t *testing.T) {
	ctx := context.Background()
	prior := baseModel(t)
	prior.Type = types.StringValue("pageSpeed")
	monitor := &client.Monitor{ID: "id", Type: "waterfall", URL: "https://example.com"}

	state, diags := monitorToState(ctx, &prior, monitor)
	if diags.HasError() {
		t.Fatalf("rendering state: %v", diags)
	}
	if state.Type.ValueString() != "pageSpeed" {
		t.Fatalf("expected the configured spelling to survive, got %v", state.Type)
	}
}

func TestMonitorToStateKeepsTheWriteOnlyOverlimitKnob(t *testing.T) {
	ctx := context.Background()
	prior := baseModel(t)
	prior.OnOverlimit = types.StringValue("disable")
	monitor := &client.Monitor{ID: "id", Type: "http", URL: "https://example.com"}

	state, diags := monitorToState(ctx, &prior, monitor)
	if diags.HasError() {
		t.Fatalf("rendering state: %v", diags)
	}
	if state.OnOverlimit.ValueString() != "disable" {
		t.Fatalf("expected the write knob to stay in state, got %v", state.OnOverlimit)
	}
}

func TestProjectSettingsJSONManagesOnlyTheDeclaredMembers(t *testing.T) {
	stored := map[string]any{"keywords": "Sign in", "timeout": float64(40000), "followRedirect": true}

	value, diags := projectSettingsJSON(`{"keywords":"Sign in"}`, stored)
	if diags.HasError() {
		t.Fatalf("projecting: %v", diags)
	}
	if value.ValueString() != `{"keywords":"Sign in"}` {
		t.Fatalf("expected the untouched configuration to be kept verbatim, got %s", value.ValueString())
	}

	drifted, diags := projectSettingsJSON(`{"keywords":"Sign in"}`, map[string]any{"keywords": "Log in"})
	if diags.HasError() {
		t.Fatalf("projecting: %v", diags)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(drifted.ValueString()), &got); err != nil {
		t.Fatal(err)
	}
	if got["keywords"] != "Log in" {
		t.Fatalf("expected the drift to be reported, got %s", drifted.ValueString())
	}
}

func TestPointerMapperPlacesFailuresOnAttributes(t *testing.T) {
	mapper := (&monitorResource{}).pointerMapper("http")

	cases := map[string]string{
		"/interval":                  "interval",
		"/locations/pools":           "locations.pools",
		"/recheck/minNumDown":        "recheck.min_num_down",
		"/settings/keywords":         "settings.http.keywords",
		"/settings/attached/webRisk": "settings.http.attached.web_risk",
		"/cronSchedule":              "cron_schedule",
	}
	for pointer, want := range cases {
		got, ok := mapper(pointer)
		if !ok {
			t.Fatalf("%s did not map to an attribute", pointer)
		}
		if got.String() != want {
			t.Fatalf("%s mapped to %s, want %s", pointer, got, want)
		}
	}

	if _, ok := mapper("/somethingElse"); ok {
		t.Fatal("expected an unknown pointer to map to nothing")
	}
}

func TestPointerMapperSendsAnUntypedTypeToSettingsJSON(t *testing.T) {
	mapper := (&monitorResource{}).pointerMapper("database")

	got, ok := mapper("/settings/query")
	if !ok || got.String() != "settings_json" {
		t.Fatalf("expected settings_json, got %s (%v)", got, ok)
	}
}
