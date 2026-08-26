package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testPageID       = "6b1f2a70-3c8e-4d51-9f2a-7c4e5d6b8a90"
	testPageMonitor  = "4e49d7a2-4ab5-45e2-b9f8-1d59f505ad45"
	testPageMonitor2 = "1a2b3c4d-5e6f-4a1b-8c9d-0e1f2a3b4c5d"
)

// settingsObjectFor builds a settings object out of the members a test
// names, leaving the rest null. It carries its own null-of-a-type rather
// than borrowing the monitor tests', because a page's settings hold a
// float and a monitor's do not.
func settingsObjectFor(t *testing.T, values map[string]attr.Value) types.Object {
	t.Helper()
	attrTypes := statusPageSettingsAttrTypes()
	complete := make(map[string]attr.Value, len(attrTypes))
	for name, attrType := range attrTypes {
		if value, ok := values[name]; ok {
			complete[name] = value
			continue
		}
		switch typed := attrType.(type) {
		case types.SetType:
			complete[name] = types.SetNull(typed.ElemType)
		default:
			switch attrType {
			case types.BoolType:
				complete[name] = types.BoolNull()
			case types.Float64Type:
				complete[name] = types.Float64Null()
			default:
				complete[name] = types.StringNull()
			}
		}
	}
	object, diags := types.ObjectValue(attrTypes, complete)
	if diags.HasError() {
		t.Fatalf("building the settings object: %v", diags)
	}
	return object
}

func componentValue(t *testing.T, values map[string]attr.Value) attr.Value {
	t.Helper()
	return partialObject(t, statusPageComponentAttrTypes, values)
}

func componentList(t *testing.T, values ...attr.Value) types.List {
	t.Helper()
	list, diags := types.ListValue(statusPageComponentObjectType, values)
	if diags.HasError() {
		t.Fatalf("building the component list: %v", diags)
	}
	return list
}

func TestStatusPageSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	settings := settingsObjectFor(t, map[string]attr.Value{
		"theme":             types.StringValue("dark"),
		"theme_color":       types.StringValue("#112233"),
		"announcement":      types.StringValue("Scheduled work on Sunday"),
		"density":           types.StringValue("compact"),
		"show_groups":       types.BoolValue(true),
		"robots_index":      types.BoolValue(false),
		"language":          types.StringValue("en"),
		"auto_add_monitors": types.BoolValue(false),
		"features":          stringSetValue(t, "subscribe", "barCharts"),
		"sla_target":        types.Float64Value(99.9),
	})

	wire, diags := encodeStatusPageSettings(ctx, settings)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}

	want := map[string]any{
		"theme":           "dark",
		"themeColor":      "#112233",
		"announcement":    "Scheduled work on Sunday",
		"density":         "compact",
		"showGroups":      true,
		"robotsIndex":     false,
		"language":        "en",
		"autoAddMonitors": false,
		// Sorted, so that two spellings of one set compare equal.
		"features":  []any{"barCharts", "subscribe"},
		"slaTarget": 99.9,
	}
	if got := normalizeWire(wire); !reflect.DeepEqual(got, normalizeWire(want)) {
		t.Fatalf("wire body\n got: %v\nwant: %v", got, want)
	}

	decoded, diags := decodeStatusPageSettings(ctx, jsonRound(t, wire))
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	attrs := decoded.Attributes()
	if attrs["theme"].(types.String).ValueString() != "dark" {
		t.Fatalf("expected the theme back, got %v", attrs["theme"])
	}
	if attrs["sla_target"].(types.Float64).ValueFloat64() != 99.9 {
		t.Fatalf("expected the target back, got %v", attrs["sla_target"])
	}
	if !attrs["logo_url"].IsNull() {
		t.Fatalf("a member the answer does not carry reads null, got %v", attrs["logo_url"])
	}
}

func TestEncodeStatusPageSettingsLeavesOutWhatIsNotWritten(t *testing.T) {
	ctx := context.Background()
	wire, diags := encodeStatusPageSettings(ctx, settingsObjectFor(t, map[string]attr.Value{
		"theme": types.StringValue("light"),
	}))
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if len(wire) != 1 || wire["theme"] != "light" {
		t.Fatalf("expected only the written member, got %v", wire)
	}

	if wire, _ := encodeStatusPageSettings(ctx, types.ObjectNull(statusPageSettingsAttrTypes())); wire != nil {
		t.Fatalf("a null settings block writes nothing, got %v", wire)
	}
}

