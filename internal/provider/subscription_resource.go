package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// A subscription is addressed by the PAIR it joins, not by an id of its
// own, so its Terraform id is the pair spelled "<monitorId>/<contactId>" -
// which is also what `terraform import` takes.

// subscriptionID spells the pair as one string.
func subscriptionID(monitorID, contactID string) string {
	return monitorID + "/" + contactID
}

// parseSubscriptionID reads the pair back out of a composite id.
func parseSubscriptionID(id string) (monitorID, contactID string, err error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("%q is not a subscription id: it is spelled \"<monitor id>/<contact id>\"", id)
	}
	return parts[0], parts[1], nil
}

// subscriptionCommonAttributes are the members both subscription
// resources share: the pair that identifies them, and the stamp.
func subscriptionCommonAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed: true,
			Description: "The pair this subscription joins, spelled `\"<monitor id>/<contact id>\"`. A " +
				"subscription has no id of its own: the pair IS the identity.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"monitor_id": schema.StringAttribute{
			Required: true,
			Description: "The monitor half of the pair. Changing it addresses a different subscription, so it " +
				"replaces this one.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"contact_id": schema.StringAttribute{
			Required: true,
			Description: "The contact half of the pair. Changing it addresses a different subscription, so it " +
				"replaces this one.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"created": schema.Int64Attribute{
			Computed:    true,
			Description: "When the subscription was created, in Unix seconds.",
		},
	}
}

func configureSubscription(what string, providerData any, diags *diag.Diagnostics) *client.Client {
	if providerData == nil {
		return nil
	}
	api, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The %s resource was configured with %T instead of a *client.Client. This is a bug in the provider.", what, providerData),
		)
		return nil
	}
	return api
}

// subscriptionPointerMapper places a problem document's pointers. Both
// bodies carry exactly one member, so the mapping is one line each.
func subscriptionPointerMapper(wireMember, attribute string) client.PointerMapper {
	return func(pointer string) (path.Path, bool) {
		if strings.TrimPrefix(pointer, "/") == wireMember {
			return path.Root(attribute), true
		}
		return path.Empty(), false
	}
}

func importSubscription(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	monitorID, contactID, err := parseSubscriptionID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"That is not a subscription id",
			fmt.Sprintf("%s\n\nFor example:\n\n    terraform import <address> "+
				"4e49d7a2-4ab5-45e2-b9f8-1d59f505ad45/0c3c7b07-cecb-43dd-9b76-8516d3b9c771", err),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), subscriptionID(monitorID, contactID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("monitor_id"), monitorID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("contact_id"), contactID)...)
}

// sortedStrings drains a set into a sorted slice, which is what makes two
// spellings of the same set compare equal on the wire.
func sortedStrings(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}
	var values []string
	diags.Append(set.ElementsAs(ctx, &values, false)...)
	sort.Strings(values)
	return values, diags
}

// ---------------------------------------------------------------------
// hosttracker_alert_subscription
// ---------------------------------------------------------------------

var (
	_ resource.Resource                = (*alertSubscriptionResource)(nil)
	_ resource.ResourceWithConfigure   = (*alertSubscriptionResource)(nil)
	_ resource.ResourceWithImportState = (*alertSubscriptionResource)(nil)
)

// NewAlertSubscriptionResource is the hosttracker_alert_subscription
// constructor.
func NewAlertSubscriptionResource() resource.Resource { return &alertSubscriptionResource{} }

type alertSubscriptionResource struct {
	api *client.Client
}

type alertSubscriptionModel struct {
	ID         types.String `tfsdk:"id"`
	MonitorID  types.String `tfsdk:"monitor_id"`
	ContactID  types.String `tfsdk:"contact_id"`
	AlertTypes types.Set    `tfsdk:"alert_types"`
	Created    types.Int64  `tfsdk:"created"`
}

func (r *alertSubscriptionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_subscription"
}

func (r *alertSubscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := subscriptionCommonAttributes()
	attributes["alert_types"] = schema.SetAttribute{
		ElementType: types.StringType,
		Required:    true,
		Description: "Which state changes this contact is alerted about: `up`, `down`, `repeatedlyDown`. Writing " +
			"the attribute replaces the whole set, and at least one is required - destroy the resource to stop " +
			"alerting this contact. The vocabulary is open, so a type newer than this provider is accepted too.",
		Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
	}

	resp.Schema = schema.Schema{
		Description: "Alerts one contact about one monitor's state changes.\n\n" +
			"The pair is the identity: there is one subscription per monitor and contact, holding a SET of alert " +
			"types. Writing it is idempotent, so creating a subscription that already exists adopts it rather " +
			"than failing - which also means Terraform will overwrite alert types set in the web app.",
		Attributes: attributes,
	}
}

func (r *alertSubscriptionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if api := configureSubscription("alert subscription", req.ProviderData, &resp.Diagnostics); api != nil {
		r.api = api
	}
}

