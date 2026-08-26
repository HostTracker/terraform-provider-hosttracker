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
	_ datasource.DataSource              = (*monitorsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*monitorsDataSource)(nil)
)

// NewMonitorsDataSource is the hosttracker_monitors data source constructor.
func NewMonitorsDataSource() datasource.DataSource { return &monitorsDataSource{} }

type monitorsDataSource struct {
	api *client.Client
}

type monitorsModel struct {
	State           types.Set    `tfsdk:"state"`
	Type            types.Set    `tfsdk:"type"`
	Tag             types.Set    `tfsdk:"tag"`
	Q               types.String `tfsdk:"q"`
	IncludeSettings types.Bool   `tfsdk:"include_settings"`
	MaxResults      types.Int64  `tfsdk:"max_results"`
	IDs             types.List   `tfsdk:"ids"`
	Monitors        types.List   `tfsdk:"monitors"`
}

func (d *monitorsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitors"
}

func (d *monitorsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Reads the account's monitors, narrowed by the filters below. The filters are ANDed, and " +
			"each list filter matches any of its values.",
		Attributes: map[string]dschema.Attribute{
			"state": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only monitors in these states: `up`, `down`, `paused`, `maintenance`.",
			},
			"type": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only monitors of these types. An unknown type is refused rather than answered with an empty list.",
			},
			"tag": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only monitors carrying any of these tags, matched exactly.",
			},
			"q": dschema.StringAttribute{
				Optional:    true,
				Description: "Keep only monitors whose name or address contains this text, matched case-insensitively.",
			},
			"include_settings": dschema.BoolAttribute{
				Optional: true,
				Description: "Also read each monitor's settings into `settings_json`. Off by default: settings are " +
					"the expensive part of a listing.",
			},
			"max_results": dschema.Int64Attribute{
				Optional: true,
				Description: fmt.Sprintf("How many monitors to collect across pages. Defaults to %d. The data "+
					"source walks the cursor itself and stops here.", client.DefaultListCap),
			},
			"ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The matched monitors' ids, in the order the API returned them.",
			},
			"monitors": dschema.ListNestedAttribute{
				Computed:     true,
				Description:  "The matched monitors.",
				NestedObject: dschema.NestedAttributeObject{Attributes: monitorRowAttributes()},
			},
		},
	}
}

func (d *monitorsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The monitors data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *monitorsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config monitorsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := client.MonitorFilter{Q: config.Q.ValueString()}
	resp.Diagnostics.Append(setToStrings(ctx, config.State, &filter.State)...)
	resp.Diagnostics.Append(setToStrings(ctx, config.Type, &filter.Type)...)
	resp.Diagnostics.Append(setToStrings(ctx, config.Tag, &filter.Tag)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.MaxResults.IsNull() {
		filter.Limit = int(config.MaxResults.ValueInt64())
	}
	if config.IncludeSettings.ValueBool() {
		filter.Expand = []string{"settings"}
	}

	monitors, err := d.api.ListMonitors(ctx, filter)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("list the monitors", err, nil)...)
		return
	}

	rowType := types.ObjectType{AttrTypes: monitorRowAttrTypes}
	rows := make([]attr.Value, 0, len(monitors))
	ids := make([]attr.Value, 0, len(monitors))
	for _, monitor := range monitors {
		row, diags := monitorToRow(ctx, monitor)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		object, objectDiags := types.ObjectValueFrom(ctx, monitorRowAttrTypes, row)
		resp.Diagnostics.Append(objectDiags...)
		rows = append(rows, object)
		ids = append(ids, types.StringValue(monitor.ID))
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

	config.Monitors = rowList
	config.IDs = idList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func setToStrings(ctx context.Context, set types.Set, out *[]string) diag.Diagnostics {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	return set.ElementsAs(ctx, out, false)
}
