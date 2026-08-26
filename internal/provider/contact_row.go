package provider

import (
	"context"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// contactRow is what both contact data sources publish.
type contactRow struct {
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
}

var contactRowAttrTypes = map[string]attr.Type{
	"id":                    types.StringType,
	"type":                  types.StringType,
	"name":                  types.StringType,
	"address":               types.StringType,
	"confirmed":             types.BoolType,
	"overlimited":           types.BoolType,
	"alert_delay":           types.Int64Type,
	"send_cost":             types.Float64Type,
	"gateway":               types.StringType,
	"language":              types.StringType,
	"grouped_alerts":        types.BoolType,
	"billing_notifications": types.BoolType,
	"send_news":             types.BoolType,
	"mime_type":             types.StringType,
	"http_headers":          types.ListType{ElemType: types.ObjectType{AttrTypes: contactHeaderAttrTypes}},
	"templates":             types.ListType{ElemType: types.ObjectType{AttrTypes: contactTemplateAttrTypes}},
	"active_period":         types.ObjectType{AttrTypes: activePeriodAttrTypes},
	"bot_id":                types.StringType,
	"created":               types.Int64Type,
	"updated":               types.Int64Type,
}

// contactRowAttributes is the read-only schema shared by both contact data
// sources.
func contactRowAttributes() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"id":          dschema.StringAttribute{Computed: true, Description: "The contact's id."},
		"type":        dschema.StringAttribute{Computed: true, Description: "How the contact is reached."},
		"name":        dschema.StringAttribute{Computed: true, Description: "The display name."},
		"address":     dschema.StringAttribute{Computed: true, Description: "Where notifications are delivered. Absent for a contact that has none, and for a token that may not read it."},
		"confirmed":   dschema.BoolAttribute{Computed: true, Description: "Whether the recipient has entered the confirmation code. Alerts to an unconfirmed contact are not delivered."},
		"overlimited": dschema.BoolAttribute{Computed: true, Description: "Whether the account's package has stopped this contact from being used."},
		"alert_delay": dschema.Int64Attribute{Computed: true, Description: "How long a failure must persist before this contact hears about it, in minutes."},
		"send_cost":   dschema.Float64Attribute{Computed: true, Description: "What one notification to this contact costs. Only the paid channels have one."},
		"gateway":     dschema.StringAttribute{Computed: true, Description: "The gateway carrying the message."},
		"language":    dschema.StringAttribute{Computed: true, Description: "The language notifications are rendered in."},
		"grouped_alerts": dschema.BoolAttribute{
			Computed:    true,
			Description: "Whether simultaneous alerts are collapsed into one message.",
		},
		"billing_notifications": dschema.BoolAttribute{Computed: true, Description: "Whether the account's billing notices go here."},
		"send_news":             dschema.BoolAttribute{Computed: true, Description: "Whether product news goes here."},
		"mime_type":             dschema.StringAttribute{Computed: true, Description: "The content type a legacy `http` contact is posted with."},
		"http_headers": dschema.ListNestedAttribute{
			Computed:    true,
			Description: "The headers a legacy `http` contact sends with each delivery.",
			NestedObject: dschema.NestedAttributeObject{
				Attributes: map[string]dschema.Attribute{
					"name":  dschema.StringAttribute{Computed: true, Description: "The header name."},
					"value": dschema.StringAttribute{Computed: true, Sensitive: true, Description: "The header value."},
				},
			},
		},
		"templates": dschema.ListNestedAttribute{
			Computed: true,
			Description: "The custom per-event bodies the contact posts. Read by the single-contact data " +
				"source only: a listing does not carry them.",
			NestedObject: dschema.NestedAttributeObject{
				Attributes: map[string]dschema.Attribute{
					"event":   dschema.StringAttribute{Computed: true, Description: "Which alert the body renders."},
					"content": dschema.StringAttribute{Computed: true, Description: "The body."},
				},
			},
		},
		"active_period": dschema.SingleNestedAttribute{
			Computed:    true,
			Description: "The daily window during which the contact accepts delivery.",
			Attributes: map[string]dschema.Attribute{
				"start":    dschema.StringAttribute{Computed: true, Description: "When the window opens, as a clock time."},
				"end":      dschema.StringAttribute{Computed: true, Description: "When it closes."},
				"days":     dschema.SetAttribute{ElementType: types.StringType, Computed: true, Description: "The weekdays it applies on."},
				"timezone": dschema.StringAttribute{Computed: true, Description: "The zone its clock times are read in."},
			},
		},
		"bot_id":  dschema.StringAttribute{Computed: true, Description: "The bot registration a messenger contact is bound to."},
		"created": dschema.Int64Attribute{Computed: true, Description: "When the contact was created, in Unix seconds."},
		"updated": dschema.Int64Attribute{Computed: true, Description: "The contact's creation instant, in Unix seconds: no edit moves it."},
	}
}

func contactToRow(ctx context.Context, c client.Contact) (contactRow, diag.Diagnostics) {
	var diags diag.Diagnostics
	row := contactRow{
		ID:                   types.StringValue(c.ID),
		Type:                 types.StringValue(c.Type),
		Name:                 stringOrNull(c.Name),
		Address:              stringOrNull(c.Address),
		Confirmed:            types.BoolValue(c.Confirmed),
		Overlimited:          types.BoolValue(c.Overlimited),
		AlertDelay:           int64OrNull(c.AlertDelay),
		SendCost:             float64OrNull(c.SendCost),
		Gateway:              stringOrNull(c.Gateway),
		Language:             stringOrNull(c.Language),
		GroupedAlerts:        types.BoolValue(c.GroupedAlerts),
		BillingNotifications: boolOrNull(c.BillingNotifications),
		SendNews:             boolOrNull(c.SendNews),
		MimeType:             stringOrNull(c.MimeType),
		BotID:                stringOrNull(c.BotID),
		Created:              types.Int64Value(c.Created),
		Updated:              types.Int64Value(c.Updated),
	}

	headers, headerDiags := decodeContactHeaders(types.ListNull(types.ObjectType{AttrTypes: contactHeaderAttrTypes}), c.HTTPHeaders)
	diags.Append(headerDiags...)
	row.HTTPHeaders = headers

	templates, templateDiags := decodeContactTemplates(types.ListNull(types.ObjectType{AttrTypes: contactTemplateAttrTypes}), c.Templates)
	diags.Append(templateDiags...)
	row.Templates = templates

	period, periodDiags := decodeActivePeriod(ctx, types.ObjectNull(activePeriodAttrTypes), c.ActivePeriod)
	diags.Append(periodDiags...)
	row.ActivePeriod = period

	return row, diags
}
