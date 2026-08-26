package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*contactResource)(nil)
	_ resource.ResourceWithConfigure      = (*contactResource)(nil)
	_ resource.ResourceWithImportState    = (*contactResource)(nil)
	_ resource.ResourceWithValidateConfig = (*contactResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*contactResource)(nil)
)

// NewContactResource is the hosttracker_contact constructor.
func NewContactResource() resource.Resource { return &contactResource{} }

type contactResource struct {
	api *client.Client
}

type contactModel struct {
	ID                   types.String  `tfsdk:"id"`
	Type                 types.String  `tfsdk:"type"`
	Address              types.String  `tfsdk:"address"`
	Name                 types.String  `tfsdk:"name"`
	AlertDelay           types.Int64   `tfsdk:"alert_delay"`
	Language             types.String  `tfsdk:"language"`
	Gateway              types.String  `tfsdk:"gateway"`
	GroupedAlerts        types.Bool    `tfsdk:"grouped_alerts"`
	BillingNotifications types.Bool    `tfsdk:"billing_notifications"`
	SendNews             types.Bool    `tfsdk:"send_news"`
	MimeType             types.String  `tfsdk:"mime_type"`
	HTTPHeaders          types.List    `tfsdk:"http_headers"`
	Templates            types.List    `tfsdk:"templates"`
	ActivePeriod         types.Object  `tfsdk:"active_period"`
	SendConfirmation     types.Bool    `tfsdk:"send_confirmation"`
	Confirmed            types.Bool    `tfsdk:"confirmed"`
	Overlimited          types.Bool    `tfsdk:"overlimited"`
	SendCost             types.Float64 `tfsdk:"send_cost"`
	BotID                types.String  `tfsdk:"bot_id"`
	Created              types.Int64   `tfsdk:"created"`
	Updated              types.Int64   `tfsdk:"updated"`
}

var activePeriodAttrTypes = map[string]attr.Type{
	"start":    types.StringType,
	"end":      types.StringType,
	"days":     types.SetType{ElemType: types.StringType},
	"timezone": types.StringType,
}

var contactHeaderAttrTypes = map[string]attr.Type{
	"name":  types.StringType,
	"value": types.StringType,
}

var contactTemplateAttrTypes = map[string]attr.Type{
	"event":   types.StringType,
	"content": types.StringType,
}

