package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*contactDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*contactDataSource)(nil)
)

// NewContactDataSource is the hosttracker_contact data source constructor.
func NewContactDataSource() datasource.DataSource { return &contactDataSource{} }

type contactDataSource struct {
	api *client.Client
}

// contactLookupModel is the row plus the two lookup keys. Its members
// repeat contactRow's rather than embedding it, because the framework maps
// state onto flat structs.
type contactLookupModel struct {
	ID                   types.String  `tfsdk:"id"`
	Type                 types.String  `tfsdk:"type"`
	Name                 types.String  `tfsdk:"name"`
	Address              types.String  `tfsdk:"address"`
	Confirmed            types.Bool    `tfsdk:"confirmed"`
	Overlimited          types.Bool    `tfsdk:"overlimited"`
	AlertDelay           types.Int64   `tfsdk:"alert_delay"`
	SendCost             types.Float64 `tfsdk:"send_cost"`
	Gateway              types.String  `tfsdk:"gateway"`
	Language             types.String  `tfsdk:"language"`
	GroupedAlerts        types.Bool    `tfsdk:"grouped_alerts"`
	BillingNotifications types.Bool    `tfsdk:"billing_notifications"`
	SendNews             types.Bool    `tfsdk:"send_news"`
	MimeType             types.String  `tfsdk:"mime_type"`
	HTTPHeaders          types.List    `tfsdk:"http_headers"`
	Templates            types.List    `tfsdk:"templates"`
	ActivePeriod         types.Object  `tfsdk:"active_period"`
	BotID                types.String  `tfsdk:"bot_id"`
	Created              types.Int64   `tfsdk:"created"`
	Updated              types.Int64   `tfsdk:"updated"`
	LookupAddress        types.String  `tfsdk:"lookup_address"`
	LookupName           types.String  `tfsdk:"lookup_name"`
}

func contactLookupModelFrom(row contactRow, lookupAddress, lookupName types.String) contactLookupModel {
	return contactLookupModel{
		ID:                   row.ID,
		Type:                 row.Type,
		Name:                 row.Name,
		Address:              row.Address,
		Confirmed:            row.Confirmed,
		Overlimited:          row.Overlimited,
		AlertDelay:           row.AlertDelay,
		SendCost:             row.SendCost,
		Gateway:              row.Gateway,
		Language:             row.Language,
		GroupedAlerts:        row.GroupedAlerts,
		BillingNotifications: row.BillingNotifications,
		SendNews:             row.SendNews,
		MimeType:             row.MimeType,
		HTTPHeaders:          row.HTTPHeaders,
		Templates:            row.Templates,
		ActivePeriod:         row.ActivePeriod,
		BotID:                row.BotID,
		Created:              row.Created,
		Updated:              row.Updated,
		LookupAddress:        lookupAddress,
		LookupName:           lookupName,
	}
}

func (d *contactDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (d *contactDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := contactRowAttributes()
	attributes["id"] = dschema.StringAttribute{
		Optional:    true,
		Computed:    true,
		Description: "The contact's id. Name it, or look the contact up by `lookup_address` or `lookup_name`.",
	}
	attributes["lookup_address"] = dschema.StringAttribute{
		Optional: true,
		Description: "Find the contact by the address it delivers to, matched exactly. The lookup must " +
			"resolve to exactly one contact.",
	}
	attributes["lookup_name"] = dschema.StringAttribute{
		Optional: true,
		Description: "Find the contact by its display name, matched exactly. The lookup must resolve to " +
			"exactly one contact.",
	}

	resp.Schema = dschema.Schema{
		Description: "Reads one contact, by id or by a lookup that resolves to exactly one.",
		Attributes:  attributes,
	}
}

func (d *contactDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The contact data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *contactDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config contactLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	byID := !config.ID.IsNull() && config.ID.ValueString() != ""
	byAddress := !config.LookupAddress.IsNull() && config.LookupAddress.ValueString() != ""
	byName := !config.LookupName.IsNull() && config.LookupName.ValueString() != ""

	chosen := 0
	for _, set := range []bool{byID, byAddress, byName} {
		if set {
			chosen++
		}
	}
	if chosen != 1 {
		resp.Diagnostics.AddError(
			"Name exactly one way of finding the contact",
			"Set one of `id`, `lookup_address` or `lookup_name`, and only one.",
		)
		return
	}

	var found *client.Contact
	switch {
	case byID:
		contact, err := d.api.GetContact(ctx, config.ID.ValueString())
		if err != nil {
			if err == client.ErrNotFound || client.IsNotFound(err) {
				resp.Diagnostics.AddAttributeError(
					path.Root("id"),
					"No such contact",
					fmt.Sprintf("No contact with id %s belongs to this account.", config.ID.ValueString()),
				)
				return
			}
			resp.Diagnostics.Append(client.Diagnose("read the contact", err, nil)...)
			return
		}
		found = contact

	default:
		filter := client.ContactFilter{Limit: 50}
		attribute := path.Root("lookup_address")
		described := ""
		if byAddress {
			filter.Address = config.LookupAddress.ValueString()
			filter.Q = config.LookupAddress.ValueString()
			described = fmt.Sprintf("address %q", config.LookupAddress.ValueString())
		} else {
			attribute = path.Root("lookup_name")
			filter.Name = config.LookupName.ValueString()
			filter.Q = config.LookupName.ValueString()
			described = fmt.Sprintf("name %q", config.LookupName.ValueString())
		}

		contacts, err := d.api.ListContacts(ctx, filter)
		if err != nil {
			resp.Diagnostics.Append(client.Diagnose("look the contact up", err, nil)...)
			return
		}
		switch len(contacts) {
		case 0:
			resp.Diagnostics.AddAttributeError(attribute, "No such contact",
				fmt.Sprintf("No contact with %s belongs to this account.", described))
			return
		case 1:
			// The listing carries no templates, so the match is re-read.
			contact, err := d.api.GetContact(ctx, contacts[0].ID)
			if err != nil {
				resp.Diagnostics.Append(client.Diagnose("read the contact", err, nil)...)
				return
			}
			found = contact
		default:
			ids := make([]string, 0, len(contacts))
			for _, c := range contacts {
				ids = append(ids, c.ID)
			}
			resp.Diagnostics.AddAttributeError(attribute, "The lookup matched more than one contact",
				fmt.Sprintf("%d contacts carry %s: %s. Name the one you mean by `id`.",
					len(contacts), described, strings.Join(ids, ", ")))
			return
		}
	}

	row, diags := contactToRow(ctx, *found)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := contactLookupModelFrom(row, config.LookupAddress, config.LookupName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
