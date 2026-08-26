package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*statusPageResource)(nil)
	_ resource.ResourceWithConfigure      = (*statusPageResource)(nil)
	_ resource.ResourceWithImportState    = (*statusPageResource)(nil)
	_ resource.ResourceWithValidateConfig = (*statusPageResource)(nil)
)

// NewStatusPageResource is the hosttracker_status_page constructor.
func NewStatusPageResource() resource.Resource { return &statusPageResource{} }

type statusPageResource struct {
	api *client.Client
}

type statusPageModel struct {
	ID                  types.String `tfsdk:"id"`
	Slug                types.String `tfsdk:"slug"`
	Title               types.String `tfsdk:"title"`
	Settings            types.Object `tfsdk:"settings"`
	Components          types.List   `tfsdk:"components"`
	PublicURL           types.String `tfsdk:"public_url"`
	CustomDomain        types.String `tfsdk:"custom_domain"`
	ComponentCount      types.Int64  `tfsdk:"component_count"`
	UnresolvedIncidents types.Int64  `tfsdk:"unresolved_incidents"`
	HasPassword         types.Bool   `tfsdk:"has_password"`
	Created             types.Int64  `tfsdk:"created"`
}

// statusPageComponentModel is one row of the component set.
type statusPageComponentModel struct {
	MonitorID   types.String `tfsdk:"monitor_id"`
	ThirdParty  types.Bool   `tfsdk:"third_party"`
	Name        types.String `tfsdk:"name"`
	Group       types.String `tfsdk:"group"`
	ManualState types.String `tfsdk:"manual_state"`
}

var statusPageComponentAttrTypes = map[string]attr.Type{
	"monitor_id":   types.StringType,
	"third_party":  types.BoolType,
	"name":         types.StringType,
	"group":        types.StringType,
	"manual_state": types.StringType,
}

var statusPageComponentObjectType = types.ObjectType{AttrTypes: statusPageComponentAttrTypes}

// slugPattern is the page's permanent public address: lowercase letters,
// digits and single hyphens.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (r *statusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *statusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A public status page: the components you publish, the incidents you declare against them, " +
			"and the appearance the page renders in.\n\n" +
			"The component set is written as a WHOLE. Terraform sends the list in `components` in full on every " +
			"change, and a component the list omits is removed from the page - so either let Terraform own the " +
			"set, or leave the attribute out entirely and manage components in the web app.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The page's id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				Required: true,
				Description: "The page's permanent public address: lowercase letters, digits and single hyphens, " +
					"3 to 64 characters, unique across the product. It cannot be changed, so editing it replaces " +
					"the page - which frees the old slug and takes the page's incidents and subscribers with it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 64),
					stringvalidator.RegexMatches(slugPattern,
						"must be lowercase letters, digits and single hyphens, for example `acme-status`"),
				},
			},
			"title": schema.StringAttribute{
				Required:    true,
				Description: "The heading the public page carries, at most 200 characters.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 200)},
			},
			"settings": statusPageSettingsSchema(),
			"components": schema.ListNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "The components the page publishes, in display order. Leave the attribute out to let " +
					"the web app own the set; write it, and every save replaces the whole set.\n\n" +
					"A component is either MONITORED - it names a `monitor_id` and takes its state from that " +
					"monitor's checks - or THIRD-PARTY: `third_party = true`, its own `name`, and a `manual_state` " +
					"you pin by hand. The provider matches each entry against the page's existing rows (by monitor " +
					"for a monitored one, by name for a third-party one) so that per-component subscriptions " +
					"survive a save.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"monitor_id": schema.StringAttribute{
							Optional:    true,
							Description: "The monitor whose state this component shows. Required unless `third_party`.",
						},
						"third_party": schema.BoolAttribute{
							Optional: true,
							Description: "True for a component this account does not monitor - a dependency you " +
								"report on by hand.",
						},
						"name": schema.StringAttribute{
							Optional: true,
							Description: "The label shown on the page, at most 200 characters. Required on a " +
								"third-party component; a monitored one inherits its monitor's name when this is " +
								"left out.",
							Validators: []validator.String{stringvalidator.LengthAtMost(200)},
						},
						"group": schema.StringAttribute{
							Optional: true,
							Description: "The heading this component is listed under, at most 100 characters. " +
								"Components with no group are listed first.",
							Validators: []validator.String{stringvalidator.LengthAtMost(100)},
						},
						"manual_state": schema.StringAttribute{
							Optional: true,
							Description: "The state pinned on a THIRD-PARTY component: `operational`, `degraded` " +
								"or `down`. A monitored component's state comes from its checks, and the API " +
								"refuses this there.",
							Validators: []validator.String{stringvalidator.OneOf("operational", "degraded", "down")},
						},
					},
				},
			},
			"public_url": schema.StringAttribute{
				Computed: true,
				Description: "Where the page is served from: `https://" + client.StatusPageHost + "/<slug>`, or the " +
					"custom domain when one is configured. The provider composes it; the API does not publish it.",
			},
			"custom_domain": schema.StringAttribute{
				Computed: true,
				Description: "The custom domain as configured, when there is one. Activation is dashboard " +
					"bookkeeping and is not managed here.",
			},
			"component_count": schema.Int64Attribute{
				Computed:    true,
				Description: "How many components the page publishes.",
			},
			"unresolved_incidents": schema.Int64Attribute{
				Computed:    true,
				Description: "How many of the page's declared incidents are still open.",
			},
			"has_password": schema.BoolAttribute{
				Computed: true,
				Description: "Whether the page is behind a password. The password itself is set in the web app, " +
					"not here.",
			},
			"created": schema.Int64Attribute{
				Computed:    true,
				Description: "When the page was created, in Unix seconds.",
			},
		},
	}
}