func (r *alertSubscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alertSubscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.write(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertSubscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alertSubscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sub, err := r.api.GetAlertSubscription(ctx, state.MonitorID.ValueString(), state.ContactID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the alert subscription", err, nil)...)
		return
	}

	alertTypes, diags := stringSet(ctx, sub.AlertTypes)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ID = types.StringValue(subscriptionID(state.MonitorID.ValueString(), state.ContactID.ValueString()))
	state.AlertTypes = alertTypes
	state.Created = types.Int64Value(sub.Created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *alertSubscriptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan alertSubscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.write(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// write sends the whole set. The door is idempotent, so create and update
// are the same call.
func (r *alertSubscriptionResource) write(ctx context.Context, plan *alertSubscriptionModel) diag.Diagnostics {
	var diags diag.Diagnostics

	alertTypes, valueDiags := sortedStrings(ctx, plan.AlertTypes)
	diags.Append(valueDiags...)
	if diags.HasError() {
		return diags
	}

	monitorID := plan.MonitorID.ValueString()
	contactID := plan.ContactID.ValueString()
	sub, err := r.api.SetAlertSubscription(ctx, monitorID, contactID, alertTypes)
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			diags.AddError(
				"No such monitor or contact",
				fmt.Sprintf("Neither monitor %s nor contact %s could be addressed. Both must exist and belong to "+
					"this account before they can be subscribed.", monitorID, contactID),
			)
			return diags
		}
		diags.Append(client.Diagnose("write the alert subscription", err,
			subscriptionPointerMapper("alertTypes", "alert_types"))...)
		return diags
	}

	stored, storedDiags := stringSet(ctx, sub.AlertTypes)
	diags.Append(storedDiags...)
	if diags.HasError() {
		return diags
	}
	plan.ID = types.StringValue(subscriptionID(monitorID, contactID))
	plan.AlertTypes = stored
	plan.Created = types.Int64Value(sub.Created)
	return diags
}

func (r *alertSubscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alertSubscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteAlertSubscription(ctx, state.MonitorID.ValueString(), state.ContactID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the alert subscription", err, nil)...)
	}
}

func (r *alertSubscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importSubscription(ctx, req, resp)
}

// ---------------------------------------------------------------------
// hosttracker_report_subscription
// ---------------------------------------------------------------------

var (
	_ resource.Resource                = (*reportSubscriptionResource)(nil)
	_ resource.ResourceWithConfigure   = (*reportSubscriptionResource)(nil)
	_ resource.ResourceWithImportState = (*reportSubscriptionResource)(nil)
)

// NewReportSubscriptionResource is the hosttracker_report_subscription
// constructor.
func NewReportSubscriptionResource() resource.Resource { return &reportSubscriptionResource{} }

type reportSubscriptionResource struct {
	api *client.Client
}

type reportSubscriptionModel struct {
	ID          types.String `tfsdk:"id"`
	MonitorID   types.String `tfsdk:"monitor_id"`
	ContactID   types.String `tfsdk:"contact_id"`
	Frequencies types.Set    `tfsdk:"frequencies"`
	Created     types.Int64  `tfsdk:"created"`
}

func (r *reportSubscriptionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_report_subscription"
}

func (r *reportSubscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := subscriptionCommonAttributes()
	attributes["frequencies"] = schema.SetAttribute{
		ElementType: types.StringType,
		Required:    true,
		Description: "How often this contact is sent a report about this monitor: `daily`, `weekly`, `monthly`, " +
			"`quarterly`, `yearly`. Writing the attribute replaces the whole set, and at least one is required - " +
			"destroy the resource to stop reporting to this contact.",
		Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
	}

	resp.Schema = schema.Schema{
		Description: "Sends one contact a periodic uptime report about one monitor.\n\n" +
			"Reports are delivered by email only, so the contact must be an email one. The pair is the identity, " +
			"holding a SET of frequencies, and writing it is idempotent: creating a subscription that already " +
			"exists adopts it rather than failing.",
		Attributes: attributes,
	}
}

func (r *reportSubscriptionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if api := configureSubscription("report subscription", req.ProviderData, &resp.Diagnostics); api != nil {
		r.api = api
	}
}

func (r *reportSubscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan reportSubscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.write(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *reportSubscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state reportSubscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sub, err := r.api.GetReportSubscription(ctx, state.MonitorID.ValueString(), state.ContactID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the report subscription", err, nil)...)
		return
	}

	frequencies, diags := stringSet(ctx, sub.Frequencies)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ID = types.StringValue(subscriptionID(state.MonitorID.ValueString(), state.ContactID.ValueString()))
	state.Frequencies = frequencies
	state.Created = types.Int64Value(sub.Created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *reportSubscriptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan reportSubscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.write(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *reportSubscriptionResource) write(ctx context.Context, plan *reportSubscriptionModel) diag.Diagnostics {
	var diags diag.Diagnostics

	frequencies, valueDiags := sortedStrings(ctx, plan.Frequencies)
	diags.Append(valueDiags...)
	if diags.HasError() {
		return diags
	}

	monitorID := plan.MonitorID.ValueString()
	contactID := plan.ContactID.ValueString()
	sub, err := r.api.SetReportSubscription(ctx, monitorID, contactID, frequencies)
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			diags.AddError(
				"No such monitor or contact",
				fmt.Sprintf("Neither monitor %s nor contact %s could be addressed. Both must exist and belong to "+
					"this account before they can be subscribed.", monitorID, contactID),
			)
			return diags
		}
		diags.Append(client.Diagnose("write the report subscription", err,
			subscriptionPointerMapper("frequencies", "frequencies"))...)
		return diags
	}

	stored, storedDiags := stringSet(ctx, sub.Frequencies)
	diags.Append(storedDiags...)
	if diags.HasError() {
		return diags
	}
	plan.ID = types.StringValue(subscriptionID(monitorID, contactID))
	plan.Frequencies = stored
	plan.Created = types.Int64Value(sub.Created)
	return diags
}

func (r *reportSubscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state reportSubscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteReportSubscription(ctx, state.MonitorID.ValueString(), state.ContactID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the report subscription", err, nil)...)
	}
}

func (r *reportSubscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importSubscription(ctx, req, resp)
}
