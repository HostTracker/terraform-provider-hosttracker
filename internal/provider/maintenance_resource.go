package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	_ resource.Resource                = (*maintenanceResource)(nil)
	_ resource.ResourceWithConfigure   = (*maintenanceResource)(nil)
	_ resource.ResourceWithImportState = (*maintenanceResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*maintenanceResource)(nil)
)

// NewMaintenanceResource is the hosttracker_maintenance constructor.
func NewMaintenanceResource() resource.Resource { return &maintenanceResource{} }

type maintenanceResource struct {
	api *client.Client
}

type maintenanceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	From        types.Int64  `tfsdk:"from"`
	To          types.Int64  `tfsdk:"to"`
	DurationSec types.Int64  `tfsdk:"duration_sec"`
	FromRFC3339 types.String `tfsdk:"from_rfc3339"`
	ToRFC3339   types.String `tfsdk:"to_rfc3339"`
	Timezone    types.String `tfsdk:"timezone"`
	Recurrence  types.Object `tfsdk:"recurrence"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	MonitorIDs  types.Set    `tfsdk:"monitor_ids"`
	Suppress    types.Object `tfsdk:"suppress"`
	Monitors    types.Set    `tfsdk:"monitors"`
	State       types.String `tfsdk:"state"`
	Overlimited types.Bool   `tfsdk:"overlimited"`
	Created     types.Int64  `tfsdk:"created"`
	Updated     types.Int64  `tfsdk:"updated"`
}

var suppressAttrTypes = map[string]attr.Type{
	"alerts": types.BoolType,
	"stats":  types.BoolType,
}

var recurrenceAttrTypes = map[string]attr.Type{
	"week_days": types.SetType{ElemType: types.StringType},
}

var maintenanceMonitorAttrTypes = map[string]attr.Type{
	"monitor_id": types.StringType,
	"suppress":   types.ObjectType{AttrTypes: suppressAttrTypes},
}

// maintenanceShape is which of the two spellings the configuration chose
// for the window's length and for its coverage. The API refuses a body
// carrying both halves of either pair, so the encoder has to be told which
// one the configuration owns.
type maintenanceShape struct {
	useDuration bool
	perMonitor  bool
}

func shapeOf(config *maintenanceModel) maintenanceShape {
	return maintenanceShape{
		useDuration: known(config.DurationSec),
		perMonitor:  known(config.Monitors),
	}
}

func (r *maintenanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance"
}

func (r *maintenanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A maintenance window: a span during which planned downtime is held out of the alerting, " +
			"of the uptime statistics, or of both.\n\n" +
			"The window's length is given once - as `to` or as `duration_sec`, never both - and its coverage is " +
			"given once as well: `monitor_ids` with one `suppress` when every monitor is treated the same, or " +
			"`monitors` when they are not. Both spellings are read back whichever one was written.\n\n" +
			"Every optional attribute is also computed, because the API reads an absent member as \"leave this " +
			"alone\" rather than as \"clear this\".",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The window's id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "A display name. Never an identifier.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"from": schema.Int64Attribute{
				Required:    true,
				Description: "When the window opens, in Unix seconds.",
				Validators:  []validator.Int64{int64validator.AtLeast(1)},
			},
			"to": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "When the window closes, in Unix seconds. Give the length once, as this or as " +
					"`duration_sec`; whichever is written, the other is read back.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
					int64validator.ExactlyOneOf(path.MatchRoot("to"), path.MatchRoot("duration_sec")),
				},
			},
			"duration_sec": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "How long the window lasts, in seconds of real elapsed time. Give the length once, " +
					"as this or as `to`.",
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"from_rfc3339": schema.StringAttribute{
				Computed:    true,
				Description: "`from` rendered in UTC, for reading. The API takes and publishes Unix seconds.",
			},
			"to_rfc3339": schema.StringAttribute{
				Computed:    true,
				Description: "`to` rendered in UTC, for reading.",
			},
			"timezone": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "The zone the window's wall clock is read in, as an IANA id (`Europe/Berlin`). A " +
					"Windows spelling is refused with the id to send instead.\n\n" +
					"Several IANA ids share one stored zone, so the API can answer with the group's " +
					"representative rather than with the id that was sent - `Europe/Rome` reads back as " +
					"`Europe/Berlin`, same clock and same daylight-saving rules. The configured spelling is " +
					"therefore kept, which also means a zone changed outside Terraform is not reported as drift.",
			},
			"recurrence": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "How the window repeats. Unset, it happens once.",
				Attributes: map[string]schema.Attribute{
					"week_days": schema.SetAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Computed:    true,
						Description: "The days it repeats on, by English name (`\"Sunday\"`), in the window's own " +
							"time zone.",
					},
				},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the window is in force. A disabled window suppresses nothing.",
			},
			"monitor_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "The monitors the window covers, all of them getting the `suppress` written beside " +
					"this attribute. Writing it replaces the coverage rather than adding to it. Use `monitors` " +
					"instead when the window treats its monitors differently.",
				Validators: []validator.Set{
					setvalidator.ConflictsWith(path.MatchRoot("monitors")),
				},
			},
			"suppress": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "What the window holds back for the monitors in `monitor_ids`. At least one of the " +
					"two must be true - a window that suppresses neither does nothing. Read back only while every " +
					"covered monitor agrees; where they differ, the answer is in `monitors`.",
				Attributes: map[string]schema.Attribute{
					"alerts": schema.BoolAttribute{
						Optional:    true,
						Computed:    true,
						Description: "Hold back alerting for the duration of the window.",
					},
					"stats": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Keep the window's checks out of the uptime statistics, so planned downtime " +
							"does not read as an outage.",
					},
				},
				Validators: []validator.Object{
					objectvalidator.ConflictsWith(path.MatchRoot("monitors")),
				},
			},
			"monitors": schema.SetNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "The window's coverage stated per monitor, for a window that holds back alerting " +
					"for one monitor and only the statistics for another. Writing it replaces the whole coverage. " +
					"It is also what a read fills in whichever spelling was written, because it is the shape the " +
					"window is stored in.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"monitor_id": schema.StringAttribute{
							Required:    true,
							Description: "The monitor this entry covers, which must be the account's own.",
							Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
						},
						"suppress": schema.SingleNestedAttribute{
							Required:    true,
							Description: "What the window holds back for this monitor.",
							Attributes: map[string]schema.Attribute{
								"alerts": schema.BoolAttribute{
									Optional:    true,
									Computed:    true,
									Description: "Hold back this monitor's alerting.",
								},
								"stats": schema.BoolAttribute{
									Optional:    true,
									Computed:    true,
									Description: "Keep this monitor's checks inside the window out of the statistics.",
								},
							},
						},
					},
				},
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "Where the window is in its life: `scheduled`, `active` or `finished`.",
			},
			"overlimited": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the account's package has stopped this window from applying.",
			},
			"created": schema.Int64Attribute{
				Computed:    true,
				Description: "When the window was created, in Unix seconds.",
			},
			"updated": schema.Int64Attribute{
				Computed: true,
				Description: "When the window last changed, in Unix seconds. Maintenance windows stamp this on " +
					"every edit.",
			},
		},
	}
}

