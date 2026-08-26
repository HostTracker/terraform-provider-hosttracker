package provider

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	_ resource.Resource                   = (*webhookResource)(nil)
	_ resource.ResourceWithConfigure      = (*webhookResource)(nil)
	_ resource.ResourceWithImportState    = (*webhookResource)(nil)
	_ resource.ResourceWithValidateConfig = (*webhookResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*webhookResource)(nil)
)

// NewWebhookResource is the hosttracker_webhook constructor.
func NewWebhookResource() resource.Resource { return &webhookResource{} }

type webhookResource struct {
	api *client.Client
}

type webhookModel struct {
	ID                       types.String `tfsdk:"id"`
	URL                      types.String `tfsdk:"url"`
	Name                     types.String `tfsdk:"name"`
	Events                   types.Set    `tfsdk:"events"`
	Scope                    types.Object `tfsdk:"scope"`
	Headers                  types.Set    `tfsdk:"headers"`
	Enabled                  types.Bool   `tfsdk:"enabled"`
	Secret                   types.String `tfsdk:"secret"`
	RotateSecret             types.Int64  `tfsdk:"rotate_secret"`
	SecretSet                types.Bool   `tfsdk:"secret_set"`
	SecretUpdatedAt          types.Int64  `tfsdk:"secret_updated_at"`
	SecretPreviousValidUntil types.Int64  `tfsdk:"secret_previous_valid_until"`
	MonitorCount             types.Int64  `tfsdk:"monitor_count"`
	ResolvedMonitorIDs       types.Set    `tfsdk:"resolved_monitor_ids"`
	DisabledReason           types.String `tfsdk:"disabled_reason"`
	ConsecutiveFailures      types.Int64  `tfsdk:"consecutive_failures"`
	LastDeliveryAt           types.Int64  `tfsdk:"last_delivery_at"`
	Created                  types.Int64  `tfsdk:"created"`
	Updated                  types.Int64  `tfsdk:"updated"`
}

// webhookHeaderModel is one custom request header. The API spells the pair
// `{header, value}`, the same words the contact door uses.
type webhookHeaderModel struct {
	Header types.String `tfsdk:"header"`
	Value  types.String `tfsdk:"value"`
}

var webhookScopeAttrTypes = map[string]attr.Type{
	"all":         types.BoolType,
	"monitor_ids": types.SetType{ElemType: types.StringType},
	"tags":        types.SetType{ElemType: types.StringType},
}

var webhookHeaderAttrTypes = map[string]attr.Type{
	"header": types.StringType,
	"value":  types.StringType,
}

var webhookHeaderObjectType = types.ObjectType{AttrTypes: webhookHeaderAttrTypes}

// httpsURLPattern is what a delivery address has to look like. The API
// accepts any absolute http(s) url; the provider insists on https, because
// a webhook body carries monitoring detail about the account and travels
// across the internet on every alert.
var httpsURLPattern = regexp.MustCompile(`^https://[^\s/@]+(/\S*)?$`)