func (r *statusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The status page resource was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.api = api
}

// ValidateConfig enforces the component union: a component names a monitor
// or declares itself third-party, never both and never neither.
func (r *statusPageResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config statusPageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateStatusPageComponents(ctx, config.Components)...)
}

// validateStatusPageComponents enforces the component union: a component
// names a monitor or declares itself third-party, never both and never
// neither.
func validateStatusPageComponents(ctx context.Context, list types.List) diag.Diagnostics {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return diags
	}

	var components []statusPageComponentModel
	diags.Append(list.ElementsAs(ctx, &components, false)...)
	if diags.HasError() {
		return diags
	}

	for i, component := range components {
		at := path.Root("components").AtListIndex(i)

		// A value that is only known once something else has been created -
		// `monitor_id = hosttracker_monitor.x.id` is the ordinary case - says
		// nothing at validate time. Only what is already known is judged;
		// the API has the last word on the rest.
		monitorKnown := !component.MonitorID.IsUnknown()
		thirdPartyKnown := !component.ThirdParty.IsUnknown()
		thirdParty := thirdPartyKnown && component.ThirdParty.ValueBool()
		hasMonitor := monitorKnown && !component.MonitorID.IsNull()
		decided := monitorKnown && thirdPartyKnown

		switch {
		case thirdParty && hasMonitor:
			diags.AddAttributeError(at, "A component is either monitored or third-party",
				"This one names a `monitor_id` and sets `third_party = true`. A third-party component is one this "+
					"account does not monitor, so it has no monitor to name.")
		case thirdParty && component.Name.IsNull():
			diags.AddAttributeError(at.AtName("name"), "A third-party component needs a name",
				"There is no monitor to inherit a label from, so `name` is what the page shows.")
		case decided && !thirdParty && !hasMonitor:
			diags.AddAttributeError(at, "The component names nothing to show",
				"Set `monitor_id` to publish one of this account's monitors, or `third_party = true` with a `name` "+
					"to report on a dependency by hand.")
		case thirdPartyKnown && !thirdParty && !component.ManualState.IsNull():
			diags.AddAttributeError(at.AtName("manual_state"), "A monitored component's state is not pinned",
				"It comes from that monitor's checks, and the API refuses a pinned state here. Remove "+
					"`manual_state`, or make the component third-party.")
		}
	}
	return diags
}