func (r *contactResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (r *contactResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A HostTracker contact: one place an alert or a report is delivered to.\n\n" +
			"A contact is a delivery address, not a subscription. What it hears about is decided by the alert " +
			"and report subscriptions that point at it.\n\n" +
			"Creating a contact whose type, address, gateway and alert delay match one the account already " +
			"holds binds to that contact rather than writing a second one, and the resource then manages it. " +
			"Every optional attribute is also computed, because the API reads an absent member as \"leave this " +
			"alone\" rather than as \"clear this\".",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The contact's id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Required: true,
				Description: "How the contact is reached: `email`, `sms` or `voiceCall`. The type cannot change " +
					"after creation: editing it replaces the contact.\n\n" +
					"The other types in the API's vocabulary are not creatable here. A messenger contact " +
					"(`telegram`, `viber`, `discord`, …) is bound by registering with the bot, `webPush` is made " +
					"from a subscription only a browser can issue, and an `http` contact is a webhook - use " +
					"`hosttracker_webhook`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"address": schema.StringAttribute{
				Required: true,
				Description: "Where the notification is delivered: an email address for `email`, a phone number " +
					"for `sms` and `voiceCall`. Changing it clears the contact's confirmation and sends a fresh " +
					"code. An email address whose domain publishes no mail host is refused rather than stored.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "A display name. Never an identifier.",
			},
			"alert_delay": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "How long a failure must persist before this contact hears about it, **in minutes**. " +
					"It is a rung on a ladder, not a free number of minutes: only the delays the contact's type " +
					"publishes are accepted, and anything else is refused with the list of allowed values. Every " +
					"type publishes `0`, `3`, `5`, `15`, `30`, `60`, `180`, `360`, `720` and `1440` today; read " +
					"the current ladder for a type from the `hosttracker_contact_types` data source " +
					"(`alert_delays`) rather than assuming this one.",
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"language": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The language notifications are rendered in. Unset, the account's own is used.",
			},
			"gateway": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Which delivery gateway carries the message, for the types that offer a choice. " +
					"Left alone, the server routes the message itself. The gateways a type publishes are in the " +
					"`hosttracker_contact_types` data source.",
			},
			"grouped_alerts": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Collapse simultaneous alerts into one message rather than sending one per " +
					"monitor.",
			},
			"billing_notifications": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Send the account's billing notices here. `email` contacts only.",
			},
			"send_news": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Send product news here. `email` contacts only.",
			},
			"mime_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "The content type a delivery is posted with. Legacy `http` contacts only; a " +
					"webhook carries its own.",
			},
			"http_headers": schema.ListNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "Extra headers to send with each delivery. Legacy `http` contacts only. Writing " +
					"this attribute replaces the whole list.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "The header name.",
						},
						"value": schema.StringAttribute{
							Optional:  true,
							Computed:  true,
							Sensitive: true,
							Description: "The header value. Marked sensitive: a header is where an integration's " +
								"credential travels.",
						},
					},
				},
			},
			"templates": schema.ListNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "Per-event message bodies. Applied to legacy `http` contacts only today. Writing " +
					"this attribute replaces the whole list.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"event": schema.StringAttribute{
							Required:    true,
							Description: "Which alert this body renders: `up`, `down` or `repeatedlyDown`.",
						},
						"content": schema.StringAttribute{
							Optional: true,
							Computed: true,
							Description: "The body, which may carry the `[[token]]` vocabulary the " +
								"`hosttracker_contact_types` data source publishes.",
						},
					},
				},
			},
			"active_period": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "The daily window during which this contact accepts delivery. Unset, it accepts " +
					"delivery every day, all day.",
				Attributes: map[string]schema.Attribute{
					"start": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Description: "When the window opens, as a clock time - `\"08:00:00\"`. Unset, it opens " +
							"at midnight.",
					},
					"end": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "When the window closes, as a clock time. Unset, it closes at the end of the day.",
					},
					"days": schema.SetAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Computed:    true,
						Description: "The weekdays the window applies on, by English name (`\"Monday\"`). Unset, " +
							"it applies every day; an empty set selects no day and stops delivery through this " +
							"contact.",
					},
					"timezone": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Description: "The zone the window's clock times are read in, spelled as the API stores " +
							"it (`\"W. Europe Standard Time\"`). A zone changed outside Terraform is not reported " +
							"as drift, because one stored zone answers to several ids.",
					},
				},
			},
			"send_confirmation": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Send the confirmation code once more, right after the contact is created. The " +
					"create already issues one for the channels that need confirming, so this is a second send " +
					"for a channel where the first can go astray - and it costs a message on the paid ones. " +
					"Changing it later does nothing: the code is entered by a person, out of band, and Terraform " +
					"never sees it. Until it is entered, alerts to this contact are not delivered.",
			},
			"confirmed": schema.BoolAttribute{
				Computed: true,
				Description: "Whether the recipient has entered the confirmation code. False right after a " +
					"create, and again after the address changes.",
			},
			"overlimited": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the account's package has stopped this contact from being used.",
			},
			"send_cost": schema.Float64Attribute{
				Computed: true,
				Description: "What one notification to this contact costs, in the units the account's message " +
					"balance is kept in. Only the paid channels have one, and it depends on the destination. A " +
					"long text message is split into several charged parts, so one alert can cost a multiple of " +
					"this.",
			},
			"bot_id": schema.StringAttribute{
				Computed:    true,
				Description: "The bot registration a messenger contact is bound to.",
			},
			"created": schema.Int64Attribute{
				Computed:    true,
				Description: "When the contact was created, in Unix seconds.",
			},
			"updated": schema.Int64Attribute{
				Computed: true,
				Description: "The contact's creation instant, in Unix seconds. The API stores no modification " +
					"time for a contact, so nothing moves this - not a rename, not a confirmation.",
			},
		},
	}
}

func (r *contactResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The contact resource was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.api = api
}

// ValidateConfig refuses the contact types that cannot be created through
// an API call, each with the way of getting one instead.
func (r *contactResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config contactModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Type.IsUnknown() || config.Type.IsNull() {
		return
	}

	if summary, detail, refused := refuseContactType(config.Type.ValueString()); refused {
		resp.Diagnostics.AddAttributeError(path.Root("type"), summary, detail)
	}
}

