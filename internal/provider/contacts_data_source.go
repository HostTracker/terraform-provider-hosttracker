package provider

import (
	"context"
	"fmt"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*contactsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*contactsDataSource)(nil)
)

// NewContactsDataSource is the hosttracker_contacts data source
// constructor.
func NewContactsDataSource() datasource.DataSource { return &contactsDataSource{} }

type contactsDataSource struct {
	api *client.Client
}

type contactsModel struct {
	Type       types.Set    `tfsdk:"type"`
	Confirmed  types.Bool   `tfsdk:"confirmed"`
	Q          types.String `tfsdk:"q"`
	MaxResults types.Int64  `tfsdk:"max_results"`
	IDs        types.List   `tfsdk:"ids"`
	Contacts   types.List   `tfsdk:"contacts"`
}

func (d *contactsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contacts"
}

func (d *contactsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Reads the account's contacts, narrowed by the filters below. The filters are ANDed, and " +
			"each list filter matches any of its values.",
		Attributes: map[string]dschema.Attribute{
			"type": dschema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Keep only contacts of these types: `email`, `sms`, `voiceCall`, `http`, `webPush` " +
					"or one of the messenger channels. An unknown type is refused rather than answered with an " +
					"empty list.",
			},
			"confirmed": dschema.BoolAttribute{
				Optional: true,
				Description: "Keep only the contacts that have confirmed their address, or only the ones that " +
					"have not. Unset, both are returned.",
			},
			"q": dschema.StringAttribute{
				Optional:    true,
				Description: "Keep only contacts whose name or address contains this text, matched case-insensitively.",
			},
			"max_results": dschema.Int64Attribute{
				Optional: true,
				Description: fmt.Sprintf("How many contacts to collect across pages. Defaults to %d. The data "+
					"source walks the cursor itself and stops here.", client.DefaultListCap),
			},
			"ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The matched contacts' ids, in the order the API returned them.",
			},
			"contacts": dschema.ListNestedAttribute{
				Computed:     true,
				Description:  "The matched contacts. A listing carries no custom templates.",
				NestedObject: dschema.NestedAttributeObject{Attributes: contactRowAttributes()},
			},
		},
	}
}

func (d *contactsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The contacts data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *contactsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config contactsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := client.ContactFilter{Q: config.Q.ValueString()}
	resp.Diagnostics.Append(setToStrings(ctx, config.Type, &filter.Type)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.Confirmed.IsNull() {
		confirmed := config.Confirmed.ValueBool()
		filter.Confirmed = &confirmed
	}
	if !config.MaxResults.IsNull() {
		filter.Limit = int(config.MaxResults.ValueInt64())
	}

	contacts, err := d.api.ListContacts(ctx, filter)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("list the contacts", err, nil)...)
		return
	}

	rowType := types.ObjectType{AttrTypes: contactRowAttrTypes}
	rows := make([]attr.Value, 0, len(contacts))
	ids := make([]attr.Value, 0, len(contacts))
	for _, contact := range contacts {
		row, diags := contactToRow(ctx, contact)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		object, objectDiags := types.ObjectValueFrom(ctx, contactRowAttrTypes, row)
		resp.Diagnostics.Append(objectDiags...)
		rows = append(rows, object)
		ids = append(ids, types.StringValue(contact.ID))
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

	config.Contacts = rowList
	config.IDs = idList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