// knownWebhookEvents is what the vocabulary holds today. It is open, so a
// value newer than this provider is accepted: the list is documentation,
// not a validator.
var knownWebhookEvents = []string{
	"monitor.down", "monitor.up", "monitor.repeatedlyDown",
	"incident.opened", "incident.closed",
	"monitor.created", "monitor.updated", "monitor.deleted",
	"maintenance.ended", "certificate.expiring", "domain.expiring",
	"contact.confirmed", "contact.updated",
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A webhook endpoint: HostTracker POSTs a signed JSON envelope to your url whenever one of " +
			"the events you subscribe to happens.\n\n" +
			"The signing secret is minted by the server and published exactly once, in the answer to the call " +
			"that set it. The provider captures it into state; a later read cannot recover it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The webhook's id. It is also the `HT-Webhook` header every delivery carries.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				Required: true,
				Description: "Where deliveries are POSTed. An absolute `https://` url, not a bare host. Any 2xx " +
					"counts as delivered; `410 Gone` disables the webhook immediately; `408`, `429` and `5xx` are " +
					"retried. After 20 consecutive failures the webhook disables itself.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(httpsURLPattern,
						"must be an absolute https url, for example https://hooks.example.com/host-tracker"),
				},
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "A display name. Never an identifier.",
			},
			"events": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Which event types this webhook receives: " + backquotedList(knownWebhookEvents) +
					". There is no wildcard, and an empty set is refused - a webhook subscribed to nothing is a " +
					"webhook that silently never fires. The vocabulary is open, so an event newer than this " +
					"provider is accepted too.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"scope": schema.SingleNestedAttribute{
				Required: true,
				Description: "Which monitors this webhook receives events for. Set exactly one of the three " +
					"members: a scope naming two would be ambiguous about which one wins, so it is refused rather " +
					"than resolved.",
				Attributes: map[string]schema.Attribute{
					"all": schema.BoolAttribute{
						Optional: true,
						Description: "Every monitor on the account, including ones created later. Must be `true` " +
							"when it is set; narrow with `monitor_ids` or `tags` instead of writing `false`.",
					},
					"monitor_ids": schema.SetAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Description: "An explicit list of monitors, by id.",
						Validators:  []validator.Set{setvalidator.SizeAtLeast(1)},
					},
					"tags": schema.SetAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Description: "Every monitor carrying any of these tags, matched afresh on every delivery.",
						Validators:  []validator.Set{setvalidator.SizeAtLeast(1)},
					},
				},
			},
			"headers": schema.SetNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "Custom request headers sent with every delivery - a bearer token for your receiver, " +
					"say. Writing this attribute replaces the whole set; `[]` clears it. `HT-*` and `webhook-*` are " +
					"reserved for the delivery's own headers and are refused.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"header": schema.StringAttribute{
							Required:    true,
							Description: "The header name, at most 100 characters.",
							Validators:  []validator.String{stringvalidator.LengthBetween(1, 100)},
						},
						"value": schema.StringAttribute{
							Required:    true,
							Sensitive:   true,
							Description: "The header value, at most 1000 characters.",
							Validators:  []validator.String{stringvalidator.LengthAtMost(1000)},
						},
					},
				},
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				Description: "Whether deliveries are attempted. A webhook the API disabled after 20 consecutive " +
					"failures reads `false` here, and the next apply switches it back on - which also clears the " +
					"failure counter. Write `enabled = false` to leave it off.",
			},
			"secret": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				Description: "The signing secret, as captured from the answer that minted it. Deliveries are signed " +
					"with it in both the `HT-Signature` and the Standard Webhooks `webhook-signature` header.\n\n" +
					"The API publishes the value exactly once. A webhook adopted with `terraform import` therefore " +
					"has no secret in state and reads null; rotate it with `rotate_secret` to obtain one.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"rotate_secret": schema.Int64Attribute{
				Optional: true,
				Description: "A rotation counter. Changing it - from unset to `1`, from `1` to `2` - makes the next " +
					"apply mint a new secret and store it in `secret`. The previous secret keeps signing in " +
					"parallel for 24 hours (`secret_previous_valid_until`), and both signatures ride the same " +
					"header, so a verifier that accepts any matching signature never sees a gap.",
			},
			"secret_set": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the webhook has a signing secret at all.",
			},
			"secret_updated_at": schema.Int64Attribute{
				Computed:    true,
				Description: "When the current secret was minted, in Unix seconds.",
			},
			"secret_previous_valid_until": schema.Int64Attribute{
				Computed: true,
				Description: "While a rotation's grace window is open, the instant the PREVIOUS secret stops " +
					"signing, in Unix seconds. Null when no rotation is in flight.",
			},
			"monitor_count": schema.Int64Attribute{
				Computed:    true,
				Description: "How many monitors the scope resolved to, including zero.",
			},
			"resolved_monitor_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Which monitors a `tags` scope resolved to. Null for the other two scope forms - an " +
					"`all` scope publishes `monitor_count` instead, because nothing can widen it.",
			},
			"disabled_reason": schema.StringAttribute{
				Computed: true,
				Description: "Why the SYSTEM switched this webhook off: `deliveryFailure` after 20 consecutive " +
					"failures, `gone` after a `410`. Null when it is enabled, or when the account switched it off.",
			},
			"consecutive_failures": schema.Int64Attribute{
				Computed:    true,
				Description: "How many deliveries have failed in a row. Any 2xx resets it.",
			},
			"last_delivery_at": schema.Int64Attribute{
				Computed:    true,
				Description: "When a delivery was last attempted, in Unix seconds.",
			},
			"created": schema.Int64Attribute{
				Computed:    true,
				Description: "When the webhook was created, in Unix seconds.",
			},
			"updated": schema.Int64Attribute{
				Computed:    true,
				Description: "When the webhook was last edited, in Unix seconds.",
			},
		},
	}
}

func (r *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The webhook resource was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.api = api
}

