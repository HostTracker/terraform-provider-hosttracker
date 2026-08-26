package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                   = (*monitorResource)(nil)
	_ resource.ResourceWithConfigure      = (*monitorResource)(nil)
	_ resource.ResourceWithImportState    = (*monitorResource)(nil)
	_ resource.ResourceWithValidateConfig = (*monitorResource)(nil)
)

// NewMonitorResource is the hosttracker_monitor constructor.
func NewMonitorResource() resource.Resource { return &monitorResource{} }

type monitorResource struct {
	api *client.Client
}

type monitorModel struct {
	ID           types.String  `tfsdk:"id"`
	Type         types.String  `tfsdk:"type"`
	URL          types.String  `tfsdk:"url"`
	EffectiveURL types.String  `tfsdk:"effective_url"`
	Name         types.String  `tfsdk:"name"`
	Interval     types.Int64   `tfsdk:"interval"`
	Enabled      types.Bool    `tfsdk:"enabled"`
	Tags         types.Set     `tfsdk:"tags"`
	CronSchedule types.String  `tfsdk:"cron_schedule"`
	FullLog      types.Bool    `tfsdk:"full_log"`
	OpenStat     types.Bool    `tfsdk:"open_stat"`
	SLATarget    types.Float64 `tfsdk:"sla_target"`
	OnOverlimit  types.String  `tfsdk:"on_overlimit"`
	Locations    types.Object  `tfsdk:"locations"`
	Recheck      types.Object  `tfsdk:"recheck"`
	Settings     types.Object  `tfsdk:"settings"`
	SettingsJSON types.String  `tfsdk:"settings_json"`
	State        types.String  `tfsdk:"state"`
	Since        types.Int64   `tfsdk:"since"`
	Created      types.Int64   `tfsdk:"created"`
	Updated      types.Int64   `tfsdk:"updated"`
}

var locationsAttrTypes = map[string]attr.Type{
	"pools":           types.SetType{ElemType: types.StringType},
	"fallback":        types.StringType,
	"excluded_agents": types.SetType{ElemType: types.StringType},
}

var recheckAttrTypes = map[string]attr.Type{
	"strategy":     types.StringType,
	"min_num_down": types.Int64Type,
}