func (r *maintenanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The maintenance resource was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.api = api
}

// ModifyPlan marks what the API recomputes as unknown, so that the plan
// does not promise values the apply will contradict. The window's length
// and its coverage are each stored in two spellings, and the one the
// configuration does not own is derived from the one it does.
func (r *maintenanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var plan, state, config maintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !maintenanceChanged(&plan, &state, &config) {
		keepPriorState(req, resp)
		return
	}

	unknown := func(attribute string, value attr.Value) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(attribute), value)...)
	}

	unknown("updated", types.Int64Unknown())
	unknown("state", types.StringUnknown())
	unknown("overlimited", types.BoolUnknown())
	unknown("from_rfc3339", types.StringUnknown())
	unknown("to_rfc3339", types.StringUnknown())

	if config.To.IsNull() {
		unknown("to", types.Int64Unknown())
	}
	if config.DurationSec.IsNull() {
		unknown("duration_sec", types.Int64Unknown())
	}
	if config.Monitors.IsNull() {
		unknown("monitors", types.SetUnknown(types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes}))
	}
	if config.MonitorIDs.IsNull() {
		unknown("monitor_ids", types.SetUnknown(types.StringType))
	}
	if config.Suppress.IsNull() {
		unknown("suppress", types.ObjectUnknown(suppressAttrTypes))
	}
}