func TestEncodeComponentsPreservesOrderAndTheUnion(t *testing.T) {
	ctx := context.Background()
	list := componentList(t,
		componentValue(t, map[string]attr.Value{
			"monitor_id": types.StringValue(testPageMonitor),
			"group":      types.StringValue("Public"),
		}),
		componentValue(t, map[string]attr.Value{
			"third_party":  types.BoolValue(true),
			"name":         types.StringValue("Payments provider"),
			"manual_state": types.StringValue("operational"),
		}),
	)

	components, diags := encodeComponents(ctx, list)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if len(components) != 2 {
		t.Fatalf("expected two components, got %d", len(components))
	}
	if components[0].MonitorID == nil || *components[0].MonitorID != testPageMonitor {
		t.Fatalf("expected the monitored component first, got %+v", components[0])
	}
	if components[0].ThirdParty {
		t.Fatalf("a monitored component is not third-party: %+v", components[0])
	}
	if !components[1].ThirdParty || components[1].Name == nil || *components[1].Name != "Payments provider" {
		t.Fatalf("expected the third-party component second, got %+v", components[1])
	}

	if components, _ := encodeComponents(ctx, types.ListNull(statusPageComponentObjectType)); components != nil {
		t.Fatalf("a null list is a set Terraform does not manage, got %v", components)
	}
}

func TestComponentsDifferSeesOrderButNotSpelling(t *testing.T) {
	first := client.StatusPageComponent{MonitorID: stringPointer(testPageMonitor)}
	second := client.StatusPageComponent{MonitorID: stringPointer(testPageMonitor2)}

	if componentsDiffer([]client.StatusPageComponent{first, second}, []client.StatusPageComponent{first, second}) {
		t.Fatal("the same set in the same order is not a change")
	}
	if !componentsDiffer([]client.StatusPageComponent{first, second}, []client.StatusPageComponent{second, first}) {
		t.Fatal("the array's order is the display order, so a re-ordering is a change")
	}
	if !componentsDiffer([]client.StatusPageComponent{first}, []client.StatusPageComponent{first, second}) {
		t.Fatal("an added component is a change")
	}
}

func TestCarryComponentIDsMatchesRowsAndClaimsEachOnce(t *testing.T) {
	existing := []client.StatusPageComponent{
		{ID: stringPointer("row-1"), MonitorID: stringPointer(testPageMonitor)},
		{ID: stringPointer("row-2"), ThirdParty: true, Name: stringPointer("Payments provider")},
		{ID: stringPointer("row-3"), MonitorID: stringPointer(testPageMonitor2)},
	}

	planned := []client.StatusPageComponent{
		{ThirdParty: true, Name: stringPointer("Payments provider"), ManualState: stringPointer("degraded")},
		{MonitorID: stringPointer(testPageMonitor), Group: stringPointer("Public")},
		{MonitorID: stringPointer("9f8e7d6c-5b4a-4938-8271-6a5b4c3d2e1f")},
	}

	carried := carryComponentIDs(planned, existing)
	if carried[0].ID == nil || *carried[0].ID != "row-2" {
		t.Fatalf("expected the third-party row to be matched by name, got %+v", carried[0])
	}
	if carried[1].ID == nil || *carried[1].ID != "row-1" {
		t.Fatalf("expected the monitored row to be matched by monitor, got %+v", carried[1])
	}
	if carried[2].ID != nil {
		t.Fatalf("a component with no row behind it is new, got %+v", carried[2])
	}
	if carried[0].ManualState == nil || *carried[0].ManualState != "degraded" {
		t.Fatalf("expected the planned members to survive, got %+v", carried[0])
	}
}