// refuseContactType reports the types a configuration cannot name, each
// with the way of getting one instead.
func refuseContactType(contactType string) (summary, detail string, refused bool) {
	switch contactType {
	case "http":
		return "An http contact is a webhook",
			"Use the `hosttracker_webhook` resource, which signs its deliveries, retries them and keeps a " +
				"delivery log. Creating an `http` contact here would make an unsigned legacy row that none of " +
				"that applies to.",
			true
	case "webPush":
		return "A webPush contact cannot be created from Terraform",
			"It is made from the push subscription a browser issues to a person sitting in front of it, which " +
				"no configuration can produce. Register the browser in the web app instead.",
			true
	case "telegram", "viber", "facebook", "googleChat", "discord", "skype":
		return "A messenger contact cannot be created from Terraform",
			fmt.Sprintf("A %q contact is created by the recipient registering with the bot, which is what binds "+
				"the account to the conversation. Once it exists, `terraform import` adopts it.", contactType),
			true
	}
	return "", "", false
}

// ModifyPlan marks the members the API recomputes as unknown when the
// address changes, so that the plan does not promise values the apply will
// contradict: a new address is unconfirmed, is priced on its own, and can
// be routed through a different gateway.
func (r *contactResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var plan, state, config contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || plan.Address.Equal(state.Address) {
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("confirmed"), types.BoolUnknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("send_cost"), types.Float64Unknown())...)
	if config.Gateway.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("gateway"), types.StringUnknown())...)
	}
}

func (r *contactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := encodeContact(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	contact, err := r.api.CreateContact(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("create the contact", err, contactPointerMapper)...)
		return
	}

	if plan.SendConfirmation.ValueBool() {
		if err := r.api.SendContactConfirmation(ctx, contact.ID); err != nil {
			// The contact exists either way, so this is a warning: losing
			// it from state over an undelivered code would be worse.
			resp.Diagnostics.AddWarning(
				"The confirmation code was not sent again",
				fmt.Sprintf("The contact was created. Sending its code a second time failed: %s. Send it from "+
					"the web app, or wait for the rate limit to pass.", err),
			)
		}
	}

	state, diags := contactToState(ctx, &plan, contact)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *contactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state contactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	contact, err := r.api.GetContact(ctx, state.ID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the contact", err, contactPointerMapper)...)
		return
	}

	refreshed, diags := contactToState(ctx, &state, contact)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *contactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	want, diags := encodeContact(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	have, diags := encodeContact(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The type is immutable and replaces the resource, so it is never part
	// of an update body.
	delete(want, "type")
	delete(have, "type")

	contact, err := r.api.UpdateContact(ctx, state.ID.ValueString(), client.Diff(have, want))
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			resp.Diagnostics.AddError(
				"The contact is gone",
				"It was deleted outside Terraform between the plan and the apply. Run the apply again to create it.",
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("update the contact", err, contactPointerMapper)...)
		return
	}

	updated, diags := contactToState(ctx, &plan, contact)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &updated)...)
}

func (r *contactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state contactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteContact(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the contact", err, nil)...)
	}
}

func (r *contactResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// contactPointerMapper places a problem document's JSON Pointers on the
// attributes a configuration wrote.
func contactPointerMapper(pointer string) (path.Path, bool) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return path.Empty(), false
	}
	switch parts[0] {
	case "type", "address", "name", "language", "gateway", "templates":
		return path.Root(parts[0]), true
	case "alertDelay":
		return path.Root("alert_delay"), true
	case "groupedAlerts":
		return path.Root("grouped_alerts"), true
	case "billingNotifications":
		return path.Root("billing_notifications"), true
	case "sendNews":
		return path.Root("send_news"), true
	case "mimeType":
		return path.Root("mime_type"), true
	case "httpHeaders":
		return path.Root("http_headers"), true
	case "activePeriod":
		p := path.Root("active_period")
		if len(parts) > 1 {
			switch parts[1] {
			case "start", "end", "days", "timezone":
				return p.AtName(parts[1]), true
			}
		}
		return p, true
	}
	return path.Empty(), false
}