// maintenanceChanged reports whether the plan asks for anything the state
// does not already say, over the members a configuration owns. A member the
// configuration leaves out is not one of them - see plan_stability.go.
func maintenanceChanged(plan, state, config *maintenanceModel) bool {
	return asksForChange([]plannedMember{
		{config.Name, plan.Name, state.Name},
		{config.From, plan.From, state.From},
		{config.To, plan.To, state.To},
		{config.DurationSec, plan.DurationSec, state.DurationSec},
		{config.Timezone, plan.Timezone, state.Timezone},
		{config.Recurrence, plan.Recurrence, state.Recurrence},
		{config.Enabled, plan.Enabled, state.Enabled},
		{config.MonitorIDs, plan.MonitorIDs, state.MonitorIDs},
		{config.Suppress, plan.Suppress, state.Suppress},
		{config.Monitors, plan.Monitors, state.Monitors},
	})
}

func (r *maintenanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config maintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := encodeMaintenance(ctx, &plan, shapeOf(&config))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	window, err := r.api.CreateMaintenance(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("create the maintenance window", err, maintenancePointerMapper)...)
		return
	}

	state, diags := maintenanceToState(ctx, &plan, window)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *maintenanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state maintenanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	window, err := r.api.GetMaintenance(ctx, state.ID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the maintenance window", err, maintenancePointerMapper)...)
		return
	}

	refreshed, diags := maintenanceToState(ctx, &state, window)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *maintenanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config maintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	shape := shapeOf(&config)
	want, diags := encodeMaintenance(ctx, &plan, shape)
	resp.Diagnostics.Append(diags...)
	have, diags := encodeMaintenance(ctx, &state, shape)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	window, err := r.api.UpdateMaintenance(ctx, state.ID.ValueString(), client.Diff(have, want))
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			resp.Diagnostics.AddError(
				"The maintenance window is gone",
				"It was deleted outside Terraform between the plan and the apply. Run the apply again to create it.",
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("update the maintenance window", err, maintenancePointerMapper)...)
		return
	}

	updated, diags := maintenanceToState(ctx, &plan, window)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &updated)...)
}

func (r *maintenanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state maintenanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteMaintenance(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the maintenance window", err, nil)...)
	}
}

func (r *maintenanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// maintenancePointerMapper places a problem document's JSON Pointers on
// the attributes a configuration wrote.
func maintenancePointerMapper(pointer string) (path.Path, bool) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return path.Empty(), false
	}
	switch parts[0] {
	case "name", "from", "to", "timezone", "enabled", "suppress", "monitors":
		return path.Root(parts[0]), true
	case "durationSec":
		return path.Root("duration_sec"), true
	case "monitorIds":
		return path.Root("monitor_ids"), true
	case "recurrence":
		p := path.Root("recurrence")
		if len(parts) > 1 && parts[1] == "weekDays" {
			return p.AtName("week_days"), true
		}
		return p, true
	}
	return path.Empty(), false
}

// encodeMaintenance turns the model into a wire body, sending only the
// spelling of the length and of the coverage that the configuration owns.
func encodeMaintenance(ctx context.Context, m *maintenanceModel, shape maintenanceShape) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{}

	putString(body, "name", m.Name)
	putInt(body, "from", m.From)
	putString(body, "timezone", m.Timezone)
	putBool(body, "enabled", m.Enabled)

	if shape.useDuration {
		putInt(body, "durationSec", m.DurationSec)
	} else {
		putInt(body, "to", m.To)
	}

	if known(m.Recurrence) {
		recurrence := map[string]any{}
		if days, ok := m.Recurrence.Attributes()["week_days"].(types.Set); ok && known(days) {
			var values []string
			diags.Append(days.ElementsAs(ctx, &values, false)...)
			sort.Strings(values)
			recurrence["weekDays"] = values
		}
		if len(recurrence) > 0 {
			body["recurrence"] = recurrence
		}
	}

	if shape.perMonitor {
		if known(m.Monitors) {
			entries := make([]map[string]any, 0, len(m.Monitors.Elements()))
			for _, element := range m.Monitors.Elements() {
				object, ok := element.(types.Object)
				if !ok || !known(object) {
					continue
				}
				attrs := object.Attributes()
				entry := map[string]any{}
				if id, ok := attrs["monitor_id"].(types.String); ok && known(id) {
					entry["monitorId"] = id.ValueString()
				}
				if suppress, ok := attrs["suppress"].(types.Object); ok && known(suppress) {
					if encoded := encodeSuppress(suppress); len(encoded) > 0 {
						entry["suppress"] = encoded
					}
				}
				entries = append(entries, entry)
			}
			sort.Slice(entries, func(i, j int) bool {
				left, _ := entries[i]["monitorId"].(string)
				right, _ := entries[j]["monitorId"].(string)
				return left < right
			})
			monitors := make([]any, 0, len(entries))
			for _, entry := range entries {
				monitors = append(monitors, entry)
			}
			body["monitors"] = monitors
		}
		return body, diags
	}

	if known(m.MonitorIDs) {
		var ids []string
		diags.Append(m.MonitorIDs.ElementsAs(ctx, &ids, false)...)
		sort.Strings(ids)
		body["monitorIds"] = ids
	}
	if known(m.Suppress) {
		if encoded := encodeSuppress(m.Suppress); len(encoded) > 0 {
			body["suppress"] = encoded
		}
	}

	return body, diags
}

