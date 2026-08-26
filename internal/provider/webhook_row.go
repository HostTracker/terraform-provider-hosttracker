package provider

import (
	"context"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// webhookRow is one webhook as a data source publishes it. The signing
// secret is deliberately absent: the API publishes its value only in the
// answer that mints it, so a read can never recover one.
type webhookRow struct {
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
}

var webhookRowAttrTypes = map[string]attr.Type{
	"id":                   types.StringType,
	"url":                  types.StringType,
	"name":                 types.StringType,
	"events":               types.SetType{ElemType: types.StringType},
	"scope":                types.ObjectType{AttrTypes: webhookScopeAttrTypes},
	"monitor_count":        types.Int64Type,
	"resolved_monitor_ids": types.SetType{ElemType: types.StringType},
	"headers":              types.SetType{ElemType: webhookHeaderObjectType},
	"enabled":              types.BoolType,
	"disabled_reason":      types.StringType,
	"consecutive_failures": types.Int64Type,
	"last_delivery_at":     types.Int64Type,
	"secret_set":           types.BoolType,
	"secret_updated_at":    types.Int64Type,
	"created":              types.Int64Type,
	"updated":              types.Int64Type,
}

func webhookRowAttributes() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"id": dschema.StringAttribute{
			Computed:    true,
			Description: "The webhook's id, which is also the `HT-Webhook` header every delivery carries.",
		},
		"url": dschema.StringAttribute{
			Computed:    true,
			Description: "Where deliveries are POSTed.",
		},
		"name": dschema.StringAttribute{
			Computed:    true,
			Description: "The display name.",
		},
		"events": dschema.SetAttribute{
			ElementType: types.StringType,
			Computed:    true,
			Description: "Which event types this webhook receives.",
		},
		"scope": dschema.SingleNestedAttribute{
			Computed:    true,
			Description: "Which monitors this webhook receives events for - exactly one of the three forms.",
			Attributes: map[string]dschema.Attribute{
				"all": dschema.BoolAttribute{
					Computed:    true,
					Description: "Set when the webhook covers every monitor on the account.",
				},
				"monitor_ids": dschema.SetAttribute{
					ElementType: types.StringType,
					Computed:    true,
					Description: "The explicit monitor list, when the scope is one.",
				},
				"tags": dschema.SetAttribute{
					ElementType: types.StringType,
					Computed:    true,
					Description: "The tags matched, when the scope is a tag one.",
				},
			},
		},
		"monitor_count": dschema.Int64Attribute{
			Computed:    true,
			Description: "How many monitors the scope resolved to, including zero.",
		},
		"resolved_monitor_ids": dschema.SetAttribute{
			ElementType: types.StringType,
			Computed:    true,
			Description: "Which monitors a `tags` scope resolved to.",
		},
		"headers": dschema.SetNestedAttribute{
			Computed:    true,
			Description: "The custom request headers every delivery carries.",
			NestedObject: dschema.NestedAttributeObject{
				Attributes: map[string]dschema.Attribute{
					"header": dschema.StringAttribute{Computed: true, Description: "The header name."},
					"value":  dschema.StringAttribute{Computed: true, Sensitive: true, Description: "The header value."},
				},
			},
		},
		"enabled": dschema.BoolAttribute{
			Computed:    true,
			Description: "Whether deliveries are attempted.",
		},
		"disabled_reason": dschema.StringAttribute{
			Computed: true,
			Description: "Why the system switched this webhook off: `deliveryFailure` or `gone`. Null when the " +
				"account switched it off, and when it is enabled.",
		},
		"consecutive_failures": dschema.Int64Attribute{
			Computed:    true,
			Description: "How many deliveries have failed in a row.",
		},
		"last_delivery_at": dschema.Int64Attribute{
			Computed:    true,
			Description: "When a delivery was last attempted, in Unix seconds.",
		},
		"secret_set": dschema.BoolAttribute{
			Computed:    true,
			Description: "Whether the webhook has a signing secret. The value itself is never readable.",
		},
		"secret_updated_at": dschema.Int64Attribute{
			Computed:    true,
			Description: "When the current secret was minted, in Unix seconds.",
		},
		"created": dschema.Int64Attribute{
			Computed:    true,
			Description: "When the webhook was created, in Unix seconds.",
		},
		"updated": dschema.Int64Attribute{
			Computed:    true,
			Description: "When the webhook was last edited, in Unix seconds.",
		},
	}
}

func webhookToRow(ctx context.Context, w client.Webhook) (webhookRow, diag.Diagnostics) {
	var diags diag.Diagnostics
	row := webhookRow{
		ID:                  types.StringValue(w.ID),
		URL:                 types.StringValue(w.URL),
		Name:                stringOrNull(w.Name),
		Enabled:             types.BoolValue(w.Enabled),
		DisabledReason:      stringOrNull(w.DisabledReason),
		ConsecutiveFailures: types.Int64Value(w.ConsecutiveFailures),
		LastDeliveryAt:      int64OrNull(w.LastDeliveryAt),
		Created:             types.Int64Value(w.Created),
		Updated:             types.Int64Value(w.Updated),
		SecretSet:           types.BoolValue(false),
		SecretUpdatedAt:     types.Int64Null(),
	}

	events, eventDiags := stringSet(ctx, w.Events)
	diags.Append(eventDiags...)
	row.Events = events

	if w.Scope == nil {
		row.Scope = types.ObjectNull(webhookScopeAttrTypes)
		row.MonitorCount = types.Int64Null()
		row.ResolvedMonitorIDs = types.SetNull(types.StringType)
	} else {
		monitorIDs, idDiags := stringSet(ctx, w.Scope.MonitorIDs)
		diags.Append(idDiags...)
		tags, tagDiags := stringSet(ctx, w.Scope.Tags)
		diags.Append(tagDiags...)
		all := types.BoolNull()
		if w.Scope.All != nil {
			all = types.BoolValue(*w.Scope.All)
		}
		object, objectDiags := types.ObjectValue(webhookScopeAttrTypes, map[string]attr.Value{
			"all":         all,
			"monitor_ids": monitorIDs,
			"tags":        tags,
		})
		diags.Append(objectDiags...)
		row.Scope = object
		row.MonitorCount = types.Int64Value(w.Scope.MonitorCount)
		resolved, resolvedDiags := stringSet(ctx, w.Scope.ResolvedMonitorIDs)
		diags.Append(resolvedDiags...)
		row.ResolvedMonitorIDs = resolved
	}

	headers, headerDiags := webhookHeadersToState(ctx, nil, w.Headers)
	diags.Append(headerDiags...)
	row.Headers = headers

	if w.Secret != nil {
		row.SecretSet = types.BoolValue(w.Secret.Set)
		row.SecretUpdatedAt = int64OrNull(w.Secret.UpdatedAt)
	}

	return row, diags
}
