package provider

import (
	"context"
	"fmt"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*maintenanceWindowsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*maintenanceWindowsDataSource)(nil)
)

// NewMaintenanceWindowsDataSource is the hosttracker_maintenance_windows
// data source constructor.
func NewMaintenanceWindowsDataSource() datasource.DataSource { return &maintenanceWindowsDataSource{} }

type maintenanceWindowsDataSource struct {
	api *client.Client
}

type maintenanceWindowsModel struct {
	From       types.Int64  `tfsdk:"from"`
	To         types.Int64  `tfsdk:"to"`
	State      types.Set    `tfsdk:"state"`
	Monitor    types.Set    `tfsdk:"monitor"`
	Name       types.String `tfsdk:"name"`
	MaxResults types.Int64  `tfsdk:"max_results"`
	IDs        types.List   `tfsdk:"ids"`
	Windows    types.List   `tfsdk:"windows"`
}

// maintenanceRow is one window as the data source publishes it.
type maintenanceRow struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	From        types.Int64  `tfsdk:"from"`
	To          types.Int64  `tfsdk:"to"`
	DurationSec types.Int64  `tfsdk:"duration_sec"`
	FromRFC3339 types.String `tfsdk:"from_rfc3339"`
	ToRFC3339   types.String `tfsdk:"to_rfc3339"`
	Timezone    types.String `tfsdk:"timezone"`
	Recurrence  types.Object `tfsdk:"recurrence"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	State       types.String `tfsdk:"state"`
	Overlimited types.Bool   `tfsdk:"overlimited"`
	Suppress    types.Object `tfsdk:"suppress"`
	MonitorIDs  types.Set    `tfsdk:"monitor_ids"`
	Monitors    types.Set    `tfsdk:"monitors"`
	Created     types.Int64  `tfsdk:"created"`
	Updated     types.Int64  `tfsdk:"updated"`
}

var maintenanceRowAttrTypes = map[string]attr.Type{
	"id":           types.StringType,
	"name":         types.StringType,
	"from":         types.Int64Type,
	"to":           types.Int64Type,
	"duration_sec": types.Int64Type,
	"from_rfc3339": types.StringType,
	"to_rfc3339":   types.StringType,
	"timezone":     types.StringType,
	"recurrence":   types.ObjectType{AttrTypes: recurrenceAttrTypes},
	"enabled":      types.BoolType,
	"state":        types.StringType,
	"overlimited":  types.BoolType,
	"suppress":     types.ObjectType{AttrTypes: suppressAttrTypes},
	"monitor_ids":  types.SetType{ElemType: types.StringType},
	"monitors":     types.SetType{ElemType: types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes}},
	"created":      types.Int64Type,
	"updated":      types.Int64Type,
}

func maintenanceRowAttributes() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"id":           dschema.StringAttribute{Computed: true, Description: "The window's id."},
		"name":         dschema.StringAttribute{Computed: true, Description: "The display name."},
		"from":         dschema.Int64Attribute{Computed: true, Description: "When the window opens, in Unix seconds."},
		"to":           dschema.Int64Attribute{Computed: true, Description: "When it closes, in Unix seconds."},
		"duration_sec": dschema.Int64Attribute{Computed: true, Description: "How long it lasts, in seconds of real elapsed time."},
		"from_rfc3339": dschema.StringAttribute{Computed: true, Description: "`from` rendered in UTC, for reading."},
		"to_rfc3339":   dschema.StringAttribute{Computed: true, Description: "`to` rendered in UTC, for reading."},
		"timezone":     dschema.StringAttribute{Computed: true, Description: "The zone the window's wall clock is read in."},
		"recurrence": dschema.SingleNestedAttribute{
			Computed:    true,
			Description: "How the window repeats, or absent for a one-time window.",
			Attributes: map[string]dschema.Attribute{
				"week_days": dschema.SetAttribute{
					ElementType: types.StringType,
					Computed:    true,
					Description: "The days it repeats on.",
				},
			},
		},
		"enabled":     dschema.BoolAttribute{Computed: true, Description: "Whether the window is in force."},
		"state":       dschema.StringAttribute{Computed: true, Description: "`scheduled`, `active` or `finished`."},
		"overlimited": dschema.BoolAttribute{Computed: true, Description: "Whether the account's package has stopped the window from applying."},
		"suppress": dschema.SingleNestedAttribute{
			Computed: true,
			Description: "What the window holds back, while every covered monitor agrees. Where they differ " +
				"it is absent and the answer is in `monitors`.",
			Attributes: map[string]dschema.Attribute{
				"alerts": dschema.BoolAttribute{Computed: true, Description: "Whether alerting is held back."},
				"stats":  dschema.BoolAttribute{Computed: true, Description: "Whether the checks are kept out of the statistics."},
			},
		},
		"monitor_ids": dschema.SetAttribute{
			ElementType: types.StringType,
			Computed:    true,
			Description: "The monitors the window covers.",
		},
		"monitors": dschema.SetNestedAttribute{
			Computed:    true,
			Description: "The coverage per monitor, with the suppression each one really carries.",
			NestedObject: dschema.NestedAttributeObject{
				Attributes: map[string]dschema.Attribute{
					"monitor_id": dschema.StringAttribute{Computed: true, Description: "The monitor this entry is about."},
					"suppress": dschema.SingleNestedAttribute{
						Computed:    true,
						Description: "What the window holds back for this monitor.",
						Attributes: map[string]dschema.Attribute{
							"alerts": dschema.BoolAttribute{Computed: true, Description: "Whether its alerting is held back."},
							"stats":  dschema.BoolAttribute{Computed: true, Description: "Whether its checks are kept out of the statistics."},
						},
					},
				},
			},
		},
		"created": dschema.Int64Attribute{Computed: true, Description: "When the window was created, in Unix seconds."},
		"updated": dschema.Int64Attribute{Computed: true, Description: "When it last changed, in Unix seconds."},
	}
}

func (d *maintenanceWindowsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance_windows"
}

func (d *maintenanceWindowsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Reads the account's maintenance windows, narrowed by the filters below. The filters are " +
			"ANDed, and each list filter matches any of its values.",
		Attributes: map[string]dschema.Attribute{
			"from": dschema.Int64Attribute{
				Optional:    true,
				Description: "Keep only windows that START at or after this instant, in Unix seconds.",
			},
			"to": dschema.Int64Attribute{
				Optional: true,
				Description: "Keep only windows that START at or before this instant, in Unix seconds. It bounds " +
					"the start, not the extent: a long window that began earlier is not matched by it.",
			},
			"state": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only windows in these states: `scheduled`, `active`, `finished`.",
			},
			"monitor": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only windows covering these monitors, by id.",
			},
			"name": dschema.StringAttribute{
				Optional:    true,
				Description: "Keep only windows with this display name, matched exactly.",
			},
			"max_results": dschema.Int64Attribute{
				Optional: true,
				Description: fmt.Sprintf("How many windows to collect across pages. Defaults to %d. The data "+
					"source walks the cursor itself and stops here.", client.DefaultListCap),
			},
			"ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The matched windows' ids, in the order the API returned them.",
			},
			"windows": dschema.ListNestedAttribute{
				Computed:     true,
				Description:  "The matched maintenance windows.",
				NestedObject: dschema.NestedAttributeObject{Attributes: maintenanceRowAttributes()},
			},
		},
	}
}

func (d *maintenanceWindowsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The maintenance windows data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *maintenanceWindowsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config maintenanceWindowsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := client.MaintenanceFilter{
		From: config.From.ValueInt64(),
		To:   config.To.ValueInt64(),
		Name: config.Name.ValueString(),
	}
	resp.Diagnostics.Append(setToStrings(ctx, config.State, &filter.State)...)
	resp.Diagnostics.Append(setToStrings(ctx, config.Monitor, &filter.Monitor)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.MaxResults.IsNull() {
		filter.Limit = int(config.MaxResults.ValueInt64())
	}

	windows, err := d.api.ListMaintenances(ctx, filter)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("list the maintenance windows", err, nil)...)
		return
	}

	rowType := types.ObjectType{AttrTypes: maintenanceRowAttrTypes}
	rows := make([]attr.Value, 0, len(windows))
	ids := make([]attr.Value, 0, len(windows))
	for _, window := range windows {
		row, diags := maintenanceToRow(ctx, window)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		object, objectDiags := types.ObjectValueFrom(ctx, maintenanceRowAttrTypes, row)
		resp.Diagnostics.Append(objectDiags...)
		rows = append(rows, object)
		ids = append(ids, types.StringValue(window.ID))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	rowList, listDiags := types.ListValue(rowType, rows)
	resp.Diagnostics.Append(listDiags...)
	idList, idDiags := types.ListValue(types.StringType, ids)
	resp.Diagnostics.Append(idDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Windows = rowList
	config.IDs = idList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func maintenanceToRow(ctx context.Context, w client.Maintenance) (maintenanceRow, diag.Diagnostics) {
	state, diags := maintenanceToState(ctx, nil, &w)
	return maintenanceRow{
		ID:          state.ID,
		Name:        state.Name,
		From:        state.From,
		To:          state.To,
		DurationSec: state.DurationSec,
		FromRFC3339: state.FromRFC3339,
		ToRFC3339:   state.ToRFC3339,
		Timezone:    state.Timezone,
		Recurrence:  state.Recurrence,
		Enabled:     state.Enabled,
		State:       state.State,
		Overlimited: state.Overlimited,
		Suppress:    state.Suppress,
		MonitorIDs:  state.MonitorIDs,
		Monitors:    state.Monitors,
		Created:     state.Created,
		Updated:     state.Updated,
	}, diags
}