// encodeContact turns the model into a wire body. Null members are left
// out, which on a create means "use the default" and on an update means
// "leave alone".
func encodeContact(ctx context.Context, m *contactModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{}

	putString(body, "type", m.Type)
	putString(body, "address", m.Address)
	putString(body, "name", m.Name)
	putInt(body, "alertDelay", m.AlertDelay)
	putString(body, "language", m.Language)
	putString(body, "gateway", m.Gateway)
	putBool(body, "groupedAlerts", m.GroupedAlerts)
	putBool(body, "billingNotifications", m.BillingNotifications)
	putBool(body, "sendNews", m.SendNews)
	putString(body, "mimeType", m.MimeType)

	if headers, ok := encodeObjectList(m.HTTPHeaders, encodeContactHeader); ok {
		body["httpHeaders"] = headers
	}
	if templates, ok := encodeObjectList(m.Templates, encodeContactTemplate); ok {
		body["templates"] = templates
	}

	if !m.ActivePeriod.IsNull() && !m.ActivePeriod.IsUnknown() {
		period := map[string]any{}
		attrs := m.ActivePeriod.Attributes()
		if start, ok := attrs["start"].(types.String); ok && known(start) {
			period["start"] = start.ValueString()
		}
		if end, ok := attrs["end"].(types.String); ok && known(end) {
			period["end"] = end.ValueString()
		}
		if timezone, ok := attrs["timezone"].(types.String); ok && known(timezone) {
			period["timezone"] = timezone.ValueString()
		}
		if days, ok := attrs["days"].(types.Set); ok && !days.IsNull() && !days.IsUnknown() {
			var values []string
			diags.Append(days.ElementsAs(ctx, &values, false)...)
			sort.Strings(values)
			period["days"] = values
		}
		if len(period) > 0 {
			body["activePeriod"] = period
		}
	}

	return body, diags
}

func encodeContactHeader(attrs map[string]attr.Value) map[string]any {
	entry := map[string]any{}
	if name, ok := attrs["name"].(types.String); ok && known(name) {
		entry["header"] = name.ValueString()
	}
	if value, ok := attrs["value"].(types.String); ok && known(value) {
		entry["value"] = value.ValueString()
	}
	return entry
}

func encodeContactTemplate(attrs map[string]attr.Value) map[string]any {
	entry := map[string]any{}
	if event, ok := attrs["event"].(types.String); ok && known(event) {
		entry["event"] = event.ValueString()
	}
	if content, ok := attrs["content"].(types.String); ok && known(content) {
		entry["content"] = content.ValueString()
	}
	return entry
}

// contactToState renders what the API returned as resource state, keeping
// the spellings only the configuration knows.
func contactToState(ctx context.Context, prior *contactModel, c *client.Contact) (contactModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	state := contactModel{}

	state.ID = types.StringValue(c.ID)
	state.Type = types.StringValue(c.Type)
	state.Address = stringOrNull(c.Address)
	state.Name = stringOrNull(c.Name)
	state.AlertDelay = int64OrNull(c.AlertDelay)
	state.Language = stringOrNull(c.Language)
	state.Gateway = stringOrNull(c.Gateway)
	state.GroupedAlerts = types.BoolValue(c.GroupedAlerts)
	state.BillingNotifications = boolOrNull(c.BillingNotifications)
	state.SendNews = boolOrNull(c.SendNews)
	state.MimeType = stringOrNull(c.MimeType)
	state.Confirmed = types.BoolValue(c.Confirmed)
	state.Overlimited = types.BoolValue(c.Overlimited)
	state.SendCost = float64OrNull(c.SendCost)
	state.BotID = stringOrNull(c.BotID)
	state.Created = types.Int64Value(c.Created)
	state.Updated = types.Int64Value(c.Updated)

	// A write knob, never stored and never read back.
	state.SendConfirmation = types.BoolValue(false)
	if prior != nil && known(prior.SendConfirmation) {
		state.SendConfirmation = prior.SendConfirmation
	}

	priorHeaders := types.ListNull(types.ObjectType{AttrTypes: contactHeaderAttrTypes})
	priorTemplates := types.ListNull(types.ObjectType{AttrTypes: contactTemplateAttrTypes})
	priorPeriod := types.ObjectNull(activePeriodAttrTypes)
	if prior != nil {
		if !prior.HTTPHeaders.IsUnknown() {
			priorHeaders = prior.HTTPHeaders
		}
		if !prior.Templates.IsUnknown() {
			priorTemplates = prior.Templates
		}
		if !prior.ActivePeriod.IsUnknown() {
			priorPeriod = prior.ActivePeriod
		}
	}

	headers, headerDiags := decodeContactHeaders(priorHeaders, c.HTTPHeaders)
	diags.Append(headerDiags...)
	state.HTTPHeaders = headers

	templates, templateDiags := decodeContactTemplates(priorTemplates, c.Templates)
	diags.Append(templateDiags...)
	state.Templates = templates

	period, periodDiags := decodeActivePeriod(ctx, priorPeriod, c.ActivePeriod)
	diags.Append(periodDiags...)
	state.ActivePeriod = period

	return state, diags
}

