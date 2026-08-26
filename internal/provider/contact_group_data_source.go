package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*contactGroupDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*contactGroupDataSource)(nil)
)

// NewContactGroupDataSource is the hosttracker_contact_group data source
// constructor.
func NewContactGroupDataSource() datasource.DataSource { return &contactGroupDataSource{} }

type contactGroupDataSource struct {
	api *client.Client
}

type contactGroupLookupModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Items      types.Set    `tfsdk:"items"`
	Created    types.Int64  `tfsdk:"created"`
	ContactIDs types.Set    `tfsdk:"contact_ids"`
	LookupName types.String `tfsdk:"lookup_name"`
}

func (d *contactGroupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_group"
}

func (d *contactGroupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Reads one contact group, by id or by the name it is known under.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The group's id. Name it, or look the group up by `lookup_name`.",
			},
			"lookup_name": dschema.StringAttribute{
				Optional: true,
				Description: "Find the group by its name, matched exactly. Group names are unique per account, " +
					"so this always resolves to one group or to none.",
			},
			"name": dschema.StringAttribute{
				Computed:    true,
				Description: "The group's name.",
			},
			"items": dschema.SetNestedAttribute{
				Computed:    true,
				Description: "The group's membership.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"contact_id": dschema.StringAttribute{Computed: true, Description: "The member contact's id."},
						"events": dschema.SetAttribute{
							ElementType: types.StringType,
							Computed:    true,
							Description: "What this membership subscribes the contact to.",
						},
					},
				},
			},
			"contact_ids": dschema.SetAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The member contacts' ids, for wiring the group's contacts into other resources.",
			},
			"created": dschema.Int64Attribute{
				Computed:    true,
				Description: "When the group was created, in Unix seconds.",
			},
		},
	}
}

func (d *contactGroupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The contact group data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *contactGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config contactGroupLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	byID := !config.ID.IsNull() && config.ID.ValueString() != ""
	byName := !config.LookupName.IsNull() && config.LookupName.ValueString() != ""
	if byID == byName {
		resp.Diagnostics.AddError(
			"Name exactly one way of finding the group",
			"Set either `id` or `lookup_name`, and only one.",
		)
		return
	}

	var found *client.ContactGroup
	if byID {
		group, err := d.api.GetContactGroup(ctx, config.ID.ValueString())
		if err != nil {
			if err == client.ErrNotFound || client.IsNotFound(err) {
				resp.Diagnostics.AddAttributeError(
					path.Root("id"),
					"No such contact group",
					fmt.Sprintf("No contact group with id %s belongs to this account.", config.ID.ValueString()),
				)
				return
			}
			resp.Diagnostics.Append(client.Diagnose("read the contact group", err, nil)...)
			return
		}
		found = group
	} else {
		groups, err := d.api.ListContactGroups(ctx, 0)
		if err != nil {
			resp.Diagnostics.Append(client.Diagnose("look the contact group up", err, nil)...)
			return
		}
		wanted := config.LookupName.ValueString()
		for i := range groups {
			if groups[i].Name != nil && strings.EqualFold(*groups[i].Name, wanted) {
				found = &groups[i]
				break
			}
		}
		if found == nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("lookup_name"),
				"No such contact group",
				fmt.Sprintf("No contact group named %q belongs to this account.", wanted),
			)
			return
		}
	}

	row, diags := contactGroupToState(ctx, found)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ids := make([]attr.Value, 0, len(found.Items))
	for _, item := range found.Items {
		ids = append(ids, types.StringValue(item.ContactID()))
	}
	contactIDs, idDiags := types.SetValue(types.StringType, ids)
	resp.Diagnostics.Append(idDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := contactGroupLookupModel{
		ID:         row.ID,
		Name:       row.Name,
		Items:      row.Items,
		Created:    row.Created,
		ContactIDs: contactIDs,
		LookupName: config.LookupName,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