func encodeSuppress(object types.Object) map[string]any {
	encoded := map[string]any{}
	attrs := object.Attributes()
	if alerts, ok := attrs["alerts"].(types.Bool); ok && known(alerts) {
		encoded["alerts"] = alerts.ValueBool()
	}
	if stats, ok := attrs["stats"].(types.Bool); ok && known(stats) {
		encoded["stats"] = stats.ValueBool()
	}
	return encoded
}

// maintenanceToState renders what the API returned as resource state. Both
// spellings of the length and of the coverage are filled in, whichever one
// the configuration wrote.
func maintenanceToState(ctx context.Context, prior *maintenanceModel, w *client.Maintenance) (maintenanceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	state := maintenanceModel{
		ID:          types.StringValue(w.ID),
		Name:        stringOrNull(w.Name),
		From:        types.Int64Value(w.From),
		To:          types.Int64Value(w.To),
		DurationSec: types.Int64Value(w.DurationSec),
		FromRFC3339: types.StringValue(client.UnixToRFC3339(w.From)),
		ToRFC3339:   types.StringValue(client.UnixToRFC3339(w.To)),
		Enabled:     types.BoolValue(w.Enabled),
		State:       stringOrNull(w.State),
		Overlimited: types.BoolValue(w.Overlimited),
		Created:     types.Int64Value(w.Created),
		Updated:     types.Int64Value(w.Updated),
	}

	priorZone := ""
	if prior != nil && known(prior.Timezone) {
		priorZone = prior.Timezone.ValueString()
	}
	state.Timezone = preferredString(priorZone, w.Timezone, client.PreferredZone)

	if w.Recurrence == nil || len(w.Recurrence.WeekDays) == 0 {
		state.Recurrence = types.ObjectNull(recurrenceAttrTypes)
	} else {
		days, dayDiags := stringSet(ctx, w.Recurrence.WeekDays)
		diags.Append(dayDiags...)
		object, objectDiags := types.ObjectValue(recurrenceAttrTypes, map[string]attr.Value{"week_days": days})
		diags.Append(objectDiags...)
		state.Recurrence = object
	}

	monitorIDs, idDiags := stringSet(ctx, w.MonitorIDs)
	diags.Append(idDiags...)
	if w.MonitorIDs == nil {
		monitorIDs = types.SetValueMust(types.StringType, []attr.Value{})
	}
	state.MonitorIDs = monitorIDs

	if w.Suppress == nil {
		state.Suppress = types.ObjectNull(suppressAttrTypes)
	} else {
		object, objectDiags := types.ObjectValue(suppressAttrTypes, map[string]attr.Value{
			"alerts": types.BoolValue(w.Suppress.Alerts),
			"stats":  types.BoolValue(w.Suppress.Stats),
		})
		diags.Append(objectDiags...)
		state.Suppress = object
	}

	elemType := types.ObjectType{AttrTypes: maintenanceMonitorAttrTypes}
	entries := make([]attr.Value, 0, len(w.Monitors))
	for _, monitor := range w.Monitors {
		suppress := types.ObjectNull(suppressAttrTypes)
		if monitor.Suppress != nil {
			object, objectDiags := types.ObjectValue(suppressAttrTypes, map[string]attr.Value{
				"alerts": types.BoolValue(monitor.Suppress.Alerts),
				"stats":  types.BoolValue(monitor.Suppress.Stats),
			})
			diags.Append(objectDiags...)
			suppress = object
		}
		object, objectDiags := types.ObjectValue(maintenanceMonitorAttrTypes, map[string]attr.Value{
			"monitor_id": types.StringValue(monitor.MonitorID),
			"suppress":   suppress,
		})
		diags.Append(objectDiags...)
		entries = append(entries, object)
	}
	monitors, monitorDiags := types.SetValue(elemType, entries)
	diags.Append(monitorDiags...)
	state.Monitors = monitors

	return state, diags
}