func decodeContactHeaders(prior types.List, wire *[]client.ContactHeader) (types.List, diag.Diagnostics) {
	elemType := types.ObjectType{AttrTypes: contactHeaderAttrTypes}
	if wire == nil {
		return types.ListNull(elemType), nil
	}
	var diags diag.Diagnostics
	values := make([]attr.Value, 0, len(*wire))
	for _, header := range *wire {
		object, objectDiags := types.ObjectValue(contactHeaderAttrTypes, map[string]attr.Value{
			"name":  types.StringValue(header.Header),
			"value": stringOrNull(header.Value),
		})
		diags.Append(objectDiags...)
		values = append(values, object)
	}
	list, listDiags := types.ListValue(elemType, values)
	diags.Append(listDiags...)
	return keepOrder(prior, list, encodeContactHeader), diags
}

func decodeContactTemplates(prior types.List, wire *[]client.ContactTemplate) (types.List, diag.Diagnostics) {
	elemType := types.ObjectType{AttrTypes: contactTemplateAttrTypes}
	if wire == nil {
		return types.ListNull(elemType), nil
	}
	var diags diag.Diagnostics
	values := make([]attr.Value, 0, len(*wire))
	for _, template := range *wire {
		object, objectDiags := types.ObjectValue(contactTemplateAttrTypes, map[string]attr.Value{
			"event":   types.StringValue(template.Event),
			"content": types.StringValue(template.Content),
		})
		diags.Append(objectDiags...)
		values = append(values, object)
	}
	list, listDiags := types.ListValue(elemType, values)
	diags.Append(listDiags...)
	return keepOrder(prior, list, encodeContactTemplate), diags
}

func decodeActivePeriod(ctx context.Context, prior types.Object, wire *client.ContactActivePeriod) (types.Object, diag.Diagnostics) {
	if wire == nil {
		return types.ObjectNull(activePeriodAttrTypes), nil
	}
	var diags diag.Diagnostics

	priorStart, priorEnd, priorZone := "", "", ""
	if !prior.IsNull() && !prior.IsUnknown() {
		attrs := prior.Attributes()
		if v, ok := attrs["start"].(types.String); ok && known(v) {
			priorStart = v.ValueString()
		}
		if v, ok := attrs["end"].(types.String); ok && known(v) {
			priorEnd = v.ValueString()
		}
		if v, ok := attrs["timezone"].(types.String); ok && known(v) {
			priorZone = v.ValueString()
		}
	}

	days, dayDiags := stringSet(ctx, wire.Days)
	diags.Append(dayDiags...)

	object, objectDiags := types.ObjectValue(activePeriodAttrTypes, map[string]attr.Value{
		"start":    preferredString(priorStart, wire.Start, client.PreferredClock),
		"end":      preferredString(priorEnd, wire.End, client.PreferredClock),
		"days":     days,
		"timezone": preferredString(priorZone, wire.Timezone, client.PreferredZone),
	})
	diags.Append(objectDiags...)
	return object, diags
}

// preferredString renders a member the API may re-spell, keeping what the
// configuration wrote when the two mean the same thing.
func preferredString(prior string, server *string, prefer func(string, string) string) types.String {
	if server == nil {
		return types.StringNull()
	}
	return types.StringValue(prefer(prior, *server))
}

// keepOrder holds on to the list the configuration wrote when the API's
// answer carries the same entries in another order, so that the order a
// read happens to use does not read as a change.
func keepOrder(prior, current types.List, encode objectEncoder) types.List {
	if prior.IsNull() || prior.IsUnknown() {
		return current
	}
	if sortedWire(prior, encode) == sortedWire(current, encode) {
		return prior
	}
	return current
}

// sortedWire renders a list of nested objects as its wire entries, sorted,
// so that two lists carrying the same entries compare equal whatever order
// they are in.
func sortedWire(list types.List, encode objectEncoder) string {
	entries, ok := encodeObjectList(list, encode)
	if !ok {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil {
			return ""
		}
		lines = append(lines, string(raw))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// objectEncoder turns one nested object's attributes into a wire entry.
type objectEncoder func(map[string]attr.Value) map[string]any

// encodeObjectList turns a list of nested objects into wire entries,
// reporting whether the attribute carried anything at all.
func encodeObjectList(list types.List, encode objectEncoder) ([]any, bool) {
	if list.IsNull() || list.IsUnknown() {
		return nil, false
	}
	entries := make([]any, 0, len(list.Elements()))
	for _, element := range list.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			continue
		}
		entries = append(entries, encode(object.Attributes()))
	}
	return entries, true
}

func known(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}

func boolOrNull(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}
