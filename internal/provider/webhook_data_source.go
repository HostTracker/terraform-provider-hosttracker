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
	_ datasource.DataSource              = (*webhookDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*webhookDataSource)(nil)
)

// NewWebhookDataSource is the hosttracker_webhook data source constructor.
func NewWebhookDataSource() datasource.DataSource { return &webhookDataSource{} }

type webhookDataSource struct {
	api *client.Client
}

// webhookLookupModel is the row plus the lookup key. Its members repeat
// webhookRow's rather than embedding it, because the framework maps state
// onto flat structs.
type webhookLookupModel struct {
	ID                  types.String `tfsdk:"id"`
	URL                 types.String `tfsdk:"url"`
	Name                types.String `tfsdk:"name"`
	Events              types.Set    `tfsdk:"events"`
	Scope               types.Object `tfsdk:"scope"`
	MonitorCount        types.Int64  `tfsdk:"monitor_count"`
	ResolvedMonitorIDs  types.Set    `tfsdk:"resolved_monitor_ids"`
	Headers             types.Set    `tfsdk:"headers"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	DisabledReason      types.String `tfsdk:"disabled_reason"`
	ConsecutiveFailures types.Int64  `tfsdk:"consecutive_failures"`
	LastDeliveryAt      types.Int64  `tfsdk:"last_delivery_at"`
	SecretSet           types.Bool   `tfsdk:"secret_set"`
	SecretUpdatedAt     types.Int64  `tfsdk:"secret_updated_at"`
	Created             types.Int64  `tfsdk:"created"`
	Updated             types.Int64  `tfsdk:"updated"`
	LookupURL           types.String `tfsdk:"lookup_url"`
}

func webhookLookupFrom(row webhookRow, lookupURL types.String) webhookLookupModel {
	return webhookLookupModel{
		ID:                  row.ID,
		URL:                 row.URL,
		Name:                row.Name,
		Events:              row.Events,
		Scope:               row.Scope,
		MonitorCount:        row.MonitorCount,
		ResolvedMonitorIDs:  row.ResolvedMonitorIDs,
		Headers:             row.Headers,
		Enabled:             row.Enabled,
		DisabledReason:      row.DisabledReason,
		ConsecutiveFailures: row.ConsecutiveFailures,
		LastDeliveryAt:      row.LastDeliveryAt,
		SecretSet:           row.SecretSet,
		SecretUpdatedAt:     row.SecretUpdatedAt,
		Created:             row.Created,
		Updated:             row.Updated,
		LookupURL:           lookupURL,
	}
}

func (d *webhookDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (d *webhookDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := webhookRowAttributes()
	attributes["id"] = dschema.StringAttribute{
		Optional:    true,
		Computed:    true,
		Description: "The webhook's id. Name it, or look the webhook up by `lookup_url`.",
	}
	attributes["lookup_url"] = dschema.StringAttribute{
		Optional: true,
		Description: "Find the webhook by the address it POSTs to, matched exactly. The lookup must resolve to " +
			"exactly one webhook.",
	}

	resp.Schema = dschema.Schema{
		Description: "Reads one webhook, by id or by the address it delivers to.\n\n" +
			"The signing secret is not among the members: the API publishes its value only in the answer that " +
			"mints it.",
		Attributes: attributes,
	}
}

func (d *webhookDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The webhook data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *webhookDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config webhookLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	byID := !config.ID.IsNull() && config.ID.ValueString() != ""
	byURL := !config.LookupURL.IsNull() && config.LookupURL.ValueString() != ""
	if byID == byURL {
		resp.Diagnostics.AddError(
			"Name exactly one way of finding the webhook",
			"Set either `id` or `lookup_url`, and only one.",
		)
		return
	}

	var found *client.Webhook
	if byID {
		webhook, err := d.api.GetWebhook(ctx, config.ID.ValueString())
		if err != nil {
			if err == client.ErrNotFound || client.IsNotFound(err) {
				resp.Diagnostics.AddAttributeError(
					path.Root("id"),
					"No such webhook",
					fmt.Sprintf("No webhook with id %s belongs to this account.", config.ID.ValueString()),
				)
				return
			}
			resp.Diagnostics.Append(client.Diagnose("read the webhook", err, nil)...)
			return
		}
		found = webhook
	} else {
		address := config.LookupURL.ValueString()
		webhooks, err := d.api.ListWebhooks(ctx, client.WebhookFilter{URL: address})
		if err != nil {
			resp.Diagnostics.Append(client.Diagnose("look the webhook up", err, nil)...)
			return
		}
		switch len(webhooks) {
		case 0:
			resp.Diagnostics.AddAttributeError(path.Root("lookup_url"), "No such webhook",
				fmt.Sprintf("No webhook delivering to %q belongs to this account.", address))
			return
		case 1:
			found = &webhooks[0]
		default:
			ids := make([]string, 0, len(webhooks))
			for _, w := range webhooks {
				ids = append(ids, w.ID)
			}
			resp.Diagnostics.AddAttributeError(path.Root("lookup_url"), "The lookup matched more than one webhook",
				fmt.Sprintf("%d webhooks deliver to %q: %s. Name the one you mean by `id`.",
					len(webhooks), address, strings.Join(ids, ", ")))
			return
		}
	}

	row, diags := webhookToRow(ctx, *found)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := webhookLookupFrom(row, config.LookupURL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------------------------------------------------------------------
// hosttracker_webhooks
// ---------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*webhooksDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*webhooksDataSource)(nil)
)

// NewWebhooksDataSource is the hosttracker_webhooks data source
// constructor.
func NewWebhooksDataSource() datasource.DataSource { return &webhooksDataSource{} }

type webhooksDataSource struct {
	api *client.Client
}

type webhooksModel struct {
	MaxResults types.Int64 `tfsdk:"max_results"`
	IDs        types.List  `tfsdk:"ids"`
	Webhooks   types.List  `tfsdk:"webhooks"`
}

func (d *webhooksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhooks"
}

func (d *webhooksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Reads the account's webhooks.",
		Attributes: map[string]dschema.Attribute{
			"max_results": dschema.Int64Attribute{
				Optional: true,
				Description: fmt.Sprintf("How many webhooks to collect across pages. Defaults to %d. The data "+
					"source walks the cursor itself and stops here.", client.DefaultListCap),
			},
			"ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The webhooks' ids, in the order the API returned them.",
			},
			"webhooks": dschema.ListNestedAttribute{
				Computed:     true,
				Description:  "The account's webhooks.",
				NestedObject: dschema.NestedAttributeObject{Attributes: webhookRowAttributes()},
			},
		},
	}
}

func (d *webhooksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The webhooks data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *webhooksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config webhooksModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := client.WebhookFilter{}
	if !config.MaxResults.IsNull() {
		filter.Limit = int(config.MaxResults.ValueInt64())
	}

	webhooks, err := d.api.ListWebhooks(ctx, filter)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("list the webhooks", err, nil)...)
		return
	}

	rowType := types.ObjectType{AttrTypes: webhookRowAttrTypes}
	rows := make([]attr.Value, 0, len(webhooks))
	ids := make([]attr.Value, 0, len(webhooks))
	for _, webhook := range webhooks {
		row, diags := webhookToRow(ctx, webhook)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		object, objectDiags := types.ObjectValueFrom(ctx, webhookRowAttrTypes, row)
		resp.Diagnostics.Append(objectDiags...)
		rows = append(rows, object)
		ids = append(ids, types.StringValue(webhook.ID))
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

	config.Webhooks = rowList
	config.IDs = idList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
