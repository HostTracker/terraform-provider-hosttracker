package provider

import (
	"context"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*accountDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*accountDataSource)(nil)
)

// NewAccountDataSource is the hosttracker_account constructor.
func NewAccountDataSource() datasource.DataSource { return &accountDataSource{} }

type accountDataSource struct {
	api *client.Client
}

type accountModel struct {
	ID         types.String `tfsdk:"id"`
	Login      types.String `tfsdk:"login"`
	Timezone   types.String `tfsdk:"timezone"`
	Language   types.String `tfsdk:"language"`
	Package    types.Object `tfsdk:"package"`
	Limits     types.Object `tfsdk:"limits"`
	Overlimits types.List   `tfsdk:"overlimits"`
	Quota      types.Object `tfsdk:"quota"`
}

var packageAttrTypes = map[string]attr.Type{
	"id":   types.StringType,
	"name": types.StringType,
}

var limitsAttrTypes = map[string]attr.Type{
	"intervals":           types.ListType{ElemType: types.Int64Type},
	"alert_delays":        types.ListType{ElemType: types.Int64Type},
	"max_bulk_items":      types.Int64Type,
	"max_bulk_selection":  types.Int64Type,
	"max_limit":           types.Int64Type,
	"default_limit":       types.Int64Type,
	"max_inline_contacts": types.Int64Type,
}

var quotaAttrTypes = map[string]attr.Type{
	"limit":       types.Int64Type,
	"used":        types.Int64Type,
	"remaining":   types.Int64Type,
	"reset_at":    types.Int64Type,
	"token_cap":   types.Int64Type,
	"api_enabled": types.BoolType,
	"scopes":      types.ListType{ElemType: types.StringType},
}

func (d *accountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (d *accountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "The account the configured token belongs to: its package, its limits, and what is left of " +
			"its API quota. Reading it needs a token carrying the `account:read` scope.",
		Attributes: map[string]dschema.Attribute{
			"id":       dschema.StringAttribute{Computed: true, Description: "The account id."},
			"login":    dschema.StringAttribute{Computed: true, Description: "The account's login, which is its email address."},
			"timezone": dschema.StringAttribute{Computed: true, Description: "The account's time zone, as an IANA id."},
			"language": dschema.StringAttribute{Computed: true, Description: "The account's interface language."},
			"package": dschema.SingleNestedAttribute{
				Computed:    true,
				Description: "The account's package.",
				Attributes: map[string]dschema.Attribute{
					"id":   dschema.StringAttribute{Computed: true, Description: "The package id."},
					"name": dschema.StringAttribute{Computed: true, Description: "The package name."},
				},
			},
			"limits": dschema.SingleNestedAttribute{
				Computed:    true,
				Description: "The bounds the account's writes and reads are held to.",
				Attributes: map[string]dschema.Attribute{
					"intervals":           dschema.ListAttribute{ElementType: types.Int64Type, Computed: true, Description: "The check intervals this package sells, in seconds."},
					"alert_delays":        dschema.ListAttribute{ElementType: types.Int64Type, Computed: true, Description: "The alert-delay ladder, in minutes."},
					"max_bulk_items":      dschema.Int64Attribute{Computed: true, Description: "Items one bulk create may carry."},
					"max_bulk_selection":  dschema.Int64Attribute{Computed: true, Description: "Monitors one bulk update or delete may target."},
					"max_limit":           dschema.Int64Attribute{Computed: true, Description: "The largest page size any collection read accepts."},
					"default_limit":       dschema.Int64Attribute{Computed: true, Description: "The page size a collection read uses when it names none."},
					"max_inline_contacts": dschema.Int64Attribute{Computed: true, Description: "Inline contacts a single monitor create may declare."},
				},
			},
			"overlimits": dschema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Which account-level overlimit bits are set: `task`, `contact`, `report`, `maintenance`. Empty when the account is within its package.",
			},
			"quota": dschema.SingleNestedAttribute{
				Computed:    true,
				Description: "What is left of the API quota for the configured token.",
				Attributes: map[string]dschema.Attribute{
					"limit":       dschema.Int64Attribute{Computed: true, Description: "The tightest-binding window's allowance, or null when nothing is metered."},
					"used":        dschema.Int64Attribute{Computed: true, Description: "Calls spent in the current window."},
					"remaining":   dschema.Int64Attribute{Computed: true, Description: "Calls left in the current window."},
					"reset_at":    dschema.Int64Attribute{Computed: true, Description: "When the window resets, in Unix seconds."},
					"token_cap":   dschema.Int64Attribute{Computed: true, Description: "The calling token's own self-cap, when it was minted with one."},
					"api_enabled": dschema.BoolAttribute{Computed: true, Description: "Whether the account is entitled to use the API at all."},
					"scopes":      dschema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "The scope vocabulary a token may be minted with."},
				},
			},
		},
	}
}

