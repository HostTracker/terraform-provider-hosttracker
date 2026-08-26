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
	_ datasource.DataSource              = (*locationsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*locationsDataSource)(nil)
)

// NewLocationsDataSource is the hosttracker_locations constructor.
func NewLocationsDataSource() datasource.DataSource { return &locationsDataSource{} }

type locationsDataSource struct {
	api *client.Client
}

type locationsModel struct {
	Country    types.Set  `tfsdk:"country"`
	Pool       types.Set  `tfsdk:"pool"`
	Capability types.Set  `tfsdk:"capability"`
	Pools      types.List `tfsdk:"pools"`
	PoolIDs    types.List `tfsdk:"pool_ids"`
	Agents     types.List `tfsdk:"agents"`
	AgentIDs   types.List `tfsdk:"agent_ids"`
}

var poolAttrTypes = map[string]attr.Type{
	"id":       types.StringType,
	"name":     types.StringType,
	"hidden":   types.BoolType,
	"priority": types.Int64Type,
	"children": types.ListType{ElemType: types.StringType},
	"parents":  types.ListType{ElemType: types.StringType},
}

var agentAttrTypes = map[string]attr.Type{
	"id":           types.StringType,
	"name":         types.StringType,
	"city":         types.StringType,
	"region":       types.StringType,
	"country":      types.StringType,
	"lat":          types.Float64Type,
	"lon":          types.Float64Type,
	"version":      types.StringType,
	"capabilities": types.ListType{ElemType: types.StringType},
	"pools":        types.ListType{ElemType: types.StringType},
	"ip":           types.StringType,
	"ipv6":         types.BoolType,
	"visible":      types.BoolType,
}

func (d *locationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_locations"
}

func (d *locationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "The monitoring locations and the pools they belong to. A monitor's `locations.pools` takes " +
			"pool ids from here; `[\"allworld\"]` means everywhere. The filters narrow the agent list only - the " +
			"pool list is always the whole tree.",
		Attributes: map[string]dschema.Attribute{
			"country": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only locations in these countries, matched case-insensitively.",
			},
			"pool": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only locations belonging to any of these pools.",
			},
			"capability": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only locations offering all of these capabilities: `icmp` (ping and trace), " +
					"`browser` (the Waterfall fleet), `internal`.",
			},
			"pool_ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Every pool id, in the order the API returned them.",
			},
			"pools": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "The pool tree.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"id":       dschema.StringAttribute{Computed: true, Description: "The pool id, as `locations.pools` takes it."},
						"name":     dschema.StringAttribute{Computed: true, Description: "The human name."},
						"hidden":   dschema.BoolAttribute{Computed: true, Description: "Whether the pool is offered in a picker. A hidden pool is still selectable."},
						"priority": dschema.Int64Attribute{Computed: true, Description: "The pool's ordering weight, ascending."},
						"children": dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Nested pool ids."},
						"parents":  dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Pools that contain this one."},
					},
				},
			},
			"agent_ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The matched locations' ids, in the order the API returned them.",
			},
			"agents": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "The matched monitoring locations.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"id":           dschema.StringAttribute{Computed: true, Description: "The location id, as `locations.excluded_agents` takes it."},
						"name":         dschema.StringAttribute{Computed: true, Description: "The label, `\"Country, State, City\"`."},
						"city":         dschema.StringAttribute{Computed: true, Description: "The city."},
						"region":       dschema.StringAttribute{Computed: true, Description: "The first-level administrative division the city sits in."},
						"country":      dschema.StringAttribute{Computed: true, Description: "The country."},
						"lat":          dschema.Float64Attribute{Computed: true, Description: "Latitude."},
						"lon":          dschema.Float64Attribute{Computed: true, Description: "Longitude."},
						"version":      dschema.StringAttribute{Computed: true, Description: "The agent build running there."},
						"capabilities": dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "What this location can check: `icmp`, `browser`, `internal`."},
						"pools":        dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "The pools this location belongs to."},
						"ip":           dschema.StringAttribute{Computed: true, Description: "The address this location's checks go out from - what an allow-list entry for it looks like."},
						"ipv6":         dschema.BoolAttribute{Computed: true, Description: "Whether this location can reach IPv6 targets. A capability, not an address."},
						"visible":      dschema.BoolAttribute{Computed: true, Description: "Whether this location is offered in a location picker."},
					},
				},
			},
		},
	}
}

