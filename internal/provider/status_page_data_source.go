package provider

import (
	"context"
	"fmt"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// statusPageRow is what a listing publishes: the page's identity and its
// counters. Settings and components belong to the item read, so a listing
// stays cheap.
type statusPageRow struct {
	ID                  types.String `tfsdk:"id"`
	Slug                types.String `tfsdk:"slug"`
	Title               types.String `tfsdk:"title"`
	PublicURL           types.String `tfsdk:"public_url"`
	ComponentCount      types.Int64  `tfsdk:"component_count"`
	UnresolvedIncidents types.Int64  `tfsdk:"unresolved_incidents"`
	HasPassword         types.Bool   `tfsdk:"has_password"`
	Created             types.Int64  `tfsdk:"created"`
}

var statusPageRowAttrTypes = map[string]attr.Type{
	"id":                   types.StringType,
	"slug":                 types.StringType,
	"title":                types.StringType,
	"public_url":           types.StringType,
	"component_count":      types.Int64Type,
	"unresolved_incidents": types.Int64Type,
	"has_password":         types.BoolType,
	"created":              types.Int64Type,
}

func statusPageRowAttributes() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"id": dschema.StringAttribute{
			Computed:    true,
			Description: "The page's id.",
		},
		"slug": dschema.StringAttribute{
			Computed:    true,
			Description: "The page's permanent public address.",
		},
		"title": dschema.StringAttribute{
			Computed:    true,
			Description: "The heading the public page carries.",
		},
		"public_url": dschema.StringAttribute{
			Computed:    true,
			Description: "Where the page is served from: `https://" + client.StatusPageHost + "/<slug>`.",
		},
		"component_count": dschema.Int64Attribute{
			Computed:    true,
			Description: "How many components the page publishes.",
		},
		"unresolved_incidents": dschema.Int64Attribute{
			Computed:    true,
			Description: "How many of the page's declared incidents are still open.",
		},
		"has_password": dschema.BoolAttribute{
			Computed:    true,
			Description: "Whether the page is behind a password.",
		},
		"created": dschema.Int64Attribute{
			Computed:    true,
			Description: "When the page was created, in Unix seconds.",
		},
	}
}

func statusPageToRow(p client.StatusPage) statusPageRow {
	return statusPageRow{
		ID:                  types.StringValue(p.ID),
		Slug:                types.StringValue(p.Slug),
		Title:               types.StringValue(p.Title),
		PublicURL:           types.StringValue(p.PublicURL()),
		ComponentCount:      types.Int64Value(p.ComponentCount),
		UnresolvedIncidents: types.Int64Value(p.UnresolvedIncidents),
		HasPassword:         types.BoolValue(p.HasPassword),
		Created:             types.Int64Value(p.Created),
	}
}

// ---------------------------------------------------------------------
// hosttracker_status_page
// ---------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*statusPageDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*statusPageDataSource)(nil)
)

// NewStatusPageDataSource is the hosttracker_status_page data source
// constructor.
func NewStatusPageDataSource() datasource.DataSource { return &statusPageDataSource{} }

type statusPageDataSource struct {
	api *client.Client
}

type statusPageLookupModel struct {
	ID                  types.String `tfsdk:"id"`
	Slug                types.String `tfsdk:"slug"`
	Title               types.String `tfsdk:"title"`
	PublicURL           types.String `tfsdk:"public_url"`
	CustomDomain        types.String `tfsdk:"custom_domain"`
	ComponentCount      types.Int64  `tfsdk:"component_count"`
	UnresolvedIncidents types.Int64  `tfsdk:"unresolved_incidents"`
	HasPassword         types.Bool   `tfsdk:"has_password"`
	Created             types.Int64  `tfsdk:"created"`
	Settings            types.Object `tfsdk:"settings"`
	Components          types.List   `tfsdk:"components"`
}

func (d *statusPageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (d *statusPageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := statusPageRowAttributes()
	attributes["id"] = dschema.StringAttribute{
		Optional:    true,
		Computed:    true,
		Description: "The page's id. Name it, or name the `slug` instead.",
	}
	attributes["slug"] = dschema.StringAttribute{
		Optional:    true,
		Computed:    true,
		Description: "The page's permanent public address. Name it, or name the `id` instead.",
	}
	attributes["custom_domain"] = dschema.StringAttribute{
		Computed:    true,
		Description: "The custom domain as configured, when there is one.",
	}
	attributes["settings"] = statusPageSettingsDataSourceSchema()
	attributes["components"] = dschema.ListNestedAttribute{
		Computed:     true,
		Description:  "The components the page publishes, in display order.",
		NestedObject: dschema.NestedAttributeObject{Attributes: statusPageComponentRowAttributes()},
	}

	resp.Schema = dschema.Schema{
		Description: "Reads one status page with its settings and component set, by id or by slug.",
		Attributes:  attributes,
	}
}

func statusPageComponentRowAttributes() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"monitor_id": dschema.StringAttribute{
			Computed:    true,
			Description: "The monitor whose state this component shows. Null on a third-party component.",
		},
		"third_party": dschema.BoolAttribute{
			Computed:    true,
			Description: "True for a component this account does not monitor.",
		},
		"name": dschema.StringAttribute{
			Computed:    true,
			Description: "The label shown on the page.",
		},
		"group": dschema.StringAttribute{
			Computed:    true,
			Description: "The heading this component is listed under.",
		},
		"manual_state": dschema.StringAttribute{
			Computed:    true,
			Description: "The state pinned on a third-party component: `operational`, `degraded` or `down`.",
		},
	}
}

