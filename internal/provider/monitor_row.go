package provider

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// monitorRow is what both monitor data sources publish. Settings arrive as
// JSON rather than as the resource's typed blocks: a read is not bound by
// the union's write rules, and JSON keeps a member this release has never
// heard of readable.
type monitorRow struct {
	ID           types.String  `tfsdk:"id"`
	Type         types.String  `tfsdk:"type"`
	Name         types.String  `tfsdk:"name"`
	URL          types.String  `tfsdk:"url"`
	EffectiveURL types.String  `tfsdk:"effective_url"`
	State        types.String  `tfsdk:"state"`
	Since        types.Int64   `tfsdk:"since"`
	Enabled      types.Bool    `tfsdk:"enabled"`
	Tags         types.Set     `tfsdk:"tags"`
	Interval     types.Int64   `tfsdk:"interval"`
	CronSchedule types.String  `tfsdk:"cron_schedule"`
	FullLog      types.Bool    `tfsdk:"full_log"`
	OpenStat     types.Bool    `tfsdk:"open_stat"`
	SLATarget    types.Float64 `tfsdk:"sla_target"`
	Created      types.Int64   `tfsdk:"created"`
	Updated      types.Int64   `tfsdk:"updated"`
	Locations    types.Object  `tfsdk:"locations"`
	Recheck      types.Object  `tfsdk:"recheck"`
	SettingsJSON types.String  `tfsdk:"settings_json"`
}

var monitorRowAttrTypes = map[string]attr.Type{
	"id":            types.StringType,
	"type":          types.StringType,
	"name":          types.StringType,
	"url":           types.StringType,
	"effective_url": types.StringType,
	"state":         types.StringType,
	"since":         types.Int64Type,
	"enabled":       types.BoolType,
	"tags":          types.SetType{ElemType: types.StringType},
	"interval":      types.Int64Type,
	"cron_schedule": types.StringType,
	"full_log":      types.BoolType,
	"open_stat":     types.BoolType,
	"sla_target":    types.Float64Type,
	"created":       types.Int64Type,
	"updated":       types.Int64Type,
	"locations":     types.ObjectType{AttrTypes: locationsAttrTypes},
	"recheck":       types.ObjectType{AttrTypes: recheckAttrTypes},
	"settings_json": types.StringType,
}

// monitorRowAttributes is the read-only schema shared by both monitor data
// sources.
func monitorRowAttributes() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"id":            dschema.StringAttribute{Computed: true, Description: "The monitor's id."},
		"type":          dschema.StringAttribute{Computed: true, Description: "Which kind of check this is."},
		"name":          dschema.StringAttribute{Computed: true, Description: "The display name."},
		"url":           dschema.StringAttribute{Computed: true, Description: "The address the check is aimed at, as it was written."},
		"effective_url": dschema.StringAttribute{Computed: true, Description: "The normalized address actually monitored."},
		"state":         dschema.StringAttribute{Computed: true, Description: "`up`, `down`, `paused` or `maintenance`."},
		"since":         dschema.Int64Attribute{Computed: true, Description: "When the current state began, in Unix seconds."},
		"enabled":       dschema.BoolAttribute{Computed: true, Description: "The configured enablement flag."},
		"tags":          dschema.SetAttribute{ElementType: types.StringType, Computed: true, Description: "The monitor's tags."},
		"interval":      dschema.Int64Attribute{Computed: true, Description: "The check interval, in seconds."},
		"cron_schedule": dschema.StringAttribute{Computed: true, Description: "The cron expression, when the monitor is cron-scheduled."},
		"full_log":      dschema.BoolAttribute{Computed: true, Description: "Whether every check is recorded."},
		"open_stat":     dschema.BoolAttribute{Computed: true, Description: "Whether the statistics are publicly readable."},
		"sla_target":    dschema.Float64Attribute{Computed: true, Description: "The uptime percentage the monitor is measured against."},
		"created":       dschema.Int64Attribute{Computed: true, Description: "When the monitor was created, in Unix seconds."},
		"updated":       dschema.Int64Attribute{Computed: true, Description: "The API's change marker, in Unix seconds."},
		"locations": dschema.SingleNestedAttribute{
			Computed:    true,
			Description: "Where the check runs from.",
			Attributes: map[string]dschema.Attribute{
				"pools":           dschema.SetAttribute{ElementType: types.StringType, Computed: true, Description: "The monitoring-location pools."},
				"fallback":        dschema.StringAttribute{Computed: true, Description: "What happens when no chosen location is available."},
				"excluded_agents": dschema.SetAttribute{ElementType: types.StringType, Computed: true, Description: "Locations explicitly kept off this check."},
			},
		},
		"recheck": dschema.SingleNestedAttribute{
			Computed:    true,
			Description: "The quorum rule applied before a failure concludes.",
			Attributes: map[string]dschema.Attribute{
				"strategy":     dschema.StringAttribute{Computed: true, Description: "The quorum rule."},
				"min_num_down": dschema.Int64Attribute{Computed: true, Description: "How many locations must report a failure."},
			},
		},
		"settings_json": dschema.StringAttribute{
			Computed:    true,
			Description: "The monitor's whole settings object as JSON. Empty when the read did not ask for settings.",
		},
	}
}