func (r *statusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := encodeStatusPage(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	components, componentDiags := encodeComponents(ctx, plan.Components)
	resp.Diagnostics.Append(componentDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if components != nil {
		// The create door takes the initial component set in the same call,
		// so a page is never briefly published empty.
		body["components"] = components
	}

	page, err := r.api.CreateStatusPage(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("create the status page", err, statusPagePointerMapper)...)
		return
	}

	state, diags := statusPageToState(ctx, &plan, page)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *statusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	page, err := r.api.GetStatusPage(ctx, state.ID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the status page", err, statusPagePointerMapper)...)
		return
	}

	refreshed, diags := statusPageToState(ctx, &state, page)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *statusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	want, diags := encodeStatusPage(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	have, diags := encodeStatusPage(ctx, &state)
	resp.Diagnostics.Append(diags...)
	wanted, diags := encodeComponents(ctx, plan.Components)
	resp.Diagnostics.Append(diags...)
	held, diags := encodeComponents(ctx, state.Components)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The slug is permanent and replaces the resource, so it is never part
	// of an update body.
	delete(want, "slug")
	delete(have, "slug")

	id := state.ID.ValueString()
	page, err := r.api.UpdateStatusPage(ctx, id, client.Diff(have, want))
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			resp.Diagnostics.AddError(
				"The status page is gone",
				"It was deleted outside Terraform between the plan and the apply. Run the apply again to create it.",
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("update the status page", err, statusPagePointerMapper)...)
		return
	}

	if wanted != nil && componentsDiffer(held, wanted) {
		// The set is written whole, and each entry carries the id of the
		// row it replaces so per-component subscriptions survive the save.
		page, err = r.api.SetStatusPageComponents(ctx, id, carryComponentIDs(wanted, page.Components))
		if err != nil {
			resp.Diagnostics.Append(client.Diagnose("write the status page components", err, statusPagePointerMapper)...)
			return
		}
	}

	updated, diags := statusPageToState(ctx, &plan, page)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &updated)...)
}

func (r *statusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteStatusPage(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the status page", err, nil)...)
	}
}

func (r *statusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// statusPagePointerMapper places a problem document's JSON Pointers on the
// attributes a configuration actually wrote.
func statusPagePointerMapper(pointer string) (path.Path, bool) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return path.Empty(), false
	}
	switch parts[0] {
	case "slug", "title":
		return path.Root(parts[0]), true
	case "settings":
		p := path.Root("settings")
		if len(parts) > 1 {
			for _, f := range statusPageSettingsFields {
				if f.wire == parts[1] {
					return p.AtName(f.name), true
				}
			}
		}
		return p, true
	case "components":
		return path.Root("components"), true
	}
	return path.Empty(), false
}

// encodeStatusPage turns the model into a wire body. Components are added
// by the caller, because only the create door takes them inline.
func encodeStatusPage(ctx context.Context, m *statusPageModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{}

	putString(body, "slug", m.Slug)
	putString(body, "title", m.Title)

	settings, settingsDiags := encodeStatusPageSettings(ctx, m.Settings)
	diags.Append(settingsDiags...)
	if len(settings) > 0 {
		body["settings"] = settings
	}
	return body, diags
}

// encodeComponents renders the component list for the wire. A null list is
// a list Terraform does not manage, and answers nil.
func encodeComponents(ctx context.Context, list types.List) ([]client.StatusPageComponent, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}

	var models []statusPageComponentModel
	diags.Append(list.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	out := make([]client.StatusPageComponent, 0, len(models))
	for _, m := range models {
		component := client.StatusPageComponent{ThirdParty: m.ThirdParty.ValueBool()}
		if !m.MonitorID.IsNull() && !m.MonitorID.IsUnknown() {
			component.MonitorID = stringPointer(m.MonitorID.ValueString())
		}
		if !m.Name.IsNull() && !m.Name.IsUnknown() {
			component.Name = stringPointer(m.Name.ValueString())
		}
		if !m.Group.IsNull() && !m.Group.IsUnknown() {
			component.Group = stringPointer(m.Group.ValueString())
		}
		if !m.ManualState.IsNull() && !m.ManualState.IsUnknown() {
			component.ManualState = stringPointer(m.ManualState.ValueString())
		}
		out = append(out, component)
	}
	return out, diags
}

// componentsDiffer compares two component sets by what they would put on
// the wire, so that a re-ordering counts and a re-spelling does not.
func componentsDiffer(have, want []client.StatusPageComponent) bool {
	body := client.Diff(
		map[string]any{"components": have},
		map[string]any{"components": want},
	)
	_, changed := body["components"]
	return changed
}

// carryComponentIDs matches each planned component against the row it
// replaces and copies that row's id onto it. Without the id the API treats
// the entry as a new component, and the old row's per-component
// subscriptions go with the row it removes.
//
// A monitored component is matched by its monitor, a third-party one by
// its name; a row is claimed once, so two entries naming the same monitor
// take two different rows.
func carryComponentIDs(planned, existing []client.StatusPageComponent) []client.StatusPageComponent {
	claimed := make([]bool, len(existing))
	out := make([]client.StatusPageComponent, 0, len(planned))

	for _, component := range planned {
		for i, row := range existing {
			if claimed[i] || row.ID == nil {
				continue
			}
			if !componentsMatch(component, row) {
				continue
			}
			claimed[i] = true
			component.ID = row.ID
			break
		}
		out = append(out, component)
	}
	return out
}