func (d *statusPageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The status page data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *statusPageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config statusPageLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	byID := !config.ID.IsNull() && config.ID.ValueString() != ""
	bySlug := !config.Slug.IsNull() && config.Slug.ValueString() != ""
	if byID == bySlug {
		resp.Diagnostics.AddError(
			"Name exactly one way of finding the page",
			"Set either `id` or `slug`, and only one.",
		)
		return
	}

	id := config.ID.ValueString()
	if bySlug {
		slug := config.Slug.ValueString()
		pages, err := d.api.ListStatusPages(ctx, client.StatusPageFilter{Slug: slug})
		if err != nil {
			resp.Diagnostics.Append(client.Diagnose("look the status page up", err, nil)...)
			return
		}
		if len(pages) == 0 {
			resp.Diagnostics.AddAttributeError(path.Root("slug"), "No such status page",
				fmt.Sprintf("No page is published at %q on this account. A slug is unique across the product, so "+
					"one that exists but belongs elsewhere reads the same way.", slug))
			return
		}
		// The slug is unique, so a listing can match at most one page.
		id = pages[0].ID
	}

	// The listing carries no settings and no components: read the page.
	page, err := d.api.GetStatusPage(ctx, id)
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.Diagnostics.AddAttributeError(path.Root("id"), "No such status page",
				fmt.Sprintf("No status page with id %s belongs to this account.", id))
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the status page", err, nil)...)
		return
	}

	settings, diags := decodeStatusPageSettings(ctx, page.Settings)
	resp.Diagnostics.Append(diags...)
	components, componentDiags := componentsToState(ctx, types.ListNull(statusPageComponentObjectType), page.Components)
	resp.Diagnostics.Append(componentDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	row := statusPageToRow(*page)
	state := statusPageLookupModel{
		ID:                  row.ID,
		Slug:                row.Slug,
		Title:               row.Title,
		PublicURL:           row.PublicURL,
		CustomDomain:        stringOrNull(page.CustomDomain),
		ComponentCount:      row.ComponentCount,
		UnresolvedIncidents: row.UnresolvedIncidents,
		HasPassword:         row.HasPassword,
		Created:             row.Created,
		Settings:            settings,
		Components:          components,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------------------------------------------------------------------
// hosttracker_status_pages
// ---------------------------------------------------------------------

var (
	_ datasource.DataSource              = (*statusPagesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*statusPagesDataSource)(nil)
)

// NewStatusPagesDataSource is the hosttracker_status_pages data source
// constructor.
func NewStatusPagesDataSource() datasource.DataSource { return &statusPagesDataSource{} }

type statusPagesDataSource struct {
	api *client.Client
}

type statusPagesModel struct {
	MaxResults  types.Int64 `tfsdk:"max_results"`
	IDs         types.List  `tfsdk:"ids"`
	StatusPages types.List  `tfsdk:"status_pages"`
}

func (d *statusPagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_pages"
}

func (d *statusPagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Reads the account's status pages. A listing carries each page's identity and counters; read " +
			"one page with the `hosttracker_status_page` data source for its settings and components.",
		Attributes: map[string]dschema.Attribute{
			"max_results": dschema.Int64Attribute{
				Optional: true,
				Description: fmt.Sprintf("How many pages to collect across the cursor. Defaults to %d.",
					client.DefaultListCap),
			},
			"ids": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "The pages' ids, in the order the API returned them.",
			},
			"status_pages": dschema.ListNestedAttribute{
				Computed:     true,
				Description:  "The account's status pages.",
				NestedObject: dschema.NestedAttributeObject{Attributes: statusPageRowAttributes()},
			},
		},
	}
}

func (d *statusPagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The status pages data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *statusPagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config statusPagesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := client.StatusPageFilter{}
	if !config.MaxResults.IsNull() {
		filter.Limit = int(config.MaxResults.ValueInt64())
	}

	pages, err := d.api.ListStatusPages(ctx, filter)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("list the status pages", err, nil)...)
		return
	}

	rowType := types.ObjectType{AttrTypes: statusPageRowAttrTypes}
	rows := make([]attr.Value, 0, len(pages))
	ids := make([]attr.Value, 0, len(pages))
	var diags diag.Diagnostics
	for _, page := range pages {
		object, objectDiags := types.ObjectValueFrom(ctx, statusPageRowAttrTypes, statusPageToRow(page))
		diags.Append(objectDiags...)
		rows = append(rows, object)
		ids = append(ids, types.StringValue(page.ID))
	}
	resp.Diagnostics.Append(diags...)
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

	config.StatusPages = rowList
	config.IDs = idList
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