// ValidateConfig enforces the scope union, which the schema cannot: exactly
// one of the three forms, and `all` spelled `true` when it is the one.
func (r *webhookResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config webhookModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateWebhookScope(config.Scope)...)
}

// validateWebhookScope enforces the union: exactly one of the three forms,
// and `all` spelled true when it is the one.
func validateWebhookScope(scope types.Object) diag.Diagnostics {
	var diags diag.Diagnostics
	if scope.IsNull() || scope.IsUnknown() {
		return diags
	}

	attrs := scope.Attributes()
	all, _ := attrs["all"].(types.Bool)
	monitorIDs, _ := attrs["monitor_ids"].(types.Set)
	tags, _ := attrs["tags"].(types.Set)

	named := make([]string, 0, 3)
	if !all.IsNull() && !all.IsUnknown() {
		named = append(named, "all")
	}
	if !monitorIDs.IsNull() && !monitorIDs.IsUnknown() {
		named = append(named, "monitor_ids")
	}
	if !tags.IsNull() && !tags.IsUnknown() {
		named = append(named, "tags")
	}

	switch len(named) {
	case 1:
		if named[0] == "all" && !all.ValueBool() {
			diags.AddAttributeError(
				path.Root("scope").AtName("all"),
				"An `all` scope must be true",
				"`all = false` says nothing about which monitors to send. Narrow the scope with `monitor_ids` or "+
					"`tags` instead, or remove the member.",
			)
		}
	case 0:
		diags.AddAttributeError(
			path.Root("scope"),
			"The scope names no monitors",
			"Set one of `all = true`, `monitor_ids` or `tags`. A webhook addressed to nothing would never fire.",
		)
	default:
		diags.AddAttributeError(
			path.Root("scope"),
			"The scope names more than one form",
			fmt.Sprintf("`%s` are all set. Exactly one of `all`, `monitor_ids` and `tags` decides which monitors "+
				"this webhook receives events for.", strings.Join(named, "`, `")),
		)
	}
	return diags
}

// ModifyPlan makes a rotation visible in the plan. Without it the secret
// would carry its old value into the plan and the apply would silently
// replace it, which is exactly the change a reader of the plan needs to see.
// It also settles whether there is anything to do at all, so that a webhook
// whose configuration leaves the headers out does not plan an update forever.
func (r *webhookResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		// Creating or destroying: there is nothing to rotate.
		return
	}

	var plan, state, config webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !rotationRequested(plan.RotateSecret, state.RotateSecret) {
		if !webhookChanged(&plan, &state, &config) {
			keepPriorState(req, resp)
		}
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("secret"), types.StringUnknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("secret_updated_at"), types.Int64Unknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("secret_previous_valid_until"), types.Int64Unknown())...)
}

// webhookChanged reports whether the plan asks for anything the state does
// not already say, over the members a configuration owns. A member the
// configuration leaves out is not one of them - see plan_stability.go.
func webhookChanged(plan, state, config *webhookModel) bool {
	return asksForChange([]plannedMember{
		{config.URL, plan.URL, state.URL},
		{config.Name, plan.Name, state.Name},
		{config.Events, plan.Events, state.Events},
		{config.Scope, plan.Scope, state.Scope},
		{config.Headers, plan.Headers, state.Headers},
		{config.Enabled, plan.Enabled, state.Enabled},
	})
}

