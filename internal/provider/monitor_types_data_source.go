package provider

import (
	"context"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*monitorTypesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*monitorTypesDataSource)(nil)
)

// NewMonitorTypesDataSource is the hosttracker_monitor_types constructor.
func NewMonitorTypesDataSource() datasource.DataSource { return &monitorTypesDataSource{} }

type monitorTypesDataSource struct {
	api *client.Client
}

type monitorTypesModel struct {
	Types types.List `tfsdk:"types"`
}

var accountLimitsAttrTypes = map[string]attr.Type{
	"min_interval": types.Int64Type,
	"available":    types.BoolType,
}

var monitorTypeAttrTypes = map[string]attr.Type{
	"type":           types.StringType,
	"label":          types.StringType,
	"creatable":      types.BoolType,
	"attachable":     types.BoolType,
	"min_interval":   types.Int64Type,
	"fixed_interval": types.Int64Type,
	"requires_pool":  types.BoolType,
	"entitlement":    types.StringType,
	"presets":        types.ListType{ElemType: types.StringType},
	"attachable_to":  types.ListType{ElemType: types.StringType},
	"account_limits": types.ObjectType{AttrTypes: accountLimitsAttrTypes},
}

func (d *monitorTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_types"
}

func (d *monitorTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "The monitor-type catalogue: what each type is called, what it costs in entitlements, and " +
			"what interval floor it enforces. `account_limits` is filled in for the configured token's account.",
		Attributes: map[string]dschema.Attribute{
			"types": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "One row per monitor type.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"type":           dschema.StringAttribute{Computed: true, Description: "The type token, as `type` on a monitor takes it."},
						"label":          dschema.StringAttribute{Computed: true, Description: "The human name."},
						"creatable":      dschema.BoolAttribute{Computed: true, Description: "Whether a monitor of this type can be created."},
						"attachable":     dschema.BoolAttribute{Computed: true, Description: "Whether this type can also ride on a parent monitor as a sub-check."},
						"min_interval":   dschema.Int64Attribute{Computed: true, Description: "The type's interval floor, in seconds."},
						"fixed_interval": dschema.Int64Attribute{Computed: true, Description: "The cadence this type is pinned to, in seconds, for the types the service schedules itself."},
						"requires_pool":  dschema.BoolAttribute{Computed: true, Description: "Whether a create is refused without `locations.pools`."},
						"entitlement":    dschema.StringAttribute{Computed: true, Description: "The package entitlement gating the type, if any."},
						"presets":        dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Server-built settings presets this type publishes."},
						"attachable_to":  dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "The parent types this sub-check attaches to."},
						"account_limits": dschema.SingleNestedAttribute{
							Computed:    true,
							Description: "The calling account's own limits for this type.",
							Attributes: map[string]dschema.Attribute{
								"min_interval": dschema.Int64Attribute{Computed: true, Description: "The account's effective interval floor for the type, in seconds."},
								"available":    dschema.BoolAttribute{Computed: true, Description: "False when the account's package does not sell this type at all."},
							},
						},
					},
				},
			},
		},
	}
}

func (d *monitorTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("The monitor types data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.api = api
}

func (d *monitorTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	limit := int32(200)
	fetch := func(ctx context.Context, cursor *string) ([]hosttracker.MonitorTypeRow, *string, error) {
		page, err := d.api.API().ListMonitorTypeWithResponse(ctx, &hosttracker.ListMonitorTypeParams{Cursor: cursor, Limit: &limit})
		if err != nil {
			return nil, nil, err
		}
		if page.JSON200 == nil {
			return nil, nil, nil
		}
		return page.JSON200.Data, page.JSON200.NextCursor, nil
	}

	catalogue, err := hosttracker.Collect(ctx, fetch, 0)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("read the monitor-type catalogue", err, nil)...)
		return
	}

	rows := make([]attr.Value, 0, len(catalogue))
	{
		for _, item := range catalogue {
			limits := types.ObjectNull(accountLimitsAttrTypes)
			if item.AccountLimits != nil {
				object, diags := types.ObjectValue(accountLimitsAttrTypes, map[string]attr.Value{
					"min_interval": types.Int64Value(int64(item.AccountLimits.MinInterval)),
					"available":    types.BoolValue(item.AccountLimits.Available),
				})
				resp.Diagnostics.Append(diags...)
				limits = object
			}
			presets, presetDiags := stringList(ctx, item.Presets)
			resp.Diagnostics.Append(presetDiags...)
			attachableTo, attachDiags := stringList(ctx, item.AttachableTo)
			resp.Diagnostics.Append(attachDiags...)

			object, diags := types.ObjectValue(monitorTypeAttrTypes, map[string]attr.Value{
				"type":           types.StringPointerValue(item.Type),
				"label":          types.StringPointerValue(item.Label),
				"creatable":      types.BoolValue(item.Creatable),
				"attachable":     types.BoolValue(item.Attachable),
				"min_interval":   types.Int64Value(int64(item.MinInterval)),
				"fixed_interval": int32PointerValue(item.FixedInterval),
				"requires_pool":  types.BoolValue(item.RequiresPool),
				"entitlement":    types.StringPointerValue(item.Entitlement),
				"presets":        presets,
				"attachable_to":  attachableTo,
				"account_limits": limits,
			})
			resp.Diagnostics.Append(diags...)
			rows = append(rows, object)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	list, diags := types.ListValue(types.ObjectType{AttrTypes: monitorTypeAttrTypes}, rows)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &monitorTypesModel{Types: list})...)
}