func TestCarryComponentIDsGivesTwoEntriesTwoRows(t *testing.T) {
	existing := []client.StatusPageComponent{
		{ID: stringPointer("row-1"), MonitorID: stringPointer(testPageMonitor)},
		{ID: stringPointer("row-2"), MonitorID: stringPointer(testPageMonitor)},
	}
	planned := []client.StatusPageComponent{
		{MonitorID: stringPointer(testPageMonitor)},
		{MonitorID: stringPointer(testPageMonitor)},
	}

	carried := carryComponentIDs(planned, existing)
	if *carried[0].ID == *carried[1].ID {
		t.Fatalf("a row is claimed once: %v and %v", *carried[0].ID, *carried[1].ID)
	}
}

func TestComponentsToStateKeepsWhatOnlyTheConfigurationKnows(t *testing.T) {
	ctx := context.Background()
	prior := componentList(t,
		// Written without a name and without third_party: the label is the
		// monitor's own, and a monitored component is not third-party.
		componentValue(t, map[string]attr.Value{"monitor_id": types.StringValue(testPageMonitor)}),
		componentValue(t, map[string]attr.Value{
			"third_party": types.BoolValue(true),
			"name":        types.StringValue("Payments provider"),
		}),
	)

	rows := []client.StatusPageComponent{
		{
			ID:        stringPointer("row-1"),
			MonitorID: stringPointer(testPageMonitor),
			Name:      stringPointer("Marketing site"),
			Group:     stringPointer("Public"),
		},
		{
			ID:          stringPointer("row-2"),
			ThirdParty:  true,
			Name:        stringPointer("Payments provider"),
			ManualState: stringPointer("operational"),
		},
	}

	list, diags := componentsToState(ctx, prior, rows)
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}

	var components []statusPageComponentModel
	list.ElementsAs(ctx, &components, false)
	if len(components) != 2 {
		t.Fatalf("expected two components, got %d", len(components))
	}
	if !components[0].Name.IsNull() {
		t.Fatalf("an inherited label stays the monitor's, got %v", components[0].Name)
	}
	if !components[0].ThirdParty.IsNull() {
		t.Fatalf("third_party that was never written stays unwritten, got %v", components[0].ThirdParty)
	}
	if components[0].Group.ValueString() != "Public" {
		t.Fatalf("expected the group back, got %v", components[0].Group)
	}
	if !components[1].ThirdParty.ValueBool() || components[1].ManualState.ValueString() != "operational" {
		t.Fatalf("expected the third-party row read back whole, got %+v", components[1])
	}
}

// TestComponentsToStateReadsAnAdoptedPageAsAConfigurationWouldWriteIt covers
// the import path, where there is no prior state to consult. A monitored
// component takes the shape a configuration for it would be written in - the
// monitor's id, and neither the label it inherits nor the `third_party` that
// naming a monitor already implies - so the first plan after an import has
// nothing to say.
func TestComponentsToStateReadsAnAdoptedPageAsAConfigurationWouldWriteIt(t *testing.T) {
	ctx := context.Background()
	rows := []client.StatusPageComponent{{
		ID:        stringPointer("row-1"),
		MonitorID: stringPointer(testPageMonitor),
		Name:      stringPointer("Marketing site"),
		Group:     stringPointer("Public"),
	}}

	list, diags := componentsToState(ctx, types.ListNull(statusPageComponentObjectType), rows)
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}

	var components []statusPageComponentModel
	list.ElementsAs(ctx, &components, false)
	if components[0].MonitorID.ValueString() != testPageMonitor {
		t.Fatalf("expected the monitor back, got %v", components[0].MonitorID)
	}
	if !components[0].Name.IsNull() {
		t.Fatalf("an inherited label is not the page's, got %v", components[0].Name)
	}
	if !components[0].ThirdParty.IsNull() {
		t.Fatalf("expected a monitored component to leave third_party unwritten, got %v", components[0].ThirdParty)
	}
	if components[0].Group.ValueString() != "Public" {
		t.Fatalf("expected the group back, got %v", components[0].Group)
	}
}