// rotationRequested reports a changed rotation counter. An unknown counter
// is treated as a rotation, because it may resolve to a new value.
func rotationRequested(planned, current types.Int64) bool {
	if planned.IsUnknown() {
		return true
	}
	if planned.IsNull() {
		// Removing the counter is not a rotation: it stops asking for one.
		return false
	}
	if current.IsNull() || current.IsUnknown() {
		return true
	}
	return planned.ValueInt64() != current.ValueInt64()
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := encodeWebhook(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.api.CreateWebhook(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("create the webhook", err, webhookPointerMapper)...)
		return
	}

	state, diags := webhookToState(ctx, &plan, webhook)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.Secret.IsNull() {
		resp.Diagnostics.AddWarning(
			"The create did not publish a signing secret",
			"Deliveries are signed, and the secret is published only in the answer that mints it. Rotate it with "+
				"`rotate_secret` to obtain a value your receiver can verify against.",
		)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.api.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the webhook", err, webhookPointerMapper)...)
		return
	}

	refreshed, diags := webhookToState(ctx, &state, webhook)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	want, diags := encodeWebhook(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	have, diags := encodeWebhook(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.Diff(have, want)
	rotating := rotationRequested(plan.RotateSecret, state.RotateSecret)
	if rotating {
		body["secret"] = map[string]any{"rotate": true}
	}

	webhook, err := r.api.UpdateWebhook(ctx, state.ID.ValueString(), body)
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			resp.Diagnostics.AddError(
				"The webhook is gone",
				"It was deleted outside Terraform between the plan and the apply. Run the apply again to create it.",
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("update the webhook", err, webhookPointerMapper)...)
		return
	}

	// A rotation must not fall back to the secret the state already holds:
	// that value stops signing when the grace window closes.
	prior := &state
	if rotating {
		withoutSecret := state
		withoutSecret.Secret = types.StringNull()
		prior = &withoutSecret
	}

	updated, diags := webhookToState(ctx, prior, webhook)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated.RotateSecret = plan.RotateSecret
	if rotating && updated.Secret.IsNull() {
		resp.Diagnostics.AddError(
			"The rotation did not publish a new secret",
			"The API answered the rotation without the new value, which it publishes exactly once. Rotate again to "+
				"obtain one; the webhook itself is unaffected.",
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &updated)...)
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteWebhook(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the webhook", err, nil)...)
	}
}

func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.AddWarning(
		"The signing secret cannot be imported",
		"The API publishes a secret's value only in the answer that mints it, so an adopted webhook reads `secret = "+
			"null`. Set `rotate_secret` and apply to mint one Terraform can hold.",
	)
}

// webhookPointerMapper places a problem document's JSON Pointers on the
// attributes a configuration actually wrote.
func webhookPointerMapper(pointer string) (path.Path, bool) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return path.Empty(), false
	}
	switch parts[0] {
	case "url", "name", "events", "enabled", "headers", "secret":
		return path.Root(parts[0]), true
	case "scope":
		p := path.Root("scope")
		if len(parts) > 1 {
			switch parts[1] {
			case "all":
				return p.AtName("all"), true
			case "monitorIds":
				return p.AtName("monitor_ids"), true
			case "tags":
				return p.AtName("tags"), true
			}
		}
		return p, true
	}
	return path.Empty(), false
}

// encodeWebhook turns the model into a wire body. Null members are left
// out, which on a create means "use the default" and on an update means
// "leave alone" - the PATCH diff decides which members reach the wire.
func encodeWebhook(ctx context.Context, m *webhookModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{}

	putString(body, "url", m.URL)
	putString(body, "name", m.Name)
	putBool(body, "enabled", m.Enabled)

	if !m.Events.IsNull() && !m.Events.IsUnknown() {
		var events []string
		diags.Append(m.Events.ElementsAs(ctx, &events, false)...)
		sort.Strings(events)
		body["events"] = events
	}

	if !m.Scope.IsNull() && !m.Scope.IsUnknown() {
		scope := map[string]any{}
		attrs := m.Scope.Attributes()
		if all, ok := attrs["all"].(types.Bool); ok && !all.IsNull() && !all.IsUnknown() {
			scope["all"] = all.ValueBool()
		}
		if ids, ok := attrs["monitor_ids"].(types.Set); ok && !ids.IsNull() && !ids.IsUnknown() {
			var values []string
			diags.Append(ids.ElementsAs(ctx, &values, false)...)
			sort.Strings(values)
			scope["monitorIds"] = values
		}
		if tags, ok := attrs["tags"].(types.Set); ok && !tags.IsNull() && !tags.IsUnknown() {
			var values []string
			diags.Append(tags.ElementsAs(ctx, &values, false)...)
			sort.Strings(values)
			scope["tags"] = values
		}
		if len(scope) > 0 {
			body["scope"] = scope
		}
	}

	if !m.Headers.IsNull() && !m.Headers.IsUnknown() {
		var headers []webhookHeaderModel
		diags.Append(m.Headers.ElementsAs(ctx, &headers, false)...)
		rows := make([]map[string]any, 0, len(headers))
		for _, h := range headers {
			if h.Header.IsNull() || h.Header.IsUnknown() {
				continue
			}
			row := map[string]any{"header": h.Header.ValueString()}
			if !h.Value.IsNull() && !h.Value.IsUnknown() {
				row["value"] = h.Value.ValueString()
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i]["header"].(string) < rows[j]["header"].(string)
		})
		body["headers"] = rows
	}

	return body, diags
}