func (d *accountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("The account data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.api = api
}

func (d *accountDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	account, err := d.api.API().GetAccountWithResponse(ctx, nil)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("read the account", err, nil)...)
		return
	}
	if account.JSON200 == nil {
		resp.Diagnostics.AddError("The account read answered nothing", "The API returned no account body.")
		return
	}
	view := account.JSON200

	state := accountModel{
		ID:       types.StringValue(view.Id.String()),
		Login:    types.StringPointerValue(view.Login),
		Timezone: types.StringPointerValue(view.Timezone),
		Language: types.StringPointerValue(view.Language),
	}

	state.Package = types.ObjectNull(packageAttrTypes)
	if view.Package != nil {
		object, diags := types.ObjectValue(packageAttrTypes, map[string]attr.Value{
			"id":   types.StringValue(view.Package.Id.String()),
			"name": types.StringPointerValue(view.Package.Name),
		})
		resp.Diagnostics.Append(diags...)
		state.Package = object
	}

	state.Limits = types.ObjectNull(limitsAttrTypes)
	if view.Limits != nil {
		intervals, intervalDiags := int32List(ctx, view.Limits.Intervals)
		resp.Diagnostics.Append(intervalDiags...)
		delays, delayDiags := int32List(ctx, view.Limits.AlertDelays)
		resp.Diagnostics.Append(delayDiags...)
		object, diags := types.ObjectValue(limitsAttrTypes, map[string]attr.Value{
			"intervals":           intervals,
			"alert_delays":        delays,
			"max_bulk_items":      types.Int64Value(int64(view.Limits.MaxBulkItems)),
			"max_bulk_selection":  types.Int64Value(int64(view.Limits.MaxBulkSelection)),
			"max_limit":           types.Int64Value(int64(view.Limits.MaxLimit)),
			"default_limit":       types.Int64Value(int64(view.Limits.DefaultLimit)),
			"max_inline_contacts": types.Int64Value(int64(view.Limits.MaxInlineContacts)),
		})
		resp.Diagnostics.Append(diags...)
		state.Limits = object
	}

	overlimits, overlimitDiags := stringList(ctx, view.Overlimits)
	resp.Diagnostics.Append(overlimitDiags...)
	state.Overlimits = overlimits

	// The quota lives behind its own read, which a token without the
	// account scope cannot make. A refusal there leaves the block null
	// rather than failing the whole data source.
	state.Quota = types.ObjectNull(quotaAttrTypes)
	quota, quotaErr := d.api.API().GetAccountQuotaWithResponse(ctx, nil)
	switch {
	case quotaErr != nil && hosttracker.IsCode(quotaErr, hosttracker.CodeMissingScope):
		// Left null on purpose.
	case quotaErr != nil:
		resp.Diagnostics.Append(client.Diagnose("read the account quota", quotaErr, nil)...)
		return
	case quota.JSON200 != nil:
		scopes := make([]string, 0)
		if quota.JSON200.Scopes != nil {
			for _, s := range *quota.JSON200.Scopes {
				if s.Scope != nil {
					scopes = append(scopes, *s.Scope)
				}
			}
		}
		scopeList, scopeDiags := stringList(ctx, &scopes)
		resp.Diagnostics.Append(scopeDiags...)
		object, diags := types.ObjectValue(quotaAttrTypes, map[string]attr.Value{
			"limit":       int32PointerValue(quota.JSON200.Limit),
			"used":        int32PointerValue(quota.JSON200.Used),
			"remaining":   int32PointerValue(quota.JSON200.Remaining),
			"reset_at":    types.Int64PointerValue(quota.JSON200.ResetAt),
			"token_cap":   int32PointerValue(quota.JSON200.TokenCap),
			"api_enabled": types.BoolValue(quota.JSON200.ApiEnabled),
			"scopes":      scopeList,
		})
		resp.Diagnostics.Append(diags...)
		state.Quota = object
	}
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func stringList(ctx context.Context, values *[]string) (types.List, diag.Diagnostics) {
	if values == nil {
		return types.ListValueFrom(ctx, types.StringType, []string{})
	}
	return types.ListValueFrom(ctx, types.StringType, *values)
}

func int32List(ctx context.Context, values *[]int32) (types.List, diag.Diagnostics) {
	if values == nil {
		return types.ListValueFrom(ctx, types.Int64Type, []int64{})
	}
	out := make([]int64, 0, len(*values))
	for _, v := range *values {
		out = append(out, int64(v))
	}
	return types.ListValueFrom(ctx, types.Int64Type, out)
}

func int32PointerValue(v *int32) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}