func componentsMatch(planned, row client.StatusPageComponent) bool {
	if planned.ThirdParty != row.ThirdParty {
		return false
	}
	if planned.ThirdParty {
		return planned.Name != nil && row.Name != nil && *planned.Name == *row.Name
	}
	return planned.MonitorID != nil && row.MonitorID != nil &&
		strings.EqualFold(*planned.MonitorID, *row.MonitorID)
}

// statusPageToState renders what the API returned as resource state.
func statusPageToState(ctx context.Context, prior *statusPageModel, p *client.StatusPage) (statusPageModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	state := statusPageModel{}

	state.ID = types.StringValue(p.ID)
	state.Slug = types.StringValue(p.Slug)
	state.Title = types.StringValue(p.Title)
	state.PublicURL = types.StringValue(p.PublicURL())
	state.CustomDomain = stringOrNull(p.CustomDomain)
	state.ComponentCount = types.Int64Value(p.ComponentCount)
	state.UnresolvedIncidents = types.Int64Value(p.UnresolvedIncidents)
	state.HasPassword = types.BoolValue(p.HasPassword)
	state.Created = types.Int64Value(p.Created)

	settings, settingsDiags := decodeStatusPageSettings(ctx, p.Settings)
	diags.Append(settingsDiags...)
	state.Settings = settings

	priorComponents := types.ListNull(statusPageComponentObjectType)
	if prior != nil && !prior.Components.IsUnknown() {
		priorComponents = prior.Components
	}
	components, componentDiags := componentsToState(ctx, priorComponents, p.Components)
	diags.Append(componentDiags...)
	state.Components = components

	return state, diags
}

// componentsToState renders the stored component rows, keeping what only
// the configuration knows: a monitored component's inherited name, and
// whether `third_party = false` was written or merely implied.
func componentsToState(ctx context.Context, prior types.List, rows []client.StatusPageComponent) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	var priorComponents []statusPageComponentModel
	if !prior.IsNull() && !prior.IsUnknown() {
		diags.Append(prior.ElementsAs(ctx, &priorComponents, false)...)
	}

	values := make([]attr.Value, 0, len(rows))
	for i, row := range rows {
		var declared *statusPageComponentModel
		if i < len(priorComponents) {
			candidate := priorComponents[i]
			if componentsMatch(componentFromModel(candidate), row) {
				declared = &candidate
			}
		}

		monitorID := stringOrNull(row.MonitorID)
		name := stringOrNull(row.Name)
		thirdParty := types.BoolValue(row.ThirdParty)
		if !row.ThirdParty {
			// Neither member reaches state unless the configuration wrote it:
			// `false` is what naming a monitor already means, and the label is
			// the monitor's own rather than the page's. An adopted page has no
			// configuration to consult, and takes the same shape - which is
			// what a configuration for it would be written as.
			if declared == nil || declared.ThirdParty.IsNull() {
				thirdParty = types.BoolNull()
			}
			if declared == nil || declared.Name.IsNull() {
				name = types.StringNull()
			}
		}

		object, objectDiags := types.ObjectValue(statusPageComponentAttrTypes, map[string]attr.Value{
			"monitor_id":   monitorID,
			"third_party":  thirdParty,
			"name":         name,
			"group":        stringOrNull(row.Group),
			"manual_state": stringOrNull(row.ManualState),
		})
		diags.Append(objectDiags...)
		values = append(values, object)
	}

	list, listDiags := types.ListValue(statusPageComponentObjectType, values)
	diags.Append(listDiags...)
	return list, diags
}

func componentFromModel(m statusPageComponentModel) client.StatusPageComponent {
	component := client.StatusPageComponent{ThirdParty: m.ThirdParty.ValueBool()}
	if !m.MonitorID.IsNull() {
		component.MonitorID = stringPointer(m.MonitorID.ValueString())
	}
	if !m.Name.IsNull() {
		component.Name = stringPointer(m.Name.ValueString())
	}
	return component
}

func stringPointer(v string) *string { return &v }