func (r *monitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *monitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A HostTracker monitor: one check of one address, run on a schedule from the monitoring " +
			"locations you choose.\n\n" +
			"Every optional attribute is also computed, because the API reads an absent member as \"leave this " +
			"alone\" rather than as \"clear this\". Removing an attribute from the configuration therefore keeps " +
			"the value it last had; to clear one, write the empty value (`\"\"`, `[]`) explicitly.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The monitor's id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Required: true,
				Description: "Which kind of check this is: `http`, `waterfall`, `ping`, `port`, `domainExp`, " +
					"`sslExp`, `dnsbl`, `webRisk`, `counter`, `cntCheck`, `api`, `database`, `snmp` or `tran`. " +
					"The vocabulary is open, so a type newer than this provider is accepted too. `pageSpeed` is " +
					"an accepted spelling of `waterfall`. The type cannot change after creation: editing it " +
					"replaces the monitor.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"url": schema.StringAttribute{
				Required: true,
				Description: "The address the check is aimed at: an absolute http(s) url for the types that " +
					"fetch one, a bare host or address for the types that do not. It is stored raw and read " +
					"back unchanged; `effective_url` carries what is actually monitored.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"effective_url": schema.StringAttribute{
				Computed: true,
				Description: "The normalized address actually monitored. Equal to `url` unless the API " +
					"canonicalized it.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "A display name. Never an identifier.",
			},
			"interval": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "How often the check runs, in seconds. Types that publish a fixed cadence schedule " +
					"themselves and ignore it; the rest fall back to the account's default. The floor is per type " +
					"and per package - read it from the `hosttracker_monitor_types` data source.",
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				Description: "Whether the check runs. This is the configured flag, not the effective one: a " +
					"monitor suspended by a package limit still reads `true` here and `paused` in `state`.",
			},
			"tags": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "Free-form labels. Writing this attribute replaces the whole set.",
			},
			"cron_schedule": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "A cron expression, replacing the fixed interval. Not accepted by the fixed-cadence " +
					"types (blacklist, certificate and domain expiry, web risk).",
			},
			"full_log": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Record every check rather than only the state changes.",
			},
			"open_stat": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Make this monitor's statistics publicly readable - the switch behind the public stats page and the uptime badge.",
			},
			"sla_target": schema.Float64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "The uptime percentage this monitor is measured against.",
			},
			"on_overlimit": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "What to do when the account's package will not fit this monitor: `fail` (the " +
					"default) refuses the write, `disable` creates it switched off with a package-limit reason. " +
					"It is a knob on the write, not a stored member, so it is never read back. Pair `disable` with " +
					"`enabled = false`: a monitor the API switches off while the configuration asks for `enabled = " +
					"true` is a state Terraform reports as an inconsistent apply.",
				Validators: []validator.String{stringvalidator.OneOf("fail", "disable")},
			},
			"locations": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "Where the check runs from. The types that run on the fixed internal network " +
					"(`dnsbl`, `sslExp`, `domainExp`, `webRisk`) refuse it.",
				Attributes: map[string]schema.Attribute{
					"pools": schema.SetAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Computed:    true,
						Description: "The monitoring-location pools to run from - at least one. `[\"allworld\"]` " +
							"means everywhere. Read the available pools from the `hosttracker_locations` data source.",
						Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
					},
					"fallback": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "What happens when no location in the chosen pools is available: `geo`, `world` or `starve`.",
						Validators:  []validator.String{stringvalidator.OneOf("geo", "world", "starve")},
					},
					"excluded_agents": schema.SetAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Computed:    true,
						Description: "Ids of monitoring locations to keep this check off.",
					},
				},
			},
			"recheck": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The quorum rule applied before a failure concludes.",
				Attributes: map[string]schema.Attribute{
					"strategy": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Description: "`noRecheck` concludes on the first failure, `fullAgreement` needs every " +
							"location to agree, `downFullAgreement` needs full agreement only on the way down, " +
							"`minNumDown` needs `min_num_down` locations to report a failure.",
						Validators: []validator.String{stringvalidator.OneOf("noRecheck", "fullAgreement", "downFullAgreement", "minNumDown")},
					},
					"min_num_down": schema.Int64Attribute{
						Optional:    true,
						Computed:    true,
						Description: "How many locations must report a failure, for `strategy = \"minNumDown\"`. Between 1 and 10.",
						Validators:  []validator.Int64{int64validator.Between(1, 10)},
					},
				},
			},
			"settings": settingsSchema(),
			"settings_json": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "The settings object as raw JSON, for a monitor type this provider has no typed " +
					"block for yet. Mutually exclusive with `settings`. Only the members this attribute names are " +
					"managed: the rest of the stored settings are left alone, and drift is reported for the named " +
					"members only.",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("settings")),
				},
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "The monitor's current state: `up`, `down`, `paused` or `maintenance`.",
			},
			"since": schema.Int64Attribute{
				Computed:    true,
				Description: "When the current state began, in Unix seconds.",
			},
			"created": schema.Int64Attribute{
				Computed:    true,
				Description: "When the monitor was created, in Unix seconds.",
			},
			"updated": schema.Int64Attribute{
				Computed: true,
				Description: "The API's change marker, in Unix seconds. It moves on creation, on an up/down " +
					"transition and on an automatic package-limit disable - not on a configuration edit.",
			},
		},
	}
}

func (r *monitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The monitor resource was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.api = api
}

// ValidateConfig catches the two mistakes the schema cannot: a settings
// block that does not match the monitor's type, and locations on a type
// that runs on the fixed internal network.
func (r *monitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config monitorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Type.IsUnknown() || config.Type.IsNull() {
		return
	}
	monitorType := client.NormalizeType(config.Type.ValueString())

	if !config.Settings.IsNull() && !config.Settings.IsUnknown() {
		for name, value := range config.Settings.Attributes() {
			obj, ok := value.(types.Object)
			if !ok || obj.IsNull() || obj.IsUnknown() {
				continue
			}
			branch, ok := branchByName(name)
			if !ok {
				continue
			}
			if branch.wireType != monitorType {
				resp.Diagnostics.AddAttributeError(
					path.Root("settings").AtName(name),
					"The settings block does not match the monitor type",
					fmt.Sprintf("This monitor is of type %q, so its settings belong in the %q block. "+
						"Remove `settings.%s`, or change `type` to %q.",
						config.Type.ValueString(), settingsBlockNameFor(monitorType), name, branch.wireType),
				)
			}
		}
	}

	if fixedNetworkTypes[monitorType] && !config.Locations.IsNull() && !config.Locations.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("locations"),
			"This monitor type takes no locations",
			fmt.Sprintf("A %q monitor always runs from the fixed internal check network, and the API refuses "+
				"`locations` on it. Remove the block.", monitorType),
		)
	}
}