func (d *locationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("The locations data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.api = api
}

func (d *locationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config locationsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var countries, pools, capabilities []string
	resp.Diagnostics.Append(setToStrings(ctx, config.Country, &countries)...)
	resp.Diagnostics.Append(setToStrings(ctx, config.Pool, &pools)...)
	resp.Diagnostics.Append(setToStrings(ctx, config.Capability, &capabilities)...)
	if resp.Diagnostics.HasError() {
		return
	}

	limit := int32(500)
	poolFetch := func(ctx context.Context, cursor *string) ([]hosttracker.AgentPoolView, *string, error) {
		page, err := d.api.API().ListAgentPoolWithResponse(ctx, &hosttracker.ListAgentPoolParams{Cursor: cursor, Limit: &limit})
		if err != nil || page.JSON200 == nil {
			return nil, nil, err
		}
		return page.JSON200.Data, page.JSON200.NextCursor, nil
	}
	poolRows, err := hosttracker.Collect(ctx, poolFetch, 0)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("read the monitoring-location pools", err, nil)...)
		return
	}

	agentFetch := func(ctx context.Context, cursor *string) ([]hosttracker.AgentView, *string, error) {
		params := &hosttracker.ListAgentParams{Cursor: cursor, Limit: &limit}
		if len(countries) > 0 {
			params.Country = &countries
		}
		if len(pools) > 0 {
			params.Pool = &pools
		}
		if len(capabilities) > 0 {
			values := make([]hosttracker.ListAgentParamsCapability, 0, len(capabilities))
			for _, c := range capabilities {
				values = append(values, hosttracker.ListAgentParamsCapability(c))
			}
			params.Capability = &values
		}
		page, err := d.api.API().ListAgentWithResponse(ctx, params)
		if err != nil || page.JSON200 == nil {
			return nil, nil, err
		}
		return page.JSON200.Data, page.JSON200.NextCursor, nil
	}
	agentRows, err := hosttracker.Collect(ctx, agentFetch, 0)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("read the monitoring locations", err, nil)...)
		return
	}

	poolValues := make([]attr.Value, 0, len(poolRows))
	poolIDs := make([]attr.Value, 0, len(poolRows))
	for _, p := range poolRows {
		children, childDiags := stringList(ctx, p.Children)
		resp.Diagnostics.Append(childDiags...)
		parents, parentDiags := stringList(ctx, p.Parents)
		resp.Diagnostics.Append(parentDiags...)
		object, diags := types.ObjectValue(poolAttrTypes, map[string]attr.Value{
			"id":       types.StringPointerValue(p.Id),
			"name":     types.StringPointerValue(p.Name),
			"hidden":   types.BoolValue(p.Hidden),
			"priority": types.Int64Value(int64(p.Priority)),
			"children": children,
			"parents":  parents,
		})
		resp.Diagnostics.Append(diags...)
		poolValues = append(poolValues, object)
		if p.Id != nil {
			poolIDs = append(poolIDs, types.StringValue(*p.Id))
		}
	}

	agentValues := make([]attr.Value, 0, len(agentRows))
	agentIDs := make([]attr.Value, 0, len(agentRows))
	for _, a := range agentRows {
		capabilityList, capDiags := stringList(ctx, a.Capabilities)
		resp.Diagnostics.Append(capDiags...)
		poolList, poolDiags := stringList(ctx, a.Pools)
		resp.Diagnostics.Append(poolDiags...)
		object, diags := types.ObjectValue(agentAttrTypes, map[string]attr.Value{
			"id":           types.StringValue(a.Id.String()),
			"name":         types.StringPointerValue(a.Name),
			"city":         types.StringPointerValue(a.City),
			"region":       types.StringPointerValue(a.Region),
			"country":      types.StringPointerValue(a.Country),
			"lat":          types.Float64PointerValue(a.Lat),
			"lon":          types.Float64PointerValue(a.Lon),
			"version":      types.StringPointerValue(a.Version),
			"capabilities": capabilityList,
			"pools":        poolList,
			"ip":           types.StringPointerValue(a.Ip),
			"ipv6":         types.BoolValue(a.Ipv6),
			"visible":      types.BoolValue(a.Visible),
		})
		resp.Diagnostics.Append(diags...)
		agentValues = append(agentValues, object)
		agentIDs = append(agentIDs, types.StringValue(a.Id.String()))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	poolList, diags := types.ListValue(types.ObjectType{AttrTypes: poolAttrTypes}, poolValues)
	resp.Diagnostics.Append(diags...)
	poolIDList, diags := types.ListValue(types.StringType, poolIDs)
	resp.Diagnostics.Append(diags...)
	agentList, diags := types.ListValue(types.ObjectType{AttrTypes: agentAttrTypes}, agentValues)
	resp.Diagnostics.Append(diags...)
	agentIDList, diags := types.ListValue(types.StringType, agentIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Pools = poolList
	config.PoolIDs = poolIDList
	config.Agents = agentList
	config.AgentIDs = agentIDList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