func TestStatusPagePatchSendsOnlyWhatChanged(t *testing.T) {
	ctx := context.Background()

	state := statusPageModel{
		Slug:     types.StringValue("acme-status"),
		Title:    types.StringValue("Acme status"),
		Settings: settingsObjectFor(t, map[string]attr.Value{"theme": types.StringValue("light")}),
	}
	plan := statusPageModel{
		Slug:     types.StringValue("acme-status"),
		Title:    types.StringValue("Acme, live"),
		Settings: settingsObjectFor(t, map[string]attr.Value{"theme": types.StringValue("light")}),
	}

	have, _ := encodeStatusPage(ctx, &state)
	want, _ := encodeStatusPage(ctx, &plan)
	delete(have, "slug")
	delete(want, "slug")
	body := client.Diff(have, want)

	if len(body) != 1 || body["title"] != "Acme, live" {
		t.Fatalf("expected only the title to travel, got %v", body)
	}
}

func TestStatusPageToStateComposesThePublicAddress(t *testing.T) {
	ctx := context.Background()
	state, diags := statusPageToState(ctx, nil, &client.StatusPage{
		ID:             testPageID,
		Slug:           "acme-status",
		Title:          "Acme status",
		ComponentCount: 2,
		Created:        1785670783,
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if state.PublicURL.ValueString() != "https://"+client.StatusPageHost+"/acme-status" {
		t.Fatalf("unexpected public address: %v", state.PublicURL)
	}
	if !state.CustomDomain.IsNull() {
		t.Fatalf("expected no custom domain, got %v", state.CustomDomain)
	}
	if state.ComponentCount.ValueInt64() != 2 {
		t.Fatalf("expected the count, got %v", state.ComponentCount)
	}
}

func TestValidateStatusPageComponents(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		component map[string]attr.Value
		wantError bool
	}{
		{"monitored", map[string]attr.Value{"monitor_id": types.StringValue(testPageMonitor)}, false},
		{"third party", map[string]attr.Value{
			"third_party": types.BoolValue(true),
			"name":        types.StringValue("Payments provider"),
		}, false},
		{"both", map[string]attr.Value{
			"third_party": types.BoolValue(true),
			"name":        types.StringValue("Payments provider"),
			"monitor_id":  types.StringValue(testPageMonitor),
		}, true},
		{"third party with no name", map[string]attr.Value{"third_party": types.BoolValue(true)}, true},
		{"neither", map[string]attr.Value{"group": types.StringValue("Public")}, true},
		{"monitored with a pinned state", map[string]attr.Value{
			"monitor_id":   types.StringValue(testPageMonitor),
			"manual_state": types.StringValue("down"),
		}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags := validateStatusPageComponents(ctx, componentList(t, componentValue(t, c.component)))
			if diags.HasError() != c.wantError {
				t.Fatalf("expected error=%v, got %v", c.wantError, diags)
			}
		})
	}

	if diags := validateStatusPageComponents(ctx, types.ListNull(statusPageComponentObjectType)); diags.HasError() {
		t.Fatalf("an unmanaged component set has nothing to refuse: %v", diags)
	}
}

func TestStatusPagePointerMapperPlacesFailuresOnAttributes(t *testing.T) {
	cases := map[string]string{
		"/slug":               "slug",
		"/title":              "title",
		"/settings/slaTarget": "settings.sla_target",
		"/settings/features":  "settings.features",
		"/components":         "components",
	}
	for pointer, want := range cases {
		got, ok := statusPagePointerMapper(pointer)
		if !ok {
			t.Fatalf("expected %s to map somewhere", pointer)
		}
		if got.String() != want {
			t.Fatalf("expected %s to map to %s, got %s", pointer, want, got)
		}
	}
	if _, ok := statusPagePointerMapper("/somethingElse"); ok {
		t.Fatal("an unrecognised pointer belongs on the resource, not an attribute")
	}
}

func TestSlugPattern(t *testing.T) {
	for _, slug := range []string{"acme-status", "acme", "a1-b2-c3", "status123"} {
		if !slugPattern.MatchString(slug) {
			t.Fatalf("expected %q to be accepted", slug)
		}
	}
	for _, slug := range []string{"Acme-Status", "acme--status", "-acme", "acme-", "acme_status", "acme status", ""} {
		if slugPattern.MatchString(slug) {
			t.Fatalf("expected %q to be refused", slug)
		}
	}
}