// fixedNetworkTypes are the types the service runs itself, which therefore
// have no monitoring location to choose.
var fixedNetworkTypes = map[string]bool{
	"dnsbl":     true,
	"sslExp":    true,
	"domainExp": true,
	"webRisk":   true,
}

func settingsBlockNameFor(monitorType string) string {
	if b, ok := branchForType(monitorType); ok {
		return b.name
	}
	return "settings_json"
}

func (r *monitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := encodeMonitor(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, err := r.api.CreateMonitor(ctx, body)
	if err != nil {
		if existing, ok := client.ExistingID(err); ok {
			resp.Diagnostics.AddError(
				"A monitor for this address already exists",
				fmt.Sprintf("The API refused the create because %s already monitors this address. "+
					"Import it instead of creating a second one:\n\n    terraform import %s %s",
					existing, "hosttracker_monitor.<name>", existing),
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("create the monitor", err, r.pointerMapper(plan.Type.ValueString()))...)
		return
	}

	state, diags := monitorToState(ctx, &plan, monitor)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *monitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, err := r.api.GetMonitor(ctx, state.ID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			// Deleted outside Terraform: drop it and let the next plan
			// propose creating it again.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the monitor", err, r.pointerMapper(state.Type.ValueString()))...)
		return
	}

	refreshed, diags := monitorToState(ctx, &state, monitor)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *monitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	want, diags := encodeMonitor(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	have, diags := encodeMonitor(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The type is immutable and replaces the resource, so it is never part
	// of an update body.
	delete(want, "type")
	delete(have, "type")

	body := client.Diff(have, want)

	monitor, err := r.api.UpdateMonitor(ctx, state.ID.ValueString(), body)
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			resp.Diagnostics.AddError(
				"The monitor is gone",
				"It was deleted outside Terraform between the plan and the apply. Run the apply again to create it.",
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("update the monitor", err, r.pointerMapper(plan.Type.ValueString()))...)
		return
	}

	updated, diags := monitorToState(ctx, &plan, monitor)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &updated)...)
}

func (r *monitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteMonitor(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the monitor", err, nil)...)
	}
}

func (r *monitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// pointerMapper places a problem document's JSON Pointers on the attributes
// a configuration actually wrote.
func (r *monitorResource) pointerMapper(monitorType string) client.PointerMapper {
	branch, hasBranch := branchForType(client.NormalizeType(monitorType))
	return func(pointer string) (path.Path, bool) {
		parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			return path.Empty(), false
		}
		switch parts[0] {
		case "type", "url", "name", "interval", "enabled", "tags":
			return path.Root(parts[0]), true
		case "cronSchedule":
			return path.Root("cron_schedule"), true
		case "fullLog":
			return path.Root("full_log"), true
		case "openStat":
			return path.Root("open_stat"), true
		case "slaTarget":
			return path.Root("sla_target"), true
		case "onOverlimit":
			return path.Root("on_overlimit"), true
		case "locations":
			p := path.Root("locations")
			if len(parts) > 1 {
				switch parts[1] {
				case "pools":
					return p.AtName("pools"), true
				case "fallback":
					return p.AtName("fallback"), true
				case "excludedAgents":
					return p.AtName("excluded_agents"), true
				}
			}
			return p, true
		case "recheck":
			p := path.Root("recheck")
			if len(parts) > 1 {
				switch parts[1] {
				case "strategy":
					return p.AtName("strategy"), true
				case "minNumDown":
					return p.AtName("min_num_down"), true
				}
			}
			return p, true
		case "settings":
			if !hasBranch {
				return path.Root("settings_json"), true
			}
			p := path.Root("settings").AtName(branch.name)
			if len(parts) < 2 {
				return p, true
			}
			if parts[1] == "attached" && len(parts) > 2 {
				for name, kind := range attachedKinds {
					if kind.wire == parts[2] {
						return p.AtName("attached").AtName(name), true
					}
				}
				return p.AtName("attached"), true
			}
			for _, f := range branch.fields {
				if f.wire == parts[1] {
					return p.AtName(f.name), true
				}
			}
			return p, true
		}
		return path.Empty(), false
	}
}

// encodeMonitor turns the model into a wire body. Null members are left
// out, which on a create means "use the default" and on an update means
// "leave alone" - the PATCH diff decides which members reach the wire.
func encodeMonitor(ctx context.Context, m *monitorModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{}

	monitorType := client.NormalizeType(m.Type.ValueString())
	if monitorType != "" {
		body["type"] = monitorType
	}
	putString(body, "url", m.URL)
	putString(body, "name", m.Name)
	putInt(body, "interval", m.Interval)
	putBool(body, "enabled", m.Enabled)
	putString(body, "cronSchedule", m.CronSchedule)
	putBool(body, "fullLog", m.FullLog)
	putBool(body, "openStat", m.OpenStat)
	putFloat(body, "slaTarget", m.SLATarget)
	putString(body, "onOverlimit", m.OnOverlimit)

	if !m.Tags.IsNull() && !m.Tags.IsUnknown() {
		var tags []string
		diags.Append(m.Tags.ElementsAs(ctx, &tags, false)...)
		sort.Strings(tags)
		body["tags"] = tags
	}

	if !m.Locations.IsNull() && !m.Locations.IsUnknown() {
		locations := map[string]any{}
		attrs := m.Locations.Attributes()
		if pools, ok := attrs["pools"].(types.Set); ok && !pools.IsNull() && !pools.IsUnknown() {
			var values []string
			diags.Append(pools.ElementsAs(ctx, &values, false)...)
			sort.Strings(values)
			locations["pools"] = values
		}
		if fallback, ok := attrs["fallback"].(types.String); ok && !fallback.IsNull() && !fallback.IsUnknown() {
			locations["fallback"] = fallback.ValueString()
		}
		if excluded, ok := attrs["excluded_agents"].(types.Set); ok && !excluded.IsNull() && !excluded.IsUnknown() {
			var values []string
			diags.Append(excluded.ElementsAs(ctx, &values, false)...)
			sort.Strings(values)
			locations["excludedAgents"] = values
		}
		if len(locations) > 0 {
			body["locations"] = locations
		}
	}

	if !m.Recheck.IsNull() && !m.Recheck.IsUnknown() {
		recheck := map[string]any{}
		attrs := m.Recheck.Attributes()
		if strategy, ok := attrs["strategy"].(types.String); ok && !strategy.IsNull() && !strategy.IsUnknown() {
			recheck["strategy"] = strategy.ValueString()
		}
		if minNumDown, ok := attrs["min_num_down"].(types.Int64); ok && !minNumDown.IsNull() && !minNumDown.IsUnknown() {
			recheck["minNumDown"] = minNumDown.ValueInt64()
		}
		if len(recheck) > 0 {
			body["recheck"] = recheck
		}
	}

	settings, settingsDiags := encodeSettings(ctx, m.Settings, monitorType)
	diags.Append(settingsDiags...)
	if len(settings) > 0 {
		body["settings"] = settings
	}

	if !m.SettingsJSON.IsNull() && !m.SettingsJSON.IsUnknown() {
		raw := map[string]any{}
		if err := json.Unmarshal([]byte(m.SettingsJSON.ValueString()), &raw); err != nil {
			diags.AddAttributeError(
				path.Root("settings_json"),
				"The settings JSON does not parse",
				fmt.Sprintf("It must be a JSON object, the same one `settings` would carry on the wire: %s.", err),
			)
		} else if len(raw) > 0 {
			body["settings"] = raw
		}
	}

	return body, diags
}

// monitorToState renders what the API returned as resource state, keeping
// the values only the configuration knows: the practitioner's spelling of
// an aliased type, and the members the API accepts and never publishes.
func monitorToState(ctx context.Context, prior *monitorModel, m *client.Monitor) (monitorModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	state := monitorModel{}

	state.ID = types.StringValue(m.ID)

	priorType := ""
	if prior != nil && !prior.Type.IsNull() && !prior.Type.IsUnknown() {
		priorType = prior.Type.ValueString()
	}
	state.Type = types.StringValue(client.PreferredType(priorType, m.Type))
	monitorType := client.NormalizeType(m.Type)

	state.URL = types.StringValue(m.URL)
	if m.EffectiveURL != nil && *m.EffectiveURL != "" {
		state.EffectiveURL = types.StringValue(*m.EffectiveURL)
	} else {
		// The API omits it whenever it agrees with the raw address.
		state.EffectiveURL = types.StringValue(m.URL)
	}

	state.Name = stringOrNull(m.Name)
	state.Interval = int64OrNull(m.Interval)
	state.Enabled = types.BoolValue(m.Enabled)
	state.CronSchedule = stringOrNull(m.CronSchedule)
	state.FullLog = types.BoolValue(m.FullLog)
	state.OpenStat = types.BoolValue(m.OpenStat)
	state.SLATarget = float64OrNull(m.SLATarget)

	// A write knob, never stored and never read back.
	state.OnOverlimit = types.StringNull()
	if prior != nil && !prior.OnOverlimit.IsNull() && !prior.OnOverlimit.IsUnknown() {
		state.OnOverlimit = prior.OnOverlimit
	}

	if m.Tags == nil {
		state.Tags = types.SetValueMust(types.StringType, []attr.Value{})
	} else {
		tags := append([]string(nil), m.Tags...)
		sort.Strings(tags)
		set, setDiags := types.SetValueFrom(ctx, types.StringType, tags)
		diags.Append(setDiags...)
		state.Tags = set
	}

	if m.Locations == nil {
		state.Locations = types.ObjectNull(locationsAttrTypes)
	} else {
		pools, poolDiags := stringSet(ctx, m.Locations.Pools)
		diags.Append(poolDiags...)
		excluded, excludedDiags := stringSet(ctx, m.Locations.ExcludedAgents)
		diags.Append(excludedDiags...)
		object, objectDiags := types.ObjectValue(locationsAttrTypes, map[string]attr.Value{
			"pools":           pools,
			"fallback":        stringOrNull(m.Locations.Fallback),
			"excluded_agents": excluded,
		})
		diags.Append(objectDiags...)
		state.Locations = object
	}

	if m.Recheck == nil || (m.Recheck.Strategy == nil && m.Recheck.MinNumDown == nil) {
		state.Recheck = types.ObjectNull(recheckAttrTypes)
	} else {
		object, objectDiags := types.ObjectValue(recheckAttrTypes, map[string]attr.Value{
			"strategy":     stringOrNull(m.Recheck.Strategy),
			"min_num_down": int64OrNull(m.Recheck.MinNumDown),
		})
		diags.Append(objectDiags...)
		state.Recheck = object
	}

	usesJSON := prior != nil && !prior.SettingsJSON.IsNull() && !prior.SettingsJSON.IsUnknown()
	if usesJSON {
		state.Settings = types.ObjectNull(settingsAttrTypes())
		value, jsonDiags := projectSettingsJSON(prior.SettingsJSON.ValueString(), m.Settings)
		diags.Append(jsonDiags...)
		state.SettingsJSON = value
	} else {
		priorSettings := types.ObjectNull(settingsAttrTypes())
		if prior != nil && !prior.Settings.IsUnknown() {
			priorSettings = prior.Settings
		}
		settings, settingsDiags := decodeSettings(ctx, priorSettings, m.Settings, monitorType)
		diags.Append(settingsDiags...)
		state.Settings = settings
		state.SettingsJSON = types.StringNull()
	}

	state.State = stringOrNull(m.State)
	state.Since = types.Int64Value(m.Since)
	state.Updated = types.Int64Value(m.Updated)
	state.Created = int64OrNull(m.Created)

	return state, diags
}

// projectSettingsJSON keeps `settings_json` about the members it names.
// The stored settings object routinely carries members the configuration
// never mentioned - server defaults, values set in the web app - and
// echoing all of them back would report drift on every plan.
func projectSettingsJSON(prior string, stored map[string]any) (types.String, diag.Diagnostics) {
	var diags diag.Diagnostics
	declared := map[string]any{}
	if err := json.Unmarshal([]byte(prior), &declared); err != nil {
		// It failed to parse on the way in too, and that failure is the
		// one worth reporting. Keep what is there.
		return types.StringValue(prior), diags
	}

	projected := make(map[string]any, len(declared))
	for key := range declared {
		if value, ok := stored[key]; ok {
			projected[key] = value
		}
	}

	current, err := json.Marshal(projected)
	if err != nil {
		diags.AddError("Could not render the stored settings as JSON", err.Error())
		return types.StringValue(prior), diags
	}
	canonical, err := json.Marshal(declared)
	if err == nil && string(canonical) == string(current) {
		// Semantically unchanged: keep the practitioner's own formatting.
		return types.StringValue(prior), diags
	}
	return types.StringValue(string(current)), diags
}

func putString(body map[string]any, key string, v types.String) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	body[key] = v.ValueString()
}

func putInt(body map[string]any, key string, v types.Int64) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	body[key] = v.ValueInt64()
}

func putBool(body map[string]any, key string, v types.Bool) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	body[key] = v.ValueBool()
}

func putFloat(body map[string]any, key string, v types.Float64) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	body[key] = v.ValueFloat64()
}

func stringOrNull(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

func int64OrNull(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}

func float64OrNull(v *float64) types.Float64 {
	if v == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*v)
}

func stringSet(ctx context.Context, values []string) (types.Set, diag.Diagnostics) {
	if values == nil {
		return types.SetNull(types.StringType), nil
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return types.SetValueFrom(ctx, types.StringType, sorted)
}