func monitorToRow(ctx context.Context, m client.Monitor) (monitorRow, diag.Diagnostics) {
	var diags diag.Diagnostics
	row := monitorRow{
		ID:           types.StringValue(m.ID),
		Type:         types.StringValue(m.Type),
		Name:         stringOrNull(m.Name),
		URL:          types.StringValue(m.URL),
		State:        stringOrNull(m.State),
		Since:        types.Int64Value(m.Since),
		Enabled:      types.BoolValue(m.Enabled),
		Interval:     int64OrNull(m.Interval),
		CronSchedule: stringOrNull(m.CronSchedule),
		FullLog:      types.BoolValue(m.FullLog),
		OpenStat:     types.BoolValue(m.OpenStat),
		SLATarget:    float64OrNull(m.SLATarget),
		Created:      int64OrNull(m.Created),
		Updated:      types.Int64Value(m.Updated),
	}

	if m.EffectiveURL != nil && *m.EffectiveURL != "" {
		row.EffectiveURL = types.StringValue(*m.EffectiveURL)
	} else {
		row.EffectiveURL = types.StringValue(m.URL)
	}

	tags := append([]string(nil), m.Tags...)
	sort.Strings(tags)
	tagSet, tagDiags := types.SetValueFrom(ctx, types.StringType, tags)
	diags.Append(tagDiags...)
	row.Tags = tagSet

	if m.Locations == nil {
		row.Locations = types.ObjectNull(locationsAttrTypes)
	} else {
		pools, poolDiags := stringSet(ctx, m.Locations.Pools)
		diags.Append(poolDiags...)
		excluded, excludedDiags := stringSet(ctx, m.Locations.ExcludedAgents)
		diags.Append(excludedDiags...)
		object, objectDiags := types.ObjectValue(locationsAttrTypes, map[string]attr.Value{
			"pools":           pools,
			"fallback":        stringOrNull(m.Locations.Fallback),
			"excluded_agents": excluded,
		})
		diags.Append(objectDiags...)
		row.Locations = object
	}

	if m.Recheck == nil {
		row.Recheck = types.ObjectNull(recheckAttrTypes)
	} else {
		object, objectDiags := types.ObjectValue(recheckAttrTypes, map[string]attr.Value{
			"strategy":     stringOrNull(m.Recheck.Strategy),
			"min_num_down": int64OrNull(m.Recheck.MinNumDown),
		})
		diags.Append(objectDiags...)
		row.Recheck = object
	}

	if m.Settings == nil {
		row.SettingsJSON = types.StringNull()
	} else {
		raw, err := json.Marshal(m.Settings)
		if err != nil {
			diags.AddError("Could not render the monitor settings as JSON", err.Error())
			row.SettingsJSON = types.StringNull()
		} else {
			row.SettingsJSON = types.StringValue(string(raw))
		}
	}

	return row, diags
}