// webhookToState renders what the API returned as resource state, keeping
// the two values only the configuration knows: the rotation counter, which
// is a trigger rather than a stored member, and the signing secret, which
// the API publishes exactly once.
func webhookToState(ctx context.Context, prior *webhookModel, w *client.Webhook) (webhookModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	state := webhookModel{}

	state.ID = types.StringValue(w.ID)
	state.URL = types.StringValue(w.URL)
	state.Name = stringOrNull(w.Name)
	state.Enabled = types.BoolValue(w.Enabled)
	state.DisabledReason = stringOrNull(w.DisabledReason)
	state.ConsecutiveFailures = types.Int64Value(w.ConsecutiveFailures)
	state.LastDeliveryAt = int64OrNull(w.LastDeliveryAt)
	state.Created = types.Int64Value(w.Created)
	state.Updated = types.Int64Value(w.Updated)

	events, eventDiags := stringSet(ctx, w.Events)
	diags.Append(eventDiags...)
	state.Events = events

	if w.Scope == nil {
		state.Scope = types.ObjectNull(webhookScopeAttrTypes)
		state.MonitorCount = types.Int64Null()
		state.ResolvedMonitorIDs = types.SetNull(types.StringType)
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
		state.Scope = object
		state.MonitorCount = types.Int64Value(w.Scope.MonitorCount)
		resolved, resolvedDiags := stringSet(ctx, w.Scope.ResolvedMonitorIDs)
		diags.Append(resolvedDiags...)
		state.ResolvedMonitorIDs = resolved
	}

	headers, headerDiags := webhookHeadersToState(ctx, prior, w.Headers)
	diags.Append(headerDiags...)
	state.Headers = headers

	state.Secret = types.StringNull()
	state.SecretSet = types.BoolValue(false)
	state.SecretUpdatedAt = types.Int64Null()
	state.SecretPreviousValidUntil = types.Int64Null()
	if w.Secret != nil {
		state.SecretSet = types.BoolValue(w.Secret.Set)
		state.SecretUpdatedAt = int64OrNull(w.Secret.UpdatedAt)
		state.SecretPreviousValidUntil = int64OrNull(w.Secret.PreviousValidUntil)
		if w.Secret.Value != "" {
			state.Secret = types.StringValue(w.Secret.Value)
		}
	}
	if state.Secret.IsNull() && prior != nil && !prior.Secret.IsNull() && !prior.Secret.IsUnknown() {
		// A read publishes only whether a secret is set, so the value the
		// mint answered with is the one state keeps.
		state.Secret = prior.Secret
	}

	state.RotateSecret = types.Int64Null()
	if prior != nil && !prior.RotateSecret.IsUnknown() {
		state.RotateSecret = prior.RotateSecret
	}

	return state, diags
}

// webhookHeadersToState renders the header set, restoring a value the API
// withheld from a header the configuration wrote.
func webhookHeadersToState(ctx context.Context, prior *webhookModel, headers []client.WebhookHeader) (types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics

	// A configuration that wrote `headers = []` asked for an empty set, and
	// the answer to that write carries no headers at all. Reading it back as
	// null would make the apply inconsistent with the plan, so the shape the
	// configuration chose is the shape state keeps.
	declared := prior != nil && !prior.Headers.IsNull() && !prior.Headers.IsUnknown()
	if headers == nil {
		if declared {
			return types.SetValueMust(webhookHeaderObjectType, []attr.Value{}), diags
		}
		return types.SetNull(webhookHeaderObjectType), diags
	}

	known := map[string]string{}
	if declared {
		var priorHeaders []webhookHeaderModel
		diags.Append(prior.Headers.ElementsAs(ctx, &priorHeaders, false)...)
		for _, h := range priorHeaders {
			if !h.Header.IsNull() && !h.Value.IsNull() {
				known[h.Header.ValueString()] = h.Value.ValueString()
			}
		}
	}

	rows := make([]attr.Value, 0, len(headers))
	for _, h := range headers {
		value := h.Value
		if value == "" {
			value = known[h.Header]
		}
		object, objectDiags := types.ObjectValue(webhookHeaderAttrTypes, map[string]attr.Value{
			"header": types.StringValue(h.Header),
			"value":  types.StringValue(value),
		})
		diags.Append(objectDiags...)
		rows = append(rows, object)
	}

	set, setDiags := types.SetValue(webhookHeaderObjectType, rows)
	diags.Append(setDiags...)
	return set, diags
}

func backquotedList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, "`"+v+"`")
	}
	return strings.Join(quoted, ", ")
}
