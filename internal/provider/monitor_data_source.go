package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*monitorDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*monitorDataSource)(nil)
)

// NewMonitorDataSource is the hosttracker_monitor data source constructor.
func NewMonitorDataSource() datasource.DataSource { return &monitorDataSource{} }

type monitorDataSource struct {
	api *client.Client
}

// monitorLookupModel is the row plus the two lookup keys. Its members
// repeat monitorRow's rather than embedding it, because the framework maps
// state onto flat structs.
type monitorLookupModel struct {
	ID           types.String  `tfsdk:"id"`
	Type         types.String  `tfsdk:"type"`
	Name         types.String  `tfsdk:"name"`
	URL          types.String  `tfsdk:"url"`
	EffectiveURL types.String  `tfsdk:"effective_url"`
	State        types.String  `tfsdk:"state"`
	Since        types.Int64   `tfsdk:"since"`
	Enabled      types.Bool    `tfsdk:"enabled"`
	Tags         types.Set     `tfsdk:"tags"`
	Interval     types.Int64   `tfsdk:"interval"`
	CronSchedule types.String  `tfsdk:"cron_schedule"`
	FullLog      types.Bool    `tfsdk:"full_log"`
	OpenStat     types.Bool    `tfsdk:"open_stat"`
	SLATarget    types.Float64 `tfsdk:"sla_target"`
	Created      types.Int64   `tfsdk:"created"`
	Updated      types.Int64   `tfsdk:"updated"`
	Locations    types.Object  `tfsdk:"locations"`
	Recheck      types.Object  `tfsdk:"recheck"`
	SettingsJSON types.String  `tfsdk:"settings_json"`
	LookupURL    types.String  `tfsdk:"lookup_url"`
	LookupName   types.String  `tfsdk:"lookup_name"`
}

func lookupModelFrom(row monitorRow, lookupURL, lookupName types.String) monitorLookupModel {
	return monitorLookupModel{
		ID:           row.ID,
		Type:         row.Type,
		Name:         row.Name,
		URL:          row.URL,
		EffectiveURL: row.EffectiveURL,
		State:        row.State,
		Since:        row.Since,
		Enabled:      row.Enabled,
		Tags:         row.Tags,
		Interval:     row.Interval,
		CronSchedule: row.CronSchedule,
		FullLog:      row.FullLog,
		OpenStat:     row.OpenStat,
		SLATarget:    row.SLATarget,
		Created:      row.Created,
		Updated:      row.Updated,
		Locations:    row.Locations,
		Recheck:      row.Recheck,
		SettingsJSON: row.SettingsJSON,
		LookupURL:    lookupURL,
		LookupName:   lookupName,
	}
}

func (d *monitorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (d *monitorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := monitorRowAttributes()
	attributes["id"] = dschema.StringAttribute{
		Optional:    true,
		Computed:    true,
		Description: "The monitor's id. Name it, or look the monitor up by `lookup_url` or `lookup_name`.",
	}
	attributes["lookup_url"] = dschema.StringAttribute{
		Optional: true,
		Description: "Find the monitor by the address it watches, matched exactly. The lookup must resolve to " +
			"exactly one monitor.",
	}
	attributes["lookup_name"] = dschema.StringAttribute{
		Optional: true,
		Description: "Find the monitor by its display name, matched exactly. The lookup must resolve to exactly " +
			"one monitor.",
	}

	resp.Schema = dschema.Schema{
		Description: "Reads one monitor, by id or by a lookup that resolves to exactly one.",
		Attributes:  attributes,
	}
}

func (d *monitorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The monitor data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *monitorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config monitorLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	byID := !config.ID.IsNull() && config.ID.ValueString() != ""
	byURL := !config.LookupURL.IsNull() && config.LookupURL.ValueString() != ""
	byName := !config.LookupName.IsNull() && config.LookupName.ValueString() != ""

	chosen := 0
	for _, set := range []bool{byID, byURL, byName} {
		if set {
			chosen++
		}
	}
	if chosen != 1 {
		resp.Diagnostics.AddError(
			"Name exactly one way of finding the monitor",
			"Set one of `id`, `lookup_url` or `lookup_name`, and only one.",
		)
		return
	}

	var found *client.Monitor
	switch {
	case byID:
		monitor, err := d.api.GetMonitor(ctx, config.ID.ValueString())
		if err != nil {
			if err == client.ErrNotFound || client.IsNotFound(err) {
				resp.Diagnostics.AddAttributeError(
					path.Root("id"),
					"No such monitor",
					fmt.Sprintf("No monitor with id %s belongs to this account.", config.ID.ValueString()),
				)
				return
			}
			resp.Diagnostics.Append(client.Diagnose("read the monitor", err, nil)...)
			return
		}
		found = monitor

	default:
		filter := client.MonitorFilter{Limit: 50, Expand: []string{"settings"}}
		attribute := path.Root("lookup_url")
		described := ""
		if byURL {
			filter.URL = []string{config.LookupURL.ValueString()}
			described = fmt.Sprintf("url %q", config.LookupURL.ValueString())
		} else {
			attribute = path.Root("lookup_name")
			filter.Name = config.LookupName.ValueString()
			filter.Q = config.LookupName.ValueString()
			described = fmt.Sprintf("name %q", config.LookupName.ValueString())
		}

		monitors, err := d.api.ListMonitors(ctx, filter)
		if err != nil {
			resp.Diagnostics.Append(client.Diagnose("look the monitor up", err, nil)...)
			return
		}
		switch len(monitors) {
		case 0:
			resp.Diagnostics.AddAttributeError(attribute, "No such monitor",
				fmt.Sprintf("No monitor with %s belongs to this account.", described))
			return
		case 1:
			found = &monitors[0]
		default:
			ids := make([]string, 0, len(monitors))
			for _, m := range monitors {
				ids = append(ids, m.ID)
			}
			resp.Diagnostics.AddAttributeError(attribute, "The lookup matched more than one monitor",
				fmt.Sprintf("%d monitors carry %s: %s. Name the one you mean by `id`.",
					len(monitors), described, strings.Join(ids, ", ")))
			return
		}
	}

	// The list read carries settings only when it was asked for them, and
	// the by-id read always does. Fill in whichever is missing.
	if found.Settings == nil {
		refreshed, err := d.api.GetMonitor(ctx, found.ID)
		if err == nil {
			found = refreshed
		}
	}

	row, diags := monitorToRow(ctx, *found)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := lookupModelFrom(row, config.LookupURL, config.LookupName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
